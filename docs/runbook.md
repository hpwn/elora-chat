# elora-chat Runbook

## Local Dev Bring-up

1. Copy the example environment and adjust any overrides:
   ```bash
   cp .env.example .env
   ```
2. Build the local images and pull the harvester dependency:
   ```bash
   make bootstrap
   ```
3. Launch the stack:
   ```bash
   make up
   ```
4. Wait for SQLite readiness. `make health` curls `/readyz` until the database can service writes. `make configz` pretty-prints the redacted runtime configuration so you can verify paths, journal mode, origins, and that `ingest.driver` is locked to `gnasty`.
5. Inspect live traffic with the containerised helpers:
   ```bash
   make ws          # all frames
   make ws-twitch   # Twitch only
   make ws-youtube  # YouTube only
   ```
6. Seed test traffic when needed:
   ```bash
   make seed:marker
   make seed:burst
   ```
7. Tear everything down while keeping the shared data volume:
   ```bash
   make down
   ```

Health endpoints:

| Endpoint | Purpose | Notes |
| --- | --- | --- |
| `/healthz` | Process is accepting HTTP requests. | Does not require SQLite. |
| `/readyz` | Backend can open the configured SQLite database. | Gated by `store.Ping`. |
| `/configz` | Redacted snapshot of runtime configuration. | Use `make configz` for jq formatting. |

### Host user mapping (bind-mounted data)

Both services run as `${DOCKER_UID:-1000}:${DOCKER_GID:-1000}` so the bind-mounted `./data` directory inherits the host user’s
ownership. This prevents SQLite errors such as `unable to open database file` when the image user (`myuser`) differs from your
workstation UID/GID (commonly `1000:1000`). If your account uses different IDs, set `DOCKER_UID` and `DOCKER_GID` in your local
`.env` before running `./scripts/run-local.sh` or `docker compose up -d --build`. The `./data/` path is gitignored and dockerign
ored, making it a safe place for `elora.db`, `gnasty.db`, and Twitch token handoff files without loosening permissions.

## Configuration Map

### SQLite store (`ELORA_DB_*`)

`main.go` wires the SQLite store using the `ELORA_DB_*` variables:

| Variable | Description | Destination |
| --- | --- | --- |
| `ELORA_DB_MODE` | `ephemeral` or `persistent`. Controls whether a temp path is created. | `sqlite.Config.Mode` |
| `ELORA_DB_PATH` | Absolute or relative database path. | `sqlite.Config.Path` and inferred offset file. |
| `ELORA_DB_MAX_CONNS` | Max open connections. | `sqlite.Config.MaxConns` |
| `ELORA_SQLITE_BUSY_TIMEOUT_MS` / `ELORA_DB_BUSY_TIMEOUT_MS` | Busy timeout in milliseconds. | `sqlite.Config.BusyTimeoutMS` |
| `ELORA_DB_PRAGMAS_EXTRA` | Comma-separated pragmas applied after connection. | `sqlite.Config.PragmasExtraCSV` |
| `ELORA_SQLITE_JOURNAL_MODE` | Journal mode override (`wal`, `delete`, etc). | `sqlite.Config.JournalMode` |

The store is initialised before routing and reused by the HTTP API, WebSocket history loader, and the DB tailer.

### Tailer / publisher (`ELORA_TAILER_*`)

`tailer.Config` is derived from `ELORA_TAILER_*` and orchestrates the background publisher that reads from SQLite and broadcasts frames over the WebSocket hub:

| Variable | Description | Effect |
| --- | --- | --- |
| `ELORA_DB_TAIL_ENABLED` / `ELORA_TAILER_ENABLED` | Enable/disable the tailer. | `tailer.Config.Enabled` |
| `ELORA_TAILER_POLL_MS` (`ELORA_DB_TAIL_POLL_MS`) | Poll interval in ms. | `tailer.Config.Interval` |
| `ELORA_TAILER_MAX_BATCH` (`ELORA_DB_TAIL_BATCH`) | Max rows per poll. | `tailer.Config.Batch` |
| `ELORA_TAILER_MAX_LAG_MS` | Warn when publish lag exceeds this threshold. | `tailer.Config.MaxLag` |
| `ELORA_TAILER_PERSIST_OFFSETS` | Persist the last seen cursor. | `tailer.Config.PersistOffsets` + `OffsetPath` |
| `ELORA_TAILER_OFFSET_PATH` | Optional override for the cursor file. | `tailer.Config.OffsetPath` |

When offsets are persisted and no explicit path is provided the backend appends `.offset.json` to `ELORA_DB_PATH`.

The tailer feeds `routes.BroadcastFromTailer`, which uses the same WebSocket hub as live ingest.

### Twitch auth (`TWITCH_OAUTH_*`, `ELORA_TWITCH_*`)

`/configz` now emits an `auth.twitch` block so operators can confirm OAuth wiring without leaking secrets:

| Field | Source | Notes |
| --- | --- | --- |
| `client_id` | `TWITCH_OAUTH_CLIENT_ID` | Always redacted to `"[redacted]"` when set. |
| `redirect_url` | `TWITCH_OAUTH_REDIRECT_URL` | Exact redirect URI configured for Twitch. |
| `write_gnasty_tokens` | `ELORA_TWITCH_WRITE_GNASTY_TOKENS` | Defaults to `true`; `0/false/no/off` disable gnasty handoff writes. |
| `access_token_path` | `ELORA_DATA_DIR` | Resolved to `<ELORA_DATA_DIR>/twitch_irc.pass` when writes are enabled. |
| `refresh_token_path` | `ELORA_DATA_DIR` | Resolved to `<ELORA_DATA_DIR>/twitch_refresh.pass` when writes are enabled. |

The access/refresh paths mirror gnasty handoff defaults so you can verify shared volume wiring. Set `ELORA_DATA_DIR` to a writable mount when gnasty and the API share tokens.

#### gnasty reload hook

The Twitch callback posts to gnasty after exporting fresh tokens. Override the target with `ELORA_GNASTY_RELOAD_URL` (defaults to `http://gnasty:${GNASTY_HTTP_PORT:-8765}/admin/twitch/reload`).

#### Channel selection

Pass Twitch and YouTube selectors via the shared `.env` file so both containers agree on the upstream rooms:

- `TWITCH_CHANNEL` and `TWITCH_NICK` inform gnasty which IRC room to join and which nickname to present.
- `YOUTUBE_URL` and/or `GNASTY_YT_CHANNEL_IDS` select the YouTube Live source.

The Twitch OAuth credentials (`TWITCH_OAUTH_CLIENT_ID`, `TWITCH_OAUTH_CLIENT_SECRET`, `TWITCH_OAUTH_REDIRECT_URL`) are required for the login flow that populates gnasty's token files.

#### Sign in with Twitch

Use the local OAuth flow to grant the chat scope pair Twitch requires:

- Start the handoff from your browser at <http://localhost:8080/auth/twitch/start>.
- Authorise the `chat:read` and `chat:edit` scopes when prompted.

When `ELORA_TWITCH_WRITE_GNASTY_TOKENS` (the "write flag") is enabled the callback writes the access token to `${ELORA_DATA_DIR}/twitch_irc.pass` and the refresh token to `${ELORA_DATA_DIR}/twitch_refresh.pass`. Both files trigger gnasty's reload hook so the harvester begins using the new credentials without manual restarts.

Verify the handoff end to end by:

1. Hitting `/configz` (or `make configz`) to confirm `auth.twitch.write_gnasty_tokens` is `true` and the resolved paths match the expected shared volume.
2. Watching gnasty's logs for its reload acknowledgement to ensure it detected the updated pass files and resumed Twitch ingestion with the new scopes.

## Topologies

### gnasty + SQLite tailer (default)

- gnasty writes frames into the shared volume (`GNASTY_SINK_SQLITE_PATH` should match `ELORA_DB_PATH`).
- Configure Twitch/YouTube selectors via `TWITCH_CHANNEL`, `TWITCH_NICK`, `YOUTUBE_URL`, and/or `GNASTY_YT_CHANNEL_IDS`.
- The elora tailer (`ELORA_DB_TAIL_ENABLED=1`) polls the same database and republishes new rows over WebSocket.
- `/configz` shows `ingest.driver="gnasty"`, the active journal mode, tailer interval/batch/lag thresholds, and the resolved offset path. The startup log includes a `config_summary` JSON line with the same fields for quick grepping alongside gnasty's logs.

## Ports, Volumes, and Troubleshooting

| Component | Port(s) | Volume(s) | Notes |
| --- | --- | --- | --- |
| elora-chat | `8080/tcp` (HTTP + WebSocket) | `elora_data` mounted at `/data` | Requires SQLite path to be writable. |
| gnasty-harvester | `8765/tcp` (status) | `elora_data` at `/data`, optional token volume | Shares the same SQLite file when using tailer mode. |
| websocat helper (`make ws*`) | none | bind-mount `scripts/ws_filter.py` | Joins `$(ELORA_DOCKER_NETWORK)` to reach the API container. |

Troubleshooting checklist:

- **`make health` fails** – confirm the SQLite path exists and the container user can create the file. If ownership is wrong, set `DOCKER_UID`/`DOCKER_GID` in `.env` so the containers run as your host user. `/configz` echoes the resolved `db.path` and journal mode.
- **`make ws-*` shows no frames** – verify `/configz` reports `tailer.enabled=true` when relying on gnasty, and that gnasty is writing to the same database path. Use `make configz` to confirm `allowed_origins` allows your websocket client.
- **`/configz` shows `allow_any_origin=false` with an empty list** – set `ELORA_WS_ALLOWED_ORIGINS` or `ELORA_ALLOWED_ORIGINS` to a comma-separated list of origins.
- **`ingest.driver` unexpected** – it should always be `gnasty`. Double-check that gnasty and elora-chat share the same SQLite volume and review the `config_summary` log line for the resolved paths.
- **Tailer lag warnings** – adjust `ELORA_TAILER_MAX_BATCH`/`ELORA_TAILER_POLL_MS`/`ELORA_TAILER_MAX_LAG_MS` to increase throughput, or reduce gnasty's flush batch size.

For deeper wiring details (env variable precedence, command examples, and failure modes) this runbook plus the `/configz` endpoint act as the canonical source of truth.

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
4. Wait for SQLite readiness. `make health` curls `/readyz` until the database can service writes. `make configz` pretty-prints the redacted runtime configuration so you can verify paths, journal mode, origins, and the ingest driver selected from `ELORA_INGEST_DRIVER`.
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

## Topologies

### 1. chatdownloader-only

- `ELORA_INGEST_DRIVER=chatdownloader` (default).
- `CHAT_URLS` contains one or more Twitch/YouTube URLs.
- The Python chatdownloader subprocess streams messages directly into the API.
- SQLite persistence is optional. With `ELORA_DB_TAIL_ENABLED=0` the WebSocket broadcast loop pushes messages as they arrive.
- `/configz` will report `ingest.driver="chatdownloader"` and `tailer.enabled=false` unless explicitly enabled for replay/bursting.

### 2. gnasty + SQLite tailer

- `ELORA_INGEST_DRIVER=gnasty` and `CHAT_URLS` describes the upstream rooms (passed through to gnasty so it can auto-subscribe).
- gnasty runs in a separate container/process and writes NDJSON into the shared SQLite database (`GNASTY_SINK_SQLITE_PATH` should match `ELORA_DB_PATH`).
- The elora tailer (`ELORA_DB_TAIL_ENABLED=1`) polls the same database and republishes new rows over WebSocket. No local chatdownloader subprocess is spawned when the driver is `gnasty`.
- The contract is "gnasty writes → SQLite → elora tailer broadcasts". All connected WebSocket clients see gnasty's frames without additional configuration.
- `/configz` shows `ingest.driver="gnasty"`, the active journal mode, tailer interval/batch/lag thresholds, and the resolved offset path. The startup log includes a `config_summary` JSON line with the same fields for quick grepping alongside gnasty's logs.

## Ports, Volumes, and Troubleshooting

| Component | Port(s) | Volume(s) | Notes |
| --- | --- | --- | --- |
| elora-chat | `8080/tcp` (HTTP + WebSocket) | `elora_data` mounted at `/data` | Requires SQLite path to be writable. |
| gnasty-harvester | `8765/tcp` (status) | `elora_data` at `/data`, optional token volume | Shares the same SQLite file when using tailer mode. |
| websocat helper (`make ws*`) | none | bind-mount `scripts/ws_filter.py` | Joins `$(ELORA_DOCKER_NETWORK)` to reach the API container. |

Troubleshooting checklist:

- **`make health` fails** – confirm the SQLite path exists and the container user can create the file. `/configz` echoes the resolved `db.path` and journal mode.
- **`make ws-*` shows no frames** – verify `/configz` reports `tailer.enabled=true` when relying on gnasty, and that gnasty is writing to the same database path. Use `make configz` to confirm `allowed_origins` allows your websocket client.
- **`/configz` shows `allow_any_origin=false` with an empty list** – set `ELORA_WS_ALLOWED_ORIGINS` or `ELORA_ALLOWED_ORIGINS` to a comma-separated list of origins.
- **`ingest.driver` unexpected** – ensure `ELORA_INGEST_DRIVER` is set in `.env`. The backend logs `ingest: selected driver="…"` on startup and the value appears in `/configz` and the `config_summary` log line.
- **Tailer lag warnings** – adjust `ELORA_TAILER_MAX_BATCH`/`ELORA_TAILER_POLL_MS`/`ELORA_TAILER_MAX_LAG_MS` to increase throughput, or reduce gnasty's flush batch size.

For deeper wiring details (env variable precedence, command examples, and failure modes) this runbook plus the `/configz` endpoint act as the canonical source of truth.

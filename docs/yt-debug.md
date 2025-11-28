# YouTube drop debugging

This playbook adds a narrow set of observability tools so we can trace a single
YouTube chat message from gnasty-chat into the elora-chat API/UI. It is designed
for local/docker-compose runs.

## Environment toggles

Enable verbose logging only when you are actively debugging drops:

- `GNASTY_YT_DEBUG=1` (gnasty-harvester container; requires the gnasty-chat repo
  to emit the fingerprint log lines—see the TODO below.)
- `ELORA_YT_DEBUG=1` (elora-chat container; unlocks logging and the debug API.)

`docker-compose.yml` wires both variables with sane defaults. You can also set
them in `.env` or export them before running `docker compose up`.

> **Note:** Keep `ELORA_DB_PATH` and `GNASTY_SINK_SQLITE_PATH` pointed at the
> same SQLite file (for example `/data/gnasty.db`) so the backend and harvester
> are looking at the same rows.

## What gets logged

With `ELORA_YT_DEBUG=1`, the Go backend emits structured `ytdebug` entries:

- `insert_sqlite_ok`: emitted when the backend inserts a YouTube message itself
  (for seeds or any future direct ingestion).
- `tailer_fanned_in`: emitted when the database tailer observes a new YouTube
  row and queues it for WebSocket broadcast.

Each log includes a deterministic `fingerprint`, `ts_ms`, `username`, optional
`channel_id` (derived from `raw_json`), and the SQLite `rowid` when available.

Tail the logs with:

```bash
docker compose logs -f elora-chat | grep ytdebug
```

On the gnasty side (`GNASTY_YT_DEBUG=1`) we expect matching fingerprints on the
harvester logs once the gnasty-chat repo grows the companion logging hook.

## Debug endpoints

`ELORA_YT_DEBUG=1` also enables a read-only endpoint for recent YouTube rows:

- `GET /api/debug/yt-latest?limit=50&username_substring=foo`

The response includes `rowid`, `id`, ISO8601 timestamp, `username`, `text`,
optional `channel_id`, and the computed fingerprint. Use it to cross-check
fingerprints seen in the logs against what landed in SQLite.

Regular history queries still work:

```bash
# Recent YouTube history (matches the UI's /api/messages call)
docker compose exec elora-chat \
  curl -sS "http://localhost:8080/api/messages?platform=YouTube&limit=400" \
  | jq -r '.items[] | "\(.username): \(.text)"'
```

```bash
# YouTube-focused debug view
docker compose exec elora-chat \
  curl -sS "http://localhost:8080/api/debug/yt-latest?limit=50" \
  | jq
```

Compare the fingerprints from gnasty-chat logs, `tailer_fanned_in`, and
`/api/debug/yt-latest` to see where a message disappears.

## TODO (gnasty-chat)

The gnasty-chat repo needs a companion change to emit the same fingerprint on
YouTube messages when `GNASTY_YT_DEBUG` is set. Mirror the hash inputs used here
(platform, channel_id when available, username, text, timestamp in ms) so the
logs line up across services.

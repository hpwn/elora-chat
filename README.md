# elora-chat 🐐

What if we were fauns? Haha. Just curious man, you don't have to get mad. Just look at that- _gets put in a chokehold_

![Elora](https://static.wikia.nocookie.net/spyro/images/a/a6/Elora_PS1.jpg/revision/latest?cb=20180824195930)

## Description 📝

elora-chat is a versatile chat application designed to unify the streaming experience across multiple platforms. It aims to simplify the chat and alert management for streamers like [Dayoman](https://www.twitch.tv/dayoman) who juggle various services and bots during their streams.

## Why? 🤔

On 1/22/24, [Dayoman](https://twitch.tv/dayoman) expressed the need for a streamlined solution to manage chats and alerts during his streams. He wished to move away from unreliable bots and desired a human touch to his alert systems. Our motivation is to enhance audience interaction and provide a seamless viewing experience across platforms, setting a new standard for multi-stream chats.

elora-chat aims to:

- Reduce the reliance on multiple bots and services.

- Offer a single, human-supported chat system for multiple streaming platforms.

- Enhance the chat experience, ensuring contributions are seen, heard, and appreciated.

- Drive audience engagement, encouraging viewers to participate actively on their preferred networks.

Inspired by pioneers like DougDoug, elora-chat aspires to revolutionize chat interaction while adhering to platform terms of service, ensuring a future-proof solution.

## Quick Start ➡️

- Clone the repository: `git clone https://github.com/hpwn/EloraChat.git`

- Navigate to the project directory: `cd EloraChat`

- Ensure [Docker](https://docs.docker.com/get-started/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/linux/) are installed and configured.

- Create environment variables: `echo "TWITCH_CLIENT_ID=\nTWITCH_CLIENT_SECRET=\nTWITCH_REDIRECT_URL=\nYOUTUBE_API_KEY=\nPORT=8080\nDEPLOYED_URL=https://localhost:8080/\nELORA_DB_MODE=ephemeral\nELORA_DB_PATH=\nELORA_DB_MAX_CONNS=16\nELORA_DB_BUSY_TIMEOUT_MS=5000\nELORA_DB_PRAGMAS_EXTRA=mmap_size=268435456,cache_size=-100000,temp_store=MEMORY" > .env`

- Start the server: `docker compose up`

- Connect with your broswer to [http://localhost:8080/](http://localhost:8080/)!

## Running with gnasty

Prefer the external [gnasty](https://github.com/hpwn/gnasty) chat fetcher instead of the bundled `chat_downloader` script? Configure the backend to spawn the gnasty binary and stream NDJSON:

1. Update your `.env` with the gnasty settings:

   ```env
   ELORA_INGEST_DRIVER=gnasty
   CHAT_URLS=https://www.twitch.tv/rifftrax,https://www.youtube.com/watch?v=jfKfPfyJRdk
   GNASTY_BIN=/usr/local/bin/gnasty-chat
   GNASTY_ARGS=--stdout,--format,ndjson
   ```

2. If you run via Docker, mount the gnasty binary into the container path you configured in `GNASTY_BIN`:

   ```bash
   docker run --name elora-chat-instance \
     -p 8080:8080 \
     --env-file .env \
     -v elora_sqlite_data:/data \
     -v /host/path/gnasty-chat:/usr/local/bin/gnasty-chat:ro \
     -d elora-chat
   ```

3. Tail the logs to confirm gnasty ingest activity:

   ```bash
   docker logs -f elora-chat-instance | grep -i 'ingest[gnasty]'
   ```

> In this slice, gnasty lines are validated and logged. The insert hook will be wired up in the next slice so messages land in SQLite.

## SQLite storage (default) 🗄️

The backend now persists chat history to SQLite by default. Ephemeral mode keeps everything in a temp file so you can run without any extra setup. To customize the database:

1. Adjust `ELORA_DB_MODE`, `ELORA_DB_PATH`, `ELORA_DB_MAX_CONNS`, `ELORA_DB_BUSY_TIMEOUT_MS`, and `ELORA_DB_PRAGMAS_EXTRA` as needed. Leaving `ELORA_DB_PATH` blank in `ephemeral` mode automatically creates a temp database such as `/tmp/elora-chat-<pid>.db`.
2. Restart the backend after changing settings. In `persistent` mode set `ELORA_DB_PATH` to a writable location (for example `./data/elora-chat.db` or `/data/elora-chat.db` when using a Docker volume like `-v elora_sqlite_data:/data`).

SQLite is the only storage backend. All chat history and authentication sessions use the same database.

Write-ahead logging, foreign keys, and sensible busy timeouts are enabled automatically via connection pragmas during startup.

### Live from SQLite (DB tailer)
If another process such as **gnasty-chat** writes directly to the same SQLite file, Elora can broadcast those rows live without
running the Python fetcher. Enable the tailer alongside your persistent database configuration:

```
ELORA_DB_MODE=persistent
ELORA_DB_PATH=/data/elora.db
ELORA_DB_TAIL_ENABLED=true
ELORA_DB_TAIL_INTERVAL_MS=200  # aka ELORA_DB_TAIL_POLL_MS
ELORA_DB_TAIL_BATCH=500
```

`ELORA_DB_TAIL_INTERVAL_MS` controls how frequently the poller checks for new rows (lower = more responsive, higher = less DB
churn) and `ELORA_DB_TAIL_BATCH` caps how many messages are streamed per poll.

The WebSocket payload shape can be wrapped for debugging or compatibility by exporting `ELORA_WS_ENVELOPE=true`, which sends
frames like `{ "type": "chat", "data": "<chat-json>" }`. The default remains plain chat JSON strings so existing clients keep
working.

Run gnasty so it ingests into `/data/elora.db` (for example via a shared Docker volume) and start Elora with the same volume
mounted to enable real-time updates.

### DB tailer + WebSocket payloads
The server can optionally wrap WS frames in an envelope:
`{ "type":"chat", "data": "<raw JSON object | JSON array | NDJSON>" }`.
Keepalive frames are `__keepalive__` and are ignored by the client.

The client now tolerates all of the above formats and fills in any missing arrays/fields so the UI never crashes on sparse payloads.

**Local testing (no OAuth):**
- OAuth buttons will 500 if the related envs aren’t set; this is expected.
- DB tailer + gnasty harvester is sufficient to see live messages.

> Heads-up: Twitch / YouTube login flows require valid OAuth secrets. If you leave those blank the auth endpoints will return
500s — that's expected while running locally without real credentials.

### HTTP: recent messages

Recent chat history can be fetched directly from the backend with `GET /api/messages`.

Query parameters:

- `limit` (optional, default 100, maximum 500)
- `since_ts` (optional, RFC3339 timestamp or Unix epoch milliseconds)

Examples:

```bash
curl "http://localhost:8080/api/messages?limit=20"
curl "http://localhost:8080/api/messages?since_ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)&limit=50"
```

### Export & Purge

Admins (or curious humans) can export chat history and purge old rows without touching the database directly.

#### Export via UI

After opening the web app (default http://localhost:8080), open **Settings → Show export panel** (gear icon near the input) to reveal the **Export chat** panel near the top of the page. Use it to:

- Choose the output **format** (NDJSON by default, CSV optional)
- Set a **limit** (defaults to 1,000 messages)
- Provide either **since_ts** or **before_ts** in Unix epoch milliseconds — the fields are mutually exclusive
- Click **Open export** to download immediately, or **Copy curl** to grab a ready-to-run CLI command

#### Export via curl

Export messages (default format is NDJSON):

```bash
# Stream the latest 1,000 messages as NDJSON
curl -s "http://localhost:8080/api/messages/export?limit=1000" > messages.ndjson

# CSV export
curl -s "http://localhost:8080/api/messages/export?format=csv&limit=500" > messages.csv

# Time filters (Unix epoch millis); since_ts and before_ts are mutually exclusive
since=$(date -u +%s%3N)
curl -s "http://localhost:8080/api/messages/export?since_ts=$since&limit=200" > recent.ndjson
```

Purge old messages (timestamps are Unix epoch millis, rows strictly older than the cutoff are removed):

```bash
cutoff=$(date -u -d '30 days ago' +%s%3N)
curl -s -X POST http://localhost:8080/api/messages/purge \
  -H "Content-Type: application/json" \
  -d "{\"before_ts\":$cutoff}"
```

## Usage ⌨️

elora-chat is easy to use. Simply start the server and connect your streaming platforms. The chat will be unified and available in your dashboard for a seamless streaming experience.

### Fetch recent messages with pagination

```bash
# Fetch the newest messages (default limit = 50)
curl -s "http://localhost:8080/api/messages" | jq "."

# Request a smaller page size
curl -s "http://localhost:8080/api/messages?limit=25" | jq "."

# Walk backwards using the returned next_before_ts cursor
resp=$(curl -s "http://localhost:8080/api/messages?limit=25")
next=$(echo "$resp" | jq -r ".next_before_ts // empty")
curl -s "http://localhost:8080/api/messages?limit=25&before_ts=$next" | jq "."
```

## Contributing 🧑🏼‍💻

If you have ideas for improvement or want to contribute to elora-chat, feel free to create a pull request or contact Hayden for collaboration.

Happy streaming! 🎮📹👾

## License

This project is licensed under the **Business Source License 1.1 (BUSL-1.1)**.  
- Non-commercial use only without prior permission.
- Commercial licensing available — [contact](mailto:hwp@arizona.edu) for inquiries.
- On April 25, 2028, the license will convert to Apache 2.0 automatically.

See [LICENSE](./LICENSE) and [COMMERCIAL_LICENSE.md](./COMMERCIAL_LICENSE.md) for more details.

## Global CSS Policy
- Use `src/frontend/src/app.css` only for reset + `:root` tokens (fonts/colors/shared sizes).
- All layout/visual rules must live in component `.svelte` files (scoped).
- Import remains via `+layout.svelte` global style import.

### Ingestion driver

The backend supports a pluggable ingestion driver via `ELORA_INGEST_DRIVER`:

- `chatdownloader` *(default)* — current implementation; reads from `CHAT_URLS`.
- `gnasty` *(stub)* — placeholder for upcoming gnasty-chat integration.

Set `CHAT_URLS` to a comma-separated list of Twitch/YouTube live URLs.

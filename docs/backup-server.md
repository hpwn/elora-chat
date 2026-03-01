# Backup Server (Ubuntu + Namecheap + Caddy)

This guide deploys `elora-chat` and bundled `gnasty-harvester` as a public backup at `https://backup.<domain>`.

## 1. Prerequisites

- Ubuntu server (Thinkpad) with Docker Engine and Docker Compose plugin installed.
- Namecheap-managed domain.
- Router/firewall forwarding TCP `80` and `443` to the Ubuntu host.
- Host firewall allows `22`, `80`, and `443`.
- Twitch OAuth app credentials for `/auth/twitch`.

## 2. DNS Setup (Namecheap)

1. In Namecheap DNS, add an `A` record:
- Host: `backup`
- Value: your WAN IP
- TTL: automatic/default
2. If your WAN IP changes, enable Namecheap Dynamic DNS on your router or Ubuntu host.

## 3. Host Setup

1. Clone the repository:

```bash
sudo mkdir -p /opt
cd /opt
sudo git clone <your-elora-chat-repo-url> elora-chat
sudo chown -R "$USER":"$USER" /opt/elora-chat
cd /opt/elora-chat
```

2. Create production env file from template:

```bash
cp deploy/.env.prod.example .env
chmod +x deploy/update.sh
```

3. Edit `.env` and replace:
- `backup.<domain>` with your real backup hostname.
- `GNASTY_IMAGE=<published-image-tag>` with the real published image tag.
- OAuth secrets (`TWITCH_OAUTH_CLIENT_ID`, `TWITCH_OAUTH_CLIENT_SECRET`).
- Twitch/YouTube selectors as needed (`TWITCH_CHANNEL`, `YOUTUBE_URL`, `TWITCH_NICK`).

4. Edit [`deploy/Caddyfile`](/opt/elora-chat/deploy/Caddyfile) and replace `backup.<domain>` with your real hostname.

## 4. Twitch Developer Console

Add this exact redirect URI to your Twitch application:

`https://backup.<domain>/auth/twitch/callback`

Keep your existing primary URI entries; this adds backup-host support.

## 5. Start the Backup Stack

```bash
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file .env up -d --remove-orphans
```

Services:
- Public: Caddy on ports `80/443`
- Internal only: `elora-chat:8080`, `gnasty-harvester:8765`

## 6. Verify

```bash
curl -fsS https://backup.<domain>/healthz
curl -fsS https://backup.<domain>/readyz
```

Check websocket path from a client: `wss://backup.<domain>/ws/chat`

Start Twitch OAuth flow:

`https://backup.<domain>/auth/twitch/start`

## 7. Update Procedure (Manual)

Use the provided script:

```bash
./deploy/update.sh
```

It runs:
1. `git fetch --all`
2. `git pull --ff-only`
3. `docker compose ... pull`
4. `docker compose ... up -d --remove-orphans`
5. `curl -fsS https://backup.<domain>/readyz`
6. Tail logs for `elora-chat` and `gnasty-harvester`

If your hostname is not `backup.<domain>`, run:

```bash
BACKUP_HOST=backup.example.com ./deploy/update.sh
```

## 8. Troubleshooting

- Certificate issuance fails:
  - Confirm DNS `A` record points to current WAN IP.
  - Confirm inbound ports `80/443` are forwarded and open.
- CORS/origin mismatch:
  - Ensure `ELORA_WS_ALLOWED_ORIGINS` and `ELORA_ALLOWED_ORIGINS` include your exact `https://backup.<domain>`.
- Twitch redirect mismatch:
  - Ensure `.env` and Twitch dev console callback URI are exactly the same.
- SQLite permission errors:
  - Set correct `DOCKER_UID`/`DOCKER_GID` in `.env`.
  - Confirm `./data` is writable by that UID/GID.
- gnasty not reachable from internet:
  - Expected in prod override. `gnasty-harvester` stays internal-only.

# Split Domains on One Host (Prod + Test + Shared Caddy)

This guide runs two isolated Elora stacks on one Ubuntu host:

- `https://dayo.hayden.it.com` (prod/dayoman-focused)
- `https://elora.hayden.it.com` (test/sandbox)

Isolation is infrastructure-only:

- Separate compose projects
- Separate Docker bridge networks
- Separate data mounts
- Separate runtime `/api/config` state
- One shared Caddy router for TLS + hostname routing

## 1. Prerequisites

- Ubuntu server with Docker Engine + Compose plugin.
- DNS control for `hayden.it.com`.
- Router/firewall forwarding TCP `80` and `443` to this host.
- Host firewall allows `22`, `80`, `443`.

## 2. DNS Records

Add `A` records:

- `dayo` -> server public IP
- `elora` -> server public IP

If your host is IPv4-only, remove any `AAAA` records for these hosts to avoid IPv6 resolution failures.

Verify DNS propagation:

```bash
dig +short dayo.hayden.it.com
dig +short elora.hayden.it.com
```

## 3. Repo Setup

```bash
sudo mkdir -p /opt
cd /opt
sudo git clone <your-elora-chat-repo-url> elora-chat
sudo chown -R "$USER":"$USER" /opt/elora-chat
cd /opt/elora-chat
chmod +x deploy/update.sh
```

## 4. Environment Files

This repo now ships:

- `deploy/.env.prod.dayo`
- `deploy/.env.prod.test`

Edit both files before first deploy:

- `GNASTY_IMAGE`
- Twitch OAuth client ID/secret
- Any default source bootstrap fields (`TWITCH_CHANNEL`, `YOUTUBE_URL`, `TWITCH_NICK`)
- UID/GID if host user is not `1000:1000`

Ensure host data paths exist and are writable:

```bash
sudo mkdir -p /data_dayo /data_test
sudo chown -R "$USER":"$USER" /data_dayo /data_test
```

## 5. Twitch OAuth Redirect URIs

In Twitch dev console, add both exact callbacks:

- `https://dayo.hayden.it.com/callback/twitch`
- `https://elora.hayden.it.com/callback/twitch`

## 6. Bring Up Stacks

1. Start shared edge Caddy:

```bash
./deploy/update.sh --target edge
```

2. Start prod app stack:

```bash
./deploy/update.sh --target dayo
```

3. Start test app stack:

```bash
./deploy/update.sh --target test
```

## 7. Verify

```bash
curl -fsS https://dayo.hayden.it.com/healthz; echo
curl -fsS https://dayo.hayden.it.com/readyz; echo
curl -fsS https://elora.hayden.it.com/healthz; echo
curl -fsS https://elora.hayden.it.com/readyz; echo
```

Check per-domain runtime config:

```bash
curl -fsS https://dayo.hayden.it.com/api/config | jq '.config | {twitchChannel,youtubeSourceUrl,apiBaseUrl,wsUrl}'
curl -fsS https://elora.hayden.it.com/api/config | jq '.config | {twitchChannel,youtubeSourceUrl,apiBaseUrl,wsUrl}'
```

## 8. Isolation Validation

1. Change source/channel on `elora.hayden.it.com`.
2. Confirm `dayo.hayden.it.com/api/config` is unchanged.
3. Restart only test stack:

```bash
./deploy/update.sh --target test --no-git
```

4. Confirm prod remains healthy.

## 9. Operational Commands

Status/logs by target:

```bash
./deploy/update.sh --target edge --action status
./deploy/update.sh --target dayo --action status
./deploy/update.sh --target test --action status

./deploy/update.sh --target edge --action logs
./deploy/update.sh --target dayo --action logs
./deploy/update.sh --target test --action logs
```

Direct compose inspection:

```bash
docker compose -f deploy/docker-compose.edge.yml --env-file deploy/.env.prod.test -p elora_edge ps
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.dayo ps
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.test ps
```

## 10. Troubleshooting

- `502` from Caddy:
  - Confirm `elora-edge` network exists.
  - Confirm both app stacks are attached and healthy.
  - Confirm container names resolve:
    - `elora_dayo-elora-chat`
    - `elora_test-elora-chat`
- OAuth redirect mismatch:
  - Callback in env must exactly match Twitch console URI.
- CORS errors:
  - Verify `ELORA_ALLOWED_ORIGINS` and `ELORA_WS_ALLOWED_ORIGINS` per domain.
- Permission errors on SQLite/token handoff:
  - Verify ownership and write perms on `/data_dayo` and `/data_test`.

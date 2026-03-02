#!/usr/bin/env bash
set -euo pipefail

# Recover Twitch badge icon enrichment by fixing Docker daemon DNS and
# redeploying dayo/test stacks with bounded runtime checks.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

step() { echo; echo "== $* =="; }
run() { echo ">> $*"; "$@"; }
try_run() { echo ">> $*"; "$@" || true; }

pick_ipv4_from_resolv() {
  local file="$1"
  [[ -f "${file}" ]] || return 0
  awk '/^nameserver /{print $2}' "${file}" \
    | rg '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' \
    | rg -v '^127\.' \
    | awk '!seen[$0]++'
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: required command not found: $1" >&2
    exit 1
  fi
}

require_cmd docker
require_cmd jq
require_cmd timeout
require_cmd curl
require_cmd rg

step "preflight"
run pwd
run sudo -v
run timeout 10s cat /etc/resolv.conf
try_run timeout 10s resolvectl dns
try_run timeout 10s cat /run/systemd/resolve/resolv.conf
try_run timeout 10s cat /etc/docker/daemon.json
try_run timeout 10s docker inspect elora_test-elora-chat --format '{{json .HostConfig.DNS}}'
try_run timeout 10s docker inspect elora_dayo-elora-chat --format '{{json .HostConfig.DNS}}'

step "select dns servers"
if [[ -n "${DNS_SERVERS:-}" ]]; then
  # Expected format: "10.0.0.2,10.0.0.3"
  DNS_CSV="$(tr ',' '\n' <<<"${DNS_SERVERS}" \
    | tr -d '[:space:]' \
    | rg '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' \
    | rg -v '^127\.' \
    | awk '!seen[$0]++' \
    | head -n 2 \
    | paste -sd, -)"
else
  DNS_CSV=""

  # 1) Prefer upstream resolvers from resolved's upstream file.
  DNS_CSV="$(pick_ipv4_from_resolv /run/systemd/resolve/resolv.conf \
    | head -n 2 \
    | paste -sd, -)"

  # 2) Fallback to /etc/resolv.conf non-loopback entries.
  if [[ -z "${DNS_CSV}" ]]; then
    DNS_CSV="$(pick_ipv4_from_resolv /etc/resolv.conf \
      | head -n 2 \
      | paste -sd, -)"
  fi
fi

DNS_COUNT="$(tr ',' '\n' <<<"${DNS_CSV}" | rg -v '^[[:space:]]*$' | wc -l | tr -d ' ')"
if [[ -z "${DNS_CSV}" || "${DNS_COUNT}" -lt 2 ]]; then
  echo "ERROR: need at least two valid IPv4 DNS resolvers. Set DNS_SERVERS='x.x.x.x,y.y.y.y' and rerun." >&2
  exit 1
fi

if tr ',' '\n' <<<"${DNS_CSV}" | rg -v '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | rg -q '.'; then
  echo "ERROR: invalid DNS resolver format detected: ${DNS_CSV}" >&2
  exit 1
fi
echo "Using DNS resolvers: ${DNS_CSV}"

step "apply docker daemon dns"
export DNS_CSV
run sudo DNS_CSV="${DNS_CSV}" python3 - <<'PY'
import json
import os
from pathlib import Path

path = Path("/etc/docker/daemon.json")
data = {}
if path.exists():
    txt = path.read_text().strip()
    if txt:
        data = json.loads(txt)

dns = [x.strip() for x in os.environ["DNS_CSV"].split(",") if x.strip()]
if len(dns) < 2:
    raise SystemExit("Need at least two DNS resolvers; got: %r" % dns)

data["dns"] = dns
tmp = path.with_suffix(".json.tmp")
tmp.write_text(json.dumps(data, indent=2) + "\n")
tmp.replace(path)
print("Updated /etc/docker/daemon.json with dns=", dns)
PY

run sudo systemctl restart docker
run timeout 20s docker info | rg -i 'Server Version|DNS'

step "redeploy edge/dayo/test (no tail)"
run docker compose -f deploy/docker-compose.edge.yml --env-file deploy/.env.prod.test -p elora_edge up -d --remove-orphans caddy
run docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.dayo build --pull elora-chat
run docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.dayo up -d --force-recreate --remove-orphans elora-chat gnasty-harvester
run docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.test build --pull elora-chat
run docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.test up -d --force-recreate --remove-orphans elora-chat gnasty-harvester

step "post-recreate dns checks"
run timeout 10s docker inspect elora_test-elora-chat --format '{{json .HostConfig.DNS}}'
run timeout 10s docker inspect elora_dayo-elora-chat --format '{{json .HostConfig.DNS}}'

step "no-op config resync"
for domain in dayo.hayden.it.com elora.hayden.it.com; do
  cfg="$(timeout 15s curl -fsS "https://${domain}/api/config" | jq '.config')"
  timeout 15s curl -fsS -X PUT "https://${domain}/api/config" \
    -H 'Content-Type: application/json' \
    --data "${cfg}" >/dev/null
done

step "health + isolation"
timeout 15s curl -fsS https://dayo.hayden.it.com/readyz; echo
timeout 15s curl -fsS https://elora.hayden.it.com/readyz; echo
timeout 15s curl -fsS https://dayo.hayden.it.com/api/config  | jq '.config | {apiBaseUrl,wsUrl,twitchChannel,youtubeSourceUrl}'
timeout 15s curl -fsS https://elora.hayden.it.com/api/config | jq '.config | {apiBaseUrl,wsUrl,twitchChannel,youtubeSourceUrl}'

step "dns + twitch endpoint checks"
timeout 30s docker run --rm --network elora-test-network curlimages/curl:8.7.1 sh -lc \
  "curl -fsS https://badges.twitch.tv/v1/badges/global/display >/dev/null && echo test-network-ok"
timeout 30s docker run --rm --network elora-dayo-network curlimages/curl:8.7.1 sh -lc \
  "curl -fsS https://badges.twitch.tv/v1/badges/global/display >/dev/null && echo dayo-network-ok"
timeout 30s docker exec -i elora_test-elora-chat wget -qO- https://badges.twitch.tv/v1/badges/global/display >/dev/null && echo test-container-ok
timeout 30s docker exec -i elora_dayo-elora-chat wget -qO- https://badges.twitch.tv/v1/badges/global/display >/dev/null && echo dayo-container-ok

step "resolver error check (recent logs)"
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.dayo logs --since=5m elora-chat \
  | rg -i 'badges: twitch badge image resolve failed|no such host' && {
    echo "ERROR: dayo still reports badge resolver failures" >&2
    exit 1
  } || true
docker compose -f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file deploy/.env.prod.test logs --since=5m elora-chat \
  | rg -i 'badges: twitch badge image resolve failed|no such host' && {
    echo "ERROR: test still reports badge resolver failures" >&2
    exit 1
  } || true

step "done"
echo "Docker DNS recovery flow completed."
echo "Next: verify live UI badge icons on dayo + elora (hard refresh)."

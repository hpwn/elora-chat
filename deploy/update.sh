#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
COMPOSE_ARGS=(
  -f docker-compose.yml
  -f deploy/docker-compose.prod.yml
  --env-file .env
)
BACKUP_HOST="${BACKUP_HOST:-backup.<domain>}"

cd "${REPO_ROOT}"

echo "[1/6] Fetch latest refs"
git fetch --all

echo "[2/6] Fast-forward current branch"
git pull --ff-only

echo "[3/6] Pull container images"
docker compose "${COMPOSE_ARGS[@]}" pull

echo "[4/6] Restart stack"
docker compose "${COMPOSE_ARGS[@]}" up -d --remove-orphans

echo "[5/6] Health check: https://${BACKUP_HOST}/readyz"
curl -fsS "https://${BACKUP_HOST}/readyz"
echo

echo "[6/6] Recent service logs"
docker compose "${COMPOSE_ARGS[@]}" logs --tail=50 elora-chat gnasty-harvester
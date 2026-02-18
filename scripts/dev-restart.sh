#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build --force-recreate
for i in $(seq 1 80); do
  if curl -fsS http://localhost:8080/healthz >/dev/null; then
    echo "ok"
    docker compose -f docker-compose.yml -f docker-compose.dev.yml ps
    exit 0
  fi
  sleep 0.25
done
echo "healthz did not become ready" >&2
docker compose -f docker-compose.yml -f docker-compose.dev.yml ps >&2 || true
exit 1

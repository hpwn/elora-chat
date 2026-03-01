#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TARGET="test"
NO_GIT=0
ACTION="deploy"

usage() {
  cat <<'EOF'
Usage: ./deploy/update.sh --target <dayo|test|edge> [--action <deploy|status|logs>] [--no-git]

Targets:
  dayo  Update dayo.hayden.it.com app stack (elora-chat + gnasty-harvester)
  test  Update elora.hayden.it.com app stack (elora-chat + gnasty-harvester)
  edge  Update shared Caddy edge router stack

Flags:
  --action  deploy (default), status, or logs
  --no-git  Skip git fetch/pull steps
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      TARGET="${2:-}"
      shift 2
      ;;
    --no-git)
      NO_GIT=1
      shift
      ;;
    --action)
      ACTION="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

cd "${REPO_ROOT}"

case "${TARGET}" in
  dayo)
    ENV_FILE="deploy/.env.prod.dayo"
    APP_HOST="dayo.hayden.it.com"
    COMPOSE_ARGS=(-f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file "${ENV_FILE}")
    SERVICES=(elora-chat gnasty-harvester)
    ;;
  test)
    ENV_FILE="deploy/.env.prod.test"
    APP_HOST="elora.hayden.it.com"
    COMPOSE_ARGS=(-f docker-compose.yml -f deploy/docker-compose.prod.yml --env-file "${ENV_FILE}")
    SERVICES=(elora-chat gnasty-harvester)
    ;;
  edge)
    ENV_FILE="deploy/.env.prod.test"
    APP_HOST=""
    COMPOSE_ARGS=(-f deploy/docker-compose.edge.yml --env-file "${ENV_FILE}" -p elora_edge)
    SERVICES=(caddy)
    ;;
  *)
    echo "Invalid target: ${TARGET}" >&2
    usage
    exit 1
    ;;
esac

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "Missing env file: ${ENV_FILE}" >&2
  exit 1
fi

case "${ACTION}" in
  deploy|status|logs)
    ;;
  *)
    echo "Invalid action: ${ACTION}" >&2
    usage
    exit 1
    ;;
esac

if [[ "${ACTION}" != "deploy" ]]; then
  if [[ "${ACTION}" == "status" ]]; then
    docker compose "${COMPOSE_ARGS[@]}" ps
  else
    if [[ "${TARGET}" == "edge" ]]; then
      docker compose "${COMPOSE_ARGS[@]}" logs --tail=100 caddy
    else
      docker compose "${COMPOSE_ARGS[@]}" logs --tail=100 elora-chat gnasty-harvester
    fi
  fi
  exit 0
fi

if [[ "${NO_GIT}" -eq 0 ]]; then
  echo "[1/6] Fetch latest refs"
  git fetch --all

  echo "[2/6] Fast-forward current branch"
  git pull --ff-only
else
  echo "[1/6] Skipping git sync (--no-git)"
  echo "[2/6] Skipping git sync (--no-git)"
fi

echo "[3/6] Ensure shared edge network exists"
docker network inspect elora-edge >/dev/null 2>&1 || docker network create elora-edge

if [[ "${TARGET}" == "edge" ]]; then
  echo "[4/6] Pull container images (${TARGET})"
  docker compose "${COMPOSE_ARGS[@]}" pull "${SERVICES[@]}"
else
  echo "[4/6] Build app image (${TARGET})"
  docker compose "${COMPOSE_ARGS[@]}" build --pull elora-chat
fi

echo "[5/6] Restart stack (${TARGET})"
docker compose "${COMPOSE_ARGS[@]}" up -d --remove-orphans "${SERVICES[@]}"

if [[ -n "${APP_HOST}" ]]; then
  echo "[6/6] Health check: https://${APP_HOST}/readyz"
  curl -fsS "https://${APP_HOST}/readyz"
  echo
  docker compose "${COMPOSE_ARGS[@]}" logs --tail=50 elora-chat gnasty-harvester
else
  echo "[6/6] Edge logs"
  docker compose "${COMPOSE_ARGS[@]}" logs --tail=50 caddy
fi

#!/usr/bin/env bash
# Usage: ./scripts/run-local.sh
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)

ENV_FILE="${PROJECT_ROOT}/.env"
ENV_EXAMPLE="${PROJECT_ROOT}/.env.example"

if [[ ! -f "${ENV_FILE}" ]]; then
  if [[ -f "${ENV_EXAMPLE}" ]]; then
    echo "Creating .env from .env.example"
    cp "${ENV_EXAMPLE}" "${ENV_FILE}"
  else
    echo "Error: ${ENV_EXAMPLE} not found" >&2
    exit 1
  fi
fi

cd "${PROJECT_ROOT}"

docker compose up -d --build

curl --fail --show-error --silent "http://localhost:8080/configz"
echo

#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker run --rm \
  -u "$(id -u):$(id -g)" \
  -v "$PWD/src/frontend:/app" \
  -w /app \
  node:20-bullseye \
  bash -lc 'npm ci && npm run build'

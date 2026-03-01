#!/usr/bin/env bash

set -euo pipefail

GO_IMAGE="${GO_IMAGE:-golang:1.24.3}"
BACKEND_DIR="${BACKEND_DIR:-$PWD/src/backend}"

echo "== go image =="
docker run --rm --entrypoint /bin/sh "${GO_IMAGE}" -c 'go version'

echo "== focused send-route tests =="
docker run --rm \
  -v "${BACKEND_DIR}:/app" \
  -w /app \
  --entrypoint /bin/sh \
  "${GO_IMAGE}" \
  -c 'go test -vet=off -p 1 ./routes -run TestSendMessageHandler -count=1'

echo "== full routes package tests =="
docker run --rm \
  -v "${BACKEND_DIR}:/app" \
  -w /app \
  --entrypoint /bin/sh \
  "${GO_IMAGE}" \
  -c 'go test -vet=off -p 1 ./routes -count=1'

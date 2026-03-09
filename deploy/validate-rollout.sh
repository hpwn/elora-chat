#!/usr/bin/env bash
set -euo pipefail

# Non-mutating rollout validation for edge + dayo/test/dylan stacks.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

DAYO_DOMAIN="${DAYO_DOMAIN:-dayo.hayden.it.com}"
TEST_DOMAIN="${TEST_DOMAIN:-elora.hayden.it.com}"
DYLAN_DOMAIN="${DYLAN_DOMAIN:-dylan.hayden.it.com}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd jq
need_cmd docker

check_http() {
  local domain="$1"
  echo "== ${domain}: health checks =="
  curl -fsS "https://${domain}/healthz" >/dev/null
  curl -fsS "https://${domain}/readyz" >/dev/null
  echo "ok: healthz/readyz"
}

check_runtime() {
  local domain="$1"
  echo "== ${domain}: runtime checks =="
  curl -fsS "https://${domain}/api/config" \
    | jq '.config | {apiBaseUrl,wsUrl,twitchChannel,youtubeSourceUrl}'
  curl -fsS "https://${domain}/configz" \
    | jq '{tailer,gnasty_sync}'
}

check_compose_target() {
  local target="$1"
  echo "== compose target: ${target} =="
  ./deploy/update.sh --target "${target}" --action status
}

check_container_dns() {
  local container="$1"
  echo "== container dns: ${container} =="
  docker inspect "${container}" --format '{{json .HostConfig.DNS}}'
}

echo "== 1) Compose status =="
check_compose_target edge
check_compose_target dayo
check_compose_target test
check_compose_target dylan

echo
echo "== 2) External endpoint checks =="
check_http "${DAYO_DOMAIN}"
check_http "${TEST_DOMAIN}"
check_http "${DYLAN_DOMAIN}"

echo
echo "== 3) Runtime snapshots =="
check_runtime "${DAYO_DOMAIN}"
check_runtime "${TEST_DOMAIN}"
check_runtime "${DYLAN_DOMAIN}"

echo
echo "== 4) Container DNS settings =="
check_container_dns elora_dayo-elora-chat
check_container_dns elora_test-elora-chat
check_container_dns elora_dylan-elora-chat

echo
echo "Validation complete."

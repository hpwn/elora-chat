#!/usr/bin/env bash
set -euo pipefail

# Live-only WS triage for split domains.
# This script does not mutate runtime config or persisted data.

DAYO_DOMAIN="${DAYO_DOMAIN:-dayo.hayden.it.com}"
ELORA_DOMAIN="${ELORA_DOMAIN:-elora.hayden.it.com}"
POLL_SECS="${POLL_SECS:-12}"
WS_TIMEOUT_SECS="${WS_TIMEOUT_SECS:-45}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd jq

run_ws_check() {
  local domain="$1"
  local url="wss://${domain}/ws/chat?replay=0"

  echo "== WS transport check: ${domain} =="
  echo "URL: ${url}"

  if command -v websocat >/dev/null 2>&1; then
    local out
    out="$(timeout "${WS_TIMEOUT_SECS}"s websocat -t "${url}" 2>/dev/null || true)"
    if grep -q "__keepalive__" <<<"${out}"; then
      echo "ws-ok: keepalive observed"
      return 0
    fi
    echo "ws-warning: no keepalive observed via local websocat within ${WS_TIMEOUT_SECS}s"
    return 1
  fi

  if command -v docker >/dev/null 2>&1; then
    local out
    out="$(
      timeout "${WS_TIMEOUT_SECS}"s docker run --rm --network host ghcr.io/vi/websocat:latest -t "${url}" 2>/dev/null || true
    )"
    if grep -q "__keepalive__" <<<"${out}"; then
      echo "ws-ok: keepalive observed (docker websocat)"
      return 0
    fi
    echo "ws-warning: no keepalive observed via docker websocat within ${WS_TIMEOUT_SECS}s"
    return 1
  fi

  echo "ws-skip: neither websocat nor docker available for raw WS probe"
  return 2
}

top_msg_fingerprint() {
  local domain="$1"
  curl -fsS "https://${domain}/api/messages?limit=1" \
    | jq -r '.items[0] | if . == null then "none" else ((.ts // "") + "|" + (.username // "") + "|" + (.platform // "") + "|" + (.text // "")) end'
}

show_runtime() {
  local domain="$1"
  echo "== Runtime snapshot: ${domain} =="
  curl -fsS "https://${domain}/configz" \
    | jq '{tailer,gnasty_sync,auth_twitch_redirect:.auth.twitch.redirect_url}'
  echo
  curl -fsS "https://${domain}/api/config" \
    | jq '.config | {apiBaseUrl,wsUrl,twitchChannel,youtubeSourceUrl}'
}

echo "== 1) Confirm runtime mode (live-only expectation) =="
show_runtime "${DAYO_DOMAIN}"
show_runtime "${ELORA_DOMAIN}"
echo "note: live-only startup expects ws replay=0 and empty UI until new inbound rows arrive."

echo
echo "== 2) Prove WS transport health (outside UI parsing) =="
run_ws_check "${DAYO_DOMAIN}" || true
run_ws_check "${ELORA_DOMAIN}" || true

echo
echo "== 3) Prove ingest movement (top row drift over ${POLL_SECS}s) =="
dayo_before="$(top_msg_fingerprint "${DAYO_DOMAIN}")"
elora_before="$(top_msg_fingerprint "${ELORA_DOMAIN}")"
echo "dayo before:  ${dayo_before}"
echo "elora before: ${elora_before}"
sleep "${POLL_SECS}"
dayo_after="$(top_msg_fingerprint "${DAYO_DOMAIN}")"
elora_after="$(top_msg_fingerprint "${ELORA_DOMAIN}")"
echo "dayo after:   ${dayo_after}"
echo "elora after:  ${elora_after}"

if [[ "${dayo_before}" == "${dayo_after}" ]]; then
  echo "dayo: no fresh top row observed in window (${POLL_SECS}s)"
else
  echo "dayo: fresh top row observed"
fi
if [[ "${elora_before}" == "${elora_after}" ]]; then
  echo "elora: no fresh top row observed in window (${POLL_SECS}s)"
else
  echo "elora: fresh top row observed"
fi

echo
echo "== 4) End-to-end publish check (requires one fresh chat message per domain) =="
echo "send one new Twitch message in #$(curl -fsS "https://${DAYO_DOMAIN}/api/config" | jq -r '.config.twitchChannel')"
echo "send one new Twitch message in #$(curl -fsS "https://${ELORA_DOMAIN}/api/config" | jq -r '.config.twitchChannel')"
echo "then run:"
echo "  curl -fsS 'https://${DAYO_DOMAIN}/api/messages?limit=3' | jq '.items[] | {platform,username,text,ts}'"
echo "  curl -fsS 'https://${ELORA_DOMAIN}/api/messages?limit=3' | jq '.items[] | {platform,username,text,ts}'"

echo
echo "== 5) If API fresh rows but UI still silent, inspect publish + source-switch logs =="
echo "  ./deploy/update.sh --target dayo --action logs | rg -i 'dbtailer: published|switch channel|youtube: switch url|ws: WebSocket|error|failed'"
echo "  ./deploy/update.sh --target test --action logs | rg -i 'dbtailer: published|switch channel|youtube: switch url|ws: WebSocket|error|failed'"

echo
echo "== 6) If no fresh API rows, inspect ingest receiver readiness =="
echo "  ./deploy/update.sh --target dayo --action logs | rg -i 'twitch receiver started|joined #|twitch token not provided|no receivers configured|error|failed'"
echo "  ./deploy/update.sh --target test --action logs | rg -i 'twitch receiver started|joined #|twitch token not provided|no receivers configured|error|failed'"

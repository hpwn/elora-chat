#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/alerts-soak.sh [options]

Options:
  --duration <value>        Soak duration (e.g. 30m, 900s, 1h). Default: 30m
  --poll <seconds>          Count polling interval in seconds. Default: 30
  --out-dir <path>          Output directory. Default: ./artifacts/alerts-soak-<timestamp>
  --gnasty-base <url>       Gnasty base URL. Default: http://localhost:8765
  --elora-base <url>        Elora base URL. Default: http://localhost:8080
  --filters <querystring>   Raw querystring forwarded to alerts endpoints (no leading '?')
                            Example: platform=twitch&type=twitch.bits
  --inconclusive-exit <n>   Exit code for INCONCLUSIVE runs (0 or 1). Default: 1
  -h, --help                Show help

Examples:
  ./scripts/alerts-soak.sh
  ./scripts/alerts-soak.sh --duration 15m --filters "platform=twitch&type=twitch.bits"
  ./scripts/alerts-soak.sh --duration 2m --poll 5 --out-dir ./artifacts/test-soak
EOF
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing dependency: $cmd" >&2
    exit 2
  fi
}

parse_duration_seconds() {
  local raw="$1"
  if [[ "$raw" =~ ^[0-9]+$ ]]; then
    echo "$raw"
    return
  fi
  if [[ "$raw" =~ ^([0-9]+)([smh])$ ]]; then
    local value="${BASH_REMATCH[1]}"
    local unit="${BASH_REMATCH[2]}"
    case "$unit" in
      s) echo "$value" ;;
      m) echo $((value * 60)) ;;
      h) echo $((value * 3600)) ;;
      *) return 1 ;;
    esac
    return
  fi
  return 1
}

join_query() {
  local base="$1"
  local qs="$2"
  if [[ -z "$qs" ]]; then
    echo "$base"
    return
  fi
  if [[ "$base" == *\?* ]]; then
    echo "${base}&${qs}"
  else
    echo "${base}?${qs}"
  fi
}

normalize_base() {
  local base="$1"
  base="${base%/}"
  echo "$base"
}

default_out_dir() {
  echo "./artifacts/alerts-soak-$(date +%Y%m%d-%H%M%S)"
}

count_endpoint() {
  local url="$1"
  curl -fsS "$url" | jq -er '.count // 0'
}

safe_count_endpoint() {
  local url="$1"
  local value
  if value="$(count_endpoint "$url" 2>/dev/null)"; then
    echo "$value"
  else
    echo "ERR"
  fi
}

parse_sse_jsonl() {
  local in_file="$1"
  local out_file="$2"
  grep '^data:' "$in_file" | sed 's/^data:[[:space:]]*//' | jq -Rc 'fromjson? | select(.)' > "$out_file" || true
}

parse_list_jsonl() {
  local in_file="$1"
  local out_file="$2"
  jq -c '
    if type == "array" then
      .[]
    elif type == "object" and (.items? | type) == "array" then
      .items[]
    else
      empty
    end
  ' "$in_file" > "$out_file" 2>/dev/null || true
}

extract_keys() {
  local in_jsonl="$1"
  local out_keys="$2"
  jq -r '
    def fp:
      [
        (.platform // ""),
        (.type // ""),
        ((.ts // .timestamp // .timestamp_ms // "") | tostring),
        (.username // ""),
        (.text // ""),
        ((.amount // "") | tostring),
        ((.count // "") | tostring)
      ] | join("|");
    (.id // .platform_event_id // fp)
  ' "$in_jsonl" | sed '/^$/d' | sort -u > "$out_keys" || true
}

DURATION_RAW="30m"
POLL_SECONDS=30
OUT_DIR="$(default_out_dir)"
GNASTY_BASE="http://localhost:8765"
ELORA_BASE="http://localhost:8080"
FILTERS=""
INCONCLUSIVE_EXIT=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --duration)
      DURATION_RAW="${2:-}"
      shift 2
      ;;
    --poll)
      POLL_SECONDS="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    --gnasty-base)
      GNASTY_BASE="${2:-}"
      shift 2
      ;;
    --elora-base)
      ELORA_BASE="${2:-}"
      shift 2
      ;;
    --filters)
      FILTERS="${2:-}"
      shift 2
      ;;
    --inconclusive-exit)
      INCONCLUSIVE_EXIT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

require_cmd curl
require_cmd jq
require_cmd grep
require_cmd sed
require_cmd sort
require_cmd comm

if ! [[ "$POLL_SECONDS" =~ ^[0-9]+$ ]] || [[ "$POLL_SECONDS" -le 0 ]]; then
  echo "--poll must be a positive integer number of seconds" >&2
  exit 2
fi

if ! [[ "$INCONCLUSIVE_EXIT" =~ ^[01]$ ]]; then
  echo "--inconclusive-exit must be 0 or 1" >&2
  exit 2
fi

if ! DURATION_SECONDS="$(parse_duration_seconds "$DURATION_RAW")"; then
  echo "invalid --duration: $DURATION_RAW (expected e.g. 30m, 900s, 1h)" >&2
  exit 2
fi

if [[ "$DURATION_SECONDS" -le 0 ]]; then
  echo "--duration must be > 0" >&2
  exit 2
fi

GNASTY_BASE="$(normalize_base "$GNASTY_BASE")"
ELORA_BASE="$(normalize_base "$ELORA_BASE")"
mkdir -p "$OUT_DIR"

START_ISO="$(date -Iseconds)"
START_MS="$(date +%s%3N)"
START_EPOCH_SEC="$(date +%s)"
START_SINCE_RFC3339="$(date -u -d "@$START_EPOCH_SEC" +%Y-%m-%dT%H:%M:%SZ)"
END_TARGET=$(( $(date +%s) + DURATION_SECONDS ))

G_COUNT_URL="$(join_query "$GNASTY_BASE/alerts/count" "$FILTERS")"
E_COUNT_URL="$(join_query "$ELORA_BASE/api/alerts/count" "$FILTERS")"
G_STREAM_URL="$(join_query "$GNASTY_BASE/alerts/stream" "$FILTERS")"
E_STREAM_URL="$(join_query "$ELORA_BASE/api/alerts/stream" "$FILTERS")"
G_LIST_URL="$(join_query "$GNASTY_BASE/alerts?since=$START_SINCE_RFC3339&limit=5000&order=asc" "$FILTERS")"
E_LIST_URL="$(join_query "$ELORA_BASE/api/alerts?since=$START_SINCE_RFC3339&limit=5000&order=asc" "$FILTERS")"

G_POLL_ERRORS=0
E_POLL_ERRORS=0
G_STREAM_DROPPED=0
E_STREAM_DROPPED=0

cleanup() {
  if [[ -n "${G_STREAM_PID:-}" ]]; then
    kill "$G_STREAM_PID" 2>/dev/null || true
  fi
  if [[ -n "${E_STREAM_PID:-}" ]]; then
    kill "$E_STREAM_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "alerts soak output: $OUT_DIR"
echo "start: $START_ISO"
echo "duration: ${DURATION_RAW} (${DURATION_SECONDS}s)"
echo "filters: ${FILTERS:-<none>}"

G_START="$(count_endpoint "$G_COUNT_URL")"
E_START="$(count_endpoint "$E_COUNT_URL")"

printf "start_counts gnasty=%s elora=%s\n" "$G_START" "$E_START" | tee "$OUT_DIR/summary.txt"

curl -NsS "$G_STREAM_URL" > "$OUT_DIR/gnasty.sse" 2> "$OUT_DIR/gnasty.stream.err" &
G_STREAM_PID=$!
curl -NsS "$E_STREAM_URL" > "$OUT_DIR/elora.sse" 2> "$OUT_DIR/elora.stream.err" &
E_STREAM_PID=$!

while [[ "$(date +%s)" -lt "$END_TARGET" ]]; do
  NOW="$(date -Iseconds)"
  G_CUR="$(safe_count_endpoint "$G_COUNT_URL")"
  E_CUR="$(safe_count_endpoint "$E_COUNT_URL")"

  if [[ "$G_CUR" == "ERR" ]]; then
    G_POLL_ERRORS=$((G_POLL_ERRORS + 1))
  fi
  if [[ "$E_CUR" == "ERR" ]]; then
    E_POLL_ERRORS=$((E_POLL_ERRORS + 1))
  fi

  printf "%s gnasty=%s elora=%s\n" "$NOW" "$G_CUR" "$E_CUR" | tee -a "$OUT_DIR/counts.log"

  if ! kill -0 "$G_STREAM_PID" 2>/dev/null; then
    G_STREAM_DROPPED=1
  fi
  if ! kill -0 "$E_STREAM_PID" 2>/dev/null; then
    E_STREAM_DROPPED=1
  fi

  sleep "$POLL_SECONDS"
done

cleanup
wait "$G_STREAM_PID" 2>/dev/null || true
wait "$E_STREAM_PID" 2>/dev/null || true

END_ISO="$(date -Iseconds)"
G_END="$(safe_count_endpoint "$G_COUNT_URL")"
E_END="$(safe_count_endpoint "$E_COUNT_URL")"

if [[ "$G_END" == "ERR" || "$E_END" == "ERR" ]]; then
  echo "final count request failed (gnasty=$G_END elora=$E_END)" >&2
fi

parse_sse_jsonl "$OUT_DIR/gnasty.sse" "$OUT_DIR/gnasty.jsonl"
parse_sse_jsonl "$OUT_DIR/elora.sse" "$OUT_DIR/elora.jsonl"

curl -fsS "$G_LIST_URL" > "$OUT_DIR/gnasty.list.json" 2> "$OUT_DIR/gnasty.list.err" || true
curl -fsS "$E_LIST_URL" > "$OUT_DIR/elora.list.json" 2> "$OUT_DIR/elora.list.err" || true

parse_list_jsonl "$OUT_DIR/gnasty.list.json" "$OUT_DIR/gnasty.list.jsonl"
parse_list_jsonl "$OUT_DIR/elora.list.json" "$OUT_DIR/elora.list.jsonl"

extract_keys "$OUT_DIR/gnasty.jsonl" "$OUT_DIR/gnasty.keys"
extract_keys "$OUT_DIR/elora.jsonl" "$OUT_DIR/elora.keys"
extract_keys "$OUT_DIR/gnasty.list.jsonl" "$OUT_DIR/gnasty.list.keys"
extract_keys "$OUT_DIR/elora.list.jsonl" "$OUT_DIR/elora.list.keys"

comm -23 "$OUT_DIR/gnasty.keys" "$OUT_DIR/elora.keys" > "$OUT_DIR/gnasty_only.keys" || true
comm -13 "$OUT_DIR/gnasty.keys" "$OUT_DIR/elora.keys" > "$OUT_DIR/elora_only.keys" || true
comm -23 "$OUT_DIR/gnasty.list.keys" "$OUT_DIR/elora.list.keys" > "$OUT_DIR/gnasty_only.list.keys" || true
comm -13 "$OUT_DIR/gnasty.list.keys" "$OUT_DIR/elora.list.keys" > "$OUT_DIR/elora_only.list.keys" || true

G_STREAM_EVENTS="$(wc -l < "$OUT_DIR/gnasty.jsonl" | tr -d ' ')"
E_STREAM_EVENTS="$(wc -l < "$OUT_DIR/elora.jsonl" | tr -d ' ')"
G_UNIQUE="$(wc -l < "$OUT_DIR/gnasty.keys" | tr -d ' ')"
E_UNIQUE="$(wc -l < "$OUT_DIR/elora.keys" | tr -d ' ')"
BOTH_UNIQUE="$(comm -12 "$OUT_DIR/gnasty.keys" "$OUT_DIR/elora.keys" | wc -l | tr -d ' ')"
G_ONLY_UNIQUE="$(wc -l < "$OUT_DIR/gnasty_only.keys" | tr -d ' ')"
E_ONLY_UNIQUE="$(wc -l < "$OUT_DIR/elora_only.keys" | tr -d ' ')"
G_LIST_EVENTS="$(wc -l < "$OUT_DIR/gnasty.list.jsonl" | tr -d ' ')"
E_LIST_EVENTS="$(wc -l < "$OUT_DIR/elora.list.jsonl" | tr -d ' ')"
G_LIST_UNIQUE="$(wc -l < "$OUT_DIR/gnasty.list.keys" | tr -d ' ')"
E_LIST_UNIQUE="$(wc -l < "$OUT_DIR/elora.list.keys" | tr -d ' ')"
BOTH_LIST_UNIQUE="$(comm -12 "$OUT_DIR/gnasty.list.keys" "$OUT_DIR/elora.list.keys" | wc -l | tr -d ' ')"
G_ONLY_LIST_UNIQUE="$(wc -l < "$OUT_DIR/gnasty_only.list.keys" | tr -d ' ')"
E_ONLY_LIST_UNIQUE="$(wc -l < "$OUT_DIR/elora_only.list.keys" | tr -d ' ')"

G_DELTA="ERR"
E_DELTA="ERR"
if [[ "$G_END" != "ERR" ]]; then
  G_DELTA=$((G_END - G_START))
fi
if [[ "$E_END" != "ERR" ]]; then
  E_DELTA=$((E_END - E_START))
fi

STATUS="FAIL"
EXIT_CODE=1
REASON="primary_parity_mismatch"
if [[ "$G_DELTA" == "0" && "$E_DELTA" == "0" && "$G_LIST_UNIQUE" == "0" && "$E_LIST_UNIQUE" == "0" ]]; then
  STATUS="INCONCLUSIVE"
  EXIT_CODE="$INCONCLUSIVE_EXIT"
  REASON="no_alerts_in_window"
elif [[ "$G_DELTA" != "ERR" && "$E_DELTA" != "ERR" && "$G_DELTA" -eq "$E_DELTA" && "$G_ONLY_LIST_UNIQUE" -eq 0 && "$E_ONLY_LIST_UNIQUE" -eq 0 ]]; then
  STATUS="PASS"
  EXIT_CODE=0
  REASON="count_and_list_parity_ok"
fi

if [[ "$STATUS" == "PASS" && "$G_DELTA" != "ERR" && "$E_DELTA" != "ERR" ]]; then
  if [[ "$G_DELTA" -gt 0 || "$E_DELTA" -gt 0 ]]; then
    if [[ "$G_LIST_EVENTS" -eq 0 && "$E_LIST_EVENTS" -eq 0 ]]; then
      STATUS="FAIL"
      EXIT_CODE=1
      REASON="list_window_empty_with_positive_delta"
    fi
  fi
fi

cat > "$OUT_DIR/summary.txt" <<EOF
status: $STATUS
reason: $REASON
start: $START_ISO
end: $END_ISO
duration_seconds: $DURATION_SECONDS
filters: ${FILTERS:-<none>}

counts:
  gnasty_start: $G_START
  gnasty_end: $G_END
  gnasty_delta: $G_DELTA
  elora_start: $E_START
  elora_end: $E_END
  elora_delta: $E_DELTA

stream_capture:
  gnasty_events: $G_STREAM_EVENTS
  elora_events: $E_STREAM_EVENTS
  gnasty_unique_keys: $G_UNIQUE
  elora_unique_keys: $E_UNIQUE
  both_unique_keys: $BOTH_UNIQUE
  gnasty_only_keys: $G_ONLY_UNIQUE
  elora_only_keys: $E_ONLY_UNIQUE

primary_parity:
  gnasty_list_events: $G_LIST_EVENTS
  elora_list_events: $E_LIST_EVENTS
  gnasty_list_unique_keys: $G_LIST_UNIQUE
  elora_list_unique_keys: $E_LIST_UNIQUE
  both_list_unique_keys: $BOTH_LIST_UNIQUE
  gnasty_only_list_keys: $G_ONLY_LIST_UNIQUE
  elora_only_list_keys: $E_ONLY_LIST_UNIQUE

errors:
  gnasty_poll_errors: $G_POLL_ERRORS
  elora_poll_errors: $E_POLL_ERRORS
  gnasty_stream_dropped: $G_STREAM_DROPPED
  elora_stream_dropped: $E_STREAM_DROPPED
EOF

jq -n \
  --arg status "$STATUS" \
  --arg reason "$REASON" \
  --arg start "$START_ISO" \
  --arg end "$END_ISO" \
  --arg filters "${FILTERS:-}" \
  --arg out_dir "$OUT_DIR" \
  --argjson duration_seconds "$DURATION_SECONDS" \
  --argjson gnasty_start "$G_START" \
  --argjson gnasty_end "${G_END/ERR/0}" \
  --arg gnasty_end_raw "$G_END" \
  --argjson gnasty_delta "${G_DELTA/ERR/0}" \
  --arg gnasty_delta_raw "$G_DELTA" \
  --argjson elora_start "$E_START" \
  --argjson elora_end "${E_END/ERR/0}" \
  --arg elora_end_raw "$E_END" \
  --argjson elora_delta "${E_DELTA/ERR/0}" \
  --arg elora_delta_raw "$E_DELTA" \
  --argjson gnasty_events "$G_STREAM_EVENTS" \
  --argjson elora_events "$E_STREAM_EVENTS" \
  --argjson gnasty_unique "$G_UNIQUE" \
  --argjson elora_unique "$E_UNIQUE" \
  --argjson both_unique "$BOTH_UNIQUE" \
  --argjson gnasty_only "$G_ONLY_UNIQUE" \
  --argjson elora_only "$E_ONLY_UNIQUE" \
  --argjson gnasty_list_events "$G_LIST_EVENTS" \
  --argjson elora_list_events "$E_LIST_EVENTS" \
  --argjson gnasty_list_unique "$G_LIST_UNIQUE" \
  --argjson elora_list_unique "$E_LIST_UNIQUE" \
  --argjson both_list_unique "$BOTH_LIST_UNIQUE" \
  --argjson gnasty_only_list "$G_ONLY_LIST_UNIQUE" \
  --argjson elora_only_list "$E_ONLY_LIST_UNIQUE" \
  --argjson gnasty_poll_errors "$G_POLL_ERRORS" \
  --argjson elora_poll_errors "$E_POLL_ERRORS" \
  --argjson gnasty_stream_dropped "$G_STREAM_DROPPED" \
  --argjson elora_stream_dropped "$E_STREAM_DROPPED" \
  '{
    status: $status,
    reason: $reason,
    start: $start,
    end: $end,
    duration_seconds: $duration_seconds,
    filters: ($filters | if . == "" then null else . end),
    out_dir: $out_dir,
    counts: {
      gnasty_start: $gnasty_start,
      gnasty_end: (if $gnasty_end_raw == "ERR" then null else $gnasty_end end),
      gnasty_delta: (if $gnasty_delta_raw == "ERR" then null else $gnasty_delta end),
      elora_start: $elora_start,
      elora_end: (if $elora_end_raw == "ERR" then null else $elora_end end),
      elora_delta: (if $elora_delta_raw == "ERR" then null else $elora_delta end)
    },
    stream_capture: {
      gnasty_events: $gnasty_events,
      elora_events: $elora_events,
      gnasty_unique_keys: $gnasty_unique,
      elora_unique_keys: $elora_unique,
      both_unique_keys: $both_unique,
      gnasty_only_keys: $gnasty_only,
      elora_only_keys: $elora_only
    },
    primary_parity: {
      gnasty_list_events: $gnasty_list_events,
      elora_list_events: $elora_list_events,
      gnasty_list_unique_keys: $gnasty_list_unique,
      elora_list_unique_keys: $elora_list_unique,
      both_list_unique_keys: $both_list_unique,
      gnasty_only_list_keys: $gnasty_only_list,
      elora_only_list_keys: $elora_only_list
    },
    errors: {
      gnasty_poll_errors: $gnasty_poll_errors,
      elora_poll_errors: $elora_poll_errors,
      gnasty_stream_dropped: ($gnasty_stream_dropped == 1),
      elora_stream_dropped: ($elora_stream_dropped == 1)
    },
    artifacts: {
      counts_log: "counts.log",
      gnasty_sse: "gnasty.sse",
      elora_sse: "elora.sse",
      gnasty_jsonl: "gnasty.jsonl",
      elora_jsonl: "elora.jsonl",
      gnasty_keys: "gnasty.keys",
      elora_keys: "elora.keys",
      gnasty_only_keys: "gnasty_only.keys",
      elora_only_keys: "elora_only.keys",
      gnasty_list_jsonl: "gnasty.list.jsonl",
      elora_list_jsonl: "elora.list.jsonl",
      gnasty_list_keys: "gnasty.list.keys",
      elora_list_keys: "elora.list.keys",
      gnasty_only_list_keys: "gnasty_only.list.keys",
      elora_only_list_keys: "elora_only.list.keys",
      gnasty_list_json: "gnasty.list.json",
      elora_list_json: "elora.list.json",
      summary_txt: "summary.txt"
    }
  }' > "$OUT_DIR/summary.json"

cat <<EOF

=== ALERT SOAK SUMMARY ===
status: $STATUS
reason: $REASON
start:  $START_ISO
end:    $END_ISO
dur:    ${DURATION_SECONDS}s

counts:
  gnasty: $G_START -> $G_END (delta $G_DELTA)
  elora : $E_START -> $E_END (delta $E_DELTA)

primary parity (count + list):
  list events captured  gnasty=$G_LIST_EVENTS elora=$E_LIST_EVENTS
  list unique keys      gnasty=$G_LIST_UNIQUE elora=$E_LIST_UNIQUE
  list overlap/mismatch both=$BOTH_LIST_UNIQUE gnasty_only=$G_ONLY_LIST_UNIQUE elora_only=$E_ONLY_LIST_UNIQUE

stream diagnostics:
  events captured      gnasty=$G_STREAM_EVENTS elora=$E_STREAM_EVENTS
  unique keys          gnasty=$G_UNIQUE elora=$E_UNIQUE
  overlap/mismatches   both=$BOTH_UNIQUE gnasty_only=$G_ONLY_UNIQUE elora_only=$E_ONLY_UNIQUE

errors:
  poll errors          gnasty=$G_POLL_ERRORS elora=$E_POLL_ERRORS
  stream dropped       gnasty=$G_STREAM_DROPPED elora=$E_STREAM_DROPPED

artifacts: $OUT_DIR
EOF

exit "$EXIT_CODE"

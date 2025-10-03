#!/usr/bin/env bash
# ===========================
# Elora Chat – Export Stress Suite
# ===========================
# Assumes:
# - API at http://localhost:8080
# - Container name elora-chat-instance
# - bash + coreutils + curl available
# - NDJSON is default export (no format param); CSV with format=csv
#
# What it does:
# - Creates /tmp/elora-stress/<run-id>
# - Hammers NDJSON and CSV with a matrix of limits / filters / invalids
# - Captures body, headers, and curl timing for each request
# - Emits a concise summary table at the end

set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
RUN_ID="run-$(date +%Y%m%d-%H%M%S)"
OUTDIR="/tmp/elora-stress/${RUN_ID}"
mkdir -p "${OUTDIR}"

echo "📦 Output dir: ${OUTDIR}"
echo "🌐 API: ${API_BASE}"

do_req () {
  local label="$1"
  local url="$2"
  local base="${OUTDIR}/${label}"
  local hdr="${base}.headers"
  local body="${base}.body"
  local meta="${base}.meta"

  /usr/bin/time -f "time_wall_real_s=%e\ntime_maxrss_kb=%M"     curl -sS -D "${hdr}" -o "${body}"       -w "http_code=%{http_code}\nsize_download=%{size_download}\ntime_total_s=%{time_total}\nspeed_download_Bps=%{speed_download}\n"       "${url}"       > "${meta}" 2>> "${meta}"

  {
    printf "bytes_on_disk=%s\n" "$(wc -c < "${body}" | awk '{print $1}')"
    printf "lines_on_disk=%s\n" "$(wc -l < "${body}" | awk '{print $1}')"
    if command -v file >/dev/null 2>&1; then
      printf "file_inspect=%s\n" "$(file -b "${body}")"
    fi
  } >> "${meta}"

  echo "✔ ${label}"
}

NOW_MS="$(($(date +%s)*1000))"
FUTURE_START_MS="$((NOW_MS + 7*24*60*60*1000))"
FUTURE_END_MS="$((FUTURE_START_MS + 60*1000))"

LIMITS_BIG="1000 5000 10000"
LIMITS_CSV="100 1000 5000"

for L in $LIMITS_BIG; do
  do_req "ndjson_limit_${L}"     "${API_BASE}/api/messages/export?limit=${L}"
done

do_req "ndjson_since_now"   "${API_BASE}/api/messages/export?since_ts=${NOW_MS}&limit=1000"
do_req "ndjson_before_now"   "${API_BASE}/api/messages/export?before_ts=${NOW_MS}&limit=1000"
do_req "ndjson_future_window"   "${API_BASE}/api/messages/export?since_ts=${FUTURE_START_MS}&before_ts=${FUTURE_END_MS}"
do_req "ndjson_invalid_negative_limit"   "${API_BASE}/api/messages/export?limit=-5"
do_req "ndjson_invalid_non_numeric_ts"   "${API_BASE}/api/messages/export?since_ts=notanumber"
do_req "ndjson_invalid_both_since_before"   "${API_BASE}/api/messages/export?since_ts=${NOW_MS}&before_ts=${NOW_MS}"
do_req "ndjson_oversized_limit_100000"   "${API_BASE}/api/messages/export?limit=100000"

for L in $LIMITS_CSV; do
  do_req "csv_limit_${L}"     "${API_BASE}/api/messages/export?format=csv&limit=${L}"
done

do_req "csv_future_window"   "${API_BASE}/api/messages/export?format=csv&since_ts=${FUTURE_START_MS}&before_ts=${FUTURE_END_MS}"
do_req "csv_invalid_negative_limit"   "${API_BASE}/api/messages/export?format=csv&limit=-10"

N=5
seq 1 $N | xargs -I{} -P $N bash -c   'curl -sS -o "'${OUTDIR}'/ndjson_concurrent_{}.body" -D "'${OUTDIR}'/ndjson_concurrent_{}.headers" -w "http_code=%{http_code}\nsize_download=%{size_download}\ntime_total_s=%{time_total}\n" "'${API_BASE}'/api/messages/export?limit=2000" > "'${OUTDIR}'/ndjson_concurrent_{}.meta"'

echo "🧾 Writing summary..."

{
  printf "label|http|bytes|lines|time_s|speed_Bps\n"
  for m in "${OUTDIR}"/*.meta; do
    label="$(basename "${m}" .meta)"
    code="$(grep -E '^http_code=' "${m}" | cut -d= -f2)"
    bytes="$(grep -E '^bytes_on_disk=' "${m}" | cut -d= -f2)"
    lines="$(grep -E '^lines_on_disk=' "${m}" | cut -d= -f2)"
    time_s="$(grep -E '^time_total_s=' "${m}" | cut -d= -f2)"
    speed="$(grep -E '^speed_download_Bps=' "${m}" | cut -d= -f2)"
    printf "%s|%s|%s|%s|%s|%s\n" "${label}" "${code:-?}" "${bytes:-?}" "${lines:-?}" "${time_s:-?}" "${speed:-?}"
  done | sort
} > "${OUTDIR}/SUMMARY.tsv"

column -t -s '|' "${OUTDIR}/SUMMARY.tsv" | sed '1s/.*/\x1b[4m&\x1b[0m/'

echo
echo "📍 Artifacts stored under: ${OUTDIR}"
echo "   - *.body (response bodies)"
echo "   - *.headers (response headers)"
echo "   - *.meta (curl + time metrics)"
echo "   - SUMMARY.tsv (tabular metrics)"
echo
echo "Tip: Inspect a header:"
echo "     sed -n '1,20p' ${OUTDIR}/ndjson_limit_1000.headers"

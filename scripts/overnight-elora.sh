#!/usr/bin/env bash
set -euo pipefail

STAMP=$(date +"%Y%m%d-%H%M%S")
LOGFILE=~/elora-overnight-$STAMP.log

cleanup() {
  echo ""
  echo "🔎 Overnight run ended. Summary from $LOGFILE:"
  echo "---------------------------------------------"
  echo "⚠️  Error lines (from logs):"
  grep -i "error\|fatal\|panic\|warn" "$LOGFILE" || echo "✅ No errors or warnings found"
  echo "---------------------------------------------"
  echo "📄 Full logs saved at: $LOGFILE"
  echo ""
  echo "🧪 Running morning stress test..."
  if [[ -x ./elora-stress.sh ]]; then
    ./elora-stress.sh
  elif [[ -x ../elora-stress.sh ]]; then
    ../elora-stress.sh
  else
    echo "⚠️ elora-stress.sh not found next to repo; skipping."
  fi
  echo ""
  echo "📊 Stress test summary:"
  ls -1dt /tmp/elora-stress/run-* 2>/dev/null | head -1 | xargs -r -I{} bash -lc 'column -t -s $'\''|'\''
 "{}"/SUMMARY.tsv | tail -n 20'
  echo "---------------------------------------------"
}
trap cleanup EXIT

cd "$(dirname "$0")/.."

echo "🔴 Stopping/removing old container..."
docker rm -f elora-chat-instance >/dev/null 2>&1 || true

echo "📦 Rebuilding image..."
docker build -t elora-chat .

echo "📂 Ensuring volume..."
docker volume create elora_sqlite_data >/dev/null 2>&1 || true

echo "🚀 Starting container..."
docker run --name elora-chat-instance \
  -p 8080:8080 \
  --env-file .env \
  -v elora_sqlite_data:/data \
  -d elora-chat

echo "⏳ Waiting for API..."
until curl -s http://localhost:8080/api/messages/export?limit=1 >/dev/null; do
  sleep 2
done
echo "✅ API is live at http://localhost:8080"

echo "📝 Tailing logs to $LOGFILE (Ctrl+C in the morning to stop and trigger stress test)..."
exec docker logs -f elora-chat-instance | tee "$LOGFILE"

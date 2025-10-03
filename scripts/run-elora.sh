#!/usr/bin/env bash
set -euo pipefail

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

echo "🧪 Running stress test..."
# Assumes elora-stress.sh sits in repo root or sibling dev folder; adjust if needed.
if [[ -x ./elora-stress.sh ]]; then
  ./elora-stress.sh
elif [[ -x ../elora-stress.sh ]]; then
  ../elora-stress.sh
else
  echo "⚠️ elora-stress.sh not found next to repo; skipping."
fi

echo "📊 Latest summary (if any):"
ls -1dt /tmp/elora-stress/run-* 2>/dev/null | head -1 | xargs -r -I{} bash -lc 'column -t -s $'\''|'\''
 "{}"/SUMMARY.tsv | tail -n 20'

echo "📜 Following logs (Ctrl+C to detach)..."
docker logs -f elora-chat-instance | tee ~/elora-overnight.log

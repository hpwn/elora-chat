#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
COMPOSE_FILES=(-f "${ROOT_DIR}/docker-compose.yml" -f "${ROOT_DIR}/docker-compose.dev.yml")

log() {
  printf '[compose-fix] %s\n' "$*"
}

if [[ "$(uname -s)" != "Linux" ]]; then
  log "This script targets Ubuntu/Linux hosts only."
  exit 1
fi

if ! command -v sudo >/dev/null 2>&1; then
  log "sudo is required."
  exit 1
fi

log "Collecting pre-change diagnostics"
docker --version | tee /tmp/elora-docker-version.before.log
docker compose version | tee /tmp/elora-docker-compose-version.before.log
which docker | tee /tmp/elora-which-docker.before.log
docker info | sed -n '1,60p' | tee /tmp/elora-docker-info.before.log || true
docker compose "${COMPOSE_FILES[@]}" config >/tmp/elora-compose-config.before.yaml
if ! docker compose "${COMPOSE_FILES[@]}" ps >/tmp/elora-compose-ps.before.out 2>/tmp/elora-compose-ps.before.log; then
  log "compose ps failed before upgrade (captured at /tmp/elora-compose-ps.before.log)"
fi

log "Removing Docker Desktop package if present"
sudo apt-get remove -y docker-desktop || true

log "Removing conflicting distro packages if present"
for pkg in docker.io docker-doc docker-compose podman-docker containerd runc; do
  sudo apt-get remove -y "$pkg" || true
done

log "Installing Docker apt prerequisites"
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg

if [[ -f /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
else
  log "/etc/os-release missing; cannot determine Ubuntu codename"
  exit 1
fi

echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null

log "Installing Docker Engine + Compose plugin"
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

log "Restarting and enabling docker daemon"
sudo systemctl enable docker
sudo systemctl restart docker

if ! groups "$USER" | grep -q '\bdocker\b'; then
  log "Adding ${USER} to docker group"
  sudo usermod -aG docker "$USER"
  log "Open a new shell (or run: newgrp docker) before next docker command."
fi

log "Collecting post-change diagnostics"
docker --version | tee /tmp/elora-docker-version.after.log
docker compose version | tee /tmp/elora-docker-compose-version.after.log
docker compose "${COMPOSE_FILES[@]}" config >/tmp/elora-compose-config.after.yaml
docker compose "${COMPOSE_FILES[@]}" ps >/tmp/elora-compose-ps.after.out 2>/tmp/elora-compose-ps.after.log
docker compose "${COMPOSE_FILES[@]}" logs --tail=50 elora-chat >/tmp/elora-compose-logs.after.log 2>&1 || true

log "Re-running elora workflow"
bash "${ROOT_DIR}/scripts/devctl.sh" build --dev --app all
bash "${ROOT_DIR}/scripts/devctl.sh" restart --dev
make -C "${ROOT_DIR}" health
bash "${ROOT_DIR}/scripts/devctl.sh" test --app all
(
  cd "${ROOT_DIR}/src/backend"
  go test ./routes
  go test ./...
)

log "Done. Diagnostics in /tmp/elora-*.{before,after}.*"

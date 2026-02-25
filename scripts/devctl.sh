#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)
GNASTY_ROOT_DEFAULT=$(cd "${PROJECT_ROOT}/.." && pwd)/gnasty-chat

COMPOSE_BASE=(docker compose -f "${PROJECT_ROOT}/docker-compose.yml")
DEV_OVERRIDE="${PROJECT_ROOT}/docker-compose.dev.yml"

usage() {
  cat <<'EOF'
Usage: ./scripts/devctl.sh <command> [options]

Commands:
  build      Build elora and/or gnasty images
  start      Start compose services (optional env overrides)
  restart    Rebuild + force recreate services
  down       Stop and remove compose services
  logs       Tail compose logs
  ws         Follow websocket frames (all|twitch|youtube)
  test       Run full test suites (elora and/or gnasty)
  help       Show this help

Global options:
  --dev                  Use docker-compose.dev.yml (build gnasty from ../gnasty-chat)

build options:
  --app <all|elora|gnasty>    Which app(s) to build (default: all)
  --no-cache                  Build without cache
  --gnasty-root <path>         Override gnasty repo path (default: ../gnasty-chat)

start/restart options:
  --twitch-channel <login>    Override TWITCH_CHANNEL for this run
  --youtube-url <url>         Override YOUTUBE_URL for this run
  --twitch-nick <nick>        Override TWITCH_NICK for this run
  --seed-enabled              Enable /api/dev/seed routes for this run only
  --service <name>            Limit to one compose service (elora-chat|gnasty-harvester)
  --follow                    Tail logs after startup

logs options:
  --service <name>            Limit logs to one service

ws options:
  --platform <all|twitch|youtube>   Filter websocket output (default: all)
                                   Uses containerized websocat+python directly (no make dependency)

test options:
  --app <all|elora|gnasty>    Which suites to run (default: all)
  --gnasty-root <path>        Override gnasty repo path (default: ../gnasty-chat)

Examples:
  ./scripts/devctl.sh build --app all --dev
  ./scripts/devctl.sh start --dev --twitch-channel rifftrax --twitch-nick hp_az --follow
  ./scripts/devctl.sh restart --dev --seed-enabled --follow
  ./scripts/devctl.sh ws --platform twitch
  ./scripts/devctl.sh test --app all
EOF
}

log() {
  printf '[devctl] %s\n' "$*"
}

die() {
  printf '[devctl] ERROR: %s\n' "$*" >&2
  exit 1
}

ensure_env_file() {
  if [[ ! -f "${PROJECT_ROOT}/.env" ]]; then
    if [[ -f "${PROJECT_ROOT}/.env.example" ]]; then
      cp "${PROJECT_ROOT}/.env.example" "${PROJECT_ROOT}/.env"
      log "created .env from .env.example"
    else
      die ".env missing and .env.example not found"
    fi
  fi
}

compose_cmd() {
  local use_dev="$1"
  shift
  local -a cmd=("${COMPOSE_BASE[@]}")
  if [[ "${use_dev}" == "1" ]]; then
    cmd+=( -f "${DEV_OVERRIDE}" )
  fi
  "${cmd[@]}" "$@"
}

gnasty_image_tag() {
  local from_env=""
  if [[ -f "${PROJECT_ROOT}/.env" ]]; then
    from_env=$(sed -n 's/^GNASTY_IMAGE=//p' "${PROJECT_ROOT}/.env" | head -n 1 | sed 's/[[:space:]]*$//')
  fi
  if [[ -n "${GNASTY_IMAGE:-}" ]]; then
    printf '%s\n' "${GNASTY_IMAGE}"
  elif [[ -n "${from_env}" ]]; then
    printf '%s\n' "${from_env}"
  else
    printf 'gnasty-chat:latest\n'
  fi
}

read_env_file_value() {
  local key="$1"
  if [[ -f "${PROJECT_ROOT}/.env" ]]; then
    sed -n "s/^${key}=//p" "${PROJECT_ROOT}/.env" | head -n 1 | sed 's/[[:space:]]*$//'
  fi
}

build_cmd() {
  local use_dev="$1"
  shift
  local app="all"
  local no_cache="0"
  local gnasty_root="$GNASTY_ROOT_DEFAULT"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --app)
        app="${2:-}"
        shift 2
        ;;
      --no-cache)
        no_cache="1"
        shift
        ;;
      --gnasty-root)
        gnasty_root="${2:-}"
        shift 2
        ;;
      *)
        die "unknown build option: $1"
        ;;
    esac
  done

  ensure_env_file

  local -a build_flags=()
  if [[ "${no_cache}" == "1" ]]; then
    build_flags+=(--no-cache)
  fi

  case "${app}" in
    all|elora)
      log "building elora-chat image"
      compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" build "${build_flags[@]}" elora-chat
      ;;
  esac

  case "${app}" in
    all|gnasty)
      if [[ "${use_dev}" == "1" ]]; then
        log "building gnasty-harvester via compose override"
        compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" build "${build_flags[@]}" gnasty-harvester
      else
        [[ -d "${gnasty_root}" ]] || die "gnasty repo not found at ${gnasty_root}"
        local tag
        tag=$(gnasty_image_tag)
        log "building gnasty image ${tag} from ${gnasty_root}"
        docker build "${build_flags[@]}" -t "${tag}" "${gnasty_root}"
      fi
      ;;
  esac

  case "${app}" in
    all|elora|gnasty) ;;
    *) die "invalid --app value: ${app}" ;;
  esac
}

compose_up() {
  local use_dev="$1"
  local rebuild="$2"
  shift 2

  local twitch_channel=""
  local youtube_url=""
  local twitch_nick=""
  local seed_enabled="0"
  local service=""
  local follow="0"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --twitch-channel)
        twitch_channel="${2:-}"
        shift 2
        ;;
      --youtube-url)
        youtube_url="${2:-}"
        shift 2
        ;;
      --twitch-nick)
        twitch_nick="${2:-}"
        shift 2
        ;;
      --seed-enabled)
        seed_enabled="1"
        shift
        ;;
      --service)
        service="${2:-}"
        shift 2
        ;;
      --follow)
        follow="1"
        shift
        ;;
      --gnasty-root)
        shift 2
        ;;
      *)
        die "unknown start/restart option: $1"
        ;;
    esac
  done

  ensure_env_file

  local -a env_prefix=()
  [[ -n "${twitch_channel}" ]] && env_prefix+=("TWITCH_CHANNEL=${twitch_channel}")
  [[ -n "${youtube_url}" ]] && env_prefix+=("YOUTUBE_URL=${youtube_url}")
  [[ -n "${twitch_nick}" ]] && env_prefix+=("TWITCH_NICK=${twitch_nick}")
  [[ "${seed_enabled}" == "1" ]] && env_prefix+=("ELORA_DEV_SEED_ENABLED=true")

  local -a up_args=(up -d --remove-orphans)
  if [[ "${rebuild}" == "1" ]]; then
    up_args+=(--build --force-recreate)
  fi
  [[ -n "${service}" ]] && up_args+=("${service}")

  if [[ ${#env_prefix[@]} -gt 0 ]]; then
    log "starting with one-run overrides: ${env_prefix[*]}"
    (
      export "${env_prefix[@]}"
      compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" "${up_args[@]}"
    )
  else
    compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" "${up_args[@]}"
  fi

  compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" ps

  if [[ "${follow}" == "1" ]]; then
    if [[ -n "${service}" ]]; then
      compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" logs -f "${service}"
    else
      compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" logs -f
    fi
  fi
}

down_cmd() {
  local use_dev="$1"
  compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" down --remove-orphans
}

logs_cmd() {
  local use_dev="$1"
  shift
  local service=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --service)
        service="${2:-}"
        shift 2
        ;;
      --gnasty-root)
        shift 2
        ;;
      *)
        die "unknown logs option: $1"
        ;;
    esac
  done

  if [[ -n "${service}" ]]; then
    compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" logs -f "${service}"
  else
    compose_cmd "${use_dev}" --project-directory "${PROJECT_ROOT}" logs -f
  fi
}

ws_cmd() {
  shift
  local platform="all"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --platform)
        platform="${2:-}"
        shift 2
        ;;
      --dev)
        shift
        ;;
      --gnasty-root)
        shift 2
        ;;
      *)
        die "unknown ws option: $1"
        ;;
    esac
  done

  ensure_env_file

  local ws_url="${VITE_PUBLIC_WS_URL:-}"
  if [[ -z "${ws_url}" ]]; then
    ws_url=$(read_env_file_value "VITE_PUBLIC_WS_URL")
  fi
  if [[ -z "${ws_url}" ]]; then
    local http_port="${ELORA_HTTP_PORT:-}"
    if [[ -z "${http_port}" ]]; then
      http_port=$(read_env_file_value "ELORA_HTTP_PORT")
    fi
    [[ -z "${http_port}" ]] && http_port="8080"
    ws_url="ws://localhost:${http_port}/ws/chat"
  fi

  local docker_network="${ELORA_DOCKER_NETWORK:-}"
  if [[ -z "${docker_network}" ]]; then
    docker_network=$(read_env_file_value "ELORA_DOCKER_NETWORK")
  fi
  [[ -z "${docker_network}" ]] && docker_network="elora-network"

  # containerized websocat cannot reach host localhost, so target the compose service.
  ws_url="${ws_url/ws:\/\/localhost/ws:\/\/elora-chat}"
  ws_url="${ws_url/ws:\/\/127.0.0.1/ws:\/\/elora-chat}"

  local ws_filter_script="${PROJECT_ROOT}/scripts/ws_filter.py"
  [[ -f "${ws_filter_script}" ]] || die "missing ws filter script at ${ws_filter_script}"

  local platform_filter=""
  case "${platform}" in
    all)
      platform_filter=""
      ;;
    twitch)
      platform_filter="Twitch"
      ;;
    youtube)
      platform_filter="YouTube"
      ;;
    *)
      die "invalid --platform value: ${platform}"
      ;;
  esac

  log "connecting websocket stream ${ws_url} on docker network ${docker_network} (platform=${platform})"
  docker run --rm -i --network "${docker_network}" ghcr.io/vi/websocat:1.12.0 -E --ping-interval=20 "${ws_url}" | \
    docker run --rm -i -e "PLATFORM=${platform_filter}" -v "${ws_filter_script}:/ws_filter.py:ro" python:3.11-slim python -u /ws_filter.py
}

test_cmd() {
  shift
  local app="all"
  local gnasty_root="$GNASTY_ROOT_DEFAULT"

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --app)
        app="${2:-}"
        shift 2
        ;;
      --gnasty-root)
        gnasty_root="${2:-}"
        shift 2
        ;;
      --dev)
        shift
        ;;
      *)
        die "unknown test option: $1"
        ;;
    esac
  done

  local -a go_env_prefix=()
  if [[ "${GOTOOLCHAIN:-}" == "local" ]]; then
    log "detected GOTOOLCHAIN=local; unsetting for go test so required toolchains can auto-resolve"
    go_env_prefix=("GOTOOLCHAIN=")
  fi

  case "${app}" in
    all|elora)
      log "running elora backend tests"
      (cd "${PROJECT_ROOT}/src/backend" && env "${go_env_prefix[@]}" go test ./...)

      log "running elora frontend tests"
      (cd "${PROJECT_ROOT}/src/frontend" && npm test)
      ;;
  esac

  case "${app}" in
    all|gnasty)
      [[ -d "${gnasty_root}" ]] || die "gnasty repo not found at ${gnasty_root}"
      log "running gnasty go test ./..."
      (cd "${gnasty_root}" && env "${go_env_prefix[@]}" go test ./...)
      ;;
  esac

  case "${app}" in
    all|elora|gnasty) ;;
    *) die "invalid --app value: ${app}" ;;
  esac
}

main() {
  [[ $# -ge 1 ]] || { usage; exit 1; }

  local cmd="$1"
  shift

  local use_dev="0"
  local -a rest=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dev)
        use_dev="1"
        shift
        ;;
      *)
        rest+=("$1")
        shift
        ;;
    esac
  done

  case "${cmd}" in
    help|-h|--help)
      usage
      ;;
    build)
      build_cmd "${use_dev}" "${rest[@]}"
      ;;
    start)
      compose_up "${use_dev}" "0" "${rest[@]}"
      ;;
    restart)
      compose_up "${use_dev}" "1" "${rest[@]}"
      ;;
    down)
      down_cmd "${use_dev}"
      ;;
    logs)
      logs_cmd "${use_dev}" "${rest[@]}"
      ;;
    ws)
      ws_cmd "${cmd}" "${rest[@]}"
      ;;
    test)
      test_cmd "${cmd}" "${rest[@]}"
      ;;
    *)
      die "unknown command: ${cmd}"
      ;;
  esac
}

main "$@"

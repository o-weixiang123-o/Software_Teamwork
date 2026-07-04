#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONFIG_LOADER="$ROOT_DIR/scripts/config/load-profile.sh"
RUN_DIR="$ROOT_DIR/.local/run"
LOG_DIR="$ROOT_DIR/.local/logs"
CURRENT_STEP="initializing"
CHINA_MIRRORS=0

OFFICIAL_GOPROXY="https://proxy.golang.org,direct"
CHINA_GOPROXY="https://goproxy.cn,direct"
OFFICIAL_GOSUMDB="sum.golang.org"
CHINA_GOSUMDB="sum.golang.google.cn"

COLOR_RESET=""
COLOR_BLUE=""
COLOR_GREEN=""
COLOR_YELLOW=""
COLOR_RED=""
COLOR_CYAN=""
if [[ -z "${NO_COLOR:-}" && ( -t 1 || "${FORCE_COLOR:-0}" == "1" || "${CLICOLOR_FORCE:-0}" == "1" ) ]]; then
  COLOR_RESET=$'\033[0m'
  COLOR_BLUE=$'\033[1;34m'
  COLOR_GREEN=$'\033[1;32m'
  COLOR_YELLOW=$'\033[1;33m'
  COLOR_RED=$'\033[1;31m'
  COLOR_CYAN=$'\033[1;36m'
fi

log_info() {
  printf '%b%s %s%b\n' "$COLOR_BLUE" "[backend]" "$*" "$COLOR_RESET"
}

log_success() {
  printf '%b%s %s%b\n' "$COLOR_GREEN" "[ok]" "$*" "$COLOR_RESET"
}

log_warn() {
  printf '%b%s %s%b\n' "$COLOR_YELLOW" "[warn]" "$*" "$COLOR_RESET" >&2
}

log_error() {
  printf '%b%s %s%b\n' "$COLOR_RED" "[fail]" "$*" "$COLOR_RESET" >&2
}

log_hint() {
  printf '%b%s %s%b\n' "$COLOR_CYAN" "[hint]" "$*" "$COLOR_RESET" >&2
}

GO_SERVICES=(
  "auth|$ROOT_DIR/services/auth|./cmd/server"
  "file|$ROOT_DIR/services/file|./cmd/server"
  "knowledge|$ROOT_DIR/services/knowledge|./cmd/adapter"
  "ai-gateway|$ROOT_DIR/services/ai-gateway|./cmd/server"
  "qa|$ROOT_DIR/services/qa|./cmd/server"
  "document|$ROOT_DIR/services/document|./cmd/server"
  "gateway|$ROOT_DIR/services/gateway|./cmd/server"
)
STARTED_SERVICES=()

usage() {
  cat <<'EOF'
Usage: ./scripts/local/run-backend.sh [--china]

Checks Go modules and starts all host-run backend services.

Options:
  --china   Use mainland China Go proxy/checksum mirrors for this run only.
  -h, --help
            Show this help.
EOF
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --china)
        CHINA_MIRRORS=1
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        echo "unknown argument: $1" >&2
        usage >&2
        exit 2
        ;;
    esac
    shift
  done
}

on_exit() {
  status=$?
  if (( status == 0 )); then
    log_success "completed successfully"
  else
    log_error "failed during ${CURRENT_STEP} (exit ${status})"
    log_hint "Check service logs under .local/logs/ and process files under .local/run/."
  fi
}
parse_args "$@"

trap on_exit EXIT

log_info "starting Go module checks and host services"

if ! command -v setsid >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
  log_error "setsid or python3 is required to manage host-run service process groups"
  exit 1
fi

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
# shellcheck disable=SC1090
. "$CONFIG_LOADER"

if (( CHINA_MIRRORS )); then
  export GOPROXY="$CHINA_GOPROXY"
  export GOSUMDB="$CHINA_GOSUMDB"
  log_info "using mainland China Go mirrors for this run (--china); profile files and .env.local are not modified"
else
  export GOPROXY="${GOPROXY:-$OFFICIAL_GOPROXY}"
  export GOSUMDB="${GOSUMDB:-$OFFICIAL_GOSUMDB}"
  case "${GOPROXY:-}" in
    *goproxy.cn*)
      log_warn "local profile output contains mainland China Go mirror values while --china was not passed."
      log_warn "continuing with user configuration; use official GOPROXY/GOSUMDB for default mode or rerun with --china."
      ;;
  esac
  if [[ "${GOSUMDB:-}" == "sum.golang.google.cn" ]]; then
    log_warn "local profile output contains mainland China GOSUMDB while --china was not passed."
  fi
fi

if [[ -n "${AI_GATEWAY_LOCAL_CHAT_MODEL:-}" && ( -z "${MODEL_ID:-}" || "${MODEL_ID:-}" == "local-placeholder-chat" ) ]]; then
  export MODEL_ID="$AI_GATEWAY_LOCAL_CHAT_MODEL"
  log_info "profile kept MODEL_ID=local-placeholder-chat; using AI_GATEWAY_LOCAL_CHAT_MODEL for host-run QA: $MODEL_ID"
fi
if [[ -n "${AI_GATEWAY_LOCAL_CHAT_MODEL:-}" && ( -z "${DOCUMENT_AI_GATEWAY_MODEL:-}" || "${DOCUMENT_AI_GATEWAY_MODEL:-}" == "local-placeholder-chat" ) ]]; then
  export DOCUMENT_AI_GATEWAY_MODEL="$AI_GATEWAY_LOCAL_CHAT_MODEL"
  log_info "profile kept DOCUMENT_AI_GATEWAY_MODEL=local-placeholder-chat; using AI_GATEWAY_LOCAL_CHAT_MODEL for host-run Document: $DOCUMENT_AI_GATEWAY_MODEL"
fi

mkdir -p "$RUN_DIR" "$LOG_DIR"

configure_app_version_current_sha() {
  if [[ -n "${GATEWAY_APP_VERSION_CURRENT_SHA:-}" ]]; then
    return
  fi
  if ! command -v git >/dev/null 2>&1; then
    log_warn "git is not available; Gateway app-version freshness will require GATEWAY_APP_VERSION_CURRENT_SHA or GATEWAY_APP_VERSION_ALLOWED_SHAS."
    return
  fi

  local sha
  sha="$(git -C "$ROOT_DIR" rev-parse HEAD 2>/dev/null || true)"
  if [[ "$sha" =~ ^[0-9a-fA-F]{40}$ ]]; then
    export GATEWAY_APP_VERSION_CURRENT_SHA="${sha,,}"
    log_info "using repository HEAD for Gateway app-version freshness: ${GATEWAY_APP_VERSION_CURRENT_SHA:0:8}"
    return
  fi

  log_warn "could not determine a 40-character repository HEAD SHA; Gateway app-version freshness will return unknown unless trusted SHAs are configured."
}

print_go_module_hint() {
  log_error "Go module download failed before backend startup completed."
  log_hint "Current effective Go module settings:"
  log_hint "  GOPROXY=${GOPROXY:-<unset>}"
  log_hint "  GOSUMDB=${GOSUMDB:-<unset>}"
  log_hint "Mainland China mirrors: ./scripts/local/run-backend.sh --china"
  log_hint "Official defaults: GOPROXY=https://proxy.golang.org,direct and GOSUMDB=sum.golang.org"
}

print_startup_failure_hint() {
  log_error "Backend process startup failed after services were forked."
  log_hint "Use the log tails above first."
  log_hint "If a log shows proxy.golang.org, sum.golang.org, i/o timeout, or go: downloading, check:"
  log_hint "  GOPROXY=${GOPROXY:-<unset>}"
  log_hint "  GOSUMDB=${GOSUMDB:-<unset>}"
  log_hint "For port binding, database, Redis, token, or runtime dependency errors, follow the specific service log instead of treating it as a Go module mirror issue."
}

check_go_module_settings() {
  local default_goproxy="$OFFICIAL_GOPROXY"
  local default_gosumdb="$OFFICIAL_GOSUMDB"
  if (( CHINA_MIRRORS )); then
    default_goproxy="$CHINA_GOPROXY"
    default_gosumdb="$CHINA_GOSUMDB"
  fi

  effective_goproxy="${GOPROXY:-$(go env GOPROXY 2>/dev/null || true)}"
  effective_gosumdb="${GOSUMDB:-$(go env GOSUMDB 2>/dev/null || true)}"

  if [[ -z "${GOPROXY:-}" && ( -z "$effective_goproxy" || "$effective_goproxy" == *"proxy.golang.org"* ) ]]; then
    export GOPROXY="$default_goproxy"
    log_info "profile did not set GOPROXY; using selected default for this run: $GOPROXY"
  elif [[ -z "${GOPROXY:-}" ]]; then
    export GOPROXY="$effective_goproxy"
    log_info "profile did not set GOPROXY; using global go env value: $GOPROXY"
  fi

  if [[ -z "${GOSUMDB:-}" && ( -z "$effective_gosumdb" || "$effective_gosumdb" == "sum.golang.org" ) ]]; then
    export GOSUMDB="$default_gosumdb"
    log_info "profile did not set GOSUMDB; using selected default for this run: $GOSUMDB"
  elif [[ -z "${GOSUMDB:-}" ]]; then
    export GOSUMDB="$effective_gosumdb"
    log_info "profile did not set GOSUMDB; using global go env value: $GOSUMDB"
  fi

  if [[ "$GOPROXY" == *"proxy.golang.org"* && "$CHINA_MIRRORS" == "0" ]]; then
    log_warn "GOPROXY includes proxy.golang.org; use --china on mainland China networks"
    log_warn "current GOPROXY=$GOPROXY"
  fi
}

check_go_modules() {
  CURRENT_STEP="checking Go module downloads"
  if ! command -v go >/dev/null 2>&1; then
    log_error "go is required for host-run backend services"
    exit 1
  fi

  check_go_module_settings

  for service in "${GO_SERVICES[@]}"; do
    IFS='|' read -r name dir _go_target <<<"$service"
    CURRENT_STEP="checking Go modules for $name"
    log_info "checking Go modules for $name"
    if ! run_go_mod_download "$dir"; then
      log_error "failed to download Go modules for $name"
      print_go_module_hint
      return 1
    fi
  done
}

run_go_mod_download() {
  local dir="$1"
  local timeout_seconds="${LOCAL_GO_MOD_DOWNLOAD_TIMEOUT_SECONDS:-180}"
  local status

  if command -v timeout >/dev/null 2>&1 && [[ "$timeout_seconds" =~ ^[0-9]+$ ]] && (( timeout_seconds > 0 )); then
    set +e
    (cd "$dir" && timeout "$timeout_seconds" go mod download)
    status=$?
    set -e
    if (( status == 124 )); then
      log_error "go mod download timed out after ${timeout_seconds}s in $dir"
    fi
    return "$status"
  fi

  (cd "$dir" && go mod download)
}

launch_process_group() {
  local dir="$1"
  shift
  cd "$dir"
  if command -v setsid >/dev/null 2>&1; then
    exec setsid "$@"
  fi
  exec python3 -c 'import os, sys; os.setsid(); os.execvp(sys.argv[1], sys.argv[1:])' "$@"
}

service_group_alive() {
  local pid_file="$1"
  [[ -f "$pid_file" ]] || return 1
  local pid
  pid="$(cat "$pid_file")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 -- "-$pid" 2>/dev/null
}

start() {
  name="$1"
  dir="$2"
  shift 2
  CURRENT_STEP="starting $name"

  if [[ "$name" == "knowledge" ]] && service_group_alive "$RUN_DIR/knowledge-adapter.pid"; then
    echo "knowledge adapter already running from run-knowledge-parse-stack.sh; reusing it"
    return
  fi

  if [[ -f "$RUN_DIR/$name.pid" ]]; then
    if service_group_alive "$RUN_DIR/$name.pid"; then
      log_success "$name already running"
      return
    fi
  fi
  rm -f "$RUN_DIR/$name.pid"

  log_info "starting $name"
  launch_process_group "$dir" "$@" >"$LOG_DIR/$name.log" 2>&1 &
  echo "$!" >"$RUN_DIR/$name.pid"
  STARTED_SERVICES+=("$name")
}

check_started_services() {
  CURRENT_STEP="checking backend processes"
  local wait_seconds="${LOCAL_BACKEND_STARTUP_CHECK_SECONDS:-8}"
  local failed=()

  if [[ "$wait_seconds" =~ ^[0-9]+$ ]] && (( wait_seconds > 0 )) && (( ${#STARTED_SERVICES[@]} > 0 )); then
    log_info "checking backend processes for ${wait_seconds}s"
    sleep "$wait_seconds"
  fi

  for name in "${STARTED_SERVICES[@]}"; do
    pid_file="$RUN_DIR/$name.pid"
    if [[ ! -f "$pid_file" ]]; then
      failed+=("$name")
      continue
    fi

    pid="$(cat "$pid_file")"
    if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 -- "-$pid" 2>/dev/null; then
      continue
    fi

    failed+=("$name")
  done

  if (( ${#failed[@]} == 0 )); then
    return 0
  fi

  log_error "backend startup failed for: ${failed[*]}"
  log_error "The failed service log tails are shown below."
  for name in "${failed[@]}"; do
    log_file="$LOG_DIR/$name.log"
    printf '%b%s%b\n' "$COLOR_YELLOW" "----- $log_file (tail) -----" "$COLOR_RESET" >&2
    if [[ -f "$log_file" ]]; then
      tail -n 40 "$log_file" >&2
    else
      log_warn "log file missing"
    fi
  done
  print_startup_failure_hint
  return 1
}

configure_app_version_current_sha
check_go_modules

for service in "${GO_SERVICES[@]}"; do
  IFS='|' read -r name dir go_target <<<"$service"
  start "$name" "$dir" go run "$go_target"
done

check_started_services

log_success "backend started; logs: .local/logs/*.log"

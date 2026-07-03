#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${KNOWLEDGE_ENV_FILE:-$ROOT_DIR/deploy/.env}"
RUN_DIR="$ROOT_DIR/.local/run"
LOG_DIR="$ROOT_DIR/.local/logs"
RUNTIME_DIR="$ROOT_DIR/services/knowledge-runtime"
ADAPTER_DIR="$ROOT_DIR/services/knowledge"
CURRENT_STEP="initializing"
STARTED_SERVICES=()
RUNTIME_MODE="host"

on_exit() {
  status=$?
  if (( status == 0 )); then
    echo "knowledge parse stack startup: completed successfully"
  else
    echo "knowledge parse stack startup: failed during ${CURRENT_STEP} (exit ${status})" >&2
    echo "Check logs under .local/logs/ and pid files under .local/run/." >&2
  fi
}
trap on_exit EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required for the host-run Knowledge parse stack" >&2
    return 1
  fi
}

append_no_proxy() {
  local item="$1"
  local current="${NO_PROXY:-${no_proxy:-}}"
  [[ -n "${item// }" ]] || return 0
  case ",$current," in
    *",$item,"*) ;;
    *)
      if [[ -z "$current" ]]; then
        current="$item"
      else
        current="$current,$item"
      fi
      ;;
  esac
  export NO_PROXY="$current"
  export no_proxy="$current"
}

url_host() {
  local url="$1"
  local rest host_port host
  rest="${url#*://}"
  host_port="${rest%%/*}"
  if [[ "$host_port" == \[*\]* ]]; then
    host="${host_port#\[}"
    host="${host%%\]*}"
  else
    host="${host_port%%:*}"
  fi
  printf '%s\n' "$host"
}

is_loopback_host() {
  case "$1" in
    ""|"localhost"|"127.0.0.1"|"::1")
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

append_no_proxy_for_url() {
  local url="$1"
  local host
  host="$(url_host "$url")"
  append_no_proxy "$host"
}

normalize_http_url() {
  local value="$1"
  if [[ "$value" != http://* && "$value" != https://* ]]; then
    value="http://$value"
  fi
  printf '%s\n' "${value%/}"
}

print_required_env_hint() {
  cat >&2 <<EOF
Required local Knowledge parse stack settings are missing.

Start from the tracked defaults, then add private provider credentials:
  cp deploy/.env.example deploy/.env

Required for this script:
  INTERNAL_SERVICE_TOKEN or KNOWLEDGE_SERVICE_TOKEN
  VENDOR_RUNTIME_SERVICE_TOKEN or KNOWLEDGE_RUNTIME_SERVICE_TOKEN
  DOC_ENGINE=elasticsearch with a reachable Elasticsearch at conf/service_conf.yaml es.hosts
  KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY
  KNOWLEDGE_RUNTIME_EMBEDDING_MODEL
  KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL
  KNOWLEDGE_RUNTIME_MODEL_API_KEY, unless using a trusted local keyless provider
EOF
}

require_env() {
  local missing=()
  local adapter_token="${KNOWLEDGE_SERVICE_TOKEN:-${INTERNAL_SERVICE_TOKEN:-}}"
  local runtime_token="${VENDOR_RUNTIME_SERVICE_TOKEN:-${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}}"
  local configured_runtime_url

  if [[ -z "${adapter_token// }" ]]; then
    echo "INTERNAL_SERVICE_TOKEN/KNOWLEDGE_SERVICE_TOKEN missing; using local development default for scripts/local"
    export INTERNAL_SERVICE_TOKEN="local-dev-internal-service-token-change-me"
    adapter_token="$INTERNAL_SERVICE_TOKEN"
  fi
  if [[ -z "${runtime_token// }" ]]; then
    echo "VENDOR_RUNTIME_SERVICE_TOKEN/KNOWLEDGE_RUNTIME_SERVICE_TOKEN missing; using local development default for scripts/local"
    export VENDOR_RUNTIME_SERVICE_TOKEN="local-dev-runtime-service-token-change-me"
    runtime_token="$VENDOR_RUNTIME_SERVICE_TOKEN"
  fi

  [[ -n "${adapter_token// }" ]] || missing+=("INTERNAL_SERVICE_TOKEN or KNOWLEDGE_SERVICE_TOKEN")
  [[ -n "${runtime_token// }" ]] || missing+=("VENDOR_RUNTIME_SERVICE_TOKEN or KNOWLEDGE_RUNTIME_SERVICE_TOKEN")

  if (( ${#missing[@]} > 0 )); then
    printf 'missing required env: %s\n' "${missing[*]}" >&2
    print_required_env_hint
    return 1
  fi

  if [[ -z "${KNOWLEDGE_SERVICE_TOKEN:-}" && -n "${INTERNAL_SERVICE_TOKEN:-}" ]]; then
    export KNOWLEDGE_SERVICE_TOKEN="$INTERNAL_SERVICE_TOKEN"
  fi
  if [[ -z "${VENDOR_RUNTIME_SERVICE_TOKEN:-}" && -n "${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}" ]]; then
    export VENDOR_RUNTIME_SERVICE_TOKEN="$KNOWLEDGE_RUNTIME_SERVICE_TOKEN"
  fi
  if [[ -z "${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}" && -n "${VENDOR_RUNTIME_SERVICE_TOKEN:-}" ]]; then
    export KNOWLEDGE_RUNTIME_SERVICE_TOKEN="$VENDOR_RUNTIME_SERVICE_TOKEN"
  fi

  configured_runtime_url="${KNOWLEDGE_PARSE_VENDOR_RUNTIME_URL:-${VENDOR_RUNTIME_URL:-http://127.0.0.1:9380}}"
  configured_runtime_url="$(normalize_http_url "$configured_runtime_url")"
  if [[ "$configured_runtime_url" == *"host.docker.internal"* ]]; then
    echo "VENDOR_RUNTIME_URL uses host.docker.internal, which is container-to-host only; using http://127.0.0.1:9380 for this host-run script"
    configured_runtime_url="http://127.0.0.1:9380"
  fi
  export VENDOR_RUNTIME_URL="$configured_runtime_url"

  RUNTIME_MODE="${KNOWLEDGE_PARSE_RUNTIME_MODE:-host}"
  if [[ -z "${KNOWLEDGE_PARSE_RUNTIME_MODE:-}" && -n "${KNOWLEDGE_PARSE_VENDOR_RUNTIME_URL:-}" ]]; then
    if ! is_loopback_host "$(url_host "$VENDOR_RUNTIME_URL")"; then
      RUNTIME_MODE="external"
    fi
  fi
  case "$RUNTIME_MODE" in
    host|external) ;;
    *)
      echo "KNOWLEDGE_PARSE_RUNTIME_MODE must be host or external" >&2
      return 1
      ;;
  esac
  export KNOWLEDGE_PARSE_RUNTIME_MODE="$RUNTIME_MODE"

  export KNOWLEDGE_HTTP_ADDR="${KNOWLEDGE_PARSE_ADAPTER_ADDR:-${KNOWLEDGE_HTTP_ADDR:-:8083}}"
  export KNOWLEDGE_AUTO_START_INGESTION="${KNOWLEDGE_PARSE_AUTO_START_INGESTION:-true}"
  export RAGFLOW_CONF="${RAGFLOW_CONF:-$RUNTIME_DIR/conf/service_conf.yaml}"
  export PYTHONPATH="."
  export LITELLM_LOCAL_MODEL_COST_MAP="${LITELLM_LOCAL_MODEL_COST_MAP:-True}"

  append_no_proxy "127.0.0.1"
  append_no_proxy "localhost"
  append_no_proxy "::1"
  append_no_proxy_for_url "$VENDOR_RUNTIME_URL"
  if [[ -n "${KNOWLEDGE_RUNTIME_ES_URL:-}" ]]; then
    append_no_proxy_for_url "$KNOWLEDGE_RUNTIME_ES_URL"
  fi
}

ensure_runtime_venv() {
  CURRENT_STEP="checking knowledge-runtime Python environment"
  if [[ -d "$RUNTIME_DIR/.venv" ]]; then
    return
  fi
  if [[ "${KNOWLEDGE_RUNTIME_AUTO_UV_SYNC:-1}" != "1" ]]; then
    echo "missing $RUNTIME_DIR/.venv; run: cd services/knowledge-runtime && uv sync --python 3.13 --frozen" >&2
    return 1
  fi
  echo "knowledge-runtime .venv missing; running uv sync --python 3.13 --frozen"
  (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen)
}

run_runtime_preflight() {
  CURRENT_STEP="checking knowledge-runtime dependencies"
  echo "checking knowledge-runtime dependencies"
  if ! (cd "$RUNTIME_DIR" && uv run python deploy/check_runtime_dependencies.py); then
    print_required_env_hint
    return 1
  fi
}

run_adapter_preflight() {
  CURRENT_STEP="checking Knowledge adapter Go modules"
  echo "checking Knowledge adapter Go modules"
  (cd "$ADAPTER_DIR" && env -u GOROOT go mod download)
}

service_group_alive() {
  local pid_file="$1"
  [[ -f "$pid_file" ]] || return 1
  local pid
  pid="$(cat "$pid_file")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 -- "-$pid" 2>/dev/null
}

start_service() {
  local name="$1"
  local dir="$2"
  shift 2
  local pid_file="$RUN_DIR/$name.pid"
  local log_file="$LOG_DIR/$name.log"

  CURRENT_STEP="starting $name"
  if service_group_alive "$pid_file"; then
    echo "$name already running"
    return
  fi

  rm -f "$pid_file"
  echo "starting $name"
  (cd "$dir" && exec setsid "$@") >"$log_file" 2>&1 &
  echo "$!" >"$pid_file"
  STARTED_SERVICES+=("$name")
}

check_started_services() {
  CURRENT_STEP="checking started process groups"
  local wait_seconds="${KNOWLEDGE_PARSE_STARTUP_CHECK_SECONDS:-8}"
  local failed=()

  if [[ "$wait_seconds" =~ ^[0-9]+$ ]] && (( wait_seconds > 0 )) && (( ${#STARTED_SERVICES[@]} > 0 )); then
    echo "checking started process groups for ${wait_seconds}s"
    sleep "$wait_seconds"
  fi

  for name in "${STARTED_SERVICES[@]}"; do
    if ! service_group_alive "$RUN_DIR/$name.pid"; then
      failed+=("$name")
    fi
  done

  if (( ${#failed[@]} == 0 )); then
    return 0
  fi

  echo "startup failed for: ${failed[*]}" >&2
  for name in "${failed[@]}"; do
    echo "----- $LOG_DIR/$name.log (tail) -----" >&2
    if [[ -f "$LOG_DIR/$name.log" ]]; then
      tail -n 60 "$LOG_DIR/$name.log" >&2
    else
      echo "log file missing" >&2
    fi
  done
  return 1
}

wait_for_http_ok() {
  local name="$1"
  local url="$2"
  local timeout_seconds="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local response_file
  response_file="$(mktemp)"

  CURRENT_STEP="waiting for $name"
  while (( SECONDS < deadline )); do
    status="$(curl --noproxy '*' -sS -o "$response_file" -w '%{http_code}' "$url" 2>/dev/null || true)"
    if [[ "$status" =~ ^2[0-9][0-9]$ ]]; then
      rm -f "$response_file"
      echo "$name is ready"
      return 0
    fi
    sleep 2
  done

  echo "$name did not become ready at $url" >&2
  if [[ -s "$response_file" ]]; then
    echo "last response:" >&2
    tail -n 20 "$response_file" >&2
  fi
  rm -f "$response_file"
  return 1
}

knowledge_base_url() {
  local addr="${KNOWLEDGE_HTTP_ADDR:-:8083}"
  if [[ "$addr" == http://* || "$addr" == https://* ]]; then
    printf '%s\n' "${addr%/}"
  elif [[ "$addr" == :* ]]; then
    printf 'http://127.0.0.1%s\n' "$addr"
  else
    printf 'http://%s\n' "$addr"
  fi
}

echo "knowledge parse stack startup: starting Knowledge parse stack"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing deploy/.env; run: cp deploy/.env.example deploy/.env" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

require_env
require_command setsid
require_command go
require_command curl
if [[ "$RUNTIME_MODE" == "host" ]]; then
  require_command uv
fi
mkdir -p "$RUN_DIR" "$LOG_DIR"

if [[ "$RUNTIME_MODE" == "host" ]]; then
  ensure_runtime_venv
  run_runtime_preflight
else
  echo "using external Knowledge runtime at $VENDOR_RUNTIME_URL"
fi
run_adapter_preflight

if [[ "$RUNTIME_MODE" == "host" ]]; then
  start_service "knowledge-runtime-api" "$RUNTIME_DIR" ./deploy/api/run-local.sh
  start_service "knowledge-runtime-worker" "$RUNTIME_DIR" ./deploy/worker/run-local.sh
fi
start_service "knowledge-adapter" "$ADAPTER_DIR" env -u GOROOT go run ./cmd/adapter

check_started_services
wait_for_http_ok "knowledge-runtime API" "$VENDOR_RUNTIME_URL/api/v1/system/ping" "${KNOWLEDGE_RUNTIME_API_READY_TIMEOUT_SECONDS:-90}"
wait_for_http_ok "Knowledge adapter" "$(knowledge_base_url)/readyz" "${KNOWLEDGE_ADAPTER_READY_TIMEOUT_SECONDS:-120}"

cat <<EOF
knowledge parse stack is running
  mode:        $RUNTIME_MODE
  runtime API: $VENDOR_RUNTIME_URL
  adapter:     $(knowledge_base_url)
  NO_PROXY:    ${NO_PROXY:-}
  logs:        .local/logs/knowledge-runtime-api.log
               .local/logs/knowledge-runtime-worker.log
               .local/logs/knowledge-adapter.log

Run the PDF E2E smoke:
  python3 scripts/local/knowledge-pdf-e2e.py DL_T_673-1999.pdf

Stop host-run processes:
  ./scripts/local/stop-backend.sh
EOF

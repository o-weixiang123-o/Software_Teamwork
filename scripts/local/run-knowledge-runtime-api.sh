#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${KNOWLEDGE_ENV_FILE:-$ROOT_DIR/deploy/.env}"
RUN_DIR="$ROOT_DIR/.local/run"
LOG_DIR="$ROOT_DIR/.local/logs"
RUNTIME_DIR="$ROOT_DIR/services/knowledge-runtime"
LOCAL_RUNTIME_DIR="$ROOT_DIR/.local/knowledge-runtime"
CURRENT_STEP="initializing"
STARTED_SERVICES=()
RAGFLOW_CONF_EXPLICIT=0
CHINA_MIRRORS=0

on_exit() {
  status=$?
  if (( status == 0 )); then
    echo "knowledge runtime API startup: completed successfully"
  else
    echo "knowledge runtime API startup: failed during ${CURRENT_STEP} (exit ${status})" >&2
    echo "Check logs under .local/logs/ and pid files under .local/run/." >&2
  fi
}
trap on_exit EXIT

usage() {
  cat <<'EOF'
Usage: ./scripts/local/run-knowledge-runtime-api.sh [--china]

Starts only the host-run Knowledge runtime API. This is the query-ready
runtime path; it does not start the parser worker or Knowledge adapter.

Options:
  --china   Use hf-mirror for HuggingFace model downloads in this run only when
            HF_ENDPOINT is not already set.
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

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required for the host-run Knowledge runtime API" >&2
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

to_lower() {
  printf '%s\n' "$1" | tr '[:upper:]' '[:lower:]'
}

print_required_env_hint() {
  cat >&2 <<EOF
Required local Knowledge runtime API settings are missing.

Start from the tracked defaults, then add private provider credentials:
  cp deploy/.env.example deploy/.env

Required for this script:
  VENDOR_RUNTIME_SERVICE_TOKEN or KNOWLEDGE_RUNTIME_SERVICE_TOKEN
  DOC_ENGINE=elasticsearch
  KNOWLEDGE_RUNTIME_ES_URL=http://127.0.0.1:9200
  KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY
  KNOWLEDGE_RUNTIME_EMBEDDING_MODEL
  KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL
  KNOWLEDGE_RUNTIME_MODEL_API_KEY, unless using a trusted local keyless provider

This API-only path installs the base runtime dependency profile. Use
./scripts/local/run-knowledge-parse-stack.sh for worker/full ingestion deps.
EOF
}

require_env() {
  local runtime_token="${VENDOR_RUNTIME_SERVICE_TOKEN:-${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}}"
  local configured_runtime_url

  if [[ -z "${runtime_token// }" ]]; then
    echo "VENDOR_RUNTIME_SERVICE_TOKEN/KNOWLEDGE_RUNTIME_SERVICE_TOKEN missing; using local development default for scripts/local"
    export VENDOR_RUNTIME_SERVICE_TOKEN="local-dev-runtime-service-token-change-me"
    runtime_token="$VENDOR_RUNTIME_SERVICE_TOKEN"
  fi

  if [[ -z "${runtime_token// }" ]]; then
    echo "missing required env: VENDOR_RUNTIME_SERVICE_TOKEN or KNOWLEDGE_RUNTIME_SERVICE_TOKEN" >&2
    print_required_env_hint
    return 1
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

  export DOC_ENGINE="${DOC_ENGINE:-elasticsearch}"
  if [[ "$(to_lower "$DOC_ENGINE")" == "elasticsearch" ]]; then
    export KNOWLEDGE_RUNTIME_ES_URL
    KNOWLEDGE_RUNTIME_ES_URL="$(normalize_http_url "${KNOWLEDGE_RUNTIME_ES_URL:-http://127.0.0.1:${KNOWLEDGE_RUNTIME_ELASTICSEARCH_PORT:-9200}}")"
  fi
  if [[ -n "${RAGFLOW_CONF:-}" ]]; then
    RAGFLOW_CONF_EXPLICIT=1
  fi
  export RAGFLOW_CONF="${RAGFLOW_CONF:-$RUNTIME_DIR/conf/service_conf.yaml}"
  export PYTHONPATH="."
  export LITELLM_LOCAL_MODEL_COST_MAP="${LITELLM_LOCAL_MODEL_COST_MAP:-True}"
  if (( CHINA_MIRRORS )) && [[ -z "${HF_ENDPOINT:-}" ]]; then
    export HF_ENDPOINT="https://hf-mirror.com"
    echo "using HF_ENDPOINT=https://hf-mirror.com for this run (--china); deploy/.env is not modified"
  fi

  append_no_proxy "127.0.0.1"
  append_no_proxy "localhost"
  append_no_proxy "::1"
  append_no_proxy_for_url "$VENDOR_RUNTIME_URL"
  if [[ -n "${KNOWLEDGE_RUNTIME_ES_URL:-}" ]]; then
    append_no_proxy_for_url "$KNOWLEDGE_RUNTIME_ES_URL"
  fi
}

prepare_runtime_config() {
  [[ "$(to_lower "${DOC_ENGINE:-elasticsearch}")" == "elasticsearch" ]] || return 0
  [[ -n "${KNOWLEDGE_RUNTIME_ES_URL:-}" ]] || return 0
  if [[ "$RAGFLOW_CONF_EXPLICIT" == "1" && "${KNOWLEDGE_RUNTIME_GENERATE_LOCAL_CONF:-0}" != "1" ]]; then
    echo "using explicit RAGFLOW_CONF=$RAGFLOW_CONF; ensure its es.hosts matches $KNOWLEDGE_RUNTIME_ES_URL"
    return 0
  fi

  CURRENT_STEP="preparing knowledge-runtime config"
  local config_file="$LOCAL_RUNTIME_DIR/service_conf.yaml"
  mkdir -p "$LOCAL_RUNTIME_DIR"
  awk -v es_url="$KNOWLEDGE_RUNTIME_ES_URL" '
    BEGIN { in_es = 0; replaced = 0 }
    /^es:[[:space:]]*$/ { in_es = 1; print; next }
    /^[^[:space:]][^:]*:/ {
      if (in_es && !replaced) {
        print "  hosts: " es_url
        replaced = 1
      }
      in_es = 0
    }
    in_es && /^[[:space:]]+hosts:[[:space:]]*/ {
      print "  hosts: " es_url
      replaced = 1
      next
    }
    { print }
    END {
      if (in_es && !replaced) {
        print "  hosts: " es_url
      }
    }
  ' "$RUNTIME_DIR/conf/service_conf.yaml" >"$config_file"
  export RAGFLOW_CONF="$config_file"
  echo "knowledge-runtime config generated: $RAGFLOW_CONF"
}

ensure_runtime_venv() {
  CURRENT_STEP="checking knowledge-runtime API Python environment"
  if [[ -d "$RUNTIME_DIR/.venv" ]]; then
    if (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --no-default-groups --check >/dev/null 2>&1); then
      return
    fi
    if [[ "${KNOWLEDGE_RUNTIME_AUTO_UV_SYNC:-1}" != "1" ]]; then
      echo "$RUNTIME_DIR/.venv is not synced with API dependencies; run: cd services/knowledge-runtime && uv sync --python 3.13 --frozen --no-default-groups" >&2
      return 1
    fi
    echo "knowledge-runtime .venv is not synced with API dependencies; running uv sync --python 3.13 --frozen --no-default-groups"
    (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --no-default-groups)
    return
  fi
  if [[ "${KNOWLEDGE_RUNTIME_AUTO_UV_SYNC:-1}" != "1" ]]; then
    echo "missing $RUNTIME_DIR/.venv; run: cd services/knowledge-runtime && uv sync --python 3.13 --frozen --no-default-groups" >&2
    return 1
  fi
  echo "knowledge-runtime .venv missing; running uv sync --python 3.13 --frozen --no-default-groups"
  (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --no-default-groups)
}

run_runtime_preflight() {
  CURRENT_STEP="checking knowledge-runtime API dependencies"
  echo "checking knowledge-runtime API dependencies"
  if ! (cd "$RUNTIME_DIR" && uv run --no-sync --no-default-groups python deploy/check_runtime_dependencies.py); then
    print_required_env_hint
    return 1
  fi
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
  launch_process_group "$dir" "$@" >"$log_file" 2>&1 &
  echo "$!" >"$pid_file"
  STARTED_SERVICES+=("$name")
}

check_started_services() {
  CURRENT_STEP="checking started process groups"
  local wait_seconds="${KNOWLEDGE_RUNTIME_API_STARTUP_CHECK_SECONDS:-8}"
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
      tail -n 80 "$LOG_DIR/$name.log" >&2
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

  if [[ -f "$RUN_DIR/knowledge-runtime-api.pid" ]] && ! service_group_alive "$RUN_DIR/knowledge-runtime-api.pid"; then
    echo "knowledge-runtime-api exited before $name became ready" >&2
    echo "----- $LOG_DIR/knowledge-runtime-api.log (tail) -----" >&2
    if [[ -f "$LOG_DIR/knowledge-runtime-api.log" ]]; then
      tail -n 80 "$LOG_DIR/knowledge-runtime-api.log" >&2
    else
      echo "log file missing" >&2
    fi
    rm -f "$response_file"
    return 1
  fi

  echo "$name did not become ready at $url" >&2
  if [[ "$name" == "Elasticsearch" ]]; then
    echo "For local Elasticsearch, rerun ./scripts/local/dev-up.sh and inspect docker compose ps/logs for elasticsearch." >&2
    echo "For external Elasticsearch, set KNOWLEDGE_RUNTIME_ES_URL to the reachable endpoint." >&2
  fi
  if [[ -s "$response_file" ]]; then
    echo "last response:" >&2
    tail -n 20 "$response_file" >&2
  fi
  rm -f "$response_file"
  return 1
}

echo "knowledge runtime API startup: starting runtime API only"
parse_args "$@"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing deploy/.env; run: cp deploy/.env.example deploy/.env" >&2
  exit 1
fi

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

require_env
if ! command -v setsid >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
  echo "setsid or python3 is required to manage host-run process groups" >&2
  exit 1
fi
require_command curl
require_command uv
mkdir -p "$RUN_DIR" "$LOG_DIR"

prepare_runtime_config
if [[ "$(to_lower "${DOC_ENGINE:-elasticsearch}")" == "elasticsearch" && -n "${KNOWLEDGE_RUNTIME_ES_URL:-}" ]]; then
  wait_for_http_ok "Elasticsearch" "$KNOWLEDGE_RUNTIME_ES_URL/_cluster/health" "${KNOWLEDGE_RUNTIME_ELASTICSEARCH_READY_TIMEOUT_SECONDS:-120}"
fi
ensure_runtime_venv
run_runtime_preflight

start_service "knowledge-runtime-api" "$RUNTIME_DIR" ./deploy/api/run-local.sh

check_started_services
wait_for_http_ok "knowledge-runtime API" "$VENDOR_RUNTIME_URL/api/v1/system/ping" "${KNOWLEDGE_RUNTIME_API_READY_TIMEOUT_SECONDS:-90}"

cat <<EOF
knowledge runtime API is running
  runtime API: $VENDOR_RUNTIME_URL
  doc engine:  ${DOC_ENGINE:-elasticsearch}
  es URL:      ${KNOWLEDGE_RUNTIME_ES_URL:-}
  NO_PROXY:    ${NO_PROXY:-}
  logs:        .local/logs/knowledge-runtime-api.log
  config:      ${RAGFLOW_CONF:-}
               .local/knowledge-runtime/service_conf.yaml when generated by this script

This API-only helper does not start knowledge-runtime-worker.
Start full ingestion explicitly with:
  ./scripts/local/run-knowledge-parse-stack.sh

Stop host-run processes:
  ./scripts/local/stop-backend.sh
EOF

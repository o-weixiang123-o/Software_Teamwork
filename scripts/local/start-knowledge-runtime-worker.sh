#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONFIG_LOADER="$ROOT_DIR/scripts/config/load-profile.sh"
RUN_DIR="$ROOT_DIR/.local/run"
LOG_DIR="$ROOT_DIR/.local/logs"
RUNTIME_DIR="$ROOT_DIR/services/knowledge-runtime"
LOCAL_RUNTIME_DIR="$ROOT_DIR/.local/knowledge-runtime"
WATCHER_SCRIPT="$ROOT_DIR/scripts/local/watch-knowledge-runtime-worker-idle.sh"
WORKER_PID_FILE="$RUN_DIR/knowledge-runtime-worker.pid"
WATCHER_PID_FILE="$RUN_DIR/knowledge-runtime-worker-idle-watcher.pid"
WORKER_LOG_FILE="$LOG_DIR/knowledge-runtime-worker.log"
WATCHER_LOG_FILE="$LOG_DIR/knowledge-runtime-worker-idle-watcher.log"
CURRENT_STEP="initializing"
RAGFLOW_CONF_EXPLICIT=0
CHINA_MIRRORS=0

on_exit() {
  status=$?
  if (( status == 0 )); then
    echo "knowledge runtime worker startup: completed successfully"
  else
    echo "knowledge runtime worker startup: failed during ${CURRENT_STEP} (exit ${status})" >&2
    echo "Check .local/logs/knowledge-runtime-worker.log and .local/run/knowledge-runtime-worker.pid." >&2
  fi
}
trap on_exit EXIT

usage() {
  cat <<'EOF'
Usage: ./scripts/local/start-knowledge-runtime-worker.sh [--china]

Starts only the host-run Knowledge runtime worker. This script is intended for
on-demand local ingestion and does not start the runtime API or Knowledge adapter.
After startup, a local idle watcher stops the worker when the runtime queue has
stayed empty for KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS.

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
    echo "$1 is required for the host-run Knowledge runtime worker" >&2
    return 1
  fi
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

require_env() {
  local runtime_token="${VENDOR_RUNTIME_SERVICE_TOKEN:-${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}}"
  if [[ -z "${runtime_token// }" ]]; then
    echo "VENDOR_RUNTIME_SERVICE_TOKEN/KNOWLEDGE_RUNTIME_SERVICE_TOKEN missing; using local development default for scripts/local"
    export VENDOR_RUNTIME_SERVICE_TOKEN="local-dev-runtime-service-token-change-me"
    runtime_token="$VENDOR_RUNTIME_SERVICE_TOKEN"
  fi
  if [[ -z "${VENDOR_RUNTIME_SERVICE_TOKEN:-}" && -n "${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}" ]]; then
    export VENDOR_RUNTIME_SERVICE_TOKEN="$KNOWLEDGE_RUNTIME_SERVICE_TOKEN"
  fi
  if [[ -z "${KNOWLEDGE_RUNTIME_SERVICE_TOKEN:-}" && -n "${VENDOR_RUNTIME_SERVICE_TOKEN:-}" ]]; then
    export KNOWLEDGE_RUNTIME_SERVICE_TOKEN="$VENDOR_RUNTIME_SERVICE_TOKEN"
  fi

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
    echo "using HF_ENDPOINT=https://hf-mirror.com for this run (--china); profile files and .env.local are not modified"
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
  CURRENT_STEP="checking knowledge-runtime worker Python environment"
  if [[ -d "$RUNTIME_DIR/.venv" ]]; then
    if (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --group worker --check >/dev/null 2>&1); then
      return
    fi
    if [[ "${KNOWLEDGE_RUNTIME_AUTO_UV_SYNC:-1}" != "1" ]]; then
      echo "$RUNTIME_DIR/.venv is not synced with worker dependencies; run: cd services/knowledge-runtime && uv sync --python 3.13 --frozen --group worker" >&2
      return 1
    fi
    echo "knowledge-runtime .venv is not synced with worker dependencies; running uv sync --python 3.13 --frozen --group worker"
    (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --group worker)
    return
  fi
  if [[ "${KNOWLEDGE_RUNTIME_AUTO_UV_SYNC:-1}" != "1" ]]; then
    echo "missing $RUNTIME_DIR/.venv; run: cd services/knowledge-runtime && uv sync --python 3.13 --frozen --group worker" >&2
    return 1
  fi
  echo "knowledge-runtime .venv missing; running uv sync --python 3.13 --frozen --group worker"
  (cd "$RUNTIME_DIR" && uv sync --python 3.13 --frozen --group worker)
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

process_alive() {
  local pid_file="$1"
  [[ -f "$pid_file" ]] || return 1
  local pid
  pid="$(cat "$pid_file")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null
}

runtime_api_url() {
  normalize_http_url "${VENDOR_RUNTIME_URL:-http://127.0.0.1:9380}"
}

runtime_api_available() {
  local base_url
  base_url="$(runtime_api_url)"
  local body
  body="$(curl --noproxy '*' -sS --max-time 3 "$base_url/api/v1/system/ping" 2>/dev/null || true)"
  [[ "$body" == "pong" ]]
}

worker_heartbeat_ready() {
  local base_url
  base_url="$(runtime_api_url)"
  curl --noproxy '*' -sS --max-time 5 \
    -H "X-Service-Token: ${VENDOR_RUNTIME_SERVICE_TOKEN:-}" \
    "$base_url/api/v1/system/status" 2>/dev/null | python3 -c '
import json
import sys

try:
    payload = json.load(sys.stdin)
    heartbeats = ((payload.get("data") or {}).get("task_executor_heartbeats") or {})
    ready = isinstance(heartbeats, dict) and any(isinstance(entries, list) and entries for entries in heartbeats.values())
except Exception:
    ready = False
sys.exit(0 if ready else 1)
'
}

print_worker_log_tail() {
  echo "----- $WORKER_LOG_FILE (tail) -----" >&2
  if [[ -f "$WORKER_LOG_FILE" ]]; then
    tail -n 80 "$WORKER_LOG_FILE" >&2
  else
    echo "log file missing" >&2
  fi
}

start_worker() {
  CURRENT_STEP="starting knowledge-runtime-worker"
  if service_group_alive "$WORKER_PID_FILE"; then
    echo "knowledge-runtime-worker already running"
    return
  fi

  rm -f "$WORKER_PID_FILE"
  echo "starting knowledge-runtime-worker"
  launch_process_group "$RUNTIME_DIR" ./deploy/worker/run-local.sh >"$WORKER_LOG_FILE" 2>&1 &
  echo "$!" >"$WORKER_PID_FILE"
}

check_worker() {
  CURRENT_STEP="checking knowledge-runtime-worker"
  local wait_seconds="${KNOWLEDGE_RUNTIME_WORKER_STARTUP_CHECK_SECONDS:-8}"
  if [[ "$wait_seconds" =~ ^[0-9]+$ ]] && (( wait_seconds > 0 )); then
    echo "checking knowledge-runtime-worker for ${wait_seconds}s"
    sleep "$wait_seconds"
  fi
  if service_group_alive "$WORKER_PID_FILE"; then
    return 0
  fi
  echo "knowledge-runtime-worker exited during startup" >&2
  print_worker_log_tail
  return 1
}

wait_for_worker_heartbeat() {
  CURRENT_STEP="waiting for knowledge-runtime-worker heartbeat"
  if ! runtime_api_available; then
    echo "knowledge-runtime API is not reachable; worker process is running but heartbeat was not checked"
    return 0
  fi

  local wait_seconds="${KNOWLEDGE_RUNTIME_WORKER_HEARTBEAT_TIMEOUT_SECONDS:-600}"
  local deadline=$((SECONDS + wait_seconds))
  echo "waiting for knowledge-runtime-worker heartbeat for ${wait_seconds}s"
  while (( SECONDS < deadline )); do
    if worker_heartbeat_ready; then
      echo "knowledge-runtime-worker heartbeat is ready"
      return 0
    fi
    if ! service_group_alive "$WORKER_PID_FILE"; then
      echo "knowledge-runtime-worker exited before heartbeat became ready" >&2
      print_worker_log_tail
      return 1
    fi
    sleep 2
  done

  echo "knowledge-runtime-worker heartbeat did not become ready" >&2
  print_worker_log_tail
  return 1
}

start_idle_shutdown_watcher() {
  CURRENT_STEP="starting knowledge-runtime-worker idle watcher"
  local idle_seconds="${KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS:-300}"
  if ! [[ "$idle_seconds" =~ ^[0-9]+$ ]] || (( idle_seconds <= 0 )); then
    echo "knowledge-runtime-worker idle shutdown disabled"
    return 0
  fi
  if ! runtime_api_available; then
    echo "knowledge-runtime API is not reachable; idle shutdown watcher was not started"
    return 0
  fi
  if process_alive "$WATCHER_PID_FILE"; then
    echo "knowledge-runtime-worker idle watcher already running"
    return 0
  fi
  rm -f "$WATCHER_PID_FILE"
  [[ -f "$WORKER_PID_FILE" ]] || return 0
  local worker_pid
  worker_pid="$(cat "$WORKER_PID_FILE")"
  [[ "$worker_pid" =~ ^[0-9]+$ ]] || return 0
  if command -v setsid >/dev/null 2>&1; then
    CONFIG_PROFILE="$CONFIG_PROFILE" CONFIG_SECRET_FILE="$CONFIG_SECRET_FILE" setsid "$WATCHER_SCRIPT" "$worker_pid" >>"$WATCHER_LOG_FILE" 2>&1 &
  else
    CONFIG_PROFILE="$CONFIG_PROFILE" CONFIG_SECRET_FILE="$CONFIG_SECRET_FILE" nohup "$WATCHER_SCRIPT" "$worker_pid" >>"$WATCHER_LOG_FILE" 2>&1 &
  fi
  echo "knowledge-runtime-worker idle watcher started"
}

echo "knowledge runtime worker startup: starting worker only"
parse_args "$@"

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
# shellcheck disable=SC1090
. "$CONFIG_LOADER"

require_env
if ! command -v setsid >/dev/null 2>&1 && ! command -v python3 >/dev/null 2>&1; then
  echo "setsid or python3 is required to manage host-run process groups" >&2
  exit 1
fi
require_command uv
require_command curl
require_command python3
mkdir -p "$RUN_DIR" "$LOG_DIR"

prepare_runtime_config
ensure_runtime_venv
start_worker
check_worker
wait_for_worker_heartbeat
start_idle_shutdown_watcher

cat <<EOF
knowledge runtime worker is running
  logs:         .local/logs/knowledge-runtime-worker.log
  idle watcher: .local/logs/knowledge-runtime-worker-idle-watcher.log
  pid:          .local/run/knowledge-runtime-worker.pid

This worker-only helper does not start knowledge-runtime-api or knowledge adapter.
EOF

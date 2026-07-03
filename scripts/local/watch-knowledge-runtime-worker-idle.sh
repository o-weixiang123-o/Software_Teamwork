#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${KNOWLEDGE_ENV_FILE:-$ROOT_DIR/deploy/.env}"
RUN_DIR="$ROOT_DIR/.local/run"
LOG_DIR="$ROOT_DIR/.local/logs"
RUNTIME_DIR="$ROOT_DIR/services/knowledge-runtime"
WORKER_PID_FILE="$RUN_DIR/knowledge-runtime-worker.pid"
WATCHER_PID_FILE="$RUN_DIR/knowledge-runtime-worker-idle-watcher.pid"

normalize_http_url() {
  local value="$1"
  if [[ "$value" != http://* && "$value" != https://* ]]; then
    value="http://$value"
  fi
  printf '%s\n' "${value%/}"
}

service_group_alive() {
  local pid_file="$1"
  [[ -f "$pid_file" ]] || return 1
  local pid
  pid="$(cat "$pid_file")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 -- "-$pid" 2>/dev/null
}

runtime_api_url() {
  normalize_http_url "${VENDOR_RUNTIME_URL:-http://127.0.0.1:9380}"
}

worker_queue_idle() {
  local base_url
  base_url="$(runtime_api_url)"
  local status_json
  status_json="$(curl --noproxy '*' -sS --max-time 5 \
    -H "X-Service-Token: ${VENDOR_RUNTIME_SERVICE_TOKEN:-}" \
    "$base_url/api/v1/system/status" 2>/dev/null || true)"
  STATUS_JSON="$status_json" HEARTBEAT_MAX_AGE_SECONDS="${KNOWLEDGE_RUNTIME_WORKER_HEARTBEAT_MAX_AGE_SECONDS:-120}" python3 -c '
import json
import os
import sys
from datetime import datetime, timezone

try:
    payload = json.loads(os.environ.get("STATUS_JSON", ""))
    heartbeats = ((payload.get("data") or {}).get("task_executor_heartbeats") or {})
    max_age = float(os.environ.get("HEARTBEAT_MAX_AGE_SECONDS", "120"))
    now = datetime.now(timezone.utc)
    fresh_seen = False
    busy = False
    for entries in heartbeats.values():
        if not isinstance(entries, list) or not entries:
            continue
        latest = entries[-1]
        if not isinstance(latest, dict):
            continue
        raw_now = str(latest.get("now") or "")
        if raw_now:
            heartbeat_at = datetime.fromisoformat(raw_now)
            if heartbeat_at.tzinfo is None:
                heartbeat_at = heartbeat_at.replace(tzinfo=timezone.utc)
            if (now - heartbeat_at.astimezone(timezone.utc)).total_seconds() > max_age:
                continue
        fresh_seen = True
        pending = int(latest.get("pending") or 0)
        lag = int(latest.get("lag") or 0)
        current = latest.get("current") or {}
        if pending > 0 or lag > 0 or bool(current):
            busy = True
            break
except Exception:
    sys.exit(1)
sys.exit(0 if fresh_seen and not busy else 1)
'
}

cleanup_worker_heartbeat() {
  local worker_name="task_executor_common_${KNOWLEDGE_RUNTIME_WORKER_ID:-0}"
  local timeout_seconds="${KNOWLEDGE_RUNTIME_WORKER_HEARTBEAT_CLEANUP_TIMEOUT_SECONDS:-5}"
  local python_bin="$RUNTIME_DIR/.venv/bin/python"
  if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]] || (( timeout_seconds <= 0 )); then
    timeout_seconds=5
  fi
  if [[ ! -x "$python_bin" ]]; then
    python_bin="python3"
  fi

  if command -v timeout >/dev/null 2>&1; then
    timeout "${timeout_seconds}s" "$python_bin" - "$worker_name" "$RAGFLOW_CONF" <<'PY' >/dev/null 2>&1 || true
import sys
from pathlib import Path

import valkey

worker_name = sys.argv[1]
config_path = Path(sys.argv[2])
redis_config = {"host": "localhost:6379", "db": "1", "username": "", "password": ""}

in_redis = False
for raw_line in config_path.read_text(encoding="utf-8").splitlines():
    stripped = raw_line.strip()
    if not stripped or stripped.startswith("#"):
        continue
    if raw_line == stripped and stripped.endswith(":"):
        in_redis = stripped == "redis:"
        continue
    if not in_redis:
        continue
    if raw_line == stripped:
        break
    if ":" not in stripped:
        continue
    key, value = stripped.split(":", 1)
    redis_config[key.strip()] = value.strip().strip("'\"")

host_value = redis_config.get("host") or "localhost:6379"
if ":" in host_value:
    host, port_value = host_value.rsplit(":", 1)
else:
    host, port_value = host_value, "6379"

client = valkey.Valkey(
    host=host,
    port=int(port_value),
    db=int(redis_config.get("db") or 0),
    username=redis_config.get("username") or None,
    password=redis_config.get("password") or None,
    socket_connect_timeout=3,
    socket_timeout=3,
    decode_responses=True,
)
client.srem("TASKEXE", worker_name)
client.delete(worker_name)
PY
    return 0
  fi

  "$python_bin" - "$worker_name" "$RAGFLOW_CONF" <<'PY' >/dev/null 2>&1 || true
import sys
from pathlib import Path

import valkey

worker_name = sys.argv[1]
config_path = Path(sys.argv[2])
redis_config = {"host": "localhost:6379", "db": "1", "username": "", "password": ""}

in_redis = False
for raw_line in config_path.read_text(encoding="utf-8").splitlines():
    stripped = raw_line.strip()
    if not stripped or stripped.startswith("#"):
        continue
    if raw_line == stripped and stripped.endswith(":"):
        in_redis = stripped == "redis:"
        continue
    if not in_redis:
        continue
    if raw_line == stripped:
        break
    if ":" not in stripped:
        continue
    key, value = stripped.split(":", 1)
    redis_config[key.strip()] = value.strip().strip("'\"")

host_value = redis_config.get("host") or "localhost:6379"
if ":" in host_value:
    host, port_value = host_value.rsplit(":", 1)
else:
    host, port_value = host_value, "6379"

client = valkey.Valkey(
    host=host,
    port=int(port_value),
    db=int(redis_config.get("db") or 0),
    username=redis_config.get("username") or None,
    password=redis_config.get("password") or None,
    socket_connect_timeout=3,
    socket_timeout=3,
    decode_responses=True,
)
client.srem("TASKEXE", worker_name)
client.delete(worker_name)
PY
}

stop_worker_group() {
  [[ -f "$WORKER_PID_FILE" ]] || return 0
  local pid
  pid="$(cat "$WORKER_PID_FILE")"
  [[ "$pid" =~ ^[0-9]+$ ]] || return 0
  if kill -0 -- "-$pid" 2>/dev/null; then
    kill -TERM -- "-$pid" 2>/dev/null || true
    for _ in {1..25}; do
      kill -0 -- "-$pid" 2>/dev/null || break
      sleep 0.2
    done
    kill -0 -- "-$pid" 2>/dev/null && kill -KILL -- "-$pid" 2>/dev/null || true
  elif kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
  fi
  rm -f "$WORKER_PID_FILE"
  cleanup_worker_heartbeat
}

on_exit() {
  rm -f "$WATCHER_PID_FILE"
}
trap on_exit EXIT

if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing deploy/.env; idle watcher exiting"
  exit 1
fi

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

export RAGFLOW_CONF="${RAGFLOW_CONF:-$ROOT_DIR/.local/knowledge-runtime/service_conf.yaml}"

worker_pid="${1:-}"
if [[ -z "$worker_pid" && -f "$WORKER_PID_FILE" ]]; then
  worker_pid="$(cat "$WORKER_PID_FILE")"
fi
if ! [[ "$worker_pid" =~ ^[0-9]+$ ]]; then
  echo "knowledge-runtime-worker idle watcher exiting: invalid worker pid"
  exit 1
fi

idle_seconds="${KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS:-300}"
check_seconds="${KNOWLEDGE_RUNTIME_WORKER_IDLE_CHECK_SECONDS:-15}"
if ! [[ "$idle_seconds" =~ ^[0-9]+$ ]] || (( idle_seconds <= 0 )); then
  echo "knowledge-runtime-worker idle watcher exiting: idle shutdown disabled"
  exit 0
fi
if ! [[ "$check_seconds" =~ ^[0-9]+$ ]] || (( check_seconds <= 0 )); then
  check_seconds=15
fi

mkdir -p "$RUN_DIR" "$LOG_DIR"
echo "$$" >"$WATCHER_PID_FILE"
echo "knowledge-runtime-worker idle watcher started: idle_shutdown_seconds=${idle_seconds}, idle_check_seconds=${check_seconds}, worker_pid=${worker_pid}"

idle_started_at=""
while true; do
  current_pid=""
  [[ -f "$WORKER_PID_FILE" ]] && current_pid="$(cat "$WORKER_PID_FILE")"
  if [[ "$current_pid" != "$worker_pid" ]] || ! service_group_alive "$WORKER_PID_FILE"; then
    echo "knowledge-runtime-worker idle watcher exiting: worker is no longer running"
    exit 0
  fi

  now_seconds="$(date +%s)"
  if worker_queue_idle; then
    if [[ -z "$idle_started_at" ]]; then
      idle_started_at="$now_seconds"
      echo "knowledge-runtime-worker idle detected at ${idle_started_at}"
    elif (( now_seconds - idle_started_at >= idle_seconds )); then
      echo "knowledge-runtime-worker idle for ${idle_seconds}s; stopping worker"
      stop_worker_group
      exit 0
    fi
  else
    if [[ -n "$idle_started_at" ]]; then
      echo "knowledge-runtime-worker became busy; clearing idle timer"
    fi
    idle_started_at=""
  fi
  sleep "$check_seconds"
done

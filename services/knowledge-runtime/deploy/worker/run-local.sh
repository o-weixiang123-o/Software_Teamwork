#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ ! -d .venv ]]; then
  echo "Run: uv sync --python 3.13 --frozen --group worker" >&2
  exit 1
fi

export PYTHONPATH=.
export RAGFLOW_CONF="${RAGFLOW_CONF:-$ROOT/conf/service_conf.yaml}"
export NLTK_DATA="${NLTK_DATA:-$ROOT/ragflow_deps/nltk_data}"
export KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA="${KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA:-1}"
WORKER_ID="${KNOWLEDGE_RUNTIME_WORKER_ID:-0}"

if [[ ! -f "$RAGFLOW_CONF" ]]; then
  echo "Missing $RAGFLOW_CONF; create it from conf/service_conf.yaml and adjust local hosts" >&2
  exit 1
fi

uv run --no-sync --group worker python deploy/check_runtime_dependencies.py

exec uv run --no-sync --group worker python rag/svr/task_executor.py -i "$WORKER_ID" -t common

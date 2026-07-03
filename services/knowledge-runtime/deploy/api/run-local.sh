#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ ! -d .venv ]]; then
  echo "Run: uv sync --python 3.13 --frozen" >&2
  exit 1
fi

export PYTHONPATH=.
export RAGFLOW_CONF="${RAGFLOW_CONF:-$ROOT/conf/service_conf.yaml}"
export HF_ENDPOINT="${HF_ENDPOINT:-https://hf-mirror.com}"

if [[ ! -f "$RAGFLOW_CONF" ]]; then
  echo "Missing $RAGFLOW_CONF; create it from conf/service_conf.yaml and adjust local hosts" >&2
  exit 1
fi

uv run python deploy/check_runtime_dependencies.py

exec uv run python api/ragflow_server.py

#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_LOADER="$ROOT_DIR/scripts/config/load-profile.sh"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.yml"
NO_PROXY_VALUE="${NO_PROXY:-localhost,127.0.0.1,::1}"
export NO_PROXY="$NO_PROXY_VALUE"

usage() {
  cat <<'USAGE'
Issue #352 Auth/Gateway/Redis full smoke.

Usage:
  bash scripts/run_issue_352_smoke.sh

The script starts only infrastructure containers (postgres and redis) from the
root deploy Compose file, applies Auth migrations, runs Auth and Gateway on the
host, and executes the env-gated Go smoke:

  AUTH_GATEWAY_REDIS_FULL_SMOKE=1 go test ./services/deploy/smoke \
    -run '^TestAuthGatewayRedisFullSmoke$' -count=1 -v

Expected pass output includes:
  AUTH_GATEWAY_REDIS_FULL_SMOKE_RESULT pass ...

Environment defaults are rendered from config/ profiles plus .env.local.
Export variables before invoking this script to override the local defaults.

Common overrides:
  AUTH_GATEWAY_REDIS_DATABASE_URL
  AUTH_GATEWAY_REDIS_ADDR
  AUTH_GATEWAY_REDIS_SERVICE_TOKEN
  AUTH_GATEWAY_REDIS_ADMIN_SERVICE_TOKEN
  AUTH_GATEWAY_REDIS_TOKEN_HASH_SECRET
USAGE
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "blocked: docker command is unavailable; install Docker Engine/Desktop and retry" >&2
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  echo "blocked: Docker daemon is unavailable; start Docker and retry" >&2
  exit 2
fi

if ! command -v go >/dev/null 2>&1; then
  echo "blocked: go command is unavailable; install Go 1.25.x and retry" >&2
  exit 2
fi

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
# shellcheck disable=SC1090
. "$CONFIG_LOADER"

: "${AUTH_GATEWAY_REDIS_DATABASE_URL:=${AUTH_DATABASE_URL:-postgres://auth_app:auth_app_dev@localhost:5432/auth_system?sslmode=disable}}"
: "${AUTH_GATEWAY_REDIS_ADDR:=${GATEWAY_REDIS_ADDR:-localhost:6379}}"
: "${AUTH_GATEWAY_REDIS_SERVICE_TOKEN:=${INTERNAL_SERVICE_TOKEN:-local-dev-internal-service-token-change-me}}"
: "${AUTH_GATEWAY_REDIS_ADMIN_SERVICE_TOKEN:=${AUTH_GATEWAY_ADMIN_SERVICE_TOKEN:-local-dev-gateway-admin-token-change-me}}"
: "${AUTH_GATEWAY_REDIS_TOKEN_HASH_SECRET:=${TOKEN_HASH_SECRET:-local-demo-token-hash-secret-change-me}}"
export AUTH_GATEWAY_REDIS_DATABASE_URL AUTH_GATEWAY_REDIS_ADDR
export AUTH_GATEWAY_REDIS_SERVICE_TOKEN AUTH_GATEWAY_REDIS_ADMIN_SERVICE_TOKEN AUTH_GATEWAY_REDIS_TOKEN_HASH_SECRET
export AUTH_GATEWAY_REDIS_FULL_SMOKE=1

compose_env_file="$CONFIG_COMPOSE_ENV_FILE"
compose=(docker compose -f "$COMPOSE_FILE" --env-file "$compose_env_file")

if ! "${compose[@]}" config --quiet; then
  echo "blocked: deploy Compose config is invalid" >&2
  exit 2
fi

if ! "${compose[@]}" up -d --wait --wait-timeout "${AUTH_GATEWAY_REDIS_INFRA_WAIT_TIMEOUT_SECONDS:-120}" postgres redis; then
  echo "blocked: postgres/redis infrastructure did not become healthy; check Docker image pulls, registry rewrite, and docker compose ps" >&2
  exit 2
fi

(
  cd "$ROOT_DIR/services/deploy/smoke"
  go test . -run '^TestAuthGatewayRedisFullSmoke$' -count=1 -v
)

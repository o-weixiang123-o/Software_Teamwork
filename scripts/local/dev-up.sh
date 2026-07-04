#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CONFIG_LOADER="$ROOT_DIR/scripts/config/load-profile.sh"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.yml"
CURRENT_STEP="initializing"
INFRA_SERVICES=(postgres redis minio elasticsearch)
PULL_SERVICES=(postgres redis minio minio-init elasticsearch)
CHINA_MIRRORS=0
SKIP_KNOWLEDGE_RUNTIME_DEPS=0

OFFICIAL_UV_DEFAULT_INDEX="https://pypi.org/simple"
CHINA_UV_DEFAULT_INDEX="https://pypi.tuna.tsinghua.edu.cn/simple"
OFFICIAL_GOPROXY="https://proxy.golang.org,direct"
CHINA_GOPROXY="https://goproxy.cn,direct"
OFFICIAL_GOSUMDB="sum.golang.org"
CHINA_GOSUMDB="sum.golang.google.cn"

CHINA_POSTGRES_IMAGE="docker.m.daocloud.io/library/postgres:16-alpine"
CHINA_REDIS_IMAGE="docker.m.daocloud.io/library/redis:7-alpine"
CHINA_MINIO_IMAGE="docker.m.daocloud.io/minio/minio:RELEASE.2025-09-07T16-13-09Z"
CHINA_MINIO_MC_IMAGE="docker.m.daocloud.io/minio/mc:RELEASE.2025-08-13T08-35-41Z"
CHINA_ELASTICSEARCH_IMAGE="docker.m.daocloud.io/docker.elastic.co/elasticsearch/elasticsearch:8.15.3"

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
  printf '%b%s %s%b\n' "$COLOR_BLUE" "[dev-up]" "$*" "$COLOR_RESET"
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

to_lower() {
  printf '%s\n' "$1" | tr '[:upper:]' '[:lower:]'
}

is_truthy() {
  case "$(to_lower "${1:-}")" in
    1|true|yes|on)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

usage() {
  cat <<'EOF'
Usage: ./scripts/local/dev-up.sh [--china] [--skip-knowledge-runtime-deps]

Starts local infrastructure, runs host migrations, and applies local seed data.

Options:
  --china   Use mainland China mirrors for this run only:
            Docker registry rewrite, Go proxy/checksum DB, uv index, and
            Knowledge runtime dependency/artifact downloads.
  --skip-knowledge-runtime-deps
            With --china, skip Knowledge runtime dependency/artifact downloads.
            You can also set LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS=1.
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
      --skip-knowledge-runtime-deps|--no-knowledge-runtime-deps)
        SKIP_KNOWLEDGE_RUNTIME_DEPS=1
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
  local compose_env_hint="${CONFIG_COMPOSE_ENV_FILE:-.local/config/${CONFIG_PROFILE:-dev}.env}"
  if (( status == 0 )); then
    log_success "completed successfully"
  else
    log_error "failed during ${CURRENT_STEP} (exit ${status})"
    case "$CURRENT_STEP" in
      "checking local tool dependencies")
        log_hint "Install the missing host tool(s), then rerun ./scripts/local/dev-up.sh."
        ;;
      "initializing MinIO buckets")
        log_hint "Check MinIO logs: docker compose -f deploy/docker-compose.yml --env-file $compose_env_hint logs minio-init"
        log_hint "Check Docker status: docker compose -f deploy/docker-compose.yml --env-file $compose_env_hint ps"
        ;;
      *)
        log_hint "Check Docker status: docker compose -f deploy/docker-compose.yml --env-file $compose_env_hint ps"
        log_hint "Mainland China network: rerun ./scripts/local/dev-up.sh --china."
        log_hint "Official mode: confirm config/base.yaml or .env.local keeps GOPROXY=https://proxy.golang.org,direct and GOSUMDB=sum.golang.org."
        ;;
    esac
  fi
}
run_step() {
  CURRENT_STEP="$1"
  shift
  log_info "${CURRENT_STEP}"
  "$@"
  log_success "${CURRENT_STEP} succeeded"
}

check_required_commands() {
  local missing=()
  for command in docker go psql; do
    if ! command -v "$command" >/dev/null 2>&1; then
      missing+=("$command")
    fi
  done
  if (( CHINA_MIRRORS )) &&
    [[ "$SKIP_KNOWLEDGE_RUNTIME_DEPS" != "1" ]] &&
    [[ "${LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS:-0}" != "1" ]] &&
    ! command -v uv >/dev/null 2>&1; then
    missing+=(uv)
  fi
  if (( ${#missing[@]} > 0 )); then
    log_error "missing required local command(s): ${missing[*]}"
    log_error "Install Docker, Go, psql, and uv in the same host environment that runs ./scripts/local/dev-up.sh."
    log_error "uv is only required when --china prepares Knowledge runtime dependencies; rerun with --skip-knowledge-runtime-deps to skip that step."
    return 1
  fi
}

run_minio_init() {
  CURRENT_STEP="initializing MinIO buckets"
  log_info "${CURRENT_STEP}"
  if ! "${compose[@]}" up --no-deps --exit-code-from minio-init minio-init; then
    log_error "minio-init failed; inspect logs with: docker compose -f deploy/docker-compose.yml --env-file $CONFIG_COMPOSE_ENV_FILE logs minio-init"
    return 1
  fi
  log_success "${CURRENT_STEP} succeeded"
}

ensure_go_module_settings() {
  if ! command -v go >/dev/null 2>&1; then
    log_error "go is required for host-run migrations"
    return 1
  fi

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

apply_china_mirrors() {
  export POSTGRES_IMAGE="$CHINA_POSTGRES_IMAGE"
  export REDIS_IMAGE="$CHINA_REDIS_IMAGE"
  export MINIO_IMAGE="$CHINA_MINIO_IMAGE"
  export MINIO_MC_IMAGE="$CHINA_MINIO_MC_IMAGE"
  export KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE="$CHINA_ELASTICSEARCH_IMAGE"
  export UV_DEFAULT_INDEX="$CHINA_UV_DEFAULT_INDEX"
  export GOPROXY="$CHINA_GOPROXY"
  export GOSUMDB="$CHINA_GOSUMDB"
  log_info "using mainland China mirrors for this run (--china); profile files and .env.local are not modified"
}

warn_legacy_mirror_env() {
  local mirrored=()
  for value in \
    "${POSTGRES_IMAGE:-}" \
    "${REDIS_IMAGE:-}" \
    "${MINIO_IMAGE:-}" \
    "${MINIO_MC_IMAGE:-}" \
    "${UV_DEFAULT_INDEX:-}" \
    "${GOPROXY:-}" \
    "${GOSUMDB:-}"; do
    case "$value" in
      *docker.m.daocloud.io*|*pypi.tuna.tsinghua.edu.cn*|*goproxy.cn*|sum.golang.google.cn)
        mirrored+=("$value")
        ;;
    esac
  done
  if (( ${#mirrored[@]} > 0 )); then
    log_warn "local profile output contains mainland China mirror values while --china was not passed."
    log_warn "continuing with user configuration; remove those values from .env.local or rerun with --china."
  fi
}

prepare_knowledge_runtime_deps() {
  if [[ "$SKIP_KNOWLEDGE_RUNTIME_DEPS" == "1" || "${LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS:-0}" == "1" ]]; then
    log_info "skipping Knowledge runtime dependency preparation"
    return
  fi

  if ! command -v uv >/dev/null 2>&1; then
    log_error "uv is required to prepare Knowledge runtime dependencies; install uv or rerun with --skip-knowledge-runtime-deps"
    return 1
  fi

  (
    cd "$ROOT_DIR/services/knowledge-runtime"
    uv run --no-project \
      --with "nltk>=3.9.4" \
      --with "huggingface-hub>=1.3.1" \
      ragflow_deps/download_deps.py --china
  )
}

migrate_service() {
  service="$1"
  database_url="$2"
  CURRENT_STEP="migrating $service"
  log_info "${CURRENT_STEP}"
  (
    cd "$ROOT_DIR/services/$service"
    go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$database_url" up
  )
  log_success "${CURRENT_STEP} succeeded"
}

parse_args "$@"
trap on_exit EXIT

log_info "starting infra, migrations, and seed"

export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
# shellcheck disable=SC1090
. "$CONFIG_LOADER"

if (( CHINA_MIRRORS )); then
  apply_china_mirrors
else
  export UV_DEFAULT_INDEX="${UV_DEFAULT_INDEX:-$OFFICIAL_UV_DEFAULT_INDEX}"
  export GOPROXY="${GOPROXY:-$OFFICIAL_GOPROXY}"
  export GOSUMDB="${GOSUMDB:-$OFFICIAL_GOSUMDB}"
  warn_legacy_mirror_env
fi

compose=(docker compose -f "$COMPOSE_FILE" --env-file "$CONFIG_COMPOSE_ENV_FILE")

apply_ai_gateway_local_seed_overlay() {
  case "${AI_GATEWAY_LOCAL_SEED_ENABLED:-}" in
    ""|0|false|False|FALSE|no|No|NO|off|Off|OFF)
      log_info "AI_GATEWAY_LOCAL_SEED_ENABLED is not true; keeping seeded placeholder model profiles"
      return
      ;;
  esac

  go run "$ROOT_DIR/scripts/local/render_ai_gateway_local_seed.go" |
    psql "$POSTGRES_ADMIN_URL" -v ON_ERROR_STOP=1
}

run_step "checking local tool dependencies" check_required_commands
if (( CHINA_MIRRORS )); then
  run_step "preparing Knowledge runtime dependencies with China mirrors" prepare_knowledge_runtime_deps
fi
run_step "validating Docker Compose config" "${compose[@]}" config --quiet
run_step "pulling infrastructure images" "${compose[@]}" pull "${PULL_SERVICES[@]}"
run_step "starting infrastructure and waiting for health" "${compose[@]}" up -d --wait --wait-timeout "${LOCAL_INFRA_WAIT_TIMEOUT_SECONDS:-180}" "${INFRA_SERVICES[@]}"
run_minio_init

run_step "checking Go module settings" ensure_go_module_settings

for item in \
  "auth:$AUTH_DATABASE_URL" \
  "file:$FILE_DATABASE_URL" \
  "knowledge:$KNOWLEDGE_DATABASE_URL" \
  "qa:$QA_DATABASE_URL" \
  "document:$DOCUMENT_DATABASE_URL" \
  "ai-gateway:$AI_GATEWAY_DATABASE_URL"; do
  service="${item%%:*}"
  database_url="${item#*:}"
  migrate_service "$service" "$database_url"
done

CURRENT_STEP="applying local demo seed"
log_info "${CURRENT_STEP}"
psql "$POSTGRES_ADMIN_URL" \
  -v ON_ERROR_STOP=1 \
  -f "$ROOT_DIR/deploy/seeds/001-local-demo-seed.sql" \
  -f "$ROOT_DIR/deploy/seeds/002-ai-gateway-model-profiles.sql" \
  -f "$ROOT_DIR/deploy/seeds/003-qa-document-mcp.sql" \
  -f "$ROOT_DIR/deploy/seeds/004-qa-default-knowledge-base.sql"
log_success "${CURRENT_STEP} succeeded"

run_step "applying AI Gateway local env seed overlay" apply_ai_gateway_local_seed_overlay

log_success "infra, migrations, and seed are ready"

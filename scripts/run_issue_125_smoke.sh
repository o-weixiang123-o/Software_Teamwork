#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_LOADER="$ROOT_DIR/scripts/config/load-profile.sh"
NO_PROXY_VALUE="${NO_PROXY:-localhost,127.0.0.1,::1}"
export NO_PROXY="$NO_PROXY_VALUE"

url_from_addr() {
  local addr="$1"
  local fallback_port="$2"
  if [[ -z "$addr" ]]; then
    printf 'http://127.0.0.1:%s' "$fallback_port"
    return
  fi
  if [[ "$addr" == http://* || "$addr" == https://* ]]; then
    printf '%s' "${addr%/}"
    return
  fi
  addr="${addr#http://}"
  addr="${addr#https://}"
  if [[ "$addr" == :* ]]; then
    printf 'http://127.0.0.1%s' "$addr"
    return
  fi
  if [[ "$addr" == 0.0.0.0:* || "$addr" == "[::]:"* ]]; then
    printf 'http://127.0.0.1:%s' "${addr##*:}"
    return
  fi
  printf 'http://%s' "$addr"
}

PROFILE_LOADED=0

load_profile_defaults() {
  if (( PROFILE_LOADED )); then
    return
  fi
  export SOFTWARE_TEAMWORK_ROOT="$ROOT_DIR"
  # shellcheck disable=SC1090
  . "$CONFIG_LOADER"

  : "${GATEWAY_BASE_URL:=$(url_from_addr "${GATEWAY_HTTP_ADDR:-}" 8080)}"
  : "${FILE_SERVICE_BASE_URL:=$(url_from_addr "${FILE_HTTP_ADDR:-}" 8082)}"
  : "${KNOWLEDGE_SERVICE_BASE_URL:=$(url_from_addr "${KNOWLEDGE_HTTP_ADDR:-}" 8083)}"
  : "${QA_SERVICE_BASE_URL:=$(url_from_addr "${QA_HTTP_ADDR:-}" 8084)}"
  : "${DOCUMENT_SERVICE_BASE_URL:=$(url_from_addr "${DOCUMENT_HTTP_ADDR:-}" 8085)}"
  export GATEWAY_BASE_URL FILE_SERVICE_BASE_URL KNOWLEDGE_SERVICE_BASE_URL
  export QA_SERVICE_BASE_URL DOCUMENT_SERVICE_BASE_URL
  PROFILE_LOADED=1
}

usage() {
  cat <<'USAGE'
Issue #125 MCP and cross-service smoke entry point.

Usage:
  bash scripts/run_issue_125_smoke.sh --list
  bash scripts/run_issue_125_smoke.sh --auth
  bash scripts/run_issue_125_smoke.sh --auth-full
  bash scripts/run_issue_125_smoke.sh --file-owner
  bash scripts/run_issue_125_smoke.sh --qa-rag
  bash scripts/run_issue_125_smoke.sh --document-rest
  bash scripts/run_issue_125_smoke.sh --document-mcp
  bash scripts/run_issue_125_smoke.sh --all

Local defaults:
  Start the stack with .env.local, ./scripts/local/dev-up.sh, and
  ./scripts/local/run-backend.sh. This script loads the selected config profile
  without overriding variables already exported in the shell.

Useful overrides:
  QA_MCP_RAG_REAL_PROVIDER=1 AI_GATEWAY_BASE_URL=http://127.0.0.1:8086  # only when AI Gateway has a real provider profile

Document MCP smoke derives the local endpoint/token from the selected config profile when
possible. Override MCP_SERVER_URL, MCP_SERVER_TOKEN, or MCP_SERVER_TOKEN_HEADER
only when testing a non-default Document MCP endpoint.

The script only enables the selected smoke gate. Individual Go tests validate
their own required environment variables and skip/fail honestly when external
services, seed data, parser images, or provider profiles are unavailable.
USAGE
}

run_deploy_smoke() {
  local gate="$1"
  local test_name="$2"
  load_profile_defaults
  (
    cd "$ROOT_DIR/services/deploy/smoke"
    env "$gate=1" go test . -run "^${test_name}$" -count=1 -v
  )
}

run_document_mcp() {
  load_profile_defaults
  (
    cd "$ROOT_DIR/services/qa"
    export MCP_TRANSPORT="${MCP_TRANSPORT:-streamable_http}"
    export MCP_SERVER_ALIAS="${MCP_SERVER_ALIAS:-document}"
    export MCP_SERVER_URL="${MCP_SERVER_URL:-${DOCUMENT_SERVICE_BASE_URL%/}/mcp}"
    export MCP_SERVER_TOKEN="${MCP_SERVER_TOKEN:-${DOCUMENT_MCP_SERVICE_TOKEN:-${INTERNAL_SERVICE_TOKEN:-}}}"
    export MCP_SERVER_TOKEN_HEADER="${MCP_SERVER_TOKEN_HEADER:-${DOCUMENT_MCP_TOKEN_HEADER:-Authorization}}"
    env QA_DOCUMENT_MCP_SMOKE=1 go test ./internal/platform/mcpclient -run '^TestDocumentMCPReportToolsSmoke$' -count=1 -v
  )
}

list_smokes() {
  cat <<'SMOKES'
Available #125 smoke slices:
  --auth           Auth/Gateway/Redis session lifecycle, spoofed header rejection, Redis token-cache safety
  --auth-full      Issue #352 full Auth/Gateway/Redis smoke: infra, Auth migrations, host-run Auth/Gateway, fake owner header capture
  --file-owner     File internal token protection plus Gateway -> Document public-response no-leak checks
  --qa-rag         Gateway -> QA MCP RAG, SSE, tool-call summary, citation snapshot, final-answer checks
  --document-rest  Gateway -> Document REST contract and error/no-leak checks
  --document-mcp   QA -> Document Streamable HTTP MCP report tool discovery/status/result/export checks
  --all            Run every slice in the order above
SMOKES
}

if [[ $# -eq 0 ]]; then
  usage
  exit 2
fi

for arg in "$@"; do
  case "$arg" in
    --help|-h)
      usage
      ;;
    --list)
      list_smokes
      ;;
    --auth)
      run_deploy_smoke AUTH_GATEWAY_REDIS_SMOKE TestAuthGatewayRedisSmoke
      ;;
    --auth-full)
      bash "$ROOT_DIR/scripts/run_issue_352_smoke.sh"
      ;;
    --file-owner)
      run_deploy_smoke FILE_OWNER_E2E_SMOKE TestFileOwnerE2ESmoke
      ;;
    --qa-rag)
      run_deploy_smoke QA_MCP_RAG_SMOKE TestQAMCPRAGSmoke
      ;;
    --document-rest)
      run_deploy_smoke DOCUMENT_REST_SMOKE TestDocumentRESTContractSmoke
      ;;
    --document-mcp)
      run_document_mcp
      ;;
    --all)
      run_deploy_smoke AUTH_GATEWAY_REDIS_SMOKE TestAuthGatewayRedisSmoke
      bash "$ROOT_DIR/scripts/run_issue_352_smoke.sh"
      run_deploy_smoke FILE_OWNER_E2E_SMOKE TestFileOwnerE2ESmoke
      run_deploy_smoke QA_MCP_RAG_SMOKE TestQAMCPRAGSmoke
      run_deploy_smoke DOCUMENT_REST_SMOKE TestDocumentRESTContractSmoke
      run_document_mcp
      ;;
    *)
      echo "unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

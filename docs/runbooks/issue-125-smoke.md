# Issue #125 MCP and Cross-Service Smoke

This runbook is the single entry point for S-008 / issue #125 smoke execution.
It composes existing targeted smoke tests without claiming that unavailable
provider, Knowledge runtime, Docker, or seed dependencies passed.

## Start Local Stack

Use the local integration baseline first:

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

Docker only starts the infrastructure services listed in
`deploy/docker-compose.yml`: `postgres`, `redis`, `minio`, `minio-init`, and
`elasticsearch`. Auth, File, Knowledge, AI Gateway, QA, Document, and
Gateway run on the host through `run-backend.sh`; do not use Compose profiles,
business-service containers, or `--build` for this smoke.

Defaults use official Docker/PyPI/Go/GitHub sources. For mainland China
networks, use explicit mirror mode for the affected step:

```bash
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china
```

`dev-up.sh --china` includes Knowledge runtime dependency and GitHub release/raw
artifact preparation. If you intentionally skipped it with
`--skip-knowledge-runtime-deps`, rerun the runtime download script manually
before claiming Knowledge runtime coverage.

If image pulls, Knowledge runtime dependency downloads, GitHub release/raw
downloads, or Go module downloads are blocked, use
[`Docker 基础设施镜像拉取环境`](./docker-image-pull-environment.md) and record the
blocked dependency instead of marking the full smoke as passed.

## Common Environment

`scripts/run_issue_125_smoke.sh` reads `deploy/.env` when it exists, without
overriding variables already exported in the shell. The script derives the
default Gateway, owner-service, AI Gateway, Redis, admin, and Document MCP
settings from that file and the standard host-run ports.

Set per-run overrides only when the environment differs from the local baseline.
For example, set `GATEWAY_BASE_URL`, `FILE_SERVICE_BASE_URL`,
`QA_SERVICE_BASE_URL`, `DOCUMENT_SERVICE_BASE_URL`, `GATEWAY_SMOKE_USERNAME`, or
`GATEWAY_SMOKE_PASSWORD` before invoking the script.

AI Gateway profile/provider prerequisites are still owned by #234. The local
`default-chat` / `local-placeholder-chat` seed only proves profile bootstrap;
real QA answers require a reachable OpenAI-compatible provider or controlled
stub profile.

## Smoke Commands

List slices:

```bash
bash scripts/run_issue_125_smoke.sh --list
```

Run the Auth/Gateway/Redis baseline:

```bash
bash scripts/run_issue_125_smoke.sh --auth
```

Run the full issue #352 Auth/Gateway/Redis smoke, including infra startup,
Auth migration apply, host-run Auth/Gateway processes, Gateway user creation,
Redis token-cache safety, logout invalidation, and fake owner-service header
capture:

```bash
bash scripts/run_issue_125_smoke.sh --auth-full
# equivalent:
bash scripts/run_issue_352_smoke.sh
```

The full smoke starts only `postgres` and `redis` from the root Compose file.
Business services still run on the host. If Docker is unavailable, image pulls
are blocked, PostgreSQL/Redis never become healthy, or Go is unavailable, the
script exits with a `blocked:` reason instead of reporting a business failure.
Optional CI is available through the manual workflow
`Auth Gateway Redis Smoke` (`.github/workflows/auth-gateway-redis-smoke.yml`);
it is not a required PR check.

Run File owner-service E2E:

```bash
bash scripts/run_issue_125_smoke.sh --file-owner
```

Knowledge document upload is not part of this File owner slice. Current
Knowledge document paths are covered by the Knowledge runtime and Gateway RAG
smokes instead of by File dependency failure checks.

Run QA MCP RAG:

```bash
bash scripts/run_issue_125_smoke.sh --qa-rag
```

The command always validates Gateway, QA, Knowledge, login, and knowledge-base
availability. Message-sending QA RAG assertions require a real AI Gateway
provider profile; enable them explicitly:

```bash
export QA_MCP_RAG_REAL_PROVIDER=1
export AI_GATEWAY_BASE_URL=http://127.0.0.1:8086
bash scripts/run_issue_125_smoke.sh --qa-rag
```

Run Document REST:

```bash
bash scripts/run_issue_125_smoke.sh --document-rest
```

Run Document MCP report tools:

```bash
bash scripts/run_issue_125_smoke.sh --document-mcp
```

The Document MCP slice defaults to `MCP_TRANSPORT=streamable_http`,
`MCP_SERVER_ALIAS=document`, `${DOCUMENT_SERVICE_BASE_URL}/mcp`, and the local
Document MCP service token from `deploy/.env`. Override the `MCP_SERVER_*`
variables only when testing a non-default endpoint.

Run all slices:

```bash
bash scripts/run_issue_125_smoke.sh --all
```

## Coverage Matrix

| Slice | Gate | Verifies |
| --- | --- | --- |
| Auth/Gateway/Redis | `AUTH_GATEWAY_REDIS_SMOKE=1` | Gateway login, `/users/me`, spoofed header rejection, logout invalidation, Redis `gateway:session:*` TTL/value safety. |
| Auth/Gateway/Redis full | `AUTH_GATEWAY_REDIS_FULL_SMOKE=1` | Auth migration apply, host-run Auth/Gateway startup, Gateway `POST /api/v1/users`, `/api/v1/sessions`, `/api/v1/users/me`, logout invalidation, Redis TTL/value/token safety, and fake owner capture of Gateway-injected `X-Caller-Service`, `X-Service-Token`, `X-User-Id`, roles, and permissions. |
| File owner E2E | `FILE_OWNER_E2E_SMOKE=1` | Gateway authentication rejection, File internal missing-token rejection, Gateway -> Document public-response no-leak checks. Current Knowledge document paths use the Knowledge runtime boundary instead of File. |
| QA MCP RAG | `QA_MCP_RAG_SMOKE=1` | Knowledge tool availability, QA SSE `tool.completed` / `citation.delta` / `answer.completed`, safe tool-call summary, citation snapshots, final answer evidence. |
| Document REST | `DOCUMENT_REST_SMOKE=1` | Gateway -> Document report type/report/outlines routes, 404 envelope, and no-leak checks. |
| Document MCP | `QA_DOCUMENT_MCP_SMOKE=1` | Document Streamable HTTP MCP tools/list, `document__*` prefixing, generation status/result/export, `reportArtifact`, permission-denied summary, optional Gateway file download. |

## Failure Rules

- Public business requests must go through Gateway `/api/v1/**`.
- Smoke failures should include request ids such as `req_*_smoke_*`; search
  Gateway and owner-service logs by that id.
- Do not paste bearer tokens, service tokens, API keys, File object keys,
  bucket names, MinIO URLs, full prompts, full MCP arguments, or raw MCP
  results into issue comments or reports.
- If a capability is not ready, the smoke should be `skipped`, `expected
  failure`, or `blocked` with the missing dependency. Do not fake a pass.

## Common Triage

| Stage | Likely failure | Action |
| --- | --- | --- |
| Auth/Gateway/Redis | `redis is not reachable` | Check `docker compose ps redis`, `GATEWAY_REDIS_ADDR`, and `NO_PROXY`. |
| Auth/Gateway/Redis full | `blocked: auth migration apply failed` | Check PostgreSQL is healthy, `AUTH_GATEWAY_REDIS_DATABASE_URL` points at the Auth database, and `deploy/postgres/init/001-create-databases.sql` ran for a fresh volume. |
| Auth/Gateway/Redis full | `blocked: postgres/redis infrastructure did not become healthy` | Check Docker daemon state, registry rewrite/image pulls, and `docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps`. |
| File owner | `no knowledge bases available` | Run local seed or create a knowledge base through Gateway first. |
| Knowledge runtime | Runtime dependency preparation or startup fails | Check Knowledge runtime API/worker logs, `UV_DEFAULT_INDEX`, `VENDOR_RUNTIME_URL`, and runtime prerequisites; record the blocked dependency if it cannot be prepared. |
| Go services | Go module download fails during host-run startup | Check the terminal summary and relevant `.local/logs/*.log` for `proxy.golang.org` timeout. On mainland China networks rerun the local script with `--china`; otherwise confirm `deploy/.env` or enterprise env contains a reachable `GOPROXY` / `GOSUMDB`. |
| QA RAG | AI Gateway/provider unavailable | Check #234 profile seed/provider setup; placeholder profiles are not real provider proof. |
| Document MCP | `initialize MCP session` / unauthorized | Check Document `/mcp`, `MCP_SERVER_TOKEN`, `DOCUMENT_MCP_SERVICE_TOKEN`, and token header. |

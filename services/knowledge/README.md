# Knowledge Service

Knowledge exposes Gateway `/internal/v1/*` contract routes via the **contract
adapter** (`cmd/adapter`). KB metadata, documents, chunks, queries, and upload
flow through the RAGFlow runtime at `VENDOR_RUNTIME_URL` (`services/knowledge-runtime/`;
deepdoc + Elasticsearch + MinIO).

Parser-config admin routes (`/internal/v1/parser-configs`) optionally use legacy
goose PostgreSQL tables when `DATABASE_URL` or `KNOWLEDGE_DATABASE_URL` is set.

## Runtime

- Go module: `go 1.25.0`
- Binary: `cmd/adapter` only (legacy `cmd/server` removed in Phase 5)
- HTTP: standard `net/http` `ServeMux`
- Logging: `log/slog`
- Parser-config storage: `pgx/v5` + hand-written SQL (optional)

See `../knowledge-runtime/README.md` for host-run vendor runtime wiring.
The MCP transport and QA integration workflow are documented in
[`docs/services/knowledge/docs/mcp-server.md`](../../docs/services/knowledge/docs/mcp-server.md).

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `VENDOR_RUNTIME_URL` | yes | `http://127.0.0.1:9380` | RAGFlow vendor HTTP base URL. |
| `VENDOR_RUNTIME_SERVICE_TOKEN` | yes | - | Token forwarded to the runtime as `X-Service-Token`; must match `KNOWLEDGE_RUNTIME_SERVICE_TOKEN`. |
| `KNOWLEDGE_SERVICE_TOKEN` / `INTERNAL_SERVICE_TOKEN` | yes | - | Shared service token required on `/internal/v1/**` via `X-Service-Token`. |
| `DATABASE_URL` / `KNOWLEDGE_DATABASE_URL` | no | - | PostgreSQL for parser-config admin; omit to return `502` on those routes. |
| `KNOWLEDGE_HTTP_ADDR` | no | `:8083` | HTTP listen address. |
| `KNOWLEDGE_SERVICE_VERSION` | no | `dev` | Version returned by readiness checks. |
| `KNOWLEDGE_ENV` | no | `local` | Runtime environment label. |
| `KNOWLEDGE_AUTO_START_INGESTION` | no | `true` | Call vendor `/documents/parse` after upload. |
| `KNOWLEDGE_VENDOR_EMBEDDING_ID` | no | - | Runtime embedding model id forwarded on dataset creation and reported in retrieval trace when configured. |
| `KNOWLEDGE_VENDOR_RERANK_ID` | no | - | Runtime rerank model id forwarded on retrieval when rerank is requested. |
| `KNOWLEDGE_SHUTDOWN_TIMEOUT` | no | `10s` | Graceful shutdown timeout. |
| `KNOWLEDGE_MCP_ADDR` | no | - | Optional Streamable HTTP MCP listen address, for example `127.0.0.1:8093`. |
| `KNOWLEDGE_MCP_USER_ID` | no | `knowledge_mcp_service` | Fixed user id used by MCP bridge calls. |
| `KNOWLEDGE_PROJECT_RUNTIME_USER_ID` | no | `KNOWLEDGE_MCP_USER_ID` | Runtime user id whose visible datasets form the project-wide QA RAG pool. |
| `KNOWLEDGE_MCP_PERMISSIONS` | no | `knowledge:read` | Fixed permission set used by MCP bridge calls; write tools require `knowledge:write`. |
| `KNOWLEDGE_MCP_ROLES` | no | - | Fixed role set used by MCP bridge calls. |
| `KNOWLEDGE_AI_GATEWAY_URL` | no | - | Enables `answer_from_knowledge` MCP tool by calling AI Gateway chat completions. |
| `KNOWLEDGE_AI_GATEWAY_SERVICE_TOKEN` | no | `INTERNAL_SERVICE_TOKEN` fallback | Service token for Knowledge -> AI Gateway chat calls. |

Upload storage and vector retrieval are configured in the vendor runtime
(`services/knowledge-runtime/conf/service_conf.yaml`): MinIO bucket
`software-teamwork-knowledge`, doc engine `elasticsearch`.
Knowledge does not call File Service, Qdrant, Redis, or `services/parser`.

## Implemented Routes

Operational routes:

- `GET /healthz`
- `GET /readyz`

Internal service routes:

All `/internal/v1/**` routes require a matching `X-Service-Token` before
user identity and permission headers are trusted.

Document singleton routes (`/internal/v1/documents/{documentId}` and its
`/chunks` and `/content` children) require `knowledgeBaseId` as a query
parameter. The adapter uses that explicit runtime dataset context and does not
scan all knowledge bases to infer it.

- `GET /internal/v1/knowledge-bases`
- `POST /internal/v1/knowledge-bases`
- `GET /internal/v1/knowledge-bases/{knowledgeBaseId}`
- `PATCH /internal/v1/knowledge-bases/{knowledgeBaseId}`
- `DELETE /internal/v1/knowledge-bases/{knowledgeBaseId}`
- `GET /internal/v1/knowledge-bases/{knowledgeBaseId}/documents`
- `POST /internal/v1/knowledge-bases/{knowledgeBaseId}/documents`
- `GET /internal/v1/documents/{documentId}`
- `GET /internal/v1/documents/{documentId}/chunks`
- `GET /internal/v1/documents/{documentId}/content`
- `PATCH /internal/v1/documents/{documentId}`
- `DELETE /internal/v1/documents/{documentId}`
- `POST /internal/v1/knowledge-queries`
- `GET|POST|PATCH|DELETE /internal/v1/parser-configs[/**]` (requires `DATABASE_URL` or `KNOWLEDGE_DATABASE_URL`)

Public gateway equivalents are documented in
`docs/services/gateway/api/public.openapi.yaml`.

## MCP Server

When `KNOWLEDGE_MCP_ADDR` is set, `cmd/adapter` also starts a Streamable HTTP
MCP server on that independent address. The endpoint uses `X-Service-Token`,
does not trust caller-supplied `X-User-*` headers, and calls the adapter with
the fixed `KNOWLEDGE_MCP_USER_ID` / roles / permissions context.

The current native tool catalog includes retrieval, answer synthesis, KB CRUD,
document CRUD, chunk listing, and document content tools. Runtime details and
the gap to the four-tool `knowledge__*` target contract are documented in
[`docs/services/knowledge/docs/mcp-server.md`](../../docs/services/knowledge/docs/mcp-server.md)
and [`docs/services/knowledge/docs/mcp-tools.md`](../../docs/services/knowledge/docs/mcp-tools.md).

## Access Context

Business routes require gateway-injected `X-User-Id` (from Auth service).
The adapter forwards this as vendor tenant context; vendor login/JWT is disabled.

Supported permission strings:

- `knowledge:read`
- `knowledge:write`
- `knowledge:admin` / `admin:parser-config:write` for parser-config admin

Rules:

- Read routes require `knowledge:read` or `knowledge:write` (or admin roles).
- Mutations require `knowledge:write` (or admin roles).
- Trusted QA retrieval is the exception: QA uses service-token authenticated
  `knowledge-queries` for project-wide RAG, so a QA user does not need
  Knowledge management `knowledge:read` merely to ask questions.
- Vendor errors map to standard `{error}` envelopes.

## Data Model

Goose migrations under `migrations/` retain legacy tables (`knowledge_bases`,
`parser_configs`, etc.) for parser-config admin. Vendor metadata uses separate
RAGFlow tables in the same PostgreSQL database when vendor PG is enabled.

## Local Integration Notes

Root Compose only starts shared infrastructure. Start the vendor Python API
(:9380) and task executor on the host, then run the adapter with
`VENDOR_RUNTIME_URL` and `VENDOR_RUNTIME_SERVICE_TOKEN` pointing at that runtime
as documented in
`../knowledge-runtime/README.md`.

For the real host-run Knowledge parsing stack, use the root helper script. It
starts `services/knowledge-runtime` API, runtime worker, and the Knowledge
adapter, and forces adapter auto-ingestion on for upload-to-parse diagnostics:

```bash
./scripts/local/run-knowledge-parse-stack.sh
python3 scripts/local/knowledge-pdf-e2e.py DL_T_673-1999.pdf
```

The helper normalizes local wiring that is easy to get wrong by hand:

- `VENDOR_RUNTIME_URL=host.docker.internal` from an old `.env` is not used for
  the host-run adapter; the script defaults to `http://127.0.0.1:9380`.
- The runtime URL host is added to `NO_PROXY`, so shell proxy settings do not
  intercept adapter calls to localhost or Docker bridge IPs.
- Old local `.env` files that lack the runtime service token use the tracked
  local development token defaults for `scripts/local` only.
- To reuse an already running runtime API, set
  `KNOWLEDGE_PARSE_VENDOR_RUNTIME_URL=http://<runtime-host>:9380`; non-loopback
  URLs automatically switch the script to external-runtime mode and start only
  the Knowledge adapter.

The PDF smoke creates an isolated knowledge base, uploads the PDF through the
adapter, waits for runtime parsing to finish, checks chunk count, executes a
`knowledge-queries` retrieval, prints the query hit count and previews, then
cleans up the created resources unless `KNOWLEDGE_PDF_E2E_KEEP_RESOURCES=1`.

`services/parser` is retired; document parsing uses vendor deepdoc.

## Migrations

Apply the service-owned migration with the project-pinned `goose@v3.27.1` command:

```bash
go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$DATABASE_URL" up
```
## Development

```bash
go test ./internal/adapter/... ./internal/adapterconfig/... ./internal/service/...
go build ./cmd/adapter
```

The Knowledge service runs the contract adapter (`cmd/adapter`) which proxies
Gateway `/internal/v1/*` routes to the RAGFlow runtime at
`VENDOR_RUNTIME_URL` (`services/knowledge-runtime/`). Document upload, deepdoc parsing, embedding, and retrieval
use runtime MinIO + Elasticsearch — not legacy parser, Qdrant, or
the removed Go ingestion worker.

Contract tests under `internal/adapter` and `internal/mcp` use fake vendor HTTP
servers or in-memory MCP transports. Live vendor tests require
`-tags=integration` and `KNOWLEDGE_VENDOR_INTEGRATION_URL`.

For end-to-end ingestion diagnostics, start both Knowledge runtime processes
(`services/knowledge-runtime/deploy/api/run-local.sh` and
`services/knowledge-runtime/deploy/worker/run-local.sh`) before the adapter. The
adapter `/readyz` checks the runtime API and task executor heartbeat; if uploads
stay in `parsing`, first inspect `/internal/v1/runtime/status` and the runtime
worker logs. Start or restart the adapter with `KNOWLEDGE_AUTO_START_INGESTION=true`;
the smoke will fail fast with an `uploaded` status if the adapter was started
with auto-start disabled. With real dependencies available, run:

```bash
# Set before starting the adapter process:
# export KNOWLEDGE_AUTO_START_INGESTION=true

KNOWLEDGE_INGESTION_SMOKE=1 \
KNOWLEDGE_SERVICE_BASE_URL=http://127.0.0.1:8083 \
INTERNAL_SERVICE_TOKEN=local-dev-internal-service-token-change-me \
go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v
```

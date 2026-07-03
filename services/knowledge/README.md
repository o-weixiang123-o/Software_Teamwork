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
| `KNOWLEDGE_RUNTIME_READINESS_MODE` | no | `ingestion` | Runtime readiness mode: `ingestion` requires the runtime task executor heartbeat; `query` only requires runtime API/query dependencies and still reports task executor status. |
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
Knowledge does not call File Service, Redis, or `services/parser`.

## Implemented Routes

Operational routes:

- `GET /healthz`
- `GET /readyz`
- `GET /internal/v1/runtime/status` (internal diagnostics; requires
  `X-Service-Token`)

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
Gateway user id is the audit and authorization context. Runtime calls use
`KNOWLEDGE_PROJECT_RUNTIME_USER_ID` when configured so normal users with
`knowledge:read` can read and search the shared project KB pool; otherwise the
adapter falls back to the caller user id as runtime tenant context. Vendor
login/JWT is disabled.

Supported permission strings:

- `knowledge:read`
- `knowledge:write`
- `knowledge:admin`
- `system:admin`
- `admin:parser-config:write` for parser-config admin

Rules:

- Read routes require `knowledge:read` or write/admin permissions. Standard
  users are expected to have `knowledge:read`, so they can list visible
  knowledge bases, inspect documents/chunks/content, and run direct
  `knowledge-queries`.
- Mutations require `knowledge:write`, `knowledge:admin`, or `system:admin`.
  Standard users must not create, update, delete, upload, or remove documents
  unless they are explicitly granted one of those write permissions.
- Document singleton reads (`GET /documents/{documentId}`, `/chunks`,
  `/content`) require the caller to provide `knowledgeBaseId` so the adapter can
  resolve the exact runtime dataset and document. Project-scope QA retrieval
  does not bypass this read authorization.
- Trusted QA retrieval is limited to `POST /knowledge-queries`: QA may use
  service-token authenticated project-wide RAG for answer generation, but
  citation source lookup and document downloads still go back through normal
  Knowledge read checks with `knowledgeBaseId`.
- Vendor errors map to standard `{error}` envelopes. Runtime authentication
  failures, including a mismatched `VENDOR_RUNTIME_SERVICE_TOKEN` /
  `KNOWLEDGE_RUNTIME_SERVICE_TOKEN`, are downstream dependency failures and
  should surface as `502 dependency_error`, not as browser login expiration.

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

For the real host-run Knowledge parsing stack, use the root helper scripts. The
default root Compose infrastructure starts Elasticsearch as the active runtime
doc engine; the runtime helper starts `services/knowledge-runtime` API, runtime worker, and
the Knowledge adapter, and forces adapter auto-ingestion on for upload-to-parse
diagnostics. First copy `deploy/.env.example` to `deploy/.env`, then fill the
runtime model provider variables documented in `../knowledge-runtime/README.md`.

```bash
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-parse-stack.sh
python3 scripts/local/knowledge-pdf-e2e.py DL_T_673-1999.pdf
```

For query-only validation against an already-built knowledge base, start only
the runtime API:

```bash
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-runtime-api.sh
```

That API-only helper uses the base Python dependency profile and does not start
`knowledge-runtime-worker`. Use `run-knowledge-parse-stack.sh` when uploads
must enqueue and consume parse work.

The helper normalizes local wiring that is easy to get wrong by hand:

- `VENDOR_RUNTIME_URL=host.docker.internal` from an old `.env` is not used for
  the host-run adapter; the script defaults to `http://127.0.0.1:9380`.
- The runtime URL host is added to `NO_PROXY`, so shell proxy settings do not
  intercept adapter calls to localhost or Docker bridge IPs.
- Old local `.env` files that lack the runtime service token use the tracked
  local development token defaults for `scripts/local` only.
- For `DOC_ENGINE=elasticsearch`, `./scripts/local/dev-up.sh` starts the root
  Compose `elasticsearch` service with the default local infrastructure.
- The script generates `.local/knowledge-runtime/service_conf.yaml` so runtime
  API and worker use `KNOWLEDGE_RUNTIME_ES_URL`.
- To reuse an already running runtime API, set
  `KNOWLEDGE_PARSE_VENDOR_RUNTIME_URL=http://<runtime-host>:9380`; non-loopback
  URLs automatically switch the script to external-runtime mode and start only
  the Knowledge adapter.

The PDF smoke creates an isolated knowledge base, uploads the PDF through the
adapter, waits for runtime parsing to finish, checks chunk count, executes a
`knowledge-queries` retrieval, prints the query hit count and previews, then
cleans up the created resources unless `KNOWLEDGE_PDF_E2E_KEEP_RESOURCES=1`.

`services/parser` is retired; document parsing uses vendor deepdoc.

For query-first deployments over an already-built knowledge base, set
`KNOWLEDGE_RUNTIME_READINESS_MODE=query` so `/readyz` does not require the
runtime task executor heartbeat. When uploads should parse documents, keep
`KNOWLEDGE_AUTO_START_INGESTION=true`; the adapter can invoke the configured
`KNOWLEDGE_RUNTIME_WORKER_START_COMMAND` when no worker heartbeat is present,
waits for that heartbeat, then calls `/documents/parse` to enqueue work.
The local helper stops the worker after the queue stays idle for
`KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS` (default 300 seconds).
Production should point that command at deployment infrastructure such as KEDA,
systemd, supervisor, or an equivalent lifecycle controller. The
Kubernetes/KEDA example lives at
[`../../deploy/k8s/knowledge-runtime-worker-keda.example.yaml`](../../deploy/k8s/knowledge-runtime-worker-keda.example.yaml).
Set `KNOWLEDGE_AUTO_START_INGESTION=false` only for deployments that should
upload without queuing `/documents/parse`.

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
use runtime MinIO + Elasticsearch — not legacy parser or
the removed Go ingestion worker.

Contract tests under `internal/adapter` and `internal/mcp` use fake vendor HTTP
servers or in-memory MCP transports. Live vendor tests require
`-tags=integration` and `KNOWLEDGE_VENDOR_INTEGRATION_URL`.

For end-to-end ingestion diagnostics, start the Knowledge runtime API
(`services/knowledge-runtime/deploy/api/run-local.sh`) before the adapter. Start
the runtime worker separately with
`services/knowledge-runtime/deploy/worker/run-local.sh` when no controlled worker
starter is configured. The adapter `/readyz` checks the runtime API and, in the
default `KNOWLEDGE_RUNTIME_READINESS_MODE=ingestion` mode, the task executor
heartbeat. In `query` mode, readiness can pass without the worker. Upload
ingestion still queues `/documents/parse` when `KNOWLEDGE_AUTO_START_INGESTION`
is true; if `KNOWLEDGE_RUNTIME_WORKER_START_COMMAND` is set and no worker
heartbeat is present, the adapter runs that command and waits for the heartbeat
before queueing parse work. The local worker helper also stops the worker after
the queue stays idle; set `KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS=0` to
disable that local scale-down behavior.
If uploads stay in `parsing`, inspect `/internal/v1/runtime/status` and the
runtime worker logs. Start or restart the adapter with
`KNOWLEDGE_AUTO_START_INGESTION=true`; the smoke will fail fast with an
`uploaded` status if the adapter was started with auto-start disabled. With real
dependencies available, run:

```bash
# Set before starting the adapter process:
# export KNOWLEDGE_AUTO_START_INGESTION=true
# export KNOWLEDGE_RUNTIME_READINESS_MODE=query
# export KNOWLEDGE_RUNTIME_WORKER_START_COMMAND=/path/to/controlled-worker-start
# export KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT=10m

KNOWLEDGE_INGESTION_SMOKE=1 \
KNOWLEDGE_SERVICE_BASE_URL=http://127.0.0.1:8083 \
INTERNAL_SERVICE_TOKEN=local-dev-internal-service-token-change-me \
go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v
```

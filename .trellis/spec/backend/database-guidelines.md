# Database Guidelines

> Database, cache, vector-search, and object-storage conventions for Go backend services.

---

## Overview

Each backend service owns its persistence concerns. A service may use
PostgreSQL, Redis, MinIO, or runtime-owned index stores only through
service-local repository, platform packages, or documented owner-service
adapters. Handlers must not talk directly to infrastructure clients.

Confirmed Go infrastructure target stack:

- PostgreSQL: `pgx` + `sqlc`.
- Migrations: `goose@v3.27.1`.
- Redis cache/session access: `go-redis`.
- Redis queues: `asynq v0.26.0`.
- Runtime-owned index stores: current Knowledge indexing and retrieval are
  behind `services/knowledge-runtime`; do not add a Go Qdrant client for the
  default Knowledge path.
- Object storage: File Service owns an `ObjectStore` port. Production target is
  MinIO or an equivalent persistent object-store adapter; the MinIO adapter is
  implemented behind the same port.

Current repository facts from `docs/architecture/technology-decisions.md`:

- Auth, Knowledge, QA, Document, File, and AI Gateway use `pgx/v5@v5.9.2`.
  New PostgreSQL services should reuse that major version and must not
  reintroduce `pgx/v4` or a third pgx major version without updating the
  technology baseline.
- The repository-wide recommended `sqlc` CLI version is `v1.31.1`. Regenerate
  service query packages with
  `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`; do not use an
  unpinned `sqlc generate` command in docs, CI, or handoff notes.
- Gateway directly uses `go-redis/v9@v9.21.0`; Knowledge Go adapter does not
  directly use go-redis. Knowledge runtime uses its own Redis/worker internals
  behind the RAGFlow runtime boundary.
- Document has fixed `asynq v0.26.0`; new Go asynchronous jobs should reuse
  that version unless a documented decision upgrades it. Knowledge ingestion
  jobs must not restore the retired Go asynq worker path.
- File Service runtime has memory/local/MinIO object-store adapters, a
  PostgreSQL metadata repository, migrations, and service-token validation.
  `FILE_DATABASE_URL` being empty is a local/test fallback, not the production
  persistence baseline.
- Knowledge owns retrieval-facing business contracts, but current vector/index
  persistence is inside `services/knowledge-runtime` (RAGFlow runtime, currently
  Elasticsearch/doc engine). The Go adapter must not restore a Go Qdrant client,
  File Service upload handoff, or Go ingestion worker. QA, Gateway, Document,
  and AI Gateway must not read or mutate runtime index/storage internals.

Do not introduce an ORM by default. If a service needs one, document the reason
in that service README, update `docs/architecture/technology-decisions.md`,
and then update this spec.

---

## PostgreSQL Ownership

- Each service owns the tables it writes.
- Do not let one service write another service's tables.
- Cross-service data needs should go through HTTP APIs, events, or explicit read-model decisions.
- Table schemas must be represented by migrations under `services/<service>/migrations/`.
- Services that use PostgreSQL must keep service-local `sqlc.yaml`, query files,
  generated `sqlc` code, and `goose` migrations. Generated query structs must
  not leak into HTTP handlers.

Use PostgreSQL for:

- user identities, roles, permissions, sessions, and tokens metadata,
- file metadata and processing states,
- knowledge metadata and ingestion status,
- document generation jobs and outputs metadata,
- audit-friendly business state.

---

## Query Patterns

- Use parameterized queries only. Never concatenate user input into SQL.
- Keep SQL in repository methods or dedicated query files.
- Keep repository methods small and named by intent, not by SQL operation.
- Return domain-oriented structs from repositories; do not leak raw DB rows into handlers.
- Pass `context.Context` through every database call.
- Use pagination for list endpoints.
- Use explicit column lists instead of `SELECT *`.

Example repository shape:

```go
type UserRepository struct {
    db *pgxpool.Pool
}

func (r *UserRepository) FindByID(ctx context.Context, id UserID) (User, error) {
    const query = `
        SELECT id, email, display_name, created_at
        FROM users
        WHERE id = $1
    `
    // scan and wrap errors here
}
```

---

## Transactions

- Start transactions at the service/use-case layer when one business operation changes multiple records.
- Keep transaction bodies short and deterministic.
- Pass transaction handles into repositories through explicit interfaces.
- Roll back on every error and wrap rollback failures only when they add useful context.
- Do not perform slow external calls while holding a PostgreSQL transaction.

## Scenario: PostgreSQL pgx v5 Service Baseline

### 1. Scope / Trigger

- Trigger: adding a PostgreSQL-backed Go service, changing a service's pgx/sqlc
  dependency, regenerating sqlc code, or touching repository pool/transaction
  wiring.
- Applies to `services/<service>/go.mod`, `services/<service>/sqlc.yaml`,
  `services/<service>/internal/repository/**`, generated sqlc code, service
  startup wiring, and service implementation docs.

### 2. Signatures

- Module dependency: `github.com/jackc/pgx/v5 v5.9.2`.
- sqlc config: `sql_package: "pgx/v5"`.
- sqlc generation command:
  `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`.
- Pool import: `github.com/jackc/pgx/v5/pgxpool`.
- Pool constructor: `pgxpool.New(ctx, databaseURL)`.
- Startup reachability check: `pool.Ping(ctx)` when the service is expected to
  fail fast on PostgreSQL unavailability.
- Transaction handle: `github.com/jackc/pgx/v5.Tx`.
- Error type: `github.com/jackc/pgx/v5/pgconn.PgError`.
- Generated pgtype package: `github.com/jackc/pgx/v5/pgtype`.

### 3. Contracts

- Repository adapters may import pgx, pgxpool, pgconn, pgtype, and service-local
  generated sqlc packages.
- HTTP handlers, service business logic, gateway code, and other services must
  not import another service's generated sqlc package.
- Generated sqlc rows and pgtype values must be mapped to service-domain structs
  before leaving `internal/repository`.
- New PostgreSQL services must not introduce `github.com/jackc/pgx/v4` or a
  third pgx major version without updating `docs/architecture/technology-decisions.md`.
- New or regenerated query packages must use the pinned `sqlc@v1.31.1`
  command. Knowledge and QA may still contain older generated code until their
  SQL changes, but the next SQL edit must regenerate with the pinned command
  and commit the result.
- Migrating from `pgxpool.Connect` to `pgxpool.New` must not silently change
  startup behavior. If the old service failed startup when PostgreSQL was
  unreachable, call `pool.Ping(ctx)` before registering the repository.
- Migration behavior is part of the repository contract; changing pgx/sqlc
  wiring still requires goose apply validation for services with migrations.

### 4. Validation & Error Matrix

| Condition | Required response |
| --- | --- |
| `go.mod` or module graph contains `github.com/jackc/pgx/v4` | Remove the v4 dependency or document a formal technology-baseline exception before merging. |
| `sqlc.yaml` uses `sql_package: "pgx/v4"` | Regenerate sqlc with `pgx/v5` and update repository mapping code. |
| Startup code calls a removed/old pool API | Use the pgx v5 pool API, for example `pgxpool.New(ctx, databaseURL)`. |
| Startup must fail fast but only calls `pgxpool.New` | Add `pool.Ping(ctx)` during startup and close the pool on ping failure. |
| HTTP handler imports pgx or generated sqlc | Move database access behind the repository/service boundary. |
| pgtype values leak into service or HTTP response models | Add repository mapping helpers that return domain structs. |
| Migrations fail against an empty PostgreSQL database | Fix migrations or document the remaining risk before PR. |

### 5. Good/Base/Bad Cases

- Good: service `go.mod` requires `pgx/v5@v5.9.2`, `sqlc.yaml` uses
  `pgx/v5`, startup preserves required PostgreSQL reachability checks,
  repository maps `pgtype.Text` / `pgtype.Timestamptz` to domain fields,
  generated query code is refreshed with `sqlc@v1.31.1`, handlers import only
  service interfaces, and goose applies cleanly.
- Base: an existing service migration preserves public/internal API semantics
  and only changes dependency/import/mapping code plus docs.
- Bad: leaving v4 in `go.mod`, hand-editing generated sqlc code without
  updating `sqlc.yaml`, returning generated sqlc rows from handlers, or
  treating unit tests as a substitute for migration apply validation.

### 6. Tests Required

- Run `go test ./...` from the changed service directory.
- Run `go build ./cmd/server` for runnable services.
- Run `go list -m all` and scan for `github.com/jackc/pgx/v4`.
- Scan `services/<service>/internal/http` for pgx and generated sqlc imports.
- Run `git diff --check`.
- For services with migrations, run:

```bash
go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$DATABASE_URL" up
```

### 7. Wrong vs Correct

#### Wrong

```go
import "github.com/jackc/pgx/v4/pgxpool"

pool, err := pgxpool.Connect(ctx, databaseURL)
```

#### Correct

```go
import "github.com/jackc/pgx/v5/pgxpool"

pool, err := pgxpool.New(ctx, databaseURL)
if err != nil {
    return err
}
if err := pool.Ping(ctx); err != nil {
    pool.Close()
    return err
}
```

---

## Migrations

- Store `goose` migrations in `services/<service>/migrations/`.
- Use forward-only migrations for the first implementation slice unless rollback
  is explicitly supported and verified by the service.
- SQL migrations executed by goose must include `-- +goose Up`; include `-- +goose Down` only when the down path is supported and verified by the service.
- Name migrations with an ordered prefix and action summary:

```text
0001_create_users.sql
0002_add_file_processing_state.sql
```

- CI should validate migrations when migration tooling is introduced.
- Schema changes must be backward-compatible when multiple services or deployments may overlap.

## Scenario: AI Gateway Provider Invocation Logging

### 1. Scope / Trigger

- Trigger: adding or changing AI Gateway model invocation persistence, provider
  attempts, chat/embedding/rerank usage summaries, or provider error recording.
- Applies to `services/ai-gateway/internal/service`,
  `services/ai-gateway/internal/repository`,
  `services/ai-gateway/internal/provider`, and
  `services/ai-gateway/migrations`.

### 2. Signatures

- Internal model route:
  - `POST /internal/v1/chat/completions`.
- Database tables:
  - `provider_invocations`.
  - `provider_invocation_attempts`.
- Repository boundary:
  - `RecordProviderInvocation(ctx, invocation, attempts)`.
  - Active credential reads must use a dedicated method such as
    `GetActiveCredential(ctx, profileID)`.

### 3. Contracts

- Provider HTTP calls must happen outside PostgreSQL transactions.
- `provider_invocations` may store request id, caller service, external user id,
  operation, profile id, provider, model, stream flag, status, provider status,
  token usage, duration, attempt count, and normalized error summary.
- `provider_invocation_attempts` may store invocation id, attempt number,
  provider, base URL host only, model, status, provider status, duration, and
  normalized error summary.
- Do not store full request bodies, `messages`, prompt text, generated answer
  text, tool schemas, full tool arguments, tool results, provider bearer tokens,
  API keys, full provider URLs, or raw provider response bodies.
- For streaming calls, use a cancellation-independent short context when writing
  the final invocation summary so caller cancellation can still be recorded.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Missing/default chat profile | OpenAI-style `not_found_error` or `invalid_request_error` |
| Disabled or wrong-purpose profile | OpenAI-style `invalid_request_error` |
| Missing active credential | OpenAI-style `invalid_request_error` |
| Credential decrypt failure | OpenAI-style `upstream_error` without secret details |
| Provider `401` | OpenAI-style authentication category; no raw body |
| Provider `403` | OpenAI-style permission category; no raw body |
| Provider `429` | OpenAI-style rate limit category; no raw body |
| Provider `5xx` or non-contract response | OpenAI-style upstream error; no raw body |
| Provider timeout | record status `timeout` |
| Caller stream cancellation | record status `cancelled` when observable |
| Invocation record write failure after provider success | return an upstream/internal dependency error without leaking payloads |

### 5. Good/Base/Bad Cases

- Good: service selects a safe chat profile, decrypts the credential only for
  the provider request, calls provider outside a transaction, then persists a
  sanitized invocation plus one attempt.
- Base: first slice records one attempt per call; retry/fallback can add more
  attempts later without changing the public model invocation contract.
- Bad: storing `messages`, tool arguments, provider raw error bodies, full base
  URLs with path/query, API keys, bearer tokens, or prompt hashes as metrics or
  database fields.

### 6. Tests Required

- Provider tests with fake HTTP servers for success, `401`, `403`, `429`, `5xx`,
  timeout, malformed/non-contract response, and stream cancellation.
- Handler tests asserting chat success is OpenAI-compatible and not wrapped in
  `{ data, requestId }`.
- Function-calling tests asserting `tools`, `tool_choice`,
  `parallel_tool_calls`, `assistant.tool_calls`, `tool_call_id`, and streaming
  `delta.tool_calls` are passed through without AI Gateway executing tools.
- Safety tests asserting responses and invocation summaries do not contain API
  keys, bearer tokens, prompt text, full tool arguments, tool schemas, tool
  results, or raw provider bodies.
- Migration validation with `goose@v3.27.1`; run a real PostgreSQL apply when a
  local or CI database URL is available.

### 7. Wrong vs Correct

#### Wrong

```text
chat handler -> provider HTTP call inside DB transaction -> store messages and raw provider error body for debugging
```

#### Correct

```text
chat handler -> service selects profile and decrypts credential -> provider HTTP call outside transaction -> repository stores sanitized invocation and attempt summary
```

---

## Naming Conventions

PostgreSQL naming:

- Tables: plural snake_case, for example `users`, `knowledge_items`.
- Columns: snake_case, for example `created_at`, `owner_user_id`.
- Primary keys: `id`.
- Foreign keys: `<entity>_id`.
- Indexes: `idx_<table>_<columns>`.
- Unique indexes: `uniq_<table>_<columns>`.

Use UTC timestamps and name them consistently:

- `created_at`
- `updated_at`
- `deleted_at` for soft delete only when the service actually supports it.

---

## Redis

Use Redis for short-lived data only:

- sessions or token deny-lists,
- cache entries,
- short-lived coordination,
- `asynq` queues.

Rules:

- Every cache key must have a stable prefix: `<service>:<resource>:<id>`.
- Every cache entry must have an explicit TTL unless it is intentionally persistent.
- Redis must not be the only source of durable business truth.
- Cache invalidation must be owned by the service that owns the underlying data.
- Queued task payloads must be JSON and include traceable fields such as
  `requestId`, `jobId`, and `userId` when available. PostgreSQL remains the
  authority for durable job state, final status, failure summary, and retry
  count.

---

## Runtime-Owned Index Stores

Use index backends only behind their owner-service boundary. Current Knowledge
document parsing, chunking, embedding/index persistence, and retrieval support
belong behind `services/knowledge-runtime` and its doc engine. The Go adapter
owns public/internal DTOs and authorization, not direct index mutation.

Rules:

- Store durable owner metadata in PostgreSQL; keep vector/index payloads inside
  the runtime/doc-engine boundary unless a documented DTO exposes a safe
  summary.
- Keep any returned index payload fields minimal and retrieval-oriented.
- Version embedding models and runtime/doc-engine index metadata when the
  embedding shape changes.
- Do not let `qa` or `document` mutate Knowledge runtime indexes directly; they
  should retrieve through `knowledge` or a documented MCP/retrieval API.
- Do not let `ai-gateway` write vector/index stores; model generation and vector
  persistence remain separate service responsibilities.

---

## Object Storage

Use the File Service object-store boundary for object payloads:

- uploaded source files,
- extracted text artifacts if they are too large for PostgreSQL,
- generated documents,
- temporary processing outputs when needed.

Rules:

- Store object metadata and ownership in PostgreSQL.
- Use bucket names that map to domain purpose, not implementation detail.
- Generate object keys server-side.
- Never expose raw internal object keys as authorization decisions.
- Prefer pre-signed URLs only after checking ownership and permission in the service.
- `FILE_STORAGE_BACKEND=memory` is for tests and early local development only.
  `local` is acceptable for durable local smoke tests. Production must use
  MinIO or an equivalent persistent object-store adapter.
- MinIO SDK usage must stay inside the File Service platform/storage adapter and
  process wiring. Handlers, owner-service clients, and service use cases depend
  only on the service-owned `ObjectStore` port.
- When adding an HTTP transport timeout for object-store clients, do not cancel
  the request context immediately when `RoundTrip` returns. For streaming
  responses, wrap the response body and cancel on `Body.Close()` so content
  reads are not interrupted.

---

## Common Mistakes

- Treating Redis cache entries as durable workflow state.
- Storing full documents in PostgreSQL when MinIO is the correct storage layer.
- Duplicating knowledge metadata between PostgreSQL and Qdrant without a source-of-truth rule.
- Running external HTTP calls inside PostgreSQL transactions.
- Letting `qa` bypass `knowledge` and directly own retrieval logic.

## Scenario: File Service Base Object Storage

### 1. Scope / Trigger

- Trigger: adding or changing File Service base object upload, metadata persistence, object storage adapters, deletion cleanup, or `/internal/v1/files/**` routes.
- Applies to `services/file/internal/service`, `services/file/internal/http`, `services/file/internal/repository`, `services/file/internal/platform/storage`, `services/file/migrations`, and `services/file/api/openapi.yaml`.

### 2. Signatures

- Internal API routes:
  - `POST /internal/v1/files` with multipart field `file` and optional `checksumSha256`.
  - `GET /internal/v1/files/{fileId}`.
  - `DELETE /internal/v1/files/{fileId}`.
  - `GET /internal/v1/files/{fileId}/content`.
- Runtime environment:
  - `FILE_ENV`, falling back to `ENV`, then `local`. Local-like values are
    `local`, `test`, and `development`.
  - `FILE_STORAGE_BACKEND=memory|local|minio`.
  - `FILE_LOCAL_STORAGE_DIR` when using `local`.
  - `FILE_MINIO_ENDPOINT`, `FILE_MINIO_ACCESS_KEY`,
    `FILE_MINIO_SECRET_KEY`, and `FILE_MINIO_BUCKET` when using `minio`.
  - Optional `FILE_MINIO_USE_SSL`, `FILE_MINIO_REGION`, and
    `FILE_MINIO_TIMEOUT`.
  - Optional `FILE_ALLOWED_CONTENT_TYPES` comma-separated effective MIME
    allowlist.
  - Optional `FILE_ALLOWED_CREATE_CALLERS`,
    `FILE_ALLOWED_READ_CALLERS`, and `FILE_ALLOWED_DELETE_CALLERS`
    comma-separated `X-Caller-Service` allowlists.
- Database files:
  - `services/file/sqlc.yaml`.
  - `services/file/internal/repository/queries/file_objects.sql`.
  - `services/file/migrations/0001_create_file_objects.sql` or later forward-only migrations.
- Storage adapters implement the service-owned `ObjectStore` port: `Put(ctx, key, body, contentType, sizeBytes)`, `Get(ctx, key)`, `Delete(ctx, key)`.

### 3. Contracts

- File metadata responses may expose only `id`, `filename`, `contentType`, `sizeBytes`, `checksumSha256`, `createdAt`, and `deletedAt`.
- Responses and logs must not expose `storage_bucket`, `storage_object_key`, object-store URLs, local filesystem paths, access keys, or secret keys.
- PostgreSQL is the durable source of metadata, deletion status, purge timestamps, and sanitized purge failure summaries.
- Object keys are generated server-side from file IDs, never from user filenames.
- The service layer must not `io.ReadAll` the full upload before calling the
  object-store port. It may read a bounded sniff prefix, then stream the full
  content through a hashing/counting reader into `ObjectStore.Put`.
- `FILE_STORAGE_BACKEND=memory` is test/local-only; `local` is acceptable for local durable smoke tests; production should use MinIO or an equivalent persistent object store adapter.
- Non-local `FILE_ENV` values must reject `FILE_STORAGE_BACKEND=memory` and an
  empty `FILE_DATABASE_URL` during config load or startup.
- `FILE_ALLOWED_CONTENT_TYPES` is checked against the sniffed/effective MIME
  type, not blindly against the multipart `Content-Type` header. Unknown binary
  content falls back to `application/octet-stream`, which must be in the
  allowlist when the allowlist is configured.
- Caller allowlists are operation-scoped: create, read metadata/content, and
  delete are configured independently. Empty allowlists preserve local
  compatibility; configured allowlists require `X-Caller-Service`.
- MinIO adapter errors returned from the storage layer must be sanitized:
  `NoSuchKey` / missing object maps to `service.ErrNotFound`, timeout and
  cancellation preserve `context` errors, and other SDK failures map to a
  dependency error without embedding bucket names, object keys, endpoints, or
  credentials.
- MinIO upload calls must preserve content type and enable SDK checksum support
  such as `PutObjectOptions.SendContentMd5`.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing multipart `file` | `400 validation_error` |
| Empty file | `400 validation_error` |
| Oversized/malformed multipart | `400 validation_error` |
| Invalid or mismatched `checksumSha256` | `400 validation_error` |
| Effective MIME not in `FILE_ALLOWED_CONTENT_TYPES` | `400 validation_error` |
| Missing trusted caller context | `401 unauthorized` |
| Caller allowlist configured but `X-Caller-Service` missing | `401 unauthorized` |
| Caller present but not allowed for create/read/delete | `403 forbidden` |
| Non-local config uses memory object storage | startup/config error |
| Non-local config has no `FILE_DATABASE_URL` | startup/config error |
| File missing, deleted, or purged | `404 not_found` |
| Storage write/read/delete failure | `502 dependency_error` |
| Metadata write/read/update failure | `502 dependency_error` |

### 5. Good/Base/Bad Cases

- Good: handler parses multipart and writes only envelope/content headers; service reads a bounded sniff prefix, streams bytes through checksum/counting into `ObjectStore.Put`, generates object key, coordinates repository plus object store, and repository persists explicit file-object columns.
- Base: a local storage adapter persists bytes under a configured directory for smoke tests while preserving the same `ObjectStore` interface and production guardrails remain disabled only in local-like environments.
- Bad: handler imports MinIO or SQL packages, service buffers the full upload to compute checksum, response DTO includes `objectKey` or `bucket`, or owner services use object keys for authorization.

### 6. Tests Required

- Config tests for `FILE_ENV` fallback, non-local memory backend rejection,
  non-local empty `FILE_DATABASE_URL` rejection, MIME allowlist parsing, and
  create/read/delete caller allowlist parsing.
- Handler tests for malformed multipart, missing file, empty file, oversized file, checksum mismatch, caller allowlist `401`/`403`, successful content stream headers, and reads after delete.
- Service tests for streaming into `ObjectStore.Put`, checksum computation/validation, MIME sniffing/allowlist behavior, object key creation, delete state transitions, and storage dependency error mapping.
- Storage adapter tests for put/get/delete, size mismatch, context cancellation where practical, and path traversal rejection for local storage.
- MinIO adapter tests for content type and checksum options, not-found mapping,
  sanitized dependency errors, size mismatch, and timeout/cancellation behavior.
- Repository or migration validation once database test tooling is available.

### 7. Wrong vs Correct

#### Wrong

```text
HTTP handler receives upload -> writes object directly to MinIO -> returns objectKey in JSON
```

#### Correct

```text
HTTP handler parses multipart -> service sniffs prefix and streams upload into ObjectStore while hashing -> repository stores safe metadata -> response returns safe FileObject fields only
```

#### Wrong

```text
custom RoundTripper adds context.WithTimeout -> defer cancel() before returning response body -> content reads fail mid-stream
```

#### Correct

```text
custom RoundTripper adds context.WithTimeout -> wraps response body -> cancel happens when Body.Close() is called
```

## Scenario: Knowledge Document Upload And Runtime Ingestion

### 1. Scope / Trigger

- Trigger: adding or changing Knowledge document upload, RAGFlow runtime
  document handoff, parser-config mapping, runtime ingestion start, or runtime
  document status/chunk/content mapping.
- Applies to `services/knowledge/internal/adapter`,
  `services/knowledge/internal/vendorclient`,
  `services/knowledge/internal/service`, `services/knowledge/internal/repository`
  parser-config code, `services/knowledge/api/openapi.yaml`,
  `services/knowledge-runtime/**`, and Knowledge runtime configuration.

### 2. Signatures

- Internal Knowledge route:
  - `POST /internal/v1/knowledge-bases/{knowledgeBaseId}/documents`
    with multipart field `file` and optional `tags`.
- RAGFlow runtime calls through the vendor client:
  - upload document bytes under the runtime dataset/document API.
  - optional parse start when `KNOWLEDGE_AUTO_START_INGESTION=true`.
  - document status, chunks, content, and retrieval are read back through the
    runtime API and mapped to Knowledge DTOs.
- Required runtime environment keys for upload:
  - `VENDOR_RUNTIME_URL`.
  - `VENDOR_RUNTIME_SERVICE_TOKEN`.
  - `KNOWLEDGE_RUNTIME_SERVICE_TOKEN` on the runtime side.
  - Optional: `KNOWLEDGE_AUTO_START_INGESTION`,
    `KNOWLEDGE_MAX_UPLOAD_BYTES`.

### 3. Contracts

- Knowledge owns document resources, public document status, permissions,
  parser-config administration, response envelopes, and error mapping.
- RAGFlow runtime owns raw bytes, parser task execution, chunks, embedding,
  index writes, retrieval support, and runtime storage/search internals.
- Public or service-local document responses may expose `jobId`, status, display
  filename, content type, size, and tags, but must not expose `fileRef`,
  File Service internal IDs, object keys, buckets, MinIO/internal URLs, raw text,
  vectors, prompts, or tokens.
- Runtime PostgreSQL/Redis/MinIO/Elasticsearch or other doc engine internals are
  not public product facts. Knowledge PostgreSQL currently supports
  parser-config admin and migration-compatible fields, not the document bytes or
  chunk source of truth.
- The Go adapter must not reintroduce `FILE_SERVICE_BASE_URL`,
  `PARSER_SERVICE_BASE_URL`, a Go Qdrant client, or a Go asynq ingestion worker
  for Knowledge document ingestion.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing user context | `401 unauthorized` |
| Missing `knowledge:write` permission | `403 forbidden` |
| Missing or empty multipart `file` | `400 validation_error` |
| Malformed or oversized multipart body | `400 validation_error` |
| Invalid `knowledgeBaseId` or hidden knowledge base | `404 not_found` |
| Invalid `tags` shape or value | `400 validation_error` |
| Runtime validation failure | `400 validation_error` owned by Knowledge |
| Runtime dependency/internal failure | `502 dependency_error` with sanitized runtime detail |
| Runtime upload succeeds but parse start fails | attempt runtime document cleanup when possible, then return classified dependency error |
| Runtime returns invalid status/chunk/content shape | `502 dependency_error`; do not leak raw vendor body |

### 5. Good/Base/Bad Cases

- Good: handler parses multipart, validates Knowledge permissions, uploads bytes
  through the vendor runtime client, optionally starts runtime parsing, maps the
  runtime document response to a public `DocumentSummary`, and sanitizes runtime
  failures.
- Base: runtime E2E is environment-gated while adapter unit tests use fake
  runtime clients to verify route paths, response mapping, and error envelopes.
- Bad: Knowledge restores File Service upload handoff, stores raw file bytes in
  Go service tables, returns `fileRef`/`fileId` publicly, or treats a Go
  Redis/asynq queue as the current Knowledge ingestion source of truth.

### 6. Tests Required

- Adapter tests for success, missing file, malformed multipart, tags,
  permission failure, runtime upload/parse failures, response mapping, and no
  public runtime/file internals leakage.
- Vendor client tests with mocked HTTP server asserting runtime route paths,
  context headers, service token, safe downstream error mapping, redirect
  blocking, and cleanup behavior where implemented.
- Parser-config repository tests when parser-config storage changes.
- Runtime route/config tests when `services/knowledge-runtime/**` changes.
- Service-local checks from `services/knowledge`: `go test ./...`,
  `go build ./cmd/adapter`, and `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```text
Knowledge upload -> File Service stores business metadata -> Go asynq worker parses -> response exposes fileId
```

#### Correct

```text
Knowledge upload -> Knowledge adapter -> RAGFlow runtime stores bytes and parses/chunks/indexes -> response exposes Knowledge document summary
```

## Scenario: Document Service Report Baseline

### 1. Scope / Trigger

- Trigger: adding or changing Document Service report-generation tables, job persistence, sqlc queries, migrations, queue identifiers, or dependency configuration.
- Applies to `services/document/internal/service`, `services/document/internal/repository`, `services/document/migrations`, `services/document/sqlc.yaml`, and `services/document/internal/config`.

### 2. Signatures

- Database migration files:
  - `services/document/migrations/0001_create_report_generation_tables.sql` or later ordered migrations.
- SQL files:
  - `services/document/sqlc.yaml`.
  - `services/document/internal/repository/queries/*.sql`.
  - Generated code under `services/document/internal/repository/sqlc/`.
- Required runtime environment keys:
  - `DOCUMENT_DATABASE_URL`.
  - `DOCUMENT_REDIS_ADDR`.
  - `DOCUMENT_FILE_SERVICE_URL`.
  - `DOCUMENT_AI_GATEWAY_URL`.
  - `DOCUMENT_AI_GATEWAY_PROFILE_ID`.

### 3. Contracts

- PostgreSQL owns durable report state for report types, templates, materials, reports, outlines, sections, section versions, jobs, attempts, events, files, and operation logs.
- `report_jobs`, `report_job_attempts`, and `report_events` are the durable authority for job status, retry history, failure summaries, and public progress events.
- Report job progress JSON must use numeric `completed` and `total` fields. Terminal status updates (`succeeded`, `partial_succeeded`, `failed`, `canceled`) must preserve existing meaningful progress such as `1/2` from multi-step generation, and only write default progress (`1/1` for success-like states, `0/1` for failure-like states) when no detailed progress exists.
- `reports.status`, `reports.latest_job_id`, and `reports.generated_at` are a denormalized public snapshot of generation lifecycle, not the detailed job authority. Generation job `pending`/`running` states must set `latest_job_id` and the matching generating report status. Terminal `partial_succeeded` keeps the report in the closest usable generated state (`outline_generated` for outline jobs, `generated` for content or section jobs), while `failed` and `canceled` map to report `failed`. Content or section `succeeded` and `partial_succeeded` jobs set `generated_at`.
- Worker-driven job status writes (`running`, `succeeded`, `partial_succeeded`, `failed`) must never overwrite a job that has already been marked `canceled` through the user/API path. Cancellation is an explicit user terminal state; a worker that loses the race must re-read the job, mark the current attempt canceled, and stop before invoking the model or finalizing success/failure.
- `report_sections.outline_id` scopes sections to the outline version that created
  or owns them. Report-level `content_generation` and `content_regeneration`
  must generate only sections whose `outline_id` matches the current
  `report_outlines.is_current` row. `section_regeneration` is the explicit
  section-targeted exception and may regenerate the requested section after the
  same-report ownership check.
- AI-generated outline creation and the derived `report_sections` skeleton
  creation are one business write. They must run in one short repository
  transaction after the AI response is parsed; if any skeleton insert fails,
  the new current outline, previous-current flag updates, and partial skeletons
  must all roll back.
- When building JSON with PostgreSQL parameters, cast ambiguous parameters explicitly, for example `jsonb_build_object('completed', $2::int, 'total', $3::int)`, so pgx/PostgreSQL can infer parameter types in integration tests and production.
- Redis/asynq may store queue payloads, delivery metadata, and task identifiers only. It must not be the only source of report job or event truth.
- File bytes for templates, materials, and generated report files belong to the File Service. Document tables may persist only service-internal file references and display metadata, never MinIO object keys or bucket names.
- Repository methods return service-layer domain structs, not generated sqlc rows or raw driver types.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing required config value | startup validation error |
| Invalid file or AI Gateway base URL | startup validation error |
| Invalid report/job UUID at repository boundary | `validation_error` |
| Missing report job | `not_found` |
| Duplicate report/job/attempt/event ID | `conflict` |
| Section skeleton insert fails after creating a current outline | rollback outline/current-flag/partial sections and return dependency error |
| PostgreSQL connect/query failure | wrapped dependency error |

### 5. Good/Base/Bad Cases

- Good: service creates a report job row in PostgreSQL, records attempts/events in PostgreSQL, stores only the asynq task ID for queue correlation, and commits AI outline plus derived section skeletons atomically.
- Base: the first implementation slice provides schema, repository, transactions, health checks, and readiness checks without implementing AI generation or DOCX export.
- Bad: worker stores final job status only in Redis, repository returns sqlc rows to HTTP handlers, outline generation leaves a new current outline with missing/partial skeletons after failure, or public responses/logs expose `file_ref`, object keys, prompts, provider raw errors, or database details.

### 6. Tests Required

- Config tests for required Document Service dependency keys and invalid URL rejection.
- Handler tests for `/healthz` and `/readyz` response envelopes, request ID propagation, and dependency failure status.
- Repository integration tests, gated by `DOCUMENT_TEST_DATABASE_URL`, that apply migrations and verify report type, report, job, attempt, event, and transaction behavior.
- Repository integration tests for multi-step jobs must assert `UpdateReportJobProgress` survives terminal status updates instead of being overwritten by generic `1/1` or `0/1` defaults.
- Repository integration tests must assert a canceled report job cannot be overwritten by later worker `running`, `succeeded`, or `failed` status updates.
- Service tests for content generation must cover old and current outline
  sections and assert progress totals include only current outline sections.
- Service tests must simulate section skeleton creation failure and assert the
  new current outline and partial skeletons are rolled back.
- Repository integration tests, gated by `DOCUMENT_TEST_DATABASE_URL`, should
  cover the report-generation transaction by creating an outline and section
  inside the transaction, forcing an error, and asserting rollback.
- Build and package checks from `services/document`: `go test ./...`, `go build ./cmd/server`, `sqlc generate`, and migration apply against an empty PostgreSQL database when migration tooling is available.

### 7. Wrong vs Correct

#### Wrong

```text
asynq task executes -> Redis stores final job status -> API reads Redis as truth
outline generation -> insert current outline -> skeleton insert fails -> incomplete current outline remains
```

#### Correct

```text
API creates report_job -> asynq task id is stored for correlation -> worker updates report_jobs/report_job_attempts/report_events in PostgreSQL
outline generation -> parse AI response outside transaction -> transaction inserts current outline + skeletons -> rollback all on failure
```

## Scenario: Document Section Version Current Switch

### 1. Scope / Trigger

- Trigger: adding or changing Document report section version creation, manual
  section edit snapshotting, or section-regeneration overwrite behavior.
- Applies to `services/document/internal/service/report_service.go`,
  `services/document/internal/service/report_generation_service.go`,
  `services/document/internal/http/reports.go`,
  `services/document/internal/repository/reports.go`, and the matching Document
  and Gateway OpenAPI section-version schemas.

### 2. Signatures

- HTTP route: `POST /reports/{reportId}/sections/{sectionId}/versions`.
- Request fields: `source` (`manual` or `ai`), optional `requirements`,
  optional `content`, optional `tables`.
- Response fields: `id`, `reportId`, `sectionId`, `version`, `source`,
  optional `content`, optional `tables`, optional `jobId`, `createdAt`.
- Durable tables: `report_sections` and `report_section_versions`.

### 3. Contracts

- `report_sections.version` is the current section version reference unless a
  future reviewed migration adds `current_version_id`.
- Creating a section version must insert `report_section_versions` and switch
  the current `report_sections` content/tables/version/source flags in the same
  `ReportRepository.WithinTx` operation.
- Deleted reports are not valid section-version write targets. A report with
  `ReportStatusDeleted` or non-nil `DeletedAt` must return `409 conflict`
  before inserting `report_section_versions` or updating `report_sections`;
  re-check and lock the report row inside the write transaction before the
  insert and current-section switch.
- The next version number must be greater than both `ReportSection.version` and
  every existing `ReportSectionVersion.version`.
- Manual content or table edits through section save/update paths must create a
  `manual` section-version snapshot in the same transaction as the current
  section update.
- Manual section update/save paths must lock and re-check the report row inside
  the write transaction before mutating sections. For each existing section
  they update, they must also re-read and lock the current section inside the
  transaction, require same-report ownership, and reject
  `generation_status = running` with `409 conflict` before writing content,
  tables, version, source, manual-edit state, or manual section-version rows.
- AI generation may call the model outside the database transaction, but the
  final generated content update plus `report_section_versions` insert must run
  in one short `WithinGenerationTx` operation.
- Before calling the model for a section, the generation service must persist
  `generation_status = running` and `last_job_id = <current job>`. If that
  marker update fails, return `dependency_error`, record `section.failed`, keep
  progress at the current completed count, and do not call the AI provider. A
  failed running-marker write is infrastructure failure, not a stale response
  skip.
- Before persisting a successful generated section after the model call, re-read
  and lock the target section inside `WithinGenerationTx`. The current section
  must still have `last_job_id` equal to the executing job, `generation_status =
  running`, and the same `version` / `manual_edited` state captured when the job
  marked the section running. If any of those checks fail, the transaction may
  return an internal `409 conflict`, but the generation service must treat that
  section as skipped: preserve the current section content, tables, version,
  source, manual edit state, `last_job_id`, and generation status; update job
  progress and record a `section.skipped` event; do not create an AI
  section-version row from the stale response.
- If generated content persistence fails after the transaction rolls back,
  failure compensation must use a narrow section status update only
  (`generation_status`, `last_job_id`, `updated_at`). It must not write a stale
  full `report_sections` snapshot over `content`, `tables_json`, `version`,
  `content_source`, or `manual_edited`, and should require `last_job_id` to
  still match the failed generation job before marking `failed`.
- For report-level content generation, a non-conflict generated-content
  persistence failure such as `report_section_versions` insert failure must be
  treated as a terminal outcome for only that section: roll back the generated
  content switch, mark that section failed with the narrow compensation update,
  increment `report_jobs.progress.completed`, record `section.failed`, and
  continue later sections. If at least one section succeeds or is skipped, the
  job finishes `partial_succeeded`; if every attempted section fails, the job
  fails with the first section error.
- A generated-section success transaction returning `409 conflict` because the
  section changed or the job was superseded is not a generation persistence
  failure. The executor must continue on a non-error result path so the worker
  does not call `markFailed`; the stale AI response must leave the current
  section status intact.
- Manual edit preservation defaults to true. `preserveUserEdits=false` is the
  public option; `preserveManualEdits=false` remains a backward-compatible
  alias. Only an explicit false value may overwrite manual edits.

### 4. Validation & Error Matrix

| Condition | Required response / behavior |
| --- | --- |
| `source` is not `manual` or `ai` | `400 validation_error` |
| Report is soft-deleted by status or `deleted_at` | `409 conflict`; do not create a version or mutate the current section |
| Target section belongs to another report | `404 not_found` |
| Target section has `generation_status = running` | `409 conflict`; do not create a version |
| Manual update/save write transaction sees a deleted report or running section | `409 conflict`; do not mutate the current section or create a manual version |
| Running-marker update before AI section generation fails | `dependency_error`; record `section.failed`; update progress with the current completed count; do not call the AI provider, increment completed progress, create a version, or mark the section skipped |
| Successful AI response finds a different `last_job_id`, non-running status, changed version, or changed manual-edit state | Skip the stale section on the non-error execution path; update progress and `section.skipped`; do not create a version, overwrite current section content, or mark the section/job/report failed |
| Version insert succeeds but current-section switch fails | Roll back inserted version and return typed dependency/not-found error |
| AI generated content update succeeds but version insert fails | Roll back the generated content switch; mark only that section failed with a narrow, current-job-matched status update, count the section as attempted progress, record `section.failed`, and continue later report-level sections |
| Manual edit changes only metadata | Do not create a new section version |

### 5. Good/Base/Bad Cases

- Good: `CreateSectionVersion` creates version 4, updates
  `report_sections.version=4`, and returns the same version in one transaction.
- Base: a manual metadata-only title/numbering save updates the current section
  without adding a history row.
- Bad: inserting `report_section_versions` and returning success while
  `report_sections.version` still points at the previous content.

### 6. Tests Required

- Service tests for conflict while generation is running.
- Service tests for deleted-report rejection, including the case where the
  report is deleted after the entry check but before the transactional insert.
- Service tests for version creation plus current-section switch in one
  transaction.
- Rollback tests where current-section update fails after version insertion.
- Manual edit snapshot tests for single-section update and bulk save.
- Manual edit race tests for single-section update and bulk save must simulate
  a report deleted after the entry check and a section becoming
  `generation_status = running` after the entry check; both cases must return
  `409 conflict`, preserve current section content/version/status, and create
  no manual section-version row.
- Generation tests for default preserve behavior, explicit
  `preserveUserEdits=false`, and rollback when version insertion fails.
- Generation tests must simulate `MarkReportSectionGenerationRunning` failure
  before the model call and assert `dependency_error`, no chat request,
  `section.failed`, unchanged section content/status, and no stale skip/success
  event.
- Generation success-path tests must simulate a concurrent manual edit and a
  superseding generation job after the AI call but before the write transaction;
  both cases must preserve the current section, preserve current generation
  status, create no stale AI version, update progress, record
  `section.skipped`, and return a non-error execution result so the worker does
  not mark the job/report failed.
- Generation rollback tests must simulate a concurrent section edit after the
  failed transaction rolls back and assert failure compensation preserves
  `content`, `tables`, `version`, `content_source`, and `manual_edited`.
- Report-level generation tests must simulate a middle section version insert
  failure and assert the failed section rolls back content, later sections are
  still attempted, progress reaches `completed=total`, and the job returns
  `partial_succeeded` when another section succeeded.
- HTTP tests that assert `source`, `requirements`, `content`, and `tables` are
  passed through to the service and returned in the response DTO.
- OpenAPI parse/schema checks that Document and Gateway section-version source
  enums and table field names stay aligned.

### 7. Wrong vs Correct

#### Wrong

```text
POST /sections/{id}/versions -> insert report_section_versions(version=3) -> return 201
report_sections.version remains 2
```

#### Correct

```text
POST /sections/{id}/versions -> transaction:
  insert report_section_versions(version=3)
  update report_sections.content/tables/version/source
return 201 after commit
```

#### Wrong

```text
generation tx fails -> restore old section snapshot -> UPDATE report_sections SET content=old_content, version=old_version, generation_status=failed
```

#### Correct

```text
generation tx fails -> rollback generated content/version -> UPDATE report_sections SET generation_status=failed, updated_at=now WHERE id=$section AND last_job_id=$job
```

#### Wrong

```text
AI call returns -> use pre-call section snapshot -> UPDATE report_sections SET content=generated, version=old+1 WHERE id=$section
```

#### Correct

```text
AI call returns -> transaction:
  SELECT report_sections FOR UPDATE
  require last_job_id=$job, generation_status=running, version/manual_edited unchanged
  update generated content and insert report_section_versions
```

#### Wrong

```text
mark section running fails -> call AI anyway -> success write sees conflict -> count section as skipped -> job succeeds
```

#### Correct

```text
mark section running fails -> record section.failed -> return dependency_error -> worker marks job/report failed
```

## Scenario: Document Initial Report Defaults Seed

### 1. Scope / Trigger

- Trigger: adding or changing Document Service report type seeds, default
  report templates, singleton `report_settings`, or first-slice local
  development defaults.
- Applies to `services/document/migrations`,
  `services/document/internal/repository`, and `services/document/README.md`.

### 2. Signatures

- Migration files:
  - `services/document/migrations/0003_seed_initial_report_defaults.sql` or a
    later ordered migration for seed changes.
- Database rows:
  - `report_types.code` values `summer_peak_inspection` and
    `coal_inventory_audit`.
  - `report_templates.id` deterministic seed UUIDs when placeholder templates
    are required.
  - `report_settings.id = 'default'`.
- Settings JSON fields:
  - `llm_json.provider = ai-gateway`.
  - `default_templates_json` maps `reportType -> reportTemplateId`.
  - `file_json.defaultFormat = docx`.
  - `file_json.defaultNumberingMode = global` unless a user value already
    exists.
  - `file_json.defaultStyleProfileId` may reference a non-secret style profile
    identifier.

### 3. Contracts

- Seed migrations must be idempotent. Use `INSERT ... ON CONFLICT DO NOTHING`
  for stable rows.
- Seed migrations must not overwrite user modifications. For JSON settings,
  merge seed defaults on the left and existing JSON on the right so existing
  keys win, for example `seed_json || existing_json`.
- Placeholder templates are allowed when formal DOCX templates are missing, but
  they must be explicitly marked with `needs_decision` metadata and a runnable
  import path. They must not pretend to be formal business templates.
- Default settings must not contain provider API keys, provider base URLs,
  object storage details, `file_ref`, object keys, prompts, or internal file
  service identifiers.
- Placeholder template rows should keep `file_ref` null until the File Service
  owns a real uploaded template object.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Report type already exists | Keep the existing row; do not update name, enabled state, or defaults. |
| Placeholder template already exists | Keep the existing row by primary key. |
| `report_settings` row is missing | Insert the safe singleton default. |
| `report_settings` row exists with user values | Add only missing keys; preserve existing values. |
| Formal template file is not available | Store clear `needs_decision` metadata and no `file_ref`. |
| Seed content includes secrets or internal file references | Reject in review and add/adjust migration tests. |

### 5. Good/Base/Bad Cases

- Good: a follow-up migration seeds missing report types, inserts placeholder
  template metadata with deterministic IDs, and merges settings so user values
  win.
- Base: the first slice can use placeholder templates with no `file_ref` while
  keeping `defaultTemplates` runnable for local development.
- Bad: updating existing report type names on every seed run, hard-coding fake
  production template file references, or storing provider keys in
  `report_settings`.

### 6. Tests Required

- Migration string tests asserting the seed includes report type codes,
  deterministic template IDs, `needs_decision` metadata, import path, and no
  sensitive markers such as API keys or `file_ref`.
- Repository integration tests, gated by `DOCUMENT_TEST_DATABASE_URL`, that
  apply migrations, verify the two enabled report types and default settings,
  re-run the seed migration, and assert no duplicate rows or user-value
  overwrites.
- Service-local checks from `services/document`: `go test ./...`,
  `go build ./cmd/server`, and `git diff --check`.
- Goose migration apply against a real PostgreSQL database when a local or CI
  database is available.

### 7. Wrong vs Correct

#### Wrong

```text
seed rerun -> UPDATE report_types SET enabled = true, default_templates_json = stock_defaults
```

#### Correct

```text
seed rerun -> INSERT stable rows ON CONFLICT DO NOTHING -> merge missing settings with existing values taking precedence
```

# API Contracts

> Contract-first rules for gateway-facing and cross-service HTTP APIs.

---

## Documentation Authority

`docs/` is the source of truth for project contracts, but do not collapse every
docs/code mismatch into a single "docs wins" or "code wins" rule. Apply the
documented hierarchy from `docs/collaboration/documentation-workflow.md`:

1. Contract layer first: Gateway OpenAPI, service boundaries, data models,
   confirmed requirements, and team-approved decisions are collaboration
   contracts.
2. `develop` code is the current implementation fact baseline. Open PRs,
   draft issues, and unmerged work must not be described as already implemented.
3. `docs/services/<service>/docs/implementation.md` records current facts,
   including implemented, partial, pending, not implemented, scaffold, memory,
   mock, and docs/code divergence states.
4. README, runbooks, and testing strategy documents provide entry points,
   commands, and limits; they do not replace contracts or implementation-status
   documents.

When this Trellis spec and `docs/` disagree, inspect and follow these files
first, then update this spec:

- `docs/architecture/service-boundaries.md`
- `docs/architecture/frontend-backend-contract.md`
- `docs/architecture/technology-decisions.md`
- `docs/services/gateway/api/public.openapi.yaml`
- `docs/services/<service>/README.md`
- `docs/services/<service>/api/public.openapi.yaml` and
  `docs/services/<service>/api/internal.openapi.yaml`

Do not implement or generate frontend/backend clients from Trellis examples that
contradict the current `docs/` contracts. If code diverges from a confirmed
contract, default to fixing code. Changing contract semantics requires a
management/team decision before an implementation PR rewrites Gateway OpenAPI,
service boundaries, core data models, confirmed acceptance semantics, or
parallel-team internal service interfaces.

## Scenario: Gateway Contract-First API

### 1. Scope / Trigger

- Trigger: any new or changed frontend-facing gateway endpoint, gateway
  response envelope, frontend API client DTO, or cross-service route ownership.
- Applies to `services/gateway/`, browser API clients under `apps/web/`,
  and the domain service that owns the endpoint's business state.

### 2. Signatures

Gateway public endpoints are documented in:

```text
docs/services/gateway/api/public.openapi.yaml
```

Public routes use these prefixes:

```text
GET /healthz
GET /readyz
/api/v1/**
```

Stable public gateway routes and service-to-service HTTP routes must be
RESTful resource-oriented APIs:

- model paths as resources or collections,
- use HTTP methods for actions,
- use `GET` for reads, `POST` for creation, `PATCH` for partial updates, and
  `DELETE` for deletion,
- do not put action verbs such as `login`, `logout`, `register`, `download`,
  `search`, `generate`, `export`, `retry`, or `revoke` in stable paths,
- model long-running work as resources such as `jobs`, `files`, `sessions`,
  `messages`, `events`, or `queries`.

`/healthz` and `/readyz` are allowed operational exceptions.

Every OpenAPI operation must include:

- `operationId`
- `tags`
- `summary`
- at least one success response
- at least one `4XX` response for user-callable operations
- `x-owner-service` for routes backed by a service boundary

### 3. Contracts

Gateway success envelope:

```json
{
  "data": {},
  "requestId": "req_123"
}
```

Gateway paginated envelope:

```json
{
  "data": [],
  "page": {
    "page": 1,
    "pageSize": 20,
    "total": 100
  },
  "requestId": "req_123"
}
```

Gateway error envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "request validation failed",
    "requestId": "req_123",
    "fields": {
      "name": "is required"
    }
  }
}
```

Public IDs are strings. Public timestamps use OpenAPI `date-time`.

Gateway must pass request context to downstream services with these headers
when values are available:

| Header | Purpose |
| --- | --- |
| `X-Request-Id` | Correlate frontend request, gateway logs, and downstream logs. |
| `X-User-Id` | Authenticated user identity. |
| `X-User-Roles` | Comma-separated authenticated roles. |
| `X-User-Permissions` | Comma-separated authenticated permissions. |
| `X-Forwarded-For` | Original client address chain. |
| `X-Forwarded-Proto` | Original request protocol. |

### 4. Validation & Error Matrix

| Condition | Public response |
| --- | --- |
| Invalid request shape or field value | `400 validation_error` |
| Missing or invalid authentication | `401 unauthorized` |
| Authenticated caller lacks permission | `403 forbidden` |
| Resource does not exist or is hidden | `404 not_found` |
| State conflict | `409 conflict` |
| Rate or quota exceeded | `429 rate_limited` |
| Active contract route scaffolded before workflow implementation | `501 not_implemented` |
| Downstream service or infrastructure failed | `502 dependency_error` |
| Unexpected gateway failure | `500 internal_error` |

Do not forward raw downstream error bodies, SQL details, object keys, tokens,
prompts, vector payloads, or internal URLs to the frontend.

### 5. Good/Base/Bad Cases

- Good: add a gateway route to `docs/services/gateway/api/public.openapi.yaml`, mark
  `x-owner-service`, use the standard envelope, and update
  `docs/architecture/service-boundaries.md` if ownership is new.
- Base: proxy a domain-service route through gateway without changing the
  domain response shape, but still normalize errors to the gateway envelope.
- Bad: add a frontend call directly to `services/knowledge` or embed runtime index,
  MinIO, SQL, prompt, or report-generation logic in gateway.

### 6. Tests Required

When implementation exists:

- Gateway handler tests assert status code, response envelope, and request id.
- Error tests cover validation, auth failure, forbidden, not found, and
  dependency failure where applicable.
- Cross-service client tests use mocked HTTP servers and assert propagated
  context headers.
- Frontend API client tests assert request path, response normalization, and
  error-code mapping.

For documentation-only contract changes:

- Run an OpenAPI linter against `docs/services/gateway/api/public.openapi.yaml`.
- Parse the YAML and verify `$ref` targets resolve.
- Check route prefix consistency: health routes stay unversioned, public API
  routes use `/api/v1/**`.
- Check stable and placeholder paths follow the RESTful resource-path rule.

### 7. Wrong vs Correct

#### Wrong

```text
frontend -> services/knowledge/search
gateway handler -> runtime index query -> raw vector payload response
```

#### Correct

```text
frontend -> gateway /api/v1/knowledge-queries
gateway -> knowledge service
knowledge service -> retrieval infrastructure
gateway -> normalized KnowledgeQueryResponse or ErrorResponse
```

## Scenario: Rate-Limited Responses And Retry-After

### 1. Scope / Trigger

- Trigger: adding or changing backend rate limits, concurrency guards, failed
  login lockout, quota enforcement, or any `429 rate_limited` behavior.
- Applies to Gateway public responses, service-to-service clients,
  service-local OpenAPI files, service README configuration tables, and Auth
  failed-login state when Auth owns the identity decision.

### 2. Signatures

- Public error code: `rate_limited`.
- HTTP status: `429`.
- Optional response header:

```text
Retry-After: <integer seconds>
```

- Current first-slice environment keys:
  - `GATEWAY_MAX_IN_FLIGHT`
  - `GATEWAY_AUTH_REFRESH_MAX_IN_FLIGHT`
  - `AUTH_CREDENTIAL_WORK_MAX_IN_FLIGHT`
  - `AUTH_LOGIN_FAILURE_LIMIT`
  - `AUTH_LOGIN_FAILURE_WINDOW`
  - `AUTH_LOGIN_LOCK_DURATION`

### 3. Contracts

- All `429` responses still use the standard error envelope:

```json
{
  "error": {
    "code": "rate_limited",
    "message": "rate limited",
    "requestId": "req_123"
  }
}
```

- `Retry-After` is present only when the service knows a concrete remaining
  wait time, such as Auth failed-login lockout derived from `locked_until`.
- Process-local saturation, such as in-flight request guards or credential
  work guards, must not invent a wait time and normally omits `Retry-After`.
- Gateway must preserve a valid numeric `Retry-After` from Auth or an owner
  service when normalizing a downstream `429`; it must not forward raw
  downstream error bodies or non-numeric header values.
- OpenAPI must document `Retry-After` on relevant `429` responses when the
  caller can observe the header.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Gateway public in-flight limit saturated | `429 rate_limited`, standard envelope, request id, no `Retry-After` |
| Gateway Auth-refresh fan-out limit saturated | `429 rate_limited`, standard envelope, no Auth call for rejected request |
| Auth credential-work guard saturated | `429 rate_limited`, standard envelope, no `Retry-After` |
| Auth failed-login lockout active or triggered | `429 rate_limited` with numeric `Retry-After` based on `locked_until - now` |
| Auth/owner service returns `429` with numeric `Retry-After` to Gateway | Gateway returns `429 rate_limited` and preserves that header |
| Downstream returns raw or sensitive error payload | Gateway normalizes message/code and does not leak raw body fields |

### 5. Good/Base/Bad Cases

- Good: Auth sets `Retry-After` from known lockout state; Gateway preserves it
  while keeping the public error envelope sanitized.
- Base: a local semaphore rejects saturated work with `429 rate_limited` and no
  `Retry-After` because no stable wait time is knowable.
- Bad: returning `503`, `500`, or raw downstream bodies for quota/rate
  conditions; fabricating a retry time for per-process saturation; documenting
  `Retry-After` without a test that proves it is emitted or propagated.

### 6. Tests Required

- Gateway handler tests assert saturated in-flight requests return `429`, the
  standard envelope, and `X-Request-Id`.
- Gateway protected-route tests assert Auth refresh fan-out is bounded and
  rejected requests do not enter the Auth client.
- Gateway proxy/auth-client tests assert numeric downstream `Retry-After` is
  preserved on `429`.
- Auth HTTP tests assert a `service.CodeRateLimited` error with known wait time
  produces a `Retry-After` header in seconds.
- Auth service tests assert process-local credential saturation returns
  `CodeRateLimited` without known retry time, and lockout returns
  `CodeRateLimited` with a positive retry duration.

### 7. Wrong vs Correct

#### Wrong

```go
// Invents a wait time for process-local saturation.
w.Header().Set("Retry-After", "1")
writeRateLimited(w, r)
```

#### Correct

```go
if lockedUntil != nil && lockedUntil.After(now) {
    return service.RateLimitedError("rate limited", lockedUntil.Sub(now))
}
return service.RateLimitedError("rate limited", 0)
```

## Related Documents

- `docs/services/gateway/README.md`
- `docs/services/gateway/api/public.openapi.yaml`
- `docs/architecture/service-boundaries.md`
- `docs/architecture/frontend-backend-contract.md`

## Scenario: Gateway Readiness Semantics

### 1. Scope / Trigger

- Trigger: changing Gateway `/healthz`, Gateway `/readyz`, Compose health checks,
  local/production readiness docs, or cross-service smoke expectations.
- Applies to `services/gateway/cmd/server/main.go`,
  `services/gateway/internal/http/server.go`, Gateway OpenAPI health routes,
  `deploy/**`, and readiness/smoke runbooks.

### 2. Signatures

Gateway operational routes stay unversioned and unauthenticated:

```text
GET /healthz
GET /readyz
```

The current Gateway runtime readiness check covers:

```text
Redis session cache readiness
Auth /readyz
Configured and syntax-validated owner service base URLs for knowledge, qa,
document, and ai-gateway
```

### 3. Contracts

- `/healthz` is process liveness only.
- `/readyz` is a lightweight Gateway readiness gate for accepting normal public
  traffic. It must not become the full cross-service business smoke.
- Gateway `/readyz` may check Gateway-owned runtime dependencies, Auth
  readiness, and owner base URL configuration needed for route dispatch.
- Non-empty owner base URLs must be absolute `http` or `https` URLs with a
  host and no credentials, query, or fragment. Hosts must be the corresponding
  owner service DNS name or a local loopback/`localhost` development target.
  Invalid configured owner URLs fail startup/config validation; direct server
  construction must still treat invalid owner URLs as not configured without
  exposing the raw URL.
- Gateway `/readyz` must not synchronously call every owner service readiness
  endpoint or run business workflows such as upload, retrieval, QA answer,
  report generation, AI Gateway provider smoke, or model-profile bootstrap.
- Complete owner-service and provider availability belongs in targeted smoke or
  diagnostics, such as Auth/Gateway/Redis smoke and Gateway -> owner service
  end-to-end smoke.
- Public readiness responses must not expose internal service URLs, Redis keys,
  database URLs, service tokens, provider credentials, or raw downstream bodies.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Gateway process can respond | `/healthz` returns `200` project envelope. |
| Redis session cache is unavailable | `/readyz` returns `503 dependency_error`; logs include request id and sanitized dependency name. |
| Auth `/readyz` is unavailable | `/readyz` returns `503 dependency_error`; logs include request id and sanitized dependency name. |
| Required owner base URL is missing or blank | `/readyz` returns `503 dependency_error` before claiming Gateway can route public traffic. |
| Required owner base URL is non-empty but malformed | Gateway startup/config validation rejects it with a sanitized owner/reason; if a server is constructed directly with an invalid owner URL, proxy routes treat that owner as not configured and return the normal sanitized dependency error. |
| Owner service business workflow is unavailable after `/readyz` passed | The affected public API returns its normal sanitized dependency error; investigate with targeted smoke and request id, not by expanding `/readyz` into a full workflow probe. |
| AI Gateway profile/provider credential is missing or placeholder | AI-dependent smoke or owner routes report the failure; Gateway `/readyz` is not the proof point. |

### 5. Good/Base/Bad Cases

- Good: Gateway `/readyz` checks Redis, Auth, and route configuration, then
  deployment runbooks require targeted smoke for Knowledge/QA/Document/AI
  workflows.
- Base: Compose uses Gateway `/readyz` for startup ordering while production
  readiness docs separately list service-level `/readyz` and provider smoke.
- Bad: Gateway `/readyz` uploads a document, queries runtime indexes, calls QA chat, or
  fails startup only because a real model provider credential has not been
  bootstrapped yet.

### 6. Tests Required

- Gateway handler tests must cover successful `/healthz`, successful `/readyz`,
  and failed `/readyz` returning `503 dependency_error` with request id.
- When `gatewayReadyCheck` behavior changes, add or update unit tests for Redis,
  Auth, and owner base URL failure classification. Owner base URL validation
  tests must cover allowed `http`/`https` URLs, blank values, unsupported
  schemes, credentials, query strings, fragments, missing hosts, public hosts,
  raw private/link-local hosts, wrong owner-service hosts, and no raw URL
  leakage in errors or public responses.
- Documentation-only readiness changes must parse the changed Gateway OpenAPI
  files, run `python3 scripts/verify_gateway_active_api.py`, and run
  `git diff --check`.
- If Compose health checks or Docker policy changes, also run the relevant
  Compose config and Docker policy checks from the Docker runbook.

### 7. Wrong vs Correct

#### Wrong

```text
GET /readyz -> gateway calls knowledge, qa, document, ai-gateway /readyz
GET /readyz -> gateway performs upload/retrieval/QA/provider smoke
```

#### Correct

```text
GET /readyz -> Redis ready + Auth ready + owner base URLs configured
targeted smoke -> Gateway public API -> owner services -> File/Knowledge runtime/doc engine/AI Gateway
```

## Scenario: Knowledge Active Operation Proxy Contracts

### 1. Scope / Trigger

- Trigger: adding, activating, or changing Knowledge browser-facing operations
  in the gateway route matrix, Knowledge internal HTTP routes, or Knowledge
  contract tests.
- Applies to `services/gateway/internal/http/routes.go`,
  `services/gateway/internal/http/*_test.go`,
  `services/knowledge/internal/http`, `services/knowledge/internal/service`,
  `services/knowledge/api/openapi.yaml`, Knowledge docs, `services/knowledge-runtime/**`,
  and local host-run env wiring for RAGFlow runtime, Redis, MinIO,
  Elasticsearch/doc engine, or AI Gateway.

### 2. Signatures

- Gateway public routes:
  - `GET /api/v1/documents/{documentId}/chunks`
  - `GET /api/v1/documents/{documentId}/content`
  - `POST /api/v1/knowledge-queries`
- Knowledge internal routes:
  - `GET /internal/v1/documents/{documentId}/chunks`
  - `GET /internal/v1/documents/{documentId}/content`
  - `POST /internal/v1/knowledge-queries`
- Content reads return binary bytes, not a JSON success envelope.
- Chunk and query reads return the standard JSON success/page envelope with
  `requestId`.

### 3. Contracts

- Gateway must proxy these routes to Knowledge and must not implement chunking,
  runtime content reads, doc-engine queries, embedding, rerank, or
  visibility rules.
- Gateway must inject `X-Request-Id`, `X-User-Id`, `X-User-Roles`,
  `X-User-Permissions`, `X-Forwarded-For`, `X-Forwarded-Proto`, and
  `X-Service-Token` when configured.
- Knowledge handlers must keep database, vendor runtime, embedding, and rerank
  access behind service/repository/platform interfaces.
- `documents/{documentId}/content` must first authorize the Knowledge-owned
  document, then stream raw bytes through the RAGFlow runtime/adapter boundary.
  Do not return `file_ref`, object keys, File IDs, MinIO URLs, runtime URLs, or
  internal storage paths in JSON responses.
- `knowledge-queries` must model retrieval as a resource creation, not a
  `/search` action path. Runtime hits are candidates only; visible document and
  chunk facts must be hydrated through Knowledge-owned DTO mapping and
  permission checks.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Missing trusted user context | `401 unauthorized` with `error.requestId`. |
| Invalid pagination or query fields | `400 validation_error` with safe `fields`. |
| Hidden, deleted, or missing document/knowledge base | `404 not_found`. |
| Runtime, Redis, doc engine, embedding, or rerank failure | `502 dependency_error`; do not leak downstream details. |
| Content success | Raw bytes with content headers and `X-Request-Id`; no JSON envelope. |
| Chunk/query success | Standard data/page envelope with request id. |

### 5. Good/Base/Bad Cases

- Good: service-local tests use fake runtime/AI adapters, gateway tests assert
  proxy path/header propagation, and docs list real dependency smoke as
  remaining risk when not run.
- Base: fake-backed contract tests cover active operation envelopes while
  runbooks document how to enable real runtime/doc engine or AI Gateway.
- Bad: gateway returns `501` for an active Knowledge operation after the
  Knowledge service route exists, or gateway directly reads RAGFlow runtime,
  File Service, doc engine, prompt/model providers, SQL rows, or
  generated sqlc types.

### 6. Tests Required

- Knowledge handler tests for chunks, content, and `knowledge-queries` success.
- Knowledge handler tests for validation, unauthorized, not found, and
  dependency error envelopes with request id assertions.
- Gateway proxy tests that the active routes no longer return `501`, map to
  `/internal/v1/**`, propagate auth/request headers, and pass binary content
  through without wrapping.
- `cd services/knowledge && go test ./...` and `go build ./cmd/server`.
- `cd services/gateway && go test ./...` and `go build ./cmd/server` when the
  gateway route matrix changes.
- Compose config parsing when env or local integration docs change.

### 7. Wrong vs Correct

#### Wrong

```text
gateway /api/v1/documents/{documentId}/content -> File Service object URL
gateway /api/v1/knowledge/search -> runtime index response payload
```

#### Correct

```text
gateway /api/v1/documents/{documentId}/content -> knowledge -> RAGFlow runtime/adapter bytes
gateway /api/v1/knowledge-queries -> knowledge -> runtime/doc-engine candidates -> Knowledge DTO hydrate
```

## Scenario: QA Owner Authorization

### 1. Scope / Trigger

- Trigger: adding or changing QA session, message, response-run, event,
  tool-call, or citation reads and mutations.
- Applies to `services/qa/internal/http`, `services/qa/internal/service`,
  `services/qa/internal/repository`, the Gateway OpenAPI QA paths, and generated
  frontend Gateway types.

### 2. Signatures

- Direct session operations:
  - `GET /api/v1/qa-sessions/{sessionId}`
  - `PATCH /api/v1/qa-sessions/{sessionId}`
  - `DELETE /api/v1/qa-sessions/{sessionId}`
- Session-owned resources include `/qa-sessions/{sessionId}/messages`,
  `/qa-sessions/{sessionId}/events`, `/response-runs/**`, `/messages/{messageId}/citations`,
  and `/citations/{citationId}`.
- QA derives the current owner only from trusted Gateway `X-User-Id` context.

### 3. Contracts

- A live session owned by another authenticated user returns `403 forbidden`
  for direct detail, update, delete, and session-addressed message list/create
  operations.
- Missing and soft-deleted sessions return `404 not_found`.
- ID-addressed child resources are filtered through their owning session and
  return `404 not_found` when missing or owned by another user.
- Empty collections are valid only after the parent message/run/session has
  been authorized.
- Administrator roles do not bypass QA ownership until a separate reviewed
  cross-user administration contract exists.
- OpenAPI response entries and `apps/web/src/api/generated/gateway.ts` must be
  regenerated together when the public status-code set changes.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing trusted user context | `401 unauthorized` |
| Non-owner accesses a live session directly or lists/creates its messages | `403 forbidden` |
| Session is missing or soft-deleted | `404 not_found` |
| Message, run, event, tool call, or citation is missing/non-owned | `404 not_found` |
| Owner cancels a running response run | `200` with cancelled run |
| Owner cancels an existing terminal response run | `409 conflict` |
| Non-owner cancels a response run | `404 not_found`, never `409 conflict` |

### 5. Good/Base/Bad Cases

- Good: repository queries filter by `external_user_id`, direct session misses
  perform a narrow access classification, and handlers preserve typed errors.
- Base: an owned message or run with no citations/tool calls returns an empty
  list only after parent authorization succeeds.
- Bad: returning `200 []` for an unknown/non-owned parent, treating every
  failed cancellation as `409`, or allowing an admin header to bypass owner checks.

### 6. Tests Required

- Handler tests assert direct non-owner session GET/PATCH/DELETE return the
  standard `403 forbidden` envelope, including when admin roles are present.
- Service tests assert session and message operations propagate forbidden
  errors before writes or model/tool execution.
- PostgreSQL integration tests assert direct session `403`, hidden child
  resource `404`, owner terminal-run `409`, and no cross-user citation data.
- Gateway contract checks and OpenAPI type generation must pass after response
  status changes.

### 7. Wrong vs Correct

#### Wrong

```text
non-owner PATCH session -> 404
non-owner PATCH response run -> 409 conflict
non-owner list citations -> 200 [] without checking the parent message
```

#### Correct

```text
non-owner PATCH session -> 403 forbidden
non-owner PATCH response run -> 404 not_found
owned message with no citations -> authorize message -> 200 []
```

## Scenario: Knowledge Runtime Internal API

### 1. Scope / Trigger

- Trigger: adding or changing the RAGFlow-based runtime API/worker used by
  Knowledge ingestion and retrieval.
- Applies to `services/knowledge-runtime/`, Knowledge vendor runtime clients
  under `services/knowledge/internal/vendorclient`, runtime parser configuration,
  and docs that describe document parsing ownership.

### 2. Signatures

- Runtime operational routes:
  - `GET /api/v1/system/healthz`
  - `GET /api/v1/system/ping`
- Runtime adapter routes:
  - dataset, document, chunk, retrieval, provider/model, system, and task routes
    explicitly allowed by `services/knowledge-runtime/api/apps/route_registry.py`
- Knowledge environment keys:
  - `VENDOR_RUNTIME_URL`
  - `KNOWLEDGE_AUTO_START_INGESTION`
  - `KNOWLEDGE_RUNTIME_READINESS_MODE`
  - runtime/storage/search keys required by host-run Knowledge runtime startup
  - deployment-level worker scaling configuration, such as the KEDA Redis
    Streams example under `deploy/k8s/`

### 3. Contracts

Knowledge owns knowledge-base and document business state, permissions, public
response envelopes, parser-config administration, and adapter error mapping.
`services/knowledge-runtime` owns document parsing, chunking, embedding/index
work, retrieval support, runtime task execution, and vendor storage/search
details. Runtime routes must be registered through an explicit allowlist; do not
expose upstream RAGFlow login/JWT/API-token, UI, file-management, or MCP
surfaces as product APIs.

Runtime API startup must remain separate from worker startup. The API-only path
uses the base Python dependency profile and must not require worker/browser/OCR
packages such as `crawl4ai`, `onnxruntime-gpu`, `opencv-python`,
`selenium-wire`, `spacy`, or `xgboost`. Full ingestion startup uses the worker
dependency profile and is the only local helper that starts
`rag/svr/task_executor.py`.

The old standalone `services/parser` and `/internal/v1/parsed-documents` API are
retired. Do not add `PARSER_SERVICE_BASE_URL` or Parser HTTP clients back to
Knowledge, QA, Gateway, or local integration scripts.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Runtime health/ping fails | Knowledge `/readyz` reports runtime dependency failure without leaking internal credentials or raw vendor bodies. |
| Runtime document parse task fails | Knowledge maps the document to a failed processing state with sanitized error text. |
| Runtime returns zero chunks for an uploaded document | Treat as ingestion failure; do not mark the document ready. |
| Runtime retrieval fails or returns invalid shape | Return caller-owned `dependency_error`; do not forward raw vendor `{code, message}` envelopes. |
| Runtime returns valid retrieval hits | Hydrate and normalize through Knowledge-owned DTOs and permission checks. |

### 5. Good/Base/Bad Cases

- Good: Knowledge calls the runtime over HTTP through the vendor client, worker
  completes parse/chunk/embed/index work, and retrieval returns normalized
  Knowledge results.
- Base: Knowledge unit tests use fake vendor runtime clients while runtime E2E
  is gated by explicit local infrastructure.
- Bad: restoring `services/parser`, adding a QA/Gateway direct parser client, or
  exposing upstream RAGFlow auth/UI/MCP surfaces through the product boundary.

### 6. Tests Required

- Knowledge vendor client/adapter tests assert route paths, propagated tenant
  headers, redirect blocking, sanitized failures, and error classification.
- Runtime route registry tests assert only the allowlisted API modules are
  registered.
- Targeted runtime tests cover config utilities and changed parser/chunking
  surfaces.
- Runtime startup scripts and Docker policy checks must pass when runtime
  deployment wiring changes.
- Real PDF E2E should prove upload -> parse/chunk/embed/index -> retrieval when
  `DL_T_673-1999.pdf` is available.

### 7. Wrong vs Correct

#### Wrong

```text
qa -> parser /internal/v1/parsed-documents
knowledge -> services/parser -> chunk/embed/index in Knowledge
```

#### Correct

```text
knowledge adapter -> services/knowledge-runtime API
knowledge-runtime worker -> parse/chunk/embed/index -> retrieval support
```

## Scenario: Internal Service Contract API

### 1. Scope / Trigger

- Trigger: adding or changing an internal service-to-service HTTP API, including
  model invocation APIs owned by `ai-gateway`.
- Applies to `docs/services/<service>/api/public.openapi.yaml`,
  `docs/services/<service>/api/internal.openapi.yaml`,
  `services/<service>/api/openapi.yaml` when implementation exists, service
  interface docs, and matching service-boundary documentation.

### 2. Signatures

Internal service routes use:

```text
GET /healthz
GET /readyz
/internal/v1/**
```

AI Gateway model invocation routes intentionally use OpenAI-compatible paths
inside the internal prefix:

```text
POST /internal/v1/chat/completions
POST /internal/v1/embeddings
POST /internal/v1/rerankings
```

`/internal/v1/rerankings` is an OpenAI-style extension because OpenAI does not
define a native rerank endpoint.

### 3. Contracts

Internal project-owned configuration or metadata APIs use the standard project
envelope:

```json
{ "data": {}, "requestId": "req_123" }
```

```json
{ "error": { "code": "validation_error", "message": "request validation failed", "requestId": "req_123" } }
```

AI Gateway model profile APIs must treat provider credentials as write-only:
requests may include `apiKey` for create/update, but responses, logs, errors,
and frontend-visible gateway admin responses may only expose `apiKeyConfigured`
and non-secret provider/model metadata.

AI Gateway chat completion, embedding, and rerank APIs use OpenAI-compatible or
OpenAI-style request, success response, streaming chunk, function-calling, and
error body shapes.
They must not wrap successful model responses in the project `data/requestId`
envelope. The request id is carried through `X-Request-Id` response headers and
logs.

AI Gateway may pass through and normalize OpenAI-compatible function-calling
fields such as `tools`, `tool_choice`, `parallel_tool_calls`,
`assistant.tool_calls`, `role=tool`, `tool_call_id`, and streaming
`delta.tool_calls`. It must not execute MCP tools or decide domain tool
permissions; the calling domain service, such as `qa`, owns tool policy,
execution, persistence, and public event projection.

Internal services must accept or propagate these headers when available:

| Header | Purpose |
| --- | --- |
| `X-Request-Id` | Correlate public gateway, domain service, AI Gateway, and provider logs. |
| `X-Service-Token` | Authenticate service-to-service calls. |
| `X-Caller-Service` | Identify the calling service, such as `qa`, `knowledge`, or `document`. |
| `X-User-Id` | Audit the user that triggered the model call when applicable. |
| `X-User-Roles` | Audit or quota context. |
| `X-User-Permissions` | Audit or quota context. |

Internal responses and logs must not expose raw API keys, provider bearer
tokens, prompt secrets, raw provider error bodies, storage object keys, vector
payloads, SQL details, or internal URLs.

### 4. Validation & Error Matrix

| Condition | Internal response |
| --- | --- |
| Invalid request shape or field value | `400 validation_error` or OpenAI-style `invalid_request_error` |
| Missing or invalid service credential | `401 unauthorized` or OpenAI-style `authentication_error` |
| Caller service lacks permission | `403 forbidden` or OpenAI-style `permission_error` |
| Profile or resource does not exist | `404 not_found` |
| State or configuration conflict | `409 conflict` |
| Rate or quota exceeded | `429 rate_limited` or OpenAI-style `rate_limit_error` |
| Active contract route scaffolded before workflow implementation | `501 not_implemented` |
| Provider or infrastructure failed | `502 dependency_error` or OpenAI-style `upstream_error` |
| Unexpected service failure | `500 internal_error` |

### 5. Good/Base/Bad Cases

- Good: `qa` calls `ai-gateway` with `POST /internal/v1/chat/completions`,
  passes approved function-calling tool definitions, executes any returned MCP
  tool calls itself, keeps conversation/message/tool-call/citation state in
  `qa`, and stores only sanitized summaries plus the normalized assistant
  response.
- Base: Knowledge or its runtime uses AI Gateway embedding/rerank profiles when
  configured, while chunk/index persistence stays inside the Knowledge runtime
  or doc engine and vector payloads never reach gateway responses.
- Bad: public `gateway` directly calls an OpenAI-compatible provider, stores an
  API key, or exposes `/internal/v1/chat/completions` to frontend clients.

### 6. Tests Required

For documentation-only contract changes:

- Parse the affected OpenAPI YAML file.
- Verify all `$ref` targets resolve.
- Verify internal business paths use `/internal/v1/**`, except `/healthz` and
  `/readyz`.
- Verify AI Gateway model invocation operations document OpenAI-compatible
  success, streaming, function-calling, and error shapes.
- Check Markdown links resolve.

When implementation exists:

- Handler tests assert project envelope or OpenAI-compatible response shape as
  appropriate for the endpoint.
- Cross-service client tests assert `X-Request-Id`, `X-Service-Token`, and
  `X-Caller-Service` propagation.
- Sensitive-data tests assert API keys, provider tokens, prompts, raw provider
  errors, and vector payloads are not logged or returned.

### 7. Wrong vs Correct

#### Wrong

```text
frontend -> gateway -> /internal/v1/chat/completions
gateway stores provider API key and streams raw provider chunks
```

#### Correct

```text
frontend -> gateway /api/v1/qa-sessions/{sessionId}/messages
gateway -> qa service
qa service -> ai-gateway /internal/v1/chat/completions
qa owns messages, MCP tool calls, citations, and public SSE event shape
```

## Scenario: AI Gateway Embeddings And Rerankings

### 1. Scope / Trigger

- Trigger: implementing or changing AI Gateway model invocation routes, provider adapters, profile purpose resolution, provider invocation summaries, or usage aggregation.
- Applies to `services/ai-gateway/internal/http`, `services/ai-gateway/internal/service`, `services/ai-gateway/internal/provider`, `services/ai-gateway/internal/repository`, `services/ai-gateway/migrations`, and `docs/services/ai-gateway/api/internal.openapi.yaml`.

### 2. Signatures

- Internal routes:
  - `POST /internal/v1/embeddings`.
  - `POST /internal/v1/rerankings`.
- Required internal headers:
  - `X-Service-Token`.
  - `X-Caller-Service`.
  - propagate `X-Request-Id` and `X-User-Id` when present.
- Profile routing:
  - embeddings require `purpose = 'embedding'`.
  - rerankings require `purpose = 'rerank'`.
  - omitted `profile_id` resolves the enabled default profile for that purpose.
- Provider paths:
  - embeddings call provider `/embeddings` under the configured profile `base_url`.
  - rerankings call provider `/rerank` under the configured profile `base_url`.
- Database tables:
  - `provider_invocations` stores one secret-safe model call summary.
  - `model_usage_aggregates` stores low-cardinality hourly usage counts and token/duration sums.

### 3. Contracts

- Embedding requests use OpenAI-compatible snake_case fields: `model`, optional `profile_id`, `input[]`, optional `dimensions`, optional `encoding_format`, and optional `user`.
- Reranking requests use the project OpenAI-style extension: `model`, optional `profile_id`, `query`, `documents[]` with `id` and `text`, optional `top_n`, and optional low-sensitive `metadata`.
- After profile resolution, the request `model` must exactly match the resolved `model_profiles.model`. AI Gateway sends `profile.Model` to the provider and records `profile.Model` in invocation summaries; callers cannot use a profile's credentials to invoke arbitrary provider models.
- Model invocation success and error responses must not use the project `{ data, requestId }` envelope. Request IDs remain in `X-Request-Id`.
- Provider credentials are decrypted only inside the model invocation boundary and sent as provider bearer tokens. They must not appear in responses, ordinary logs, invocation summaries, usage aggregates, metrics labels, or test failure messages.
- `provider_invocations` may store profile ID, provider, model, operation, status, provider status code, token usage, input count, dimensions/topN, duration, attempt count, normalized error code/type, caller service, external user ID, and timestamps.
- `provider_invocations` must not store embedding input text, rerank query text, rerank document text, embedding vectors, full provider request/response bodies, raw provider URL query, API keys, bearer tokens, or credential fingerprints.
- Embedding provider responses must contain exactly one `data[]` item per input item. Each item must be `object = "embedding"`, have valid JSON `embedding`, and have an `index` equal to its input position with no duplicates or out-of-range values.
- Reranking provider responses must normalize every result back to the original request `documents[]` by index. Each result index must be in range, unique, and the returned `document_id` must match `documents[index].id` when the provider supplies one; otherwise the adapter must fill it from the request document.
- Rerank provider requests should avoid asking providers to echo document text, for example by sending `return_documents=false` when the provider supports it.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing or invalid service token | `401` OpenAI-style `authentication_error`, code `unauthorized` |
| Missing or unknown caller service | `401`/`403` OpenAI-style auth/permission error |
| Missing `model`, empty `input`, empty `query`, empty `documents`, invalid `dimensions`, or invalid `top_n` | `400` OpenAI-style `invalid_request_error`, code `validation_error` |
| `profile_id` references the wrong purpose, such as chat profile for embeddings | `400` OpenAI-style `invalid_request_error`, code `validation_error` |
| Request `model` does not match the resolved profile's configured model | `400` OpenAI-style `invalid_request_error`, code `validation_error`, param `model` |
| Missing explicit profile or missing enabled default profile | `404` OpenAI-style `not_found_error`, code `not_found` |
| Profile has no active credential or credential cannot be decrypted | `502` OpenAI-style `upstream_error`, code `dependency_error` |
| Provider returns missing, duplicate, out-of-range, or wrong-order embedding indexes | `502` OpenAI-style `upstream_error`, code `dependency_error` |
| Provider returns rerank indexes outside `documents[]`, duplicate rerank indexes, or mismatched `document_id` values | `502` OpenAI-style `upstream_error`, code `dependency_error` |
| Provider returns request validation failure | `400` OpenAI-style `invalid_request_error`, code `validation_error` |
| Provider rate limits | `429` OpenAI-style `rate_limit_error`, code `rate_limited` |
| Provider auth, permission, network, timeout, malformed JSON, or 5xx failure | `502` OpenAI-style `upstream_error`, code `dependency_error` |

### 5. Good/Base/Bad Cases

- Good: handler decodes OpenAI-style JSON, service resolves and validates an enabled purpose-matched profile and matching model, decrypts only the active credential, provider adapter calls a fake-testable HTTP endpoint with `profile.Model`, service records a secret-safe invocation summary, and the response body remains OpenAI-compatible.
- Base: a fake provider test asserts embeddings pass batch input and dimensions, rerank passes text-only documents with `return_documents=false`, and provider errors never include raw provider bodies in returned errors.
- Bad: returning project envelopes from model invocation routes, writing raw `input` or `documents[].text` to `provider_invocations`, logging provider bearer tokens, using a chat profile for embeddings/rerank, or letting Knowledge/QA call providers directly.

### 6. Tests Required

- Handler tests for auth failure, validation error shape, successful embedding response shape, successful reranking response shape, and no API key/request text leakage.
- Service tests for default profile resolution, explicit wrong-purpose profile rejection, request model/profile model mismatch rejection, provider model fixed to `profile.Model`, dimensions/topN resolution, provider error normalization, embedding count/index validation, rerank index/document mapping validation, invocation status/error fields, and secret-safe summaries.
- Provider client tests with fake HTTP servers for request path, bearer token placement, batch input, dimensions, rerank `top_n`, `return_documents=false`, rerank `data[]` index/document mapping, provider error mapping, and malformed provider response handling.
- Repository/migration validation should be added when a local PostgreSQL test harness is available; until then, migrations must be reviewed for explicit columns, no raw payload columns, safe indexes, and goose `-- +goose Up`.
- Required checks from `services/ai-gateway`: `go test ./...`, `go build ./cmd/server`, and `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```text
knowledge -> OpenAI provider directly
ai-gateway logs request body and provider error body for debugging
provider_invocations.input_text = documents[].text
```

#### Correct

```text
knowledge -> ai-gateway /internal/v1/embeddings
ai-gateway resolves an embedding profile and calls provider /embeddings
provider_invocations stores counts, dimensions, usage, status, and normalized error only
Knowledge runtime/doc engine owns embedding/index persistence and chunk state
```

#### Wrong

```text
knowledge -> ai-gateway /internal/v1/embeddings with profile_id=mp_bge_m3 and model=other-expensive-model
ai-gateway forwards model=other-expensive-model using the profile credential
```

#### Correct

```text
knowledge -> ai-gateway /internal/v1/embeddings with profile_id=mp_bge_m3 and model=BAAI/bge-m3
ai-gateway verifies request model matches profile.model before decrypting credentials
ai-gateway forwards model=profile.model to the provider
```

## Scenario: Knowledge Document Lifecycle APIs

### 1. Scope / Trigger

- Trigger: implementing or changing Knowledge-owned document detail update,
  deletion, original content reads, or chunk listing.
- Applies to `services/knowledge/internal/http`,
  `services/knowledge/internal/service`, `services/knowledge/internal/repository`,
  `services/knowledge/internal/platform/fileclient`,
  `services/gateway/internal/http/routes.go`, and the matching Gateway OpenAPI
  document routes.

### 2. Signatures

Gateway public routes:

```text
PATCH  /api/v1/documents/{documentId}
DELETE /api/v1/documents/{documentId}
GET    /api/v1/documents/{documentId}/chunks?page=&pageSize=
GET    /api/v1/documents/{documentId}/content
```

Knowledge internal routes:

```text
PATCH  /internal/v1/documents/{documentId}
DELETE /internal/v1/documents/{documentId}
GET    /internal/v1/documents/{documentId}/chunks?page=&pageSize=
GET    /internal/v1/documents/{documentId}/content
```

Runtime content read:

```text
Knowledge adapter -> RAGFlow runtime document/content route
```

Database state involved:

- Runtime document and task state is mapped to public Knowledge document status.
- Runtime chunks are mapped to Knowledge `DocumentChunk` DTOs and query results.
- Knowledge PostgreSQL may retain parser-config and migration-compatible fields,
  but current document bytes/chunks/index facts come from RAGFlow runtime.

### 3. Contracts

- Knowledge is the owner service for document lifecycle business state. Gateway
  only authenticates, injects trusted context headers, and proxies active routes.
- JSON success responses use the standard `{ data, requestId }` or
  `{ data, page, requestId }` envelope.
- `GET /documents/{documentId}/content` successful responses stream the original
  bytes without the JSON envelope. Error responses still use the standard error
  envelope.
- `PATCH /documents/{documentId}` first-slice support is tags only. Unknown
  fields or a body without `tags` must fail validation. Tags use the same
  trim/dedupe/max-count/max-length rules as document upload.
- `DELETE /documents/{documentId}` applies the Knowledge document delete/hidden
  semantics and delegates runtime document/chunk/index lifecycle to RAGFlow
  runtime. It must not restore the old File cleanup worker path.
- `GET /documents/{documentId}/chunks` must authorize the parent document. An
  existing document with no chunks, including pending processing states, returns
  an empty paginated list rather than `501` or a dependency error.
- `GET /documents/{documentId}/content` must authorize through Knowledge first,
  then stream bytes through the runtime adapter. Public responses and errors
  must not include `fileRef`, File Service IDs, buckets, object keys, runtime
  storage paths, internal URLs, or raw downstream error bodies.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing trusted `X-User-Id` | `401 unauthorized` |
| Missing `knowledge:write` or admin role for PATCH/DELETE | `403 forbidden` |
| Missing or blank `documentId` | `400 validation_error` |
| PATCH body omits supported fields or includes unknown fields | `400 validation_error` |
| Invalid page/pageSize or tag constraints | `400 validation_error` |
| Missing, deleted, or hidden document | `404 not_found` |
| Existing pending/empty document chunks | `200` with `data: []` and `page.total: 0` |
| Runtime content is missing or fails | caller-owned sanitized error, usually `502 dependency_error` |
| PostgreSQL or other infrastructure failure | `502 dependency_error` |

### 5. Good/Base/Bad Cases

- Good: Gateway proxies `/api/v1/documents/doc_1/content` to Knowledge;
  Knowledge authorizes `doc_1`, streams runtime/adapter bytes, and never returns
  runtime object storage details in JSON.
- Base: deleting a document sets `deleted_at`, hides it from future reads, and
  delegates runtime internal object/chunk/index lifecycle to RAGFlow runtime.
- Bad: Gateway returns `501` for active document lifecycle routes, Knowledge
  exposes `fileRef` or object-storage details, or deletion restores slow File
  Service or runtime-index calls inside a PostgreSQL transaction.

### 6. Tests Required

- Knowledge handler tests for PATCH, DELETE, chunks, content streaming, standard
  envelopes, missing user context, write permission, and no `fileRef` leakage.
- Knowledge service tests for tag normalization/validation, delete hiding,
  content authorization before runtime reads, and chunk empty-list behavior.
- Runtime/vendor client tests with fake HTTP servers for content path, context
  headers, body streaming, and sanitized downstream errors.
- Gateway tests asserting the route matrix no longer marks implemented document
  lifecycle routes `NotImplemented` and binary content is proxied without a JSON
  envelope.
- Required checks from changed services: `go test ./...`, `go build ./cmd/server`,
  and `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```text
frontend -> gateway /api/v1/documents/doc_1/content
gateway -> file /internal/v1/files/file_123/content
response headers/body expose file_123 or object key on errors
```

#### Correct

```text
frontend -> gateway /api/v1/documents/doc_1/content
gateway -> knowledge /internal/v1/documents/doc_1/content
knowledge authorizes doc_1, calls runtime adapter for content bytes
success streams bytes; errors stay sanitized and document-owned
```

#### Wrong

```text
DELETE /documents/doc_1 -> delete file object and runtime index points while holding
the document transaction; if cleanup fails the document stays visible
```

#### Correct

```text
DELETE /documents/doc_1 -> knowledge authorizes doc_1, delegates RAGFlow
runtime document/chunk/index lifecycle, and returns 204 without exposing
runtime storage details
```

## Scenario: Knowledge Adapter Runtime Mode

### 1. Scope / Trigger

- Trigger: implementing or changing `KNOWLEDGE_RUNTIME_MODE=adapter`, the
  contract adapter binary (`cmd/adapter`), or vendor-runtime proxy routes.
- Applies to `services/knowledge/cmd/adapter/`,
  `services/knowledge/internal/adapter/`,
  `services/knowledge/internal/vendorclient/`,
  `services/knowledge-runtime/`, and Gateway routes owned by Knowledge.

### 2. Signatures

Adapter mode exposes the same internal contract routes as legacy Knowledge:

```text
GET|POST|PATCH|DELETE /internal/v1/knowledge-bases[/**]
GET|POST|PATCH|DELETE /internal/v1/documents/{documentId}[/**]
POST /internal/v1/knowledge-queries
GET|POST|PATCH|DELETE /internal/v1/parser-configs[/**]  # Phase 3b
```

The document singleton routes are dataset-scoped even though the path only
contains `documentId`. These routes must require `knowledgeBaseId` as a query
parameter:

```text
GET /internal/v1/documents/{documentId}?knowledgeBaseId={knowledgeBaseId}
PATCH /internal/v1/documents/{documentId}?knowledgeBaseId={knowledgeBaseId}
DELETE /internal/v1/documents/{documentId}?knowledgeBaseId={knowledgeBaseId}
GET /internal/v1/documents/{documentId}/chunks?knowledgeBaseId={knowledgeBaseId}
GET /internal/v1/documents/{documentId}/content?knowledgeBaseId={knowledgeBaseId}
```

Vendor runtime mapping (adapter → RAGFlow):

```text
knowledge-bases -> /api/v1/datasets
documents upload  -> /api/v1/datasets/{id}/documents?type=local
documents parse   -> /api/v1/datasets/{id}/documents/parse  # auto after upload when KNOWLEDGE_AUTO_START_INGESTION=true
document metadata/lookup -> /api/v1/datasets/{kb}/documents?page=&page_size=&id={doc}
document update  -> /api/v1/datasets/{kb}/documents/{doc}
document delete  -> DELETE /api/v1/datasets/{kb}/documents with ids[]
chunks            -> /api/v1/datasets/{kb}/documents/{doc}/chunks
content           -> /api/v1/datasets/{kb}/documents/{doc} (binary)
knowledge-queries -> /api/v1/datasets/search
```

Runtime guardrail environment keys:

```text
KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS=true|false
METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT=10000
```

### 3. Contracts

- Adapter must preserve Gateway-facing JSON envelopes and error codes; do not
  leak vendor `{code, message}` shapes to callers.
- Adapter forwards tenant identity with `X-Tenant-Id` and `X-User-Id` set to
  Gateway `X-User-Id`; RBAC checks stay in adapter using
  `knowledge:read` / `knowledge:write` and admin permissions.
- Document singleton, chunk, and content routes must use the caller-provided
  `knowledgeBaseId` to access dataset-scoped runtime routes. Do not scan every
  knowledge base to infer document ownership.
- Parser-config routes delegate to legacy goose PostgreSQL tables when
  `DATABASE_URL` is configured; without it they return `502 dependency_error`.
- Document `PATCH` with `tags` maps to vendor dataset-document metadata updates;
  other fields remain validation errors until explicitly supported.
- Invalid or non-object `chunkStrategy` JSON must return `400 validation_error`;
  silently dropping the field is not allowed.
- After upload, adapter mode queues vendor deepdoc ingestion via
  `POST /api/v1/datasets/{id}/documents/parse` when
  `KNOWLEDGE_AUTO_START_INGESTION` is true (default). Adapter mode does not call
  `services/parser` or `PARSER_SERVICE_BASE_URL`. If
  `KNOWLEDGE_RUNTIME_WORKER_START_COMMAND` is configured and the runtime reports
  no task executor heartbeat, adapter mode invokes that controlled command before
  queueing parse work and requires a heartbeat before the queue call proceeds.
  Local helpers may stop the worker after the queue stays idle; adapter readiness
  must treat stale task executor heartbeats as unavailable so the next upload can
  trigger a fresh worker start. Production commands must point at systemd, K8s,
  supervisor, or another deployment-owned entrypoint rather than baking local
  shell scripts into production.
- `KNOWLEDGE_RUNTIME_READINESS_MODE=ingestion` is the default readiness mode and
  requires the runtime task executor heartbeat. `query` mode allows `/readyz` to
  pass without that heartbeat when runtime API and query-time dependencies are
  healthy, but `/internal/v1/runtime/status` must still report
  `task_executor_ready` and `task_executor_count`.
- Upload ingestion does not require the worker to be running at adapter startup.
  Adapter mode calls `/documents/parse` when `KNOWLEDGE_AUTO_START_INGESTION`
  is true. If no worker start command is configured, the deployment layer remains
  responsible for running or scaling the runtime worker to consume queued Redis
  Stream tasks.
- Object storage for uploaded documents uses vendor MinIO configuration
  (`software-teamwork-knowledge` bucket); Knowledge adapter does not call File
  Service for upload in vendor mode.
- Vector retrieval uses the configured vendor doc engine only.
- Gateway/Auth service owns identity; adapter forwards `X-User-Id` as vendor tenant
  context. Vendor login/JWT/API-token surfaces remain disabled.
- The vendored runtime HTTP surface must be registered through an explicit
  allowlist. Keep only the route modules needed for dataset, document, chunk,
  model/provider, system, and task workflows used by the adapter. Do not
  expose upstream RAGFlow MCP, file-management, login/JWT/API-token, or UI-only
  routes as project runtime APIs.
- Adapter dataset creation sends the embedding choice with vendor field
  `embedding_model`. The project env value uses the composite
  `<model>@<provider>` shape, for example `BAAI/bge-m3@SILICONFLOW`.
- Adapter retrieval rerank config sends `rerank_id`. The runtime expects the
  composite `<model>@<tenant>@<provider>` shape, for example
  `BAAI/bge-reranker-v2-m3@default@SILICONFLOW`; a bare model name can resolve
  to an empty provider and fail at runtime.
- Retrieval trace must not invent runtime facts. Use configured runtime values
  when available; use `runtime-managed` and `embeddingDimension: -1` when the
  vendor runtime owns the value but does not expose it.
- Vendor runtime error mapping must classify by stable HTTP status/code carried
  by `vendorclient.APIError`; do not match free-form messages such as
  "not found" or "invalid dataset".
- Runtime route auth declarations must include `GATEWAY`. A valid
  `X-Service-Token` is not sufficient for routes decorated only with legacy
  `JWT`/`API`/`BETA` auth types.
- Gateway tenant auto-provisioning is controlled by
  `KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS`. When disabled, auth must return a
  clear tenant/provisioning failure and must not synthesize user or tenant rows.
- Dataset-level RAPTOR/GraphRAG tasks use the explicit
  `DATASET_SCOPE_TASK_DOC_ID` sentinel. Real source document IDs must not equal
  the dataset-scope sentinel.
- Empty or whitespace-only embedding chunks are non-indexable. Do not embed the
  literal placeholder `"None"` as document content.
- Metadata filter pushdown failures may fall back to in-memory filtering only
  when the candidate document count is at or below
  `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT`; over-cap cases must fail clearly.
  Lazy metadata loaders and doc-engine pagination must receive the configured
  cap before materializing document metadata, and may fetch at most one extra
  record or use a stable total count to prove the fallback is over-cap.
- Missing retrieval indexes may return an empty result only when the runtime or
  doc engine exposes a stable missing-index signal such as
  `index_not_found_exception`. Other `*_not_found` failures from datasets,
  models, providers, or dependencies must keep their typed error mapping.
- Adapter-owned parser config trace fields such as
  `software_teamwork_parser_config` must not be forwarded into strict vendor
  runtime request bodies unless the vendor schema explicitly allows them.
- Document metadata is read from the dataset document-list endpoint. Do not use
  the binary content route as JSON metadata; `GET /api/v1/datasets/{kb}/documents/{doc}`
  is the download/content path.
- Runtime model initialization may upsert the env-selected embedding/rerank
  provider, model instance, and tenant defaults at startup so new datasets do
  not silently fall back to the vendor Builtin embedding model.
- Vendor document `run` maps to Gateway status: `RUNNING` → `parsing`, `DONE` →
  `ready`, `FAIL`/`CANCEL` → `parse_failed`.
- `GET /documents/{documentId}/content` streams bytes without JSON envelope.
- Runtime and adapter logs must recursively redact API keys, tokens, secrets,
  and authorization values before logging config or provider payload metadata.
- Alpine runtime entrypoints must be POSIX `sh` scripts unless the image
  explicitly installs `bash`. Compose `command` should pass application args to
  the entrypoint instead of repeating the entrypoint path.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing `X-User-Id` | `401 unauthorized` |
| Read without `knowledge:read` or write permission | `403 forbidden` |
| Mutations without `knowledge:write` | `403 forbidden` |
| Parser-config admin without admin permissions | `403 forbidden` |
| Parser-config without `DATABASE_URL` in adapter mode | `502 dependency_error` |
| Vendor runtime unreachable | `502 dependency_error` |
| Runtime task executor heartbeat missing | Default `ingestion` mode returns degraded `/readyz`; `query` mode keeps `/readyz` ready but reports task executor diagnostics. |
| Upload ingestion enabled while no worker is alive | Adapter still calls `/documents/parse`; deployment-owned worker scaling must consume the queued Redis Stream task. |
| Runtime route module is outside the allowlist | It must not be registered or documented as active. |
| Document singleton route omits `knowledgeBaseId` | `400 validation_error` with a `knowledgeBaseId` field error. |
| Adapter scans all datasets to find one document | Treat as an adapter bug; require explicit dataset context or a bounded direct mapping. |
| `chunkStrategy` is invalid JSON or not a JSON object | `400 validation_error`; do not silently omit `parser_config`. |
| Vendor returns HTTP 401/403/404 with arbitrary text | Map by HTTP status to `unauthorized`/`forbidden`/`not_found`; do not inspect message substrings. |
| Runtime route declares only legacy auth types | Reject as `unauthorized` even when `X-Service-Token` is valid. |
| Missing runtime tenant while auto-provisioning is disabled | Reject auth clearly and perform no provisioning writes. |
| Empty chunk reaches embedding/indexing | Skip it or return a validation error; never index a vector for `"None"`. |
| Metadata fallback candidate set exceeds cap | Return a clear too-large/degraded error; pass the cap into the loader/doc-engine request and do not load the full set into memory. |
| Retrieval raises `index_not_found_exception` for an uncreated index | Return an empty result payload. |
| Retrieval raises another `not_found` dependency/model/provider error | Preserve typed error mapping; do not turn it into empty success by string matching. |
| Dataset creation lacks `KNOWLEDGE_VENDOR_EMBEDDING_ID` in vendor mode | Startup/config error or documented fallback; do not claim external embedding E2E coverage. |
| Rerank ID is not `<model>@<tenant>@<provider>` | Retrieval may return a sanitized `502 dependency_error`; fix env wiring instead of stripping rerank. |
| Metadata lookup calls the binary content route and decodes JSON | Treat as an adapter bug; use dataset document-list metadata lookup. |
| Runtime log input includes API keys/tokens/secrets | Redact before emitting logs; never rely on caller-side masking only. |

### 5. Good/Base/Bad Cases

- Good: host-run runtime startup wires `VENDOR_RUNTIME_URL`,
  `KNOWLEDGE_VENDOR_EMBEDDING_ID`, `KNOWLEDGE_VENDOR_RERANK_ID`, and matching
  `KNOWLEDGE_RUNTIME_*` provider env keys; the runtime starts with an explicit
  route allowlist and initializes tenant defaults for embedding and rerank.
- Base: adapter unit tests use fake vendor HTTP servers to assert route paths,
  field names, metadata lookup, and sanitized vendor errors without starting
  the Python runtime.
- Bad: forwarding parser trace fields into vendor JSON, calling the binary
  content route for metadata, using bare rerank model IDs, exposing upstream
  RAGFlow MCP, or logging raw `sk-...` provider keys.

### 6. Tests Required

- Runtime Python tests assert the route allowlist registers only supported
  modules, route auth rejects legacy-only declarations, tenant provisioning is
  env-controlled, and config logging redacts nested secret/token/API-key values.
- Adapter Go contract tests assert dataset creation uses `embedding_model`,
  retrieval sends the configured `rerank_id`, parser trace fields are filtered,
  document routes require `knowledgeBaseId` without all-KB scans, invalid
  `chunkStrategy` returns `validation_error`, stable vendor status drives error
  classification, and document metadata is loaded through the dataset
  document-list endpoint.
- Runtime guardrail tests assert dataset-scope task IDs are explicit,
  whitespace chunks are skipped or rejected before embedding, and metadata
  fallback caps fail before unbounded in-memory scans.
- Docker/Compose checks must include:
  `python3 scripts/check_docker_policy.py`,
  `docker compose --env-file deploy/.env.example config --quiet`.
- For real runtime E2E, upload, parse, chunk, and query `DL_T_673-1999.pdf`
  when available; report document readiness, chunk count, query hit count, and
  the fact that no real provider key was committed.

### 7. Wrong vs Correct

#### Wrong

```text
Compose env: KNOWLEDGE_VENDOR_RERANK_ID=BAAI/bge-reranker-v2-m3
Adapter: DELETE /internal/v1/documents/{doc} -> scan every dataset to find kb
Trace: embeddingModel=vendor-default, embeddingDimension=0
Runtime auth: @login_required(auth_types=[AUTH_JWT, AUTH_API]) accepts service token
Embedding: whitespace chunk -> encode("None") -> index vector
Adapter: GET /api/v1/datasets/{kb}/documents/{doc} -> decode JSON metadata
Runtime: auto-import every restful_apis module, including mcp_api and file_api
Entrypoint: #!/usr/bin/env bash in an Alpine image without bash
```

#### Correct

```text
Compose env: KNOWLEDGE_VENDOR_RERANK_ID=BAAI/bge-reranker-v2-m3@default@SILICONFLOW
Adapter: DELETE /internal/v1/documents/{doc}?knowledgeBaseId={kb} -> dataset-scoped delete
Trace: embeddingModel from config or runtime-managed, embeddingDimension=-1 when unavailable
Runtime auth: route declaration must include GATEWAY before token is trusted
Embedding: skip whitespace chunks before vector indexing
Adapter: GET /api/v1/datasets/{kb}/documents?id={doc} -> read metadata
Runtime: register only the project-required allowlisted route modules
Entrypoint: #!/bin/sh with command args passed through Compose
```

## Scenario: Missing Downstream API Contracts

### 1. Scope / Trigger

- Trigger: a downstream service such as `knowledge`, `qa`, `document`, or an
  aggregation surface has not finalized its frontend/backend contract yet.
- Applies to `docs/services/gateway/api/public.openapi.yaml`,
  `docs/services/gateway/README.md`,
  `docs/architecture/service-boundaries.md`, and
  `docs/architecture/frontend-backend-contract.md`.

### 2. Signatures

Unfinalized endpoints must not be added as active `paths` operations in:

```text
docs/services/gateway/api/public.openapi.yaml
```

Instead, list them under the OpenAPI root extension:

```yaml
x-missing-contracts:
  - service: gateway
    status: missing
    reason: Management overview aggregation fields are not finalized yet.
    placeholderOperations:
      - GET /api/v1/admin-overview
      - GET /api/v1/admin-metrics
```

### 3. Contracts

Active OpenAPI `paths` represent stable frontend-facing contracts. Missing
placeholder operations are TODO markers only:

- frontend clients must not generate callable methods from placeholders,
- backend services must not treat placeholders as implementation commitments,
- docs may describe expected ownership, but not stable request/response fields.

### 4. Validation & Error Matrix

| Condition | Required handling |
| --- | --- |
| Endpoint request/response shape is finalized | Add an active OpenAPI operation with owner, schemas, and error responses. |
| Endpoint owner is known but shape is not finalized | Keep it in `x-missing-contracts` only. |
| Placeholder overlaps with an active operation | Use method-level placeholders, not broad path globs that hide stable operations. |
| Frontend needs a missing endpoint | First finalize and review the OpenAPI operation, then generate or implement clients. |

### 5. Good/Base/Bad Cases

- Good: keep management overview and cross-service metric aggregation under
  `x-missing-contracts` until request, response, owner, and aggregation source
  fields are finalized.
- Base: keep only management overview or metric aggregation placeholders in
  `x-missing-contracts` until their sources and display fields are finalized.
  Do not mark QA, document, knowledge, auth, or admin runtime configuration
  routes missing once they are active paths in the gateway OpenAPI.
- Bad: add placeholder management overview schemas to OpenAPI active `paths`
  just to reserve routes.

### 6. Tests Required

For documentation-only contract changes:

- Parse `docs/services/gateway/api/public.openapi.yaml`.
- Verify every active `/api/v1/**` operation has an allowed finalized owner.
- Verify only genuinely unfinalized downstream areas are present in
  `x-missing-contracts`.
- Check broad placeholders do not contradict active operations.
- Check Markdown links resolve.

### 7. Wrong vs Correct

#### Wrong

```text
OpenAPI paths include GET /api/v1/admin-overview with made-up fields even
though management overview aggregation is still listed as missing.
```

#### Correct

```text
x-missing-contracts lists only placeholders such as
GET /api/v1/admin-overview and GET /api/v1/admin-metrics until those contracts are finalized.
```

## Scenario: Domain Service Interface Documents

### 1. Scope / Trigger

- Trigger: adding or changing a service-level interface document such as
  `docs/services/auth/README.md` or `docs/services/file/README.md`.
- Applies when gateway-facing routes depend on an internal domain service
  contract, even if the service code has not been implemented yet.

### 2. Signatures

Service interface documents must list every related gateway route with:

- HTTP method
- gateway path
- authentication requirement
- owner service
- short behavior summary

If an internal service route is proposed, mark it as an internal draft and keep
it separate from the public gateway contract.

### 3. Contracts

Document request and response fields using the same public IDs, timestamps,
envelopes, and error shapes defined in
`docs/services/gateway/api/public.openapi.yaml`.
Binary success responses, such as file content, may omit the JSON envelope,
but error responses must still use the standard error shape.

### 4. Validation & Error Matrix

For each documented endpoint, separate:

- status codes already declared in OpenAPI,
- future status codes that require an OpenAPI update before frontend reliance.

### 5. Good/Base/Bad Cases

- Good: `docs/services/file/README.md` documents file-owned routes, notes knowledge-owned
  related routes, and calls out that object keys must not reach the frontend.
- Base: a service document summarizes the gateway OpenAPI without adding
  implementation-only behavior.
- Bad: a service document declares a new frontend-facing status code or field
  as stable without updating `docs/services/gateway/api/public.openapi.yaml`.

### 6. Tests Required

For documentation-only changes:

- Parse `docs/services/gateway/api/public.openapi.yaml`.
- Verify documented public paths exist in the OpenAPI file.
- Check Markdown links resolve.

When implementation exists, add handler or client tests for the documented
status codes, envelopes, request id propagation, and context headers.

### 7. Wrong vs Correct

#### Wrong

```text
docs/services/file/README.md declares GET /api/v1/files/{id}/download as stable
docs/services/gateway/api/public.openapi.yaml has no matching public path
```

#### Correct

```text
docs/services/file/README.md references /api/v1/documents/{documentId}/content
docs/services/gateway/api/public.openapi.yaml owns the same public path and owner-service marker
```

## Scenario: Internal Domain Service APIs

### 1. Scope / Trigger

- Trigger: implementing a domain service HTTP API that gateway or another backend
  service will call directly, even when the public gateway contract is unchanged.
- Applies to `services/<service>/api/openapi.yaml`, `services/<service>/internal/http/`,
  service README files, and matching domain docs such as
  `docs/services/file/README.md`.

### 2. Signatures

Internal domain-service routes must use service-local versioned resource paths:

```text
GET /healthz
GET /readyz
/internal/v1/**
```

Business routes under `/internal/v1/**` must remain RESTful and resource-oriented.
They may be close to public gateway paths, but they are not public frontend
contracts unless the same operation is active in
`docs/services/gateway/api/public.openapi.yaml`.

### 3. Contracts

Every implemented domain service should document internal API signatures in:

```text
services/<service>/api/openapi.yaml
```

Internal JSON responses use the same envelope and error shapes as gateway:

```json
{ "data": {}, "requestId": "req_123" }
```

```json
{ "error": { "code": "validation_error", "message": "request validation failed", "requestId": "req_123" } }
```

Internal metadata responses may include service-owned integration fields that
are not yet public frontend fields, for example `contentType` or `sizeBytes`
for file-owned metadata. They must not expose storage object keys, bucket names,
internal URLs, SQL details, tokens, credentials, vector payloads, or prompts.

Domain services must accept gateway context headers when present:

| Header | Purpose |
| --- | --- |
| `X-Request-Id` | Correlate gateway, service logs, and downstream calls. |
| `X-User-Id` | Authenticated user identity injected by gateway. |
| `X-User-Roles` | Comma-separated roles injected by gateway. |
| `X-User-Permissions` | Comma-separated permissions injected by gateway. |
| `X-Forwarded-For` | Original client address chain. |
| `X-Forwarded-Proto` | Original request protocol. |

### 4. Validation & Error Matrix

| Condition | Internal response |
| --- | --- |
| Invalid request shape or field value | `400 validation_error` |
| Missing required gateway user context | `401 unauthorized` |
| Authenticated caller lacks permission | `403 forbidden` |
| Resource does not exist, is deleted, or should be hidden | `404 not_found` |
| State conflict | `409 conflict` |
| Infrastructure dependency failed | `502 dependency_error` |
| Unexpected service failure | `500 internal_error` |

### 5. Good/Base/Bad Cases

- Good: file service adds `GET /internal/v1/documents/{documentId}` for
  file-owned metadata and documents it in `services/file/api/openapi.yaml`,
  while public `GET /api/v1/documents/{documentId}` remains knowledge-owned and
  exposes only knowledge document fields.
- Base: gateway proxies an active public route to a matching internal route and
  normalizes any service-owned extra fields before returning to frontend.
- Bad: a domain service adds a public-looking `/api/v1/**` route or exposes raw
  object keys, bucket names, MinIO URLs, SQL errors, prompts, or vector payloads
  in an internal response body.

### 6. Tests Required

When implementation exists:

- Handler tests assert envelope shape, request id propagation, and expected
  status codes for validation, auth context failure, not found, and dependency
  failures where applicable.
- DTO or handler tests assert service-owned integration fields are returned only
  by internal contracts when they are not public gateway fields.
- Content or streaming endpoints assert binary success responses and JSON error
  responses separately.
- Cross-service client tests assert gateway context headers are propagated.

### 7. Wrong vs Correct

#### Wrong

```text
services/file exposes GET /api/v1/documents/{documentId}
response includes objectKey: documents/doc_123
```

#### Correct

```text
services/file exposes GET /internal/v1/documents/{documentId}
response includes contentType and sizeBytes, but no objectKey
public GET /api/v1/documents/{documentId} stays knowledge-owned and does not expose objectKey
```

## Scenario: Document Report Template And Material APIs

### 1. Scope / Trigger

- Trigger: adding or changing Document Service report-type, report-template,
  template-structure, or report-material APIs.
- Applies to `services/document/internal/http`, `services/document/internal/service`,
  `services/document/internal/repository`, `services/document/internal/platform/fileclient`,
  and the matching gateway contract in `docs/services/gateway/api/public.openapi.yaml`.

### 2. Signatures

Service-local Document routes should mirror the gateway resource paths unless the
team introduces a versioned internal Document API:

- `GET /report-types`
- `GET /report-templates`
- `POST /report-templates` with multipart field `file`, `templateName`,
  `reportType`, and optional `description`
- `GET /report-templates/{reportTemplateId}`
- `PATCH /report-templates/{reportTemplateId}` with optional `templateName`,
  `description`, and `enabled`
- `DELETE /report-templates/{reportTemplateId}`
- `GET /report-templates/{reportTemplateId}/structure`
- `PATCH /report-templates/{reportTemplateId}/structure`
- `GET /report-materials`
- `POST /report-materials` with multipart field `file`, `materialName`,
  `materialType`, optional `category`, `description`, and `tags`
- `GET /report-materials/{materialId}`
- `DELETE /report-materials/{materialId}`

Document calls File Service through:

- `POST /internal/v1/files`
- `DELETE /internal/v1/files/{fileId}` for best-effort cleanup when a Document
  business insert fails after upload

### 3. Contracts

- Gateway-facing responses use `{ data, requestId }`; list responses use
  `{ data, page, requestId }`.
- Public template DTOs may include `id`, `templateName`, `reportType`, `version`,
  `description`, `enabled`, `filename`, `fileSize`, `createdBy`, `createdAt`,
  and `updatedAt`.
- Public material DTOs may include `id`, `materialName`, `materialType`,
  `category`, `description`, `tags`, `enabled`, `filename`, `fileSize`,
  `createdBy`, `createdAt`, and `updatedAt`.
- Template structure follows gateway OpenAPI: `outlineSchema` array and
  `styleConfig` object. Do not expose `materialMappings` unless the gateway
  OpenAPI contract is updated first.
- Document may persist `file_ref` internally, but public responses must not
  expose File Service IDs, `file_ref`, buckets, object keys, internal URLs,
  signed URLs, or storage credentials.
- Template/material deletion should soft-delete business rows with `deleted_at`
  and hide them from list/detail responses.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing gateway user context | `401 unauthorized` |
| Invalid page, pageSize, enabled, UUID, JSON shape, or multipart body | `400 validation_error` |
| Missing `templateName`, `reportType`, `materialName`, `materialType`, or upload file | `400 validation_error` |
| Template upload is not a DOCX in the first implementation slice | `400 validation_error` |
| Disabled or missing report type on template create | `400 validation_error` |
| Missing or soft-deleted template/material | `404 not_found` |
| File Service upload failure | `502 dependency_error` |
| PostgreSQL query/insert/update failure | `502 dependency_error` unless a typed domain error applies |

### 5. Good/Base/Bad Cases

- Good: handler parses multipart, service validates business fields and calls
  File Service, repository stores `file_ref` plus safe display metadata, and the
  response omits all internal file identifiers.
- Base: template/material rows are soft-deleted and hidden from read APIs while
  historical report references remain intact.
- Bad: returning File Service `id` as a public template/material field, exposing
  object storage details, or calling File Service while holding a database
  transaction.

### 6. Tests Required

- Handler tests for response envelopes, pagination metadata, request ID
  propagation, invalid query parameters, missing upload file, and JSON decode
  errors.
- Service tests or handler fakes for File Service dependency failure mapping,
  required field validation, DOCX validation, and disabled/missing report type.
- Repository integration tests, when `DOCUMENT_TEST_DATABASE_URL` is available,
  for list filters, soft delete hiding, structure JSON round-trip, and tags JSON
  round-trip.
- Response safety tests asserting public bodies do not contain `file_ref`,
  `fileRef`, raw File Service IDs, object keys, buckets, internal URLs, or signed
  URLs.

### 7. Wrong vs Correct

#### Wrong

```text
POST /report-templates -> document stores uploaded bytes itself -> response returns fileRef/fileId
```

#### Correct

```text
POST /report-templates -> document calls file /internal/v1/files -> stores file_ref internally -> response returns only template id and safe display metadata
```

## Scenario: Document Report Job Creation

### 1. Scope / Trigger

- Trigger: adding or changing Document Service report job creation, report job
  target payloads, generation job lifecycle metadata, worker enqueue behavior, or
  matching Gateway/Document OpenAPI contracts.
- Applies to `services/document/internal/http`, `services/document/internal/service`,
  `services/document/internal/repository`, `services/document/internal/worker`,
  `services/document/api/openapi.yaml`, `docs/services/document/api/public.openapi.yaml`,
  and `docs/services/gateway/api/public.openapi.yaml`.

### 2. Signatures

- `POST /reports/{reportId}/jobs`
- `GET /reports/{reportId}/jobs`
- `GET /report-jobs/{jobId}`
- `GET /report-jobs/{jobId}/attempts`
- `POST /report-jobs/{jobId}/attempts`

`CreateReportJobRequest.target.scope` submit-time enum values are exactly:

```text
report | section
```

### 3. Contracts

- Report jobs are the public resource for long-running outline generation,
  content generation, section regeneration, and report file creation.
- Supported `jobType` values are `outline_generation`, `outline_regeneration`,
  `content_generation`, `content_regeneration`, `section_regeneration`, and
  `report_file_creation`.
- `target.scope` must only list implemented submit-time values in OpenAPI.
  Reserved or future values such as `outline` or `file` must not be placed in
  the enum until the service accepts them.
- Omitted `target.scope` means report-level generation. `section_regeneration`
  requires `target.sectionId`, and the section must exist on the same report.
  `target.scope=section` and any submitted `target.sectionId` are valid only
  for `section_regeneration`; report-level job types must reject them before
  persisting a job.
- Deleted reports are not valid job targets. `ReportStatusDeleted` and non-nil
  `DeletedAt` must be rejected before creating a `report_jobs` row, an initial
  `report_job_attempts` row, a report file row, or an asynq task.
- For accepted jobs, PostgreSQL remains the authority for job, attempt, event,
  report generation status, and report file state. Redis/asynq only queues work.
- The initial durable state for an accepted job (`report_jobs`, generation
  status snapshot, initial `report_job_attempts`, and optional `report_files`)
  must be created atomically before enqueue. Any failure before enqueue must
  roll back the initial rows instead of leaving a pending job without an
  attempt or queue task.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Unsupported `jobType` | `400 validation_error` |
| Unsupported `target.scope` | `400 validation_error` |
| Non-`section_regeneration` job submits `target.scope=section` or `target.sectionId` | `400 validation_error` before job/attempt/file/enqueue side effects |
| `section_regeneration` missing `target.sectionId` | `400 validation_error` |
| Target section is missing or belongs to another report | `404 not_found` |
| Report is soft-deleted by status or `deleted_at` | `409 conflict` before persistence/enqueue |
| Report becomes invalid while creating initial durable job state | rollback job/attempt/file rows and return the typed conflict or dependency error |
| Job accepted but enqueue fails | mark created job/attempt/file failed and return dependency error |
| PostgreSQL query/insert/update failure | `502 dependency_error` unless a typed domain error applies |

### 5. Good/Base/Bad Cases

- Good: a deleted report returns `409 conflict` before any job, attempt, file, or
  queue side effect; a post-job-insert conflict before enqueue rolls back the
  inserted job; target scope enums in service-local, Document public, and Gateway
  public OpenAPI all match `report | section`; `content_generation` with a
  section target returns `400 validation_error`.
- Base: a report-level generation request omits `target`, creating a pending job
  and first pending attempt before enqueueing the asynq task.
- Bad: OpenAPI advertises `target.scope: file` while the service returns
  `unsupported target scope`, a deleted/concurrently invalid report leaves an
  orphan pending job, or `content_generation` persists a section target that the
  worker later ignores.

### 6. Tests Required

- Service tests must cover deleted-report rejection for every supported job type
  and assert no job, attempt, report file, or enqueue side effect occurs.
- Service or repository tests must cover a failure after `report_jobs` insert but
  before enqueue and assert the inserted initial job state is rolled back.
- Service tests must cover section target existence and same-report ownership.
- Service tests must reject section targets for every non-`section_regeneration`
  job type and assert no job, attempt, report file, or enqueue side effect.
- Contract checks must parse the changed OpenAPI files and verify
  `CreateReportJobRequest.target.scope` only advertises implemented values.
- Run `cd services/document && go test ./...` and
  `cd services/document && go build ./cmd/server`.
- Run `git diff --check`.

### 7. Wrong vs Correct

#### Wrong

```text
OpenAPI target.scope enum: report | outline | section | file
CreateJob(report status=deleted) -> insert report_jobs(status=pending) -> enqueue/update later fails
CreateJob(jobType=content_generation, target.scope=section) -> persisted section target -> worker generates all sections
```

#### Correct

```text
OpenAPI target.scope enum: report | section
CreateJob(report status=deleted) -> 409 conflict before job, attempt, file, or enqueue side effects
CreateJob(jobType=content_generation, target.scope=section) -> 400 validation_error before persistence
```

## Scenario: Document Report Settings Statistics And Operation Logs

### 1. Scope / Trigger

- Trigger: adding or changing Document Service report settings, report
  statistics, operation-log APIs, AI Gateway profile validation for report
  settings, or operation-log write paths.
- Applies to `services/document/internal/http`, `services/document/internal/service`,
  `services/document/internal/repository`, `services/document/internal/platform/aigateway`,
  `services/document/internal/worker`, `services/document/migrations`, and the
  matching gateway contract in `docs/services/gateway/api/public.openapi.yaml`.

### 2. Signatures

Service-local Document routes mirror the gateway resource paths:

- `GET /report-settings`
- `PATCH /report-settings`
- `GET /report-statistics/overview`
- `GET /report-statistics/daily?days=<1..366>`
- `GET /report-operation-logs?page=&pageSize=&targetType=&targetId=&operationType=&requestId=&requestSource=&toolName=`

Database and integration signatures:

- `report_settings` stores singleton settings with `llm_json`,
  `default_templates_json`, `file_json`, and `updated_at`.
- `report_operation_logs` uses existing `parameter_summary_json` and
  `metadata_json` columns.
- Document validates settings profiles through AI Gateway
  `GET /internal/v1/model-profiles/{profileId}` with `X-Caller-Service:
  document`, propagated request/user headers, and optional `X-Service-Token`.
- Runtime env includes `DOCUMENT_AI_GATEWAY_URL`,
  `DOCUMENT_AI_GATEWAY_PROFILE_ID`, optional
  `DOCUMENT_AI_GATEWAY_SERVICE_TOKEN`, and optional fallback
  `INTERNAL_SERVICE_TOKEN`.

### 3. Contracts

- Gateway-facing responses use `{ data, requestId }`; operation-log lists use
  `{ data, page, requestId }`.
- `GET /report-settings`, `PATCH /report-settings`,
  `GET /report-statistics/overview`, `GET /report-statistics/daily`, and
  `GET /report-operation-logs` are management/audit surfaces and must reject
  non-admin callers in the Document service layer even when gateway already
  authenticates the user.
- `ReportSettings.llm.provider` is fixed to `ai-gateway`; provider base URLs
  and API keys remain owned by AI Gateway and must not be stored in Document.
- `ReportSettings.defaultTemplates` is a full `reportType ->
  reportTemplateId` map, not a single default template id.
- `PATCH /report-settings` may update only the supplied sections. Omitted
  `llm.profileId` preserves the current profile/model; explicit empty
  `profileId` clears the profile/model.
- Omitted `file.defaultStyleProfileId` preserves the current style profile;
  explicit empty `file.defaultStyleProfileId` clears it.
- Statistics overview includes `reportCount`, `templateCount`,
  `materialCount`, optional `jobStatusCounts`, and `recentDays`; daily
  statistics is bounded by `days`.
- Operation-log public filters are exactly the gateway-documented filters:
  `targetType`, `targetId`, `operationType`, `requestId`, `requestSource`, and
  `toolName`. Adding public filters requires a gateway OpenAPI update first.
- Operation logs may store sanitized summaries only. They must not include
  prompt text, raw document content, File Service IDs/file refs, object keys,
  buckets, signed URLs, internal URLs, provider tokens, API keys, database URLs,
  or full request/response bodies. Sanitization must inspect string values, not
  only sensitive field names, and mutation paths must not write user-provided
  free text such as retry reasons into operation-log summaries.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing gateway user context | `401 unauthorized` |
| Non-admin caller reads or patches settings, statistics, or operation logs | `403 forbidden` |
| Unsupported `llm.provider` | `400 validation_error` |
| Non-empty `llm.profileId` missing, disabled, or not a chat profile | `400 validation_error` |
| `defaultTemplates` report type is missing/disabled | `400 validation_error` |
| `defaultTemplates` template is missing, disabled, soft-deleted, or wrong report type | `400 validation_error` |
| Unsupported `file.defaultFormat` or `file.defaultNumberingMode` | `400 validation_error` |
| `days` outside `1..366` or invalid pagination | `400 validation_error` |
| AI Gateway or PostgreSQL failure | `502 dependency_error` |

### 5. Good/Base/Bad Cases

- Good: settings update validates profile and template references before
  saving, returns only `updatedAt`, and writes a sanitized
  `update_report_settings` operation log.
- Base: statistics queries use bounded date filters or indexed count/group
  paths, and operation-log pagination runs a separate count so empty pages keep
  the correct `total`.
- Bad: storing a single `default_template_id`, accepting missing profile/template
  references, returning `trend30d` instead of the gateway statistics schema, or
  exposing prompts/object keys in `parameterSummary`.

### 6. Tests Required

- Handler tests for settings/statistics/log response envelopes, request id
  propagation, query parsing, PATCH clear-vs-omit semantics, and route coverage
  no longer returning `not_implemented`.
- Service tests for admin authorization, profile validation, default-template
  validation, file-default validation, bounded days, operation-log filtering,
  and sensitive-field sanitization.
- Repository or migration tests for `report_settings`, operation-log insert/list
  with separate total count, daily statistics bounds, soft-delete exclusion, and
  indexes for every documented operation-log filter.
- Mutation-path tests confirming templates, materials, reports, outlines,
  sections, jobs, retries, worker status transitions, and failure paths record
  sanitized operation logs where Document owns the write.

### 7. Wrong vs Correct

#### Wrong

```text
PATCH /report-settings -> store default_template_id=tpl_1 and llm.profileId=missing
GET /report-operation-logs -> return raw prompt, fileRef, and objectKey
```

#### Correct

```text
PATCH /report-settings -> validate defaultTemplates[reportType] and AI Gateway chat profile -> store JSON settings map
Document mutation -> record operation log with IDs and low-sensitive metadata only
GET /report-operation-logs -> filter by documented fields and return sanitized summaries
```

## Scenario: Gateway Redis Session Cache

### 1. Scope / Trigger

- Trigger: adding or changing user creation, session creation, current session
  deletion, current-user behavior, auth middleware, or session identity fields.
- Applies to `services/gateway/`, `services/auth/`,
  `docs/services/auth/README.md`, `docs/services/gateway/README.md`,
  `docs/architecture/frontend-backend-contract.md`, and
  `docs/services/gateway/api/public.openapi.yaml`.

### 2. Signatures

Public auth routes stay under:

```text
POST /api/v1/users
POST /api/v1/sessions
DELETE /api/v1/sessions/current
GET  /api/v1/users/me
```

Auth success responses must include `data.user` and `data.session`.

### 3. Contracts

`data.user` must include:

- `id`
- `username`
- `roles`
- `permissions`

`data.session` must include:

- `sessionId`
- `accessToken`
- `tokenType`
- `expiresAt`

`accessToken` is an opaque random Bearer token, not a JWT. Gateway, frontend,
and downstream services must not parse claims from it.

Auth stores password credentials with `argon2id` and stores access-token hashes,
not raw access tokens. Gateway Redis cache keys use the token hash and must not
log raw tokens or hashes.

Gateway must store the runtime session in Redis using:

```text
gateway:session:<accessTokenHash>
```

The cached value must include enough fields to inject `X-User-Id`,
`X-User-Roles`, and `X-User-Permissions` without calling auth on every
business request. The Redis TTL must not outlive `data.session.expiresAt`.
Redis is not the durable source of user, role, permission, or session truth.

### 4. Validation & Error Matrix

| Condition | Public response |
| --- | --- |
| Missing bearer credential | `401 unauthorized` |
| Redis session miss, expired session, or malformed cache value | `401 unauthorized` |
| Auth rejects session credentials | `401 unauthorized` |
| Gateway cannot access Redis for an authenticated business request | `502 dependency_error` |
| Auth service or durable auth store is unavailable during user/session operations | `502 dependency_error` |

Do not expose raw tokens, token hashes, Redis keys, session secrets, or auth
internal URLs to frontend responses or logs.

### 5. Good/Base/Bad Cases

- Good: session creation response returns `user` plus `session`; gateway hashes the access
  token for the Redis key, sets TTL from `expiresAt`, and injects downstream
  identity headers from the cache.
- Base: `/api/v1/users/me` reads the Redis session cache and returns `UserResponse`
  without calling auth for every request.
- Bad: gateway stores original access tokens in logs or treats Redis as the
  durable source of permissions.

### 6. Tests Required

When implementation exists:

- Auth handler/client tests assert `SessionResponse` includes `user.permissions`
  and `session`.
- Gateway auth middleware tests cover Redis hit, miss, expired session,
  malformed session, and Redis dependency failure.
- Gateway downstream client tests assert `X-User-Id`, `X-User-Roles`,
  `X-User-Permissions`, and `X-Request-Id` are propagated.
- Current-session deletion tests assert auth invalidation is called and Redis cache is deleted.

For documentation-only changes:

- Parse `docs/services/gateway/api/public.openapi.yaml`.
- Verify `SessionResponse` requires `user` and `session`.
- Verify docs mention `gateway:session:<accessTokenHash>` and Redis TTL.

### 7. Wrong vs Correct

#### Wrong

```text
gateway receives Authorization: Bearer token
gateway calls auth service on every business request
gateway logs the raw token on failures
```

#### Correct

```text
gateway receives Authorization: Bearer token
gateway hashes token and reads gateway:session:<accessTokenHash>
gateway injects cached user, roles, and permissions into downstream headers
```

## Scenario: Auth Service Source-of-Truth API

### 1. Scope / Trigger

- Trigger: changing user creation, session creation, token hashing, RBAC source
  reads, session revocation, security events, or auth-owned migrations.
- Applies to `services/auth/internal/service`, `services/auth/internal/http`,
  `services/auth/internal/repository`, `services/auth/migrations`,
  `services/auth/api/openapi.yaml`, `docs/services/auth/api/public.openapi.yaml`,
  and `docs/services/auth/api/internal.openapi.yaml`.

### 2. Signatures

- Internal routes:
  - `POST /internal/v1/users`
  - `POST /internal/v1/sessions`
  - `GET /internal/v1/users/{userId}`
  - `GET /internal/v1/users/{userId}/permissions`
  - `GET /internal/v1/sessions/{sessionId}`
  - `DELETE /internal/v1/sessions/{sessionId}`
- Required caller context: `X-Service-Token` and `X-Caller-Service`; propagate
  `X-Request-Id` when present.
- In OpenAPI, model service-token authentication as an API key header:
  `type: apiKey`, `in: header`, `name: X-Service-Token`. Do not model
  project service tokens as `Authorization: Bearer` unless the implementation
  actually accepts the `Authorization` header.
- Environment keys:
  - `AUTH_DATABASE_URL`
  - `AUTH_INTERNAL_SERVICE_TOKEN` required when `AUTH_DATABASE_URL` is set
  - `AUTH_TOKEN_HASH_SECRET` required when `AUTH_DATABASE_URL` is set
  - `AUTH_TOKEN_HASH_KEY_VERSION`, default `v1`
  - `AUTH_SESSION_TTL`, default `24h`
  - `AUTH_DEFAULT_ROLE_CODE`, default `standard`
- Database source tables include `auth_users`, `auth_credentials`,
  `auth_roles`, `auth_permissions`, `user_roles`, `role_permissions`,
  `auth_sessions`, `session_revocations`, and `auth_security_events`.

### 3. Contracts

- `POST /internal/v1/users` creates a user, password credential, default role
  assignment, session, and security events, then returns
  `{ data: { user, session }, requestId }`.
- `POST /internal/v1/sessions` validates username/password without account
  enumeration and returns the same session response shape.
- Passwords are stored as `argon2id-v1` PHC strings with `m=65536`, `t=3`,
  `p=2`, `salt=16`, and `key=32`.
- Access tokens are opaque bearer tokens. Auth persists only
  `hmac-sha256:<keyVersion>:<hex>` token hashes.
- Raw access tokens may appear only in create-user/create-session success
  responses. Session read responses must not include raw tokens and should not
  include token hashes unless a reviewed internal diagnostics contract requires
  it.
- Default role/permission seed data must include `standard`, `admin`, and
  `super_admin` system roles.
- Security events must cover user creation, session creation failure, session
  creation success, default role assignment, and session revocation.
- Security events that are part of the same durable transaction may fail the
  operation and roll back the business write. Security events written after a
  durable user/session/revocation write has already committed are best-effort:
  log a structured warning, but do not return a failed response for business
  state that is already effective.

### 4. Validation & Error Matrix

| Condition | Response/error |
| --- | --- |
| Missing or blank username/password | `400 validation_error` |
| Missing or invalid service token | `401 unauthorized` |
| Missing internal caller context | `401 unauthorized` |
| Unknown username or wrong password | `401 unauthorized` with the same message |
| Disabled, locked, or otherwise unavailable user | `401 unauthorized` |
| Duplicate username | `409 conflict` |
| Missing user/session source record | `404 not_found` for internal reads/deletes |
| Missing database or token hash secret at runtime | `502 dependency_error` |
| Repository or migration-dependent write fails | `502 dependency_error` |
| Post-commit security event write fails after successful durable write | success response is preserved; log `warn` with `operation=record_security_event` |

### 5. Good/Base/Bad Cases

- Good: handler decodes JSON and maps path values; service validates
  credentials and generates password/token material; repository writes SQL
  records and maps rows back to domain structs; post-commit security-event
  failures are logged without making successful user/session writes look
  failed; response exposes only safe DTOs.
- Base: gateway calls auth once for user/session creation, stores the returned
  session identity in Redis, and later uses auth source reads only for cache
  repair or revocation workflows.
- Bad: handler hashes passwords directly, stores raw access tokens, returns
  `accessTokenHash` to public callers, or logs raw credentials/token material.

### 6. Tests Required

- Service tests for duplicate username, wrong password, token hash generation,
  session creation, security-event recording, post-commit security-event
  failure semantics, and revoked token lookup failure.
- HTTP tests for success envelopes, request id propagation, validation errors,
  missing caller context, and no token/hash leakage from session read responses.
- Repository tests for explicit-column queries, user roles/permissions mapping,
  revocation mapping, and security event writes where database tooling exists.
- Config tests for `AUTH_TOKEN_HASH_SECRET` requirements and TTL/key-version
  parsing.

### 7. Wrong vs Correct

#### Wrong

```text
POST /internal/v1/sessions -> handler verifies password -> DB stores accessToken
GET /internal/v1/sessions/{id} -> returns accessTokenHash to gateway/frontend
OpenAPI serviceTokenAuth -> Authorization: Bearer, while handler reads X-Service-Token
POST /internal/v1/users commits user -> post-commit event fails -> handler returns 502
```

#### Correct

```text
POST /internal/v1/sessions -> service verifies argon2id password -> DB stores hmac token hash
GET /internal/v1/sessions/{id} -> returns session identity without raw token/hash
OpenAPI serviceTokenAuth -> apiKey header X-Service-Token, matching handler auth
POST /internal/v1/users commits user -> post-commit event fails -> warn log + 201 response
```

# MCP Agent Runtime

> Executable contract for the QA ReAct loop, MCP client, and model Function Calling adapter.

## Scenario: QA MCP Agent Loop

### 1. Scope / Trigger

- Trigger: changing `services/qa` model messages, tool discovery/execution, MCP transport, runtime limits, or provider environment wiring.
- The QA service is the MCP Host and owns the Agent Loop. The model API chooses tools; MCP Servers execute tools; neither dependency owns QA conversation state.

### 2. Signatures

MCP lifecycle and tool methods:

```text
initialize
notifications/initialized
tools/list
tools/call
```

Service boundaries:

```go
type ModelClient interface {
    Complete(context.Context, []Message, []ToolDefinition) (Completion, error)
}

type ToolClient interface {
    ListTools(context.Context) ([]ToolDefinition, error)
    CallTool(context.Context, string, json.RawMessage) (ToolResult, error)
}
```

Supported MCP transports:

```text
disabled         # built-in Function Calling tools only
streamable_http  # runtime MCP server
stdio            # package-test-only SDK lifecycle helper
```

### 3. Contracts

Environment keys:

| Key | Required | Contract |
| --- | --- | --- |
| `DEEPSEEK_API_KEY` | when `AI_GATEWAY_URL` is absent | Provider token; never log or persist. |
| `DEEPSEEK_BASE_URL` | no | Defaults to `https://api.deepseek.com`; `/chat/completions` is appended. |
| `MODEL_ID` | no | Defaults to `deepseek-v4-pro`. |
| `AI_GATEWAY_URL` | production override | Full OpenAI-compatible endpoint; takes precedence over direct DeepSeek settings. |
| `MCP_TRANSPORT` | no | Defaults to `disabled`; runtime values are `disabled` and `streamable_http`; `stdio` is rejected outside package tests. |
| `MCP_SERVER_COMMAND` | no | Reserved for package-owned stdio tests; runtime configuration must not launch local commands. |
| `MCP_SERVER_ARGS_JSON` | no | Reserved for package-owned stdio tests; runtime MCP servers use Streamable HTTP. |
| `MCP_SERVER_URL` | for HTTP | Absolute HTTP(S) Streamable HTTP endpoint. |
| `MCP_SERVER_ALIAS` | for HTTP | Stable `[a-z0-9_]{2,32}` namespace; model-facing names are `<alias>__<server-tool>`. |
| `MCP_SERVER_TOKEN` | no | Remote MCP credential; never log or persist. |
| `MCP_SERVER_TOKEN_HEADER` | no | Credential header, default `Authorization`; Authorization tokens are sent as Bearer. |
| `MCP_TOOL_TIMEOUT` | no | Positive Go duration, default `30s`. |
| `AGENT_MAX_ITERATIONS` | no | Positive integer, default `8`. |
| `AGENT_WORKDIR` | no | Existing workspace root for built-in file/command tools; defaults to process cwd. |
| `AGENT_MAX_FILE_BYTES` | no | Positive read/write/edit limit, default 1 MiB. |
| `AGENT_ENABLE_COMMAND_TOOL` | no | Boolean, default `false`; controls whether `bash` is model-visible. |
| `AGENT_COMMAND_TIMEOUT` | no | Positive Go duration, default `120s`. |

Mapping contract:

```text
MCP Tool{name, description, inputSchema}
  -> model function{name, description, parameters}

model assistant.tool_calls[]
  -> MCP tools/call(name, arguments)

MCP CallToolResult
  -> role=tool, tool_call_id=<model call id>, content=<bounded result>
```

Runtime MCP configuration merge:

```text
database mcp_servers metadata + matching environment bootstrap credential
  -> RuntimeMCPConfig[]
```

Built-in Function Calling tools:

```text
read_file(path, limit?)
write_file(path, content)
edit_file(path, old_text, new_text)
bash(command, timeout_seconds?)  # only when explicitly enabled
```

- The SDK owns initialization and JSON-RPC framing.
- Stdio server stdout contains MCP JSON-RPC only; diagnostics use stderr.
- The loop executes every tool call in a model turn, appends correlated tool results, and calls the model again.
- Built-in and MCP tools are merged behind one `ToolClient`; duplicate names fail discovery instead of silently shadowing another tool.
- Database MCP rows are the administrative authority. For a matching alias, an encrypted database token wins; when the enabled row has no token, use the environment bootstrap token. A disabled matching row suppresses bootstrap. When only unrelated database aliases exist, append bootstrap instead of dropping it.
- Local seeds may store alias, endpoint, header, timeout, and enabled state, but must not persist plaintext or environment-specific encrypted service tokens.
- File tool paths are relative to `AGENT_WORKDIR` and checked after symlink resolution. File content must be UTF-8 and bounded.
- The command tool runs inside `AGENT_WORKDIR`, has bounded output and timeout, and is disabled by default. It only permits path-free diagnostic commands; file access must use the workspace-bounded file tools.
- Tool failures become sanitized tool-result messages so the model can recover. Raw downstream errors, prompts, credentials, and internal payloads are not returned to the model or frontend.
- The loop terminates on a final non-empty assistant message, context cancellation, dependency failure, or maximum iterations.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Missing model URL/key/model | Startup configuration error. |
| Stdio transport in runtime configuration | Startup configuration error; use `streamable_http`. |
| HTTP without absolute endpoint | Startup configuration error. |
| Matching enabled database alias without stored token | Merge the matching environment token; retain database endpoint/timeout metadata. |
| Matching disabled database alias | Keep disabled; do not append or re-enable environment bootstrap. |
| Database contains only unrelated MCP aliases | Keep those enabled rows and append environment bootstrap. |
| Invalid `MCP_SERVER_ARGS_JSON` | Startup configuration error; never invoke a shell. |
| Missing or invalid `AGENT_WORKDIR` | Startup configuration error. |
| Absolute, traversal, or escaping-symlink file path | Return sanitized `invalid_path`; do not access it. |
| Oversized write/edit | Return `file_too_large`. |
| Command tool disabled | Do not include `bash` in the model tool list. |
| Blocked command or command timeout | Return `command_blocked` or `command_timeout`. |
| Local/MCP duplicate tool name | Abort tool discovery with a duplicate-name error. |
| Duplicate or empty MCP tool name | Abort the run before calling the model. |
| Model requests unknown tool | Append sanitized `unknown_tool` result; do not execute it. |
| Tool arguments are not a JSON object | Append `invalid_tool_arguments`. |
| Tool call times out/fails | Append sanitized `tool_execution_failed`; preserve cancellation. |
| Model returns empty final message | Return invalid-response error. |
| Iteration limit reached | Return `ErrMaxIterations`. |

### 5. Good/Base/Bad Cases

- Good: expose workspace-bounded local tools through Function Calling, optionally merge official-SDK MCP tools, and correlate results by `tool_call_id`.
- Good: seed non-secret remote MCP metadata and inject its credential from the host environment by matching alias.
- Base: run with `MCP_TRANSPORT=disabled` and use read/write/edit only.
- Bad: treat the existence of any database MCP row as a reason to discard an unrelated environment bootstrap, or let bootstrap re-enable an explicitly disabled alias.
- Bad: parse JSON-RPC manually inside the Agent Loop, invoke commands through a shell, print logs to MCP stdout, or send tokens/tool arguments to logs.

### 6. Tests Required

- Runner unit test: tool call → tool result message → second model call → final answer.
- Runner unit test: multiple tool calls, unknown tool, tool failure, invalid final response, and maximum iterations.
- MCP integration test: real SDK lifecycle over package-test-only stdio and runtime Streamable HTTP, then `tools/list` and `tools/call`.
- Local-tool tests: read/write/edit round trip, traversal and symlink escape, file limits, command opt-in, dangerous pattern, and timeout.
- Composite-tool tests: merge and route local/MCP providers; reject duplicate names.
- Model client contract test: Authorization header, `tools`, `tool_choice`, assistant `tool_calls`, and sanitized non-2xx handling.
- Configuration tests: transport-specific required fields, JSON argument parsing, URL validation, and defaults.
- Runtime configuration tests: matching token fallback, stored-token precedence, disabled-alias precedence, and bootstrap append with unrelated database aliases.
- Service checks: `gofmt -l .`, `go vet ./...`, `go test ./...`, and `go build ./cmd/agent`.

### 7. Wrong vs Correct

#### Wrong

```text
LLM returns tool name
-> concatenate name/arguments into a shell command
-> log raw result and token
```

#### Correct

```text
LLM returns structured tool_call
-> verify it exists in the merged local/MCP registry
-> decode arguments as a JSON object
-> MCP tools/call with context timeout
-> append bounded, sanitized role=tool result using the same tool_call_id
```

## Scenario: QA HTTP, SSE, and PostgreSQL Runtime

### 1. Scope / Trigger

- Trigger: changing the QA executable server, conversation/message APIs, SSE
  events, PostgreSQL persistence, migrations, or frontend chat integration.
- Gateway owns every public `/api/v1/**` QA route. QA exposes only matching
  `/internal/v1/**` resources and requires both `X-Service-Token` and the
  Gateway-injected `X-User-Id` context.

### 2. Signatures

```text
GET    /healthz
GET    /readyz
GET/POST          /api/v1/qa-sessions
GET/PATCH/DELETE  /api/v1/qa-sessions/{sessionId}
GET/POST          /api/v1/qa-sessions/{sessionId}/messages
GET               /api/v1/qa-sessions/{sessionId}/events
GET/PATCH         /api/v1/response-runs/{responseRunId}
GET               /api/v1/response-runs/{responseRunId}/tool-calls
GET               /api/v1/messages/{messageId}/citations, /api/v1/citations/{citationId}
POST              /api/v1/citation-lookups
GET               /api/v1/qa-config-versions/current, /api/v1/llm-config-versions/current
POST              /api/v1/qa-config-versions, /api/v1/llm-config-versions
POST              /api/v1/llm-connection-tests, /api/v1/retrieval-test-runs
GET               /api/v1/retrieval-test-runs/{testRunId}
GET               /api/v1/qa-metrics/**
```

Gateway rewrites the public prefix without changing the resource suffix:

```text
/api/v1/<qa-owned-resource> -> /internal/v1/<qa-owned-resource>
```

```text
conversations -> messages -> message_content_blocks
              -> response_runs -> response_process_steps
```

### 3. Contracts

- Ask JSON uses `message`, optional `mode`, `knowledgeBaseIds`, `retrieval`,
  and `agent`; unknown fields are rejected.
- `POST .../messages` returns JSON normally and SSE when
  `Accept: text/event-stream` is present. Do not add a separate
  `messages:stream` action path.
- SSE event names are `message.created`, `agent.iteration.started`,
  `reasoning.step`, `tool.started`, `tool.completed`, `tool.failed`,
  `answer.delta`, `citation.delta`, `answer.completed`, `error`, and optional
  non-persisted `heartbeat`.
- Persist replayable events in `response_stream_events`; project safe tool
  summaries into `agent_tool_calls`; update `response_runs.current_iteration`.
- JSON responses use `{ data, requestId }`; list responses use
  `{ data, page, requestId }`. Internal errors use the same stable error
  envelope so Gateway can preserve expected 4xx errors and replace unexpected
  downstream 5xx responses with `502 dependency_error`.
- QA LLM configuration stores an AI Gateway `profileId`, model name, and safe
  generation parameters. It must not store or return provider API keys. Model
  calls send `profile_id` to AI Gateway.
- Retrieval test runs call Knowledge through
  `POST /internal/v1/knowledge-queries` with service/user/request context.
- `reasoning.step` contains only displayable Agent progress. It must never
  contain prompts, tool arguments, tool results, or private chain-of-thought.
- Model calls may use OpenAI-compatible streaming completions when configured.
  Agent progress is sent immediately. Provider `delta.content` chunks are
  buffered for each model turn and projected into ordered `answer.delta` events
  only after the completed assistant message is confirmed to contain no
  `tool_calls`. Streamed content from tool-call turns must be discarded and must
  not be emitted or persisted as public `answer.delta`; if a model returns tool
  calls when no tools were exposed, QA treats that as an invalid model response.
  Non-streaming responses, or streaming providers that do not emit answer
  content chunks, fall back to one final `answer.delta`. If emitted answer deltas
  do not match the final assistant answer, the run must fail instead of
  completing with divergent SSE/replay content.
- `QA_DATABASE_URL` is required by `cmd/server`; `cmd/agent` does not require
  PostgreSQL. `QA_HTTP_ADDR`, `QA_MAX_REQUEST_BYTES`, and
  `QA_SHUTDOWN_TIMEOUT` have safe local defaults.
- Migration files are append-only after use by a persistent Docker volume.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Missing/invalid `X-Service-Token` on an internal route | `401 unauthorized` JSON error. |
| Missing `X-User-Id` on a business route | `401 unauthorized` JSON error. |
| Empty/oversized message or invalid pagination | `400 validation_error`. |
| Unsupported `data_analysis` mode | `422 unsupported_intent`. |
| Missing or foreign session/run/message/citation | `404 not_found`; do not reveal ownership. |
| Cancelling a non-running response run | `409 conflict`. |
| PostgreSQL/model/tool failure before JSON response | `502 dependency_error`. |
| Failure after SSE headers | `event: error` with sanitized code/message. |
| Client disconnect | Cancel Agent context; bounded cleanup marks the message cancelled/failed. |
| PostgreSQL unavailable | `/healthz` stays healthy; `/readyz` returns `502`. |

### 5. Good/Base/Bad Cases

- Good: Gateway authenticates and rewrites the documented resource path; QA
  persists user/assistant/run state before the Agent call, streams safe progress,
  stores replay/tool summaries, then atomically updates final content/status.
- Base: run with `MCP_TRANSPORT=disabled`; built-in Function Calling tools and
  persisted general chat remain available.
- Bad: keep a legacy public `/api/v1/qa/conversations` alias, add
  `messages:stream`, return raw model errors, or emit tool arguments/results as
  reasoning events.

### 6. Tests Required

- Handler tests assert authentication, status codes, JSON shapes, request IDs,
  exact SSE event names, `Accept` negotiation, and no duplicate error event.
- Service tests assert history reconstruction, automatic first-question title,
  active-run cancellation, final message persistence, displayable step
  persistence, and unsupported-intent behavior.
- Repository integration checks apply all migrations to PostgreSQL and assert a
  real session/message/run/event/tool-call/config/retrieval/metrics round trip.
- Contract checks compare every active QA-owned Gateway OpenAPI operation with
  both Gateway public route registration and QA internal route registration.
- Frontend checks assert contract DTO mapping and use a browser smoke test for
  session recovery plus an SSE answer.
- Final checks: `go test ./...`, `go vet ./...`, `go build ./cmd/server`, frontend
  typecheck/lint/build, Docker health, and `/readyz`.

### 7. Wrong vs Correct

#### Wrong

```text
browser -> QA /api/v1/qa/conversations with provider token
QA exposes messages:stream and raw provider/tool payloads
browser localStorage is the only conversation history
```

#### Correct

```text
browser -> Gateway /api/v1/qa-sessions/** with Bearer token
Gateway -> QA /internal/v1/qa-sessions/** with service token + X-User-Id
QA persists user + generating assistant messages
QA Agent -> model Function Calling -> local/MCP tool
QA emits and persists safe SSE progress + answer delta + tool summaries
QA persists final message + displayable steps; Gateway preserves envelopes
```

## Scenario: Knowledge MCP Server

### 1. Scope / Trigger

- Trigger: changing `services/knowledge/internal/mcp`, MCP tool schemas, adapter bridge, or AI Gateway wiring for Knowledge-owned tools.
- Knowledge owns the **MCP Server** (tool implementation). QA and other products are **MCP Clients** only.
- MCP must not call `knowledge-runtime` directly; all tools go through the existing adapter handler layer (in-process `Bridge`).

### 2. Signatures

```text
MCP server name: knowledge-mcp
Transport: Streamable HTTP (official go-sdk)
Listener env: KNOWLEDGE_MCP_ADDR (optional; omit to disable MCP)
```

v1 tools (14):

```text
search_knowledge
answer_from_knowledge
list_knowledge_bases | get_knowledge_base | create_knowledge_base | update_knowledge_base | delete_knowledge_base
list_documents | get_document | create_document | update_document | delete_document
list_document_chunks | get_document_content
```

Bridge (in-process, no loopback HTTP):

```go
Bridge.Do(ctx, caller, method, path, body []byte) (status int, body []byte, headers http.Header, err error)
Bridge.DoJSON / DoGET / DoMultipart
```

### 3. Contracts

Environment keys:

| Key | Required | Contract |
| --- | --- | --- |
| `KNOWLEDGE_MCP_ADDR` | no | e.g. `:8084`; empty disables MCP listener |
| `KNOWLEDGE_HTTP_ADDR` | yes | Adapter REST, default `:8083` |
| `KNOWLEDGE_AI_GATEWAY_URL` | for `answer_from_knowledge` | Absolute HTTP(S) base; joins `/internal/v1/chat/completions` |
| `KNOWLEDGE_AI_GATEWAY_SERVICE_TOKEN` | no | Falls back to `INTERNAL_SERVICE_TOKEN` |

MCP session → adapter headers:

| Header | Source |
| --- | --- |
| `X-User-Id` | MCP HTTP request |
| `X-Request-Id` | MCP HTTP request or generated |
| `X-User-Roles` / `X-User-Permissions` | optional |

Tool contracts:

- `search_knowledge` → `POST /internal/v1/knowledge-queries` (retrieval only, no LLM).
- `answer_from_knowledge` → retrieval + AI Gateway chat with numbered citations in prompt; returns `{ answer, citations[], retrieval }`.
- CRUD tools → matching `/internal/v1/knowledge-bases*` and `/internal/v1/documents*` adapter routes.
- `create_document` → decode `fileContentBase64`, `Bridge.DoMultipart` to upload route.

Do **not** expose RAGFlow upstream MCP (`--enable-mcpserver`) as the product tool surface.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| `KNOWLEDGE_MCP_ADDR` unset | MCP listener not started; REST adapter unaffected |
| `answer_from_knowledge` without AI Gateway URL | Tool error: gateway not configured |
| Missing `X-User-Id` on MCP HTTP | Defaults to `mcp_anonymous` + read permission (tests); production clients must send real user id |
| Invalid base64 in `create_document` | Tool validation error |
| Adapter/vendor failure | Propagate adapter error message to MCP tool result |

### 5. Good/Base/Bad Cases

- Good: QA calls `search_knowledge` via Streamable HTTP; Knowledge forwards to adapter; citations returned for QA snapshot projection.
- Base: MCP disabled; Gateway REST on `:8083` only.
- Bad: QA calls RAGFlow runtime MCP on `:9382`; duplicate auth models; bypass adapter contract.

### 6. Tests Required

- `internal/mcp`: `tools/list` returns 14 tools; `search_knowledge` with fake vendor; `answer_from_knowledge` with fake vendor + fake AI Gateway; KB create/list; `create_document` multipart upload.
- `internal/aigateway`: chat client request headers and response decode.
- Final: `go test ./...` in `services/knowledge`.

### 7. Wrong vs Correct

#### Wrong

```text
Document service -> knowledge-runtime :9380 /api/v1/datasets/search
QA -> RAGFlow MCP with API key auth
```

#### Correct

```text
QA MCP Client -> KNOWLEDGE_MCP_ADDR (Streamable HTTP)
Knowledge MCP -> Bridge -> adapter handlers -> vendorclient -> knowledge-runtime
answer_from_knowledge -> Bridge retrieval -> KNOWLEDGE_AI_GATEWAY_URL chat
```

## Scenario: QA Project-Global Knowledge Retrieval

### 1. Scope / Trigger

- Trigger: changing QA long-term RAG scope, QA `defaultKnowledgeBaseIds`,
  Knowledge MCP search behavior, Knowledge `knowledge-queries`, citation source
  visibility checks, or runtime identity configuration.
- QA owns user-facing permission to ask questions. Knowledge owns persistent
  knowledge bases, documents, parsing, embedding, and retrieval execution.
- Session attachments are a separate temporary QA context source and must not be
  inserted into persistent Knowledge indexes as part of QA ask handling.

### 2. Signatures

QA direct retrieval uses the Knowledge internal resource:

```text
POST /internal/v1/knowledge-queries
GET  /internal/v1/documents/{documentId}
```

Trusted QA retrieval headers:

```text
X-Service-Token: <internal service token>
X-Caller-Service: qa
X-Knowledge-Retrieval-Scope: project
X-User-Id: <authenticated QA user for audit context>
X-Request-Id: <optional request id>
```

Knowledge runtime identity configuration:

```text
KNOWLEDGE_PROJECT_RUNTIME_USER_ID=<runtime user id>
```

Default:

```text
KNOWLEDGE_PROJECT_RUNTIME_USER_ID defaults to KNOWLEDGE_MCP_USER_ID
KNOWLEDGE_MCP_USER_ID defaults to knowledge_mcp_service
```

### 3. Contracts

- QA `defaultKnowledgeBaseIds` is an optional narrowing allowlist for long-term
  Knowledge retrieval. Empty means "do not narrow", not "disable RAG".
- A QA request with no explicit `knowledgeBaseIds` and an empty active
  `defaultKnowledgeBaseIds` must use the project-wide QA RAG pool.
- A non-empty active `defaultKnowledgeBaseIds` becomes the default retrieval
  scope when the request omits `knowledgeBaseIds`.
- When both non-empty defaults and explicit request `knowledgeBaseIds` exist,
  QA must reject IDs outside the default allowlist before calling Knowledge.
- A standard QA user does not need Knowledge management `knowledge:read` to
  retrieve through QA's trusted path.
- Knowledge management APIs keep normal `knowledge:read` / `knowledge:write`
  authorization. Only trusted QA retrieval and citation source visibility use
  `X-Knowledge-Retrieval-Scope: project`.
- Knowledge resolves the project pool by listing/searching runtime datasets
  visible to `KNOWLEDGE_PROJECT_RUNTIME_USER_ID`.
- QA must send the project retrieval scope header for both direct
  `knowledge-queries` retrieval and citation source checks.
- Knowledge MCP search may omit `knowledgeBaseIds`; an empty list delegates to
  the adapter's project/runtime pool. The QA MCP guard still injects narrowed
  IDs when request/default QA scope is non-empty.
- Disabling long-term RAG is done by removing `search_knowledge` /
  `knowledge__search` from the enabled tool list, not by setting an empty
  default knowledge-base list.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Direct Knowledge caller lacks `knowledge:read` and has no trusted QA project scope header | `403 forbidden`. |
| Trusted QA caller has project scope header and user lacks `knowledge:read` | Retrieval or citation visibility proceeds under project runtime identity. |
| QA default allowlist is non-empty and request includes an outside KB id | QA rejects before retrieval or returns an error tool result; do not call Knowledge with the outside id. |
| Request/default scope is empty and project runtime identity has no visible KBs | Knowledge returns `400 validation_error` for no project knowledge bases. |
| Runtime rejects an explicit/default KB under project identity | Map to the existing sanitized Knowledge/QA dependency or forbidden error path. |

### 5. Good/Base/Bad Cases

- Good: QA asks with no KB filter, Knowledge receives trusted project scope,
  expands dataset ids with `KNOWLEDGE_PROJECT_RUNTIME_USER_ID`, and searches the
  project pool.
- Good: QA config has `defaultKnowledgeBaseIds=["kb_a"]`; direct and MCP
  retrieval both use only `kb_a` unless the request supplies a subset.
- Base: Knowledge management UI calls the same `knowledge-queries` endpoint with
  `knowledge:read`; it keeps user-scoped management authorization.
- Bad: require the end user's `knowledge:read` for QA answers, use the end user's
  runtime tenant to expand empty QA retrieval scope, or interpret empty defaults
  as disabled RAG.

### 6. Tests Required

- QA service tests: empty default preserves global retrieval semantics; non-empty
  default narrows; outside explicit IDs are rejected.
- QA Knowledge client tests: trusted retrieval and citation source checks include
  `X-Knowledge-Retrieval-Scope: project` and do not require
  `X-User-Permissions`.
- Knowledge adapter tests: trusted QA `knowledge-queries` without
  `knowledge:read` uses project runtime identity for dataset listing and search.
- Knowledge adapter tests: trusted QA document visibility without
  `knowledge:read` uses project runtime identity.
- Knowledge MCP/QA manager tests: MCP `search_knowledge` can omit IDs for global
  scope and receives injected narrowed IDs when QA scope is non-empty.

### 7. Wrong vs Correct

#### Wrong

```text
QA user without knowledge:read
-> QA forwards only X-User-Id
-> Knowledge readScope rejects retrieval with 403
```

#### Correct

```text
QA user with qa:use
-> QA forwards X-Caller-Service: qa and X-Knowledge-Retrieval-Scope: project
-> Knowledge keeps X-User-Id for audit context
-> Knowledge lists/searches using KNOWLEDGE_PROJECT_RUNTIME_USER_ID
```

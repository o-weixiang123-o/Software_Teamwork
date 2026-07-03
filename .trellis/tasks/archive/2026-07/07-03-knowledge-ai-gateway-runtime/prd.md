# Unify Knowledge runtime model calls through AI Gateway

## Goal

Make `services/ai-gateway` the single outbound model plane for Knowledge runtime embedding and rerank calls, while cleaning up stale Knowledge MCP / AI Gateway concepts that currently make the architecture ambiguous.

The user-facing value is that document parsing, indexing, retrieval, rerank, QA via MCP, and citation-bearing RAG flows can be operated and audited through one credential, auth, and invocation-recording path instead of split provider keys.

## Confirmed Facts

- AI Gateway already exposes `POST /internal/v1/embeddings` and `POST /internal/v1/rerankings` (`services/ai-gateway/internal/http/server.go:79`, `services/ai-gateway/internal/http/server.go:80`, `services/ai-gateway/internal/http/server.go:81`).
- AI Gateway internal model invocation auth requires `X-Service-Token` and `X-Caller-Service`; allowed callers include `knowledge` (`services/ai-gateway/internal/http/server.go:402`, `services/ai-gateway/internal/http/server.go:430`).
- AI Gateway embedding requests accept `input []string`, `model`, and optional `profile_id`; rerank requests accept `query`, `documents[]`, `model`, and optional `profile_id` (`services/ai-gateway/internal/http/dto.go:61`, `services/ai-gateway/internal/http/dto.go:70`).
- AI Gateway enforces exact request model to selected profile model (`services/ai-gateway/internal/service/invocations.go:171`).
- AI Gateway records embedding and reranking invocation context, input count, dimensions/topN, usage, status, and request id (`services/ai-gateway/internal/service/invocations.go:41`, `services/ai-gateway/internal/service/invocations.go:101`, `services/ai-gateway/internal/service/models.go:303`).
- Knowledge runtime discovers embedding/rerank providers by `_FACTORY_NAME`, so adding an `AI_GATEWAY` factory fits the existing provider model (`services/knowledge-runtime/rag/llm/__init__.py:165`).
- Knowledge runtime env initialization writes `KNOWLEDGE_RUNTIME_*_MODEL` and `KNOWLEDGE_RUNTIME_*_FACTORY` into tenant model IDs as `model@default@factory` (`services/knowledge-runtime/api/db/init_data.py:60`, `services/knowledge-runtime/api/db/init_data.py:132`).
- Current Knowledge MCP publishes four read-only tools through `ToolCatalog()`: `search`, `list_documents`, `get_document`, `get_chunk` (`services/knowledge/internal/mcp/tools.go:25`).
- Knowledge still contains an unregistered `answer_from_knowledge` handler and a thin `internal/aigateway` chat client that are only wired into startup and tests, not the MCP v1 catalog (`services/knowledge/internal/mcp/handlers.go:179`, `services/knowledge/cmd/adapter/main.go:58`).
- Docs currently contain stale claims that Knowledge has a current 14-tool MCP catalog and existing AI Gateway embedding/rerank adapter, while current runtime docs still recommend direct SiliconFlow provider env (`docs/services/knowledge/docs/implementation.md:72`, `services/knowledge-runtime/README_zh.md:76`).

## Requirements

- R1. Remove or fully retire the unused Knowledge-local AI Gateway chat client and unregistered `answer_from_knowledge` / write-tool MCP implementation so the current MCP surface is unambiguously four read-only tools.
- R2. Update Knowledge docs and capability matrix so they describe the current four-tool MCP contract and distinguish "current direct runtime provider path" from the new AI Gateway runtime provider path.
- R3. Add `AI_GATEWAY` embedding provider support in Knowledge runtime that calls AI Gateway `/internal/v1/embeddings` with explicit `X-Service-Token`, `X-Caller-Service: knowledge`, and `X-Request-Id`.
- R4. Add `AI_GATEWAY` rerank provider support in Knowledge runtime that calls AI Gateway `/internal/v1/rerankings` with the same explicit internal headers.
- R5. Preserve batch embedding order by AI Gateway response `index`, preserve rerank input index/document id semantics, and surface provider errors without silently falling back to direct provider calls.
- R6. Keep direct provider support as an explicit operator-selected legacy/local path only; do not implement automatic runtime fallback from AI Gateway to SiliconFlow or any other provider.
- R7. Update local env examples and runtime docs so the recommended production/local integration path uses AI Gateway profiles (`default-embedding`, `default-rerank`) and the direct provider key is documented only as a legacy/emergency path.
- R8. Add focused tests that prove headers, request-id propagation/generation, exact model/profile requests, batch ordering, rerank score mapping, and error handling.
- R9. Provide a real or controlled smoke path whose acceptance evidence includes AI Gateway `provider_invocations` for embedding and reranking, not only a successful Knowledge query.

## Acceptance Criteria

- [ ] A1. `go test ./...` passes in `services/knowledge` after removing the stale chat client and unregistered MCP code.
- [ ] A2. Targeted Knowledge runtime unit tests pass for `AI_GATEWAY` embedding and rerank providers, including batch order and required internal headers.
- [ ] A3. `go test ./...` passes in `services/ai-gateway`, proving the existing internal model contract still holds.
- [ ] A4. Docs no longer say the current Knowledge MCP catalog has 14 tools or that existing Knowledge runtime embedding/rerank calls are already governed by AI Gateway.
- [ ] A5. `deploy/.env.example`, `deploy/README.md`, and runtime README files document AI Gateway as the preferred embedding/rerank provider path and direct provider env as an explicit legacy/local fallback.
- [ ] A6. A smoke procedure is documented and, where the local environment permits, run to show upload/parse/index/query/rerank through AI Gateway with `provider_invocations.request_id`, `operation=embedding|reranking`, and `caller_service=knowledge`.
- [ ] A7. No provider API key is duplicated into Knowledge runtime docs or committed env defaults; AI Gateway local seed remains the place that owns external provider credentials.

## Out of Scope

- Changing QA agent orchestration semantics beyond consuming the existing Knowledge MCP/search path.
- Adding an AI Gateway-side alias/mapping table before a concrete multi-profile alias need exists.
- Making AI Gateway accept internal service tokens through `Authorization: Bearer`.
- Adding automatic failure fallback from AI Gateway to direct provider calls.

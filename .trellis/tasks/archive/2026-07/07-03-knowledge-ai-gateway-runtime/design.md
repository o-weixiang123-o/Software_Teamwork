# Design

## Architecture Boundary

`services/ai-gateway` is the only component that owns provider credentials, model profile selection, provider invocation records, and model usage aggregation. `services/knowledge-runtime` remains the parser, chunker, embedding caller, index writer, retriever, and reranker engine, but its preferred external model provider becomes AI Gateway.

The Knowledge Go adapter continues to bridge Gateway/QA/MCP calls to runtime APIs. It should not contain an extra chat-only "AI Gateway" client for an unpublished Knowledge answer tool.

## Data Flow

1. Operator seeds AI Gateway model profiles:
   - `default-embedding`, purpose `embedding`, model such as `BAAI/bge-m3`.
   - `default-rerank`, purpose `rerank`, model such as `BAAI/bge-reranker-v2-m3`.
2. Operator configures Knowledge runtime:
   - `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=AI_GATEWAY`
   - `KNOWLEDGE_RUNTIME_EMBEDDING_MODEL=<exact AI Gateway profile model>`
   - `KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL=http://127.0.0.1:8086/internal/v1`
   - `KNOWLEDGE_RUNTIME_RERANK_FACTORY=AI_GATEWAY`
   - `KNOWLEDGE_RUNTIME_RERANK_MODEL=<exact AI Gateway profile model>`
   - `KNOWLEDGE_RUNTIME_RERANK_BASE_URL=http://127.0.0.1:8086/internal/v1`
   - service token via explicit AI Gateway runtime env, not provider API key.
3. Runtime env initialization stores model IDs as:
   - `<embedding-model>@default@AI_GATEWAY`
   - `<rerank-model>@default@AI_GATEWAY`
4. Runtime provider classes call:
   - `POST /internal/v1/embeddings`
   - `POST /internal/v1/rerankings`
5. AI Gateway selects the explicit `profile_id` when supplied, otherwise its default purpose profile, enforces exact model match, invokes the external provider, and records `provider_invocations`.

## Provider Contract

The `AI_GATEWAY` runtime provider sends:

- `Content-Type: application/json`
- `Accept: application/json`
- `X-Service-Token: <internal token>`
- `X-Caller-Service: knowledge`
- `X-Request-Id: <existing request id when available; generated UUID when unavailable>`

Embedding request body:

```json
{
  "profile_id": "default-embedding",
  "model": "BAAI/bge-m3",
  "input": ["chunk 1", "chunk 2"],
  "encoding_format": "float"
}
```

Rerank request body:

```json
{
  "profile_id": "default-rerank",
  "model": "BAAI/bge-reranker-v2-m3",
  "query": "question",
  "documents": [
    {"id": "0", "text": "candidate text"}
  ],
  "top_n": 1
}
```

The runtime provider parses OpenAI-style AI Gateway responses, sorts embeddings by `data[].index`, and maps rerank `data[].index` scores back to the original text order.

## Configuration Shape

Use separate internal-service env from external provider API key:

- `KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN`, falling back to `AI_GATEWAY_SERVICE_TOKEN` or `INTERNAL_SERVICE_TOKEN` for local integration.
- `KNOWLEDGE_RUNTIME_AI_GATEWAY_CALLER_SERVICE`, default `knowledge`.
- Optional profile env:
  - `KNOWLEDGE_RUNTIME_AI_GATEWAY_EMBEDDING_PROFILE_ID`, default `default-embedding`.
  - `KNOWLEDGE_RUNTIME_AI_GATEWAY_RERANK_PROFILE_ID`, default `default-rerank`.

`KNOWLEDGE_RUNTIME_MODEL_API_KEY` remains only for direct provider factories such as `SILICONFLOW`. The `AI_GATEWAY` provider must not require or interpret it as an external provider key.

## Cleanup

Remove `services/knowledge/internal/aigateway` and the unregistered `answer_from_knowledge` implementation from the MCP server. Keep only current v1 read-only MCP tools:

- `search`
- `list_documents`
- `get_document`
- `get_chunk`

Delete skipped tests that only exercise unpublished write/answer tools. Keep and strengthen tests for the four published tools.

## Error And Fallback Policy

- AI Gateway auth failures, profile/model mismatches, provider 429/5xx, malformed responses, and timeout errors fail the runtime provider call.
- Runtime does not automatically retry by switching to a direct provider.
- Direct provider factories remain available only when explicitly selected by factory/model env or persisted runtime model config.
- Model mismatch remains a configuration error. It should be visible in runtime logs/tests and must not be masked.

## Smoke Acceptance

The smoke pass must prove governance, not only retrieval success:

- Use a stable `X-Request-Id` for the test flow where possible.
- Upload/parse/index a small document.
- Execute a retrieval with rerank enabled.
- Verify Knowledge/QA result contains citations.
- Query AI Gateway storage or logs for `provider_invocations` with:
  - `caller_service=knowledge`
  - `operation=embedding`
  - `operation=reranking`
  - matching request id when the runtime can propagate it, or at least a generated `stw-kgw-*` request id from the runtime provider.

If a full real-provider smoke is not possible in this environment, controlled tests and an env-gated smoke procedure are still required, and the final report must say exactly what was not run and why.

# Design

## Authority Boundary

AI Gateway owns provider-facing model configuration:

- provider name and base URL,
- model name,
- provider API key or credential state,
- timeout/default invocation parameters,
- invocation audit and usage records.

Business services own business context and prompts. They choose an AI Gateway
profile by `profile_id` or rely on the default purpose profile, then AI Gateway
derives the provider model from that profile.

`deploy/.env.example` remains the source for local bootstrap values. Its
`AI_GATEWAY_LOCAL_*` keys seed demo profiles during host-run local startup, but
the running AI Gateway profile store is the runtime authority.

## Invocation Contract

Chat request body keeps OpenAI-compatible fields and optional `profile_id`.
`model` becomes optional for AI Gateway callers:

```json
{
  "profile_id": "default-chat",
  "messages": [{ "role": "user", "content": "hello" }]
}
```

Embedding request body may omit `model`:

```json
{
  "profile_id": "default-embedding",
  "input": ["chunk 1", "chunk 2"]
}
```

Rerank request body may omit `model`:

```json
{
  "profile_id": "default-rerank",
  "query": "question",
  "documents": [{ "id": "0", "text": "candidate" }]
}
```

If `model` is present, AI Gateway validates exact equality with the selected
profile model. This keeps existing caller safety checks while letting profile-only
callers stop duplicating provider model values.

## Data Flow

1. Caller sends a model invocation request with `profile_id` or relies on the
   default profile for the operation purpose.
2. AI Gateway selects the profile and decrypts/loads the provider credential.
3. AI Gateway derives the provider model from `profile.Model`.
4. For chat, AI Gateway merges caller OpenAI-compatible body with profile
   defaults and writes `model=profile.Model` into the provider payload.
5. For embedding and rerank, AI Gateway passes `profile.Model` to the provider
   client and records it in `provider_invocations`.
6. Downstream services keep their business-level model/profile settings only
   where required for compatibility or profile selection.

## Downstream Clients

- QA: `MODEL_ID` becomes optional when `AI_GATEWAY_PROFILE_ID` is configured.
  The AI Gateway request omits `model` when no explicit model is configured.
- Document: `DOCUMENT_AI_GATEWAY_MODEL` is optional. The client should not
  fallback `model` to `DOCUMENT_AI_GATEWAY_PROFILE_ID`; an empty model means
  profile-only invocation.
- Knowledge runtime: AI Gateway providers can omit `model` when the runtime model
  name is empty or a documented profile-only sentinel is configured. Existing
  runtime model labels may remain for RAGFlow compatibility, but docs must not
  present them as provider truth.

## Compatibility And Errors

- Existing callers that send a matching `model` keep working.
- Missing profile/default profile still fails as today.
- Supplied mismatched model remains a validation error.
- Provider payloads and invocation records never trust a downstream mismatched
  model string.
- No provider API key is moved into QA, Document, Knowledge, or deploy-local
  runtime docs.

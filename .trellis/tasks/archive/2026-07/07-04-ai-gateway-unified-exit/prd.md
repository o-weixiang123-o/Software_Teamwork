# Optimize AI Gateway as unified model-provider exit

## Goal

Make `services/ai-gateway` the single authoritative exit for AI model-provider
traffic in this repository: chat completions, streaming chat, embeddings,
reranking, provider credentials, provider error normalization, invocation audit,
usage aggregation, and model-profile runtime configuration.

This task is about AI model/provider calls only. It is not a public API Gateway
rewrite and should not change general business API routing. The work should
remove or prepare removal of duplicated provider-gateway-like paths in QA,
Document, Knowledge runtime, docs, and configuration without breaking local
integration flows.

## Confirmed Facts

- `docs/architecture/service-boundaries.md:17` defines `services/ai-gateway` as
  the owner of model profiles, provider config, chat completions, embeddings,
  rerankings, and provider error normalization.
- `docs/architecture/service-boundaries.md:47` says provider model calls are
  internal AI Gateway APIs; domain services own prompts, business context,
  MCP execution, and persistence.
- `docs/architecture/service-boundaries.md:85` explicitly marks direct provider
  calls from `gateway`, `qa`, `knowledge`, or `document` as an error pattern.
- `docs/services/ai-gateway/README.md:5` states frontend callers must not call
  AI Gateway directly and domain services call it through internal HTTP.
- `docs/services/ai-gateway/README.md:24-32` lists the intended AI Gateway
  responsibilities: provider config, chat, streaming, function-calling
  transport, embeddings, rerankings, error normalization, secret handling, and
  request correlation.
- `docs/services/ai-gateway/README.md:34` explicitly excludes public gateway
  routing, QA sessions, Agent Run state, MCP execution, Knowledge state, and
  Document report state from AI Gateway.
- `services/ai-gateway/internal/http/server.go:71-81` registers internal
  model-profile, chat, embedding, and reranking routes.
- `services/ai-gateway/internal/http/server.go:335-366` has HTTP handlers for
  `/internal/v1/embeddings` and `/internal/v1/rerankings`.
- `services/qa/internal/service/resources.go:696-761` lets QA create and test
  LLM config versions, but `validateLLMProfile` requires provider
  `ai-gateway` at `services/qa/internal/service/resources.go:896-899`.
- `services/qa/internal/service/settings.go:635-655` already validates runtime
  LLM endpoints as trusted AI Gateway chat-completion endpoints.
- `services/qa/internal/service/settings.go:687-694` still presents stored LLM
  settings with provider `"direct"` and API-key fields, which conflicts with
  the intended AI Gateway-only presentation.
- `services/qa/internal/platform/modelclient/openai.go:52-79` constructs a QA
  model client only after parsing an AI Gateway chat-completions endpoint.
- `services/qa/internal/platform/modelclient/openai.go:141-180` sends
  `profile_id`, optional exact-match `model`, service token, caller service,
  request id, and user id to AI Gateway.
- `services/document/internal/platform/aigateway/chat_client.go:26-43` requires
  a trusted AI Gateway URL and default chat profile for Document generation.
- `services/document/internal/platform/aigateway/chat_client.go:46-120` sends
  Document chat completions to AI Gateway with profile, optional model, service
  token, request id, user context, roles, and permissions.
- `services/knowledge-runtime/README.md:32-55` recommends Knowledge runtime
  embedding/rerank through AI Gateway and says provider/base URL/credential and
  invocation audit authority remain in AI Gateway profiles.
- `services/knowledge-runtime/README.md:57-58` still allows direct provider
  factories as explicit local/emergency choices and notes that they bypass AI
  Gateway invocation audit and usage aggregation.

## Requirements

- R1. Treat `services/ai-gateway` as the only stable model-provider exit for
  chat, streaming chat, embeddings, rerankings, provider credentials,
  model-profile selection, provider error normalization, invocation audit, and
  usage aggregation.
- R2. Inventory all model-provider access paths in `qa`, `document`,
  `knowledge`, `knowledge-runtime`, scripts, config, runbooks, and service docs.
  Scan `services/gateway` only to verify it does not make direct model/provider
  calls; do not treat public business routing as part of this task.
- R3. Convert AI-call-facing docs/config/API surfaces that still look like
  direct-provider gateways into AI Gateway profile-based language.
- R4. Prepare cleanup for legacy or emergency direct-provider paths by marking
  ownership, allowed scope, deprecation/removal criteria, and validation guards.
- R5. Do not remove Knowledge runtime direct provider fallbacks in the first
  implementation slice unless the remaining test and local-integration paths
  prove AI Gateway coverage for the affected embedding/rerank workflows.
- R6. Preserve domain-service ownership: QA owns sessions/Agent/MCP decisions,
  Document owns report jobs/files, Knowledge owns ingestion/retrieval state, and
  AI Gateway owns only provider/model invocation boundaries.
- R7. Add or adjust automated checks so future changes cannot reintroduce direct
  OpenAI-compatible/SiliconFlow/local provider exits outside AI Gateway without
  an explicit allowlist.
- R8. Update service-boundary docs, implementation docs, README/runbook entries,
  and config descriptions so the single-exit rule is discoverable by humans and
  by future agents.
- R9. Keep sensitive material out of logs, docs, tests, validation failures,
  and public responses: provider API keys, service tokens, full prompts,
  provider raw error bodies, object keys, and internal URLs.
- R10. Leave general public API routing and `services/gateway` business gateway
  behavior unchanged unless implementation finds an actual direct model/provider
  call there.

## Acceptance Criteria

- [ ] AC1. The final docs state unambiguously that `ai-gateway` is the sole
  model-provider exit for AI model/provider calls.
- [ ] AC2. A checked-in inventory or implementation note lists every discovered
  model-provider-like path and classifies it as `active via ai-gateway`,
  `temporary local/emergency fallback`, `not a model/provider call`, or
  `remove/cleanup candidate`.
- [ ] AC3. QA LLM config/user-facing settings no longer present direct-provider
  API endpoint/API key semantics as the normal path; the normal path is an AI
  Gateway profile reference.
- [ ] AC4. Document and QA model clients continue to target only trusted AI
  Gateway internal endpoints and retain request/user context propagation.
- [ ] AC5. Knowledge runtime defaults remain AI Gateway-based for embedding and
  rerank; any direct-provider fallback is explicitly documented as temporary,
  opt-in, non-default, and outside normal audit guarantees.
- [ ] AC6. Tests or repository policy checks fail on new direct provider exits
  outside `services/ai-gateway` unless the path is in an intentional allowlist
  with a cleanup note.
- [ ] AC7. `services/ai-gateway` service-local tests and builds pass after
  implementation changes.
- [ ] AC8. Touched owner services run their relevant local tests and builds:
  `services/gateway`, `services/qa`, `services/document`, and targeted
  `services/knowledge-runtime` tests when those areas are changed.
- [ ] AC9. `services/gateway` has no direct model/provider call path after this
  cleanup; if no gateway files are changed, Gateway contract validation is not a
  required check for this task.
- [ ] AC10. No final implementation changes expose provider secrets, raw
  provider errors, prompts, embeddings, rerank payloads, or internal URLs in
  public responses or normal logs.

## Out Of Scope

- Changing public business API routing or replacing the existing public Gateway
  service.
- Cleaning up public Gateway route ownership unless a route directly performs
  model/provider calls.
- Moving QA Agent orchestration, MCP tool execution, Knowledge ingestion, or
  Document report generation state into AI Gateway.
- Removing all Knowledge runtime direct-provider code in one slice without
  follow-up coverage and migration proof.
- Building a new frontend UI unless implementation reveals a small contract
  label/type change that must be reflected in existing generated clients.
- Running real-provider smoke by default; real-provider checks remain explicit
  and credential-gated.

## Decisions

- D1. "统一对外出口" means all AI model/provider calls must go through
  `services/ai-gateway`. It is unrelated to public business API Gateway routing.

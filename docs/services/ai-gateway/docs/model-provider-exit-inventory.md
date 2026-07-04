# Model Provider Exit Inventory

Last verified: 2026-07-04

`services/ai-gateway` is the only normal exit for AI model/provider traffic in
this repository. This inventory is about AI model/provider calls only. It does
not change public `services/gateway` business API routing.

## Classification Legend

| Classification | Meaning |
| --- | --- |
| `active via ai-gateway` | Normal runtime path calls `services/ai-gateway` internal APIs. |
| `temporary local/emergency fallback` | Existing fallback remains allowed only when explicitly selected and outside normal audit/usage guarantees. |
| `not a model/provider call` | The path manages public routing, config metadata, or docs without invoking providers. |
| `cleanup candidate` | Compatibility code should be removed after migration/coverage criteria are met. |

## Inventory

| Area | Evidence | Classification | Notes |
| --- | --- | --- | --- |
| AI Gateway internal API | `services/ai-gateway/internal/http/server.go:74` to `services/ai-gateway/internal/http/server.go:81` | `active via ai-gateway` | Owns model profiles, chat completions, embeddings, and rerankings. |
| AI Gateway provider adapters | `services/ai-gateway/internal/provider/client.go:18`, `services/ai-gateway/internal/provider/client.go:39` | `active via ai-gateway` | Direct provider HTTP calls belong here. Invocation recording and error normalization stay in AI Gateway service code. |
| QA model client | `services/qa/internal/modelendpoint/endpoint.go:31`, `services/qa/internal/platform/modelclient/openai.go:53`, `services/qa/internal/platform/modelclient/openai.go:83`, `services/qa/internal/platform/modelclient/openai.go:168` | `active via ai-gateway` | QA parses a trusted AI Gateway chat endpoint, sends `profile_id`, and marks `X-Caller-Service: qa`. |
| QA LLM config versions | `services/qa/internal/service/resources.go:896` to `services/qa/internal/service/resources.go:899` | `active via ai-gateway` | New/current LLM config APIs require provider `ai-gateway` and a profile id. |
| QA legacy aggregate settings | `services/qa/internal/service/settings.go:20`, `services/qa/internal/service/settings.go:422`, `services/qa/internal/service/settings.go:439`, `services/qa/internal/service/settings.go:522`, `services/qa/internal/service/settings.go:705` to `services/qa/internal/service/settings.go:709` | `cleanup candidate` | Legacy `direct` rows are read only as trusted AI Gateway endpoint compatibility. New writes store `ai-gateway` profile semantics and public settings no longer expose endpoint/API-key fields as the normal path. Remove the legacy branch after local data and any old aggregate settings caller are migrated. |
| QA legacy settings SQL | `services/qa/internal/repository/settings_postgres.go:117` to `services/qa/internal/repository/settings_postgres.go:121` | `active via ai-gateway` | Old generated sqlc insert still describes the retired direct-provider shape; the active legacy settings write uses inline SQL to persist provider/profile fields and no longer persists endpoint/key material. |
| Document model generation client | `services/document/internal/platform/aigateway/profile_client.go:45`, `services/document/internal/platform/aigateway/chat_client.go:71`, `services/document/internal/platform/aigateway/chat_client.go:84`, `services/document/internal/platform/aigateway/chat_client.go:141` | `active via ai-gateway` | Document validates trusted AI Gateway base URLs, looks up model profiles, and sends chat requests with `X-Caller-Service: document`. |
| Document docs/settings | `docs/services/document/README.md:197`, `docs/services/document/README.md:205`, `docs/services/document/README.md:219` | `active via ai-gateway` | Document stores only profile/model/business settings. Provider base URLs and API keys stay in AI Gateway model profiles. |
| Knowledge runtime AI Gateway embedding/rerank | `services/knowledge-runtime/README.md:39` to `services/knowledge-runtime/README.md:47`, `services/knowledge-runtime/rag/llm/ai_gateway_utils.py:23` to `services/knowledge-runtime/rag/llm/ai_gateway_utils.py:25`, `services/knowledge-runtime/rag/llm/embedding_model.py:980` to `services/knowledge-runtime/rag/llm/embedding_model.py:987`, `services/knowledge-runtime/rag/llm/rerank_model.py:360` to `services/knowledge-runtime/rag/llm/rerank_model.py:367` | `active via ai-gateway` | Default Knowledge runtime embedding/rerank setup is `AI_GATEWAY`, using AI Gateway service token/profile configuration. |
| Knowledge runtime direct provider factories | `services/knowledge-runtime/README.md:57`, `services/knowledge-runtime/rag/llm/__init__.py:75`, `services/knowledge-runtime/rag/llm/__init__.py:90`, `services/knowledge-runtime/rag/llm/chat_model.py:31`, `services/knowledge-runtime/rag/llm/embedding_model.py:28`, `services/knowledge-runtime/rag/llm/rerank_model.py:246`, `services/knowledge-runtime/rag/llm/rerank_model.py:321` | `temporary local/emergency fallback` | Explicit fallback only. These paths bypass AI Gateway invocation audit and usage aggregation and must not be used as the default local integration path. Cleanup requires proven AI Gateway coverage for affected runtime workflows and an agreed migration for vendored runtime provider support. |
| Knowledge Go adapter | `docs/services/knowledge/README.md:39`, `docs/services/knowledge/docs/api-contract.md:978` | `active via ai-gateway` | Knowledge stores model profile references and does not own provider credentials or direct provider adapters. |
| Public Gateway admin model-profile routes | `services/gateway/internal/http/routes.go:44` to `services/gateway/internal/http/routes.go:48`, `docs/services/gateway/README.md:60`, `docs/services/gateway/README.md:101`, `docs/services/gateway/README.md:198` | `not a model/provider call` | Gateway only exposes public/admin routing for AI Gateway-owned model profile resources. It must not call providers directly. |
| Gateway direct-provider guard | `services/gateway/internal/http/routes_internal_test.go:207` | `not a model/provider call` | Existing test forbids Gateway production code from importing provider SDKs such as `github.com/openai/`. |
| Config defaults | `config/base.yaml:231` | `active via ai-gateway` | QA default model endpoint is the AI Gateway internal chat completions endpoint. Document and Knowledge runtime profile defaults are also AI Gateway profile based in the same config file. |
| AI Gateway local seed scripts | `scripts/local/render_ai_gateway_local_seed.go`, `scripts/verify_local_seed_contract.py` | `active via ai-gateway` | These scripts write local provider credentials into AI Gateway model profiles; they do not make domain services direct provider callers. |
| Repository policy check | `scripts/check_ai_gateway_provider_policy.py:1`, `scripts/check_ai_gateway_provider_policy.py:48`, `scripts/check_ai_gateway_provider_policy.py:121` | `active via ai-gateway` | Fails on new direct provider SDK/base URL/provider endpoint usage outside AI Gateway unless explicitly allowlisted. |

## Cleanup Criteria

- QA legacy `direct` compatibility can be removed after old `llm_config_versions`
  rows are migrated/backfilled to `provider='ai-gateway'` with profile ids and
  any old aggregate settings caller is retired or rewritten to the profile-based
  `/llm-config-versions` API.
- Knowledge runtime direct provider factories can be disabled from normal
  profiles after embedding and rerank workflows have repeatable AI Gateway
  smoke evidence, including `provider_invocations` records for
  `caller_service=knowledge`.
- Any new direct provider path outside `services/ai-gateway` must either be
  removed or added to `scripts/check_ai_gateway_provider_policy.py` with a
  narrow path allowlist and cleanup rationale.

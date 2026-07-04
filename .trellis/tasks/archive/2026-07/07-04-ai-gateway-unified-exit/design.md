# Design: AI Gateway Unified Model-Provider Exit

## Boundary Decision

This task treats `services/ai-gateway` as the single model-provider exit. It is
not about public business API routing and should not change general Gateway
behavior.

The implementation must make the model/provider-call boundary explicit and
testable: QA, Document, Knowledge, Knowledge runtime, scripts, and config should
not create parallel provider gateways or direct-provider normal paths.

## Target Architecture

```text
services/qa
  -> services/ai-gateway /internal/v1/chat/completions

services/document
  -> services/ai-gateway /internal/v1/chat/completions

services/knowledge / services/knowledge-runtime
  -> services/ai-gateway /internal/v1/embeddings
  -> services/ai-gateway /internal/v1/rerankings

services/ai-gateway
  -> OpenAI-compatible / SiliconFlow / local-compatible providers
```

## Work Areas

### AI Gateway

- Keep internal endpoints as the canonical provider exit:
  `/internal/v1/model-profiles`, `/internal/v1/chat/completions`,
  `/internal/v1/embeddings`, and `/internal/v1/rerankings`.
- Ensure service docs and implementation docs describe it as the model-provider
  exit, not a public frontend gateway.
- Use existing invocation logging and error normalization as the enforcement
  surface.

### QA

- Preserve QA ownership of sessions, Agent loop, MCP tool policy, tool-call
  records, citations, and settings.
- Normalize LLM configuration naming away from direct-provider semantics where
  it is still presented as endpoint/API-key based.
- Keep compatibility fields only where required for migration, but make the
  active validated path provider=`ai-gateway` plus `profileId`.

### Document

- Preserve Document ownership of report settings, report jobs, report files,
  and generation lifecycle.
- Keep the AI Gateway client as the only model-call path.
- Ensure settings and docs describe model choice as AI Gateway profile-based.

### Knowledge Runtime

- Keep default embedding/rerank factories configured through AI Gateway.
- Treat direct providers such as `SILICONFLOW` as temporary explicit
  local/emergency fallbacks and document their audit/usage aggregation gap.
- Add cleanup criteria rather than deleting the fallbacks prematurely.

### Config, Scripts, Docs

- Align `.env.example`, `config/base.yaml`, service READMEs, runbooks, and
  implementation docs around "AI Gateway profile" as the normal model
  configuration primitive.
- Keep real-provider credentials in AI Gateway seed/profile flows.
- Treat `services/gateway` as out of scope except for a scan proving it does
  not directly call model providers.

## Enforcement

Add a repository check that scans for direct model-provider exits outside
`services/ai-gateway` and fails unless the match is in an intentional allowlist.

The allowlist should be narrow and explain why the occurrence is acceptable:

- AI Gateway clients in QA, Document, and Knowledge runtime.
- Documentation that describes the deprecated emergency path.
- Tests that assert direct-provider paths are blocked or explicitly temporary.
- Provider lists/config shipped by the vendored Knowledge runtime when they are
  not used by the default path.

## Compatibility

- Do not change public API paths as part of this task.
- If public response field semantics change, update Gateway OpenAPI and frontend
  generated types in the same implementation slice.
- Prefer additive compatibility fields or corrected labels over destructive DB
  migrations for QA `llm_config_versions` until the active runtime path is
  verified through Gateway -> QA -> AI Gateway checks.

## Rollback

- Documentation and policy-check changes can be reverted independently.
- Owner-service behavior changes should be behind existing config defaults where
  possible.
- Avoid irreversible migration cleanup in the first slice; mark direct-provider
  data fields as cleanup candidates before dropping them.

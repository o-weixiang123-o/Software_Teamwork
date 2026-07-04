# Converge model configuration authority to AI Gateway profiles

## Goal

Make AI Gateway model profiles the single runtime authority for provider,
model name, base URL, credentials, timeout/default parameters, usage, and audit
state. Local deployment env remains a bootstrap seed for local/demo profiles,
not a second model configuration center.

The user-facing value is that QA, Document, and Knowledge runtime can route
model calls by AI Gateway profile without duplicating provider model/base URL/API
key decisions in each business service.

## Background

- `docs/architecture/service-boundaries.md` already assigns model profile
  management, provider config, API key state, chat, embeddings, rerank, and
  provider error normalization to `services/ai-gateway`.
- `docs/services/ai-gateway/docs/data-models.md` records AI Gateway storage for
  model profiles, credential write state, config audit, invocation records, and
  usage.
- Recent local integration work added `deploy/.env.example` seed variables such
  as `AI_GATEWAY_LOCAL_*`. Those variables are useful for local bootstrap, but
  must not become the architecture authority for runtime model selection.
- Existing AI Gateway invocation APIs require a request `model` even after a
  profile/default profile is selected, which forces downstream services to
  duplicate a value AI Gateway already owns.

## Requirements

- R1. AI Gateway chat, embedding, and rerank invocation APIs accept requests
  without `model` when an explicit `profile_id` or default purpose profile can be
  selected.
- R2. When a caller supplies `model`, AI Gateway still validates it exactly
  matches the selected profile model and returns the existing validation error on
  mismatch.
- R3. Provider calls and invocation records always use the selected profile's
  model, never an unchecked downstream model string.
- R4. QA and Document AI Gateway clients should allow profile-only requests and
  avoid sending fallback profile IDs as `model`.
- R5. Knowledge runtime AI Gateway providers may omit `model` when configured
  for profile-only operation. If runtime still needs a model label for RAGFlow
  compatibility, docs must make clear that it is not the source of provider
  truth.
- R6. `deploy/.env.example`, deploy docs, runbooks, and service docs must
  describe `AI_GATEWAY_LOCAL_*` as local seed/bootstrap inputs only.
- R7. Preserve backward compatibility for existing callers that still send
  matching `model`.
- R8. Add focused tests covering omitted model, exact-match validation, and
  downstream clients omitting model when configured to rely on profiles.

## Acceptance Criteria

- [x] A1. AI Gateway tests prove chat, embedding, and rerank requests without
  `model` use the selected/default profile model.
- [x] A2. Existing AI Gateway model/profile mismatch tests still fail closed.
- [x] A3. QA and Document client tests prove profile-only configuration sends no
  duplicate `model` field.
- [x] A4. Knowledge runtime AI Gateway provider tests or docs prove model
  duplication is no longer required for AI Gateway authority.
- [x] A5. Deploy/runbook docs no longer imply deploy env is the runtime model
  configuration authority.
- [x] A6. Required local integration guard checks pass when deploy env/docs or
  seed contract scripts change.

## Notes

- This task intentionally narrows the previous Knowledge runtime AI Gateway work:
  it changes model authority semantics, not the broader RAGFlow runtime vendor
  integration.
- Do not remove direct-provider fallback code paths unless they are already
  explicitly legacy and outside the local integration default.

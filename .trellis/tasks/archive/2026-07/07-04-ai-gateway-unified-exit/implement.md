# Implement Plan: AI Gateway Unified Model-Provider Exit

## Step 1: Inventory And Classification

- Search model-provider and gateway-like terms across code, docs, config, and
  scripts.
- Create an implementation note under the task or docs that classifies each
  finding:
  - active via AI Gateway
  - temporary local/emergency fallback
  - not a model/provider call
  - cleanup candidate
- Include file:line evidence for QA LLM config, Document AI Gateway clients,
  Knowledge runtime direct provider fallback, and AI Gateway internal endpoints.
- Scan `services/gateway` only for direct model/provider calls; do not inventory
  normal public business routes.

## Step 2: Boundary Docs

- Update service-boundary and AI Gateway docs only if wording still leaves the
  AI model/provider-call exit ambiguous.
- Update QA, Document, Knowledge runtime, config, and runbook wording so normal
  model setup points to AI Gateway profiles.
- Mark direct provider fallbacks as opt-in, non-default, unaudited by AI
  Gateway, and cleanup candidates.

## Step 3: Code/API Cleanup

- QA:
  - Fix any public or internal settings response that still says provider
    `"direct"` for an AI Gateway-backed config.
  - Remove normal-path API-key/endpoint semantics from responses/docs where
    possible without a breaking migration.
  - Keep strict validation that provider must be `ai-gateway` and endpoint must
    target trusted AI Gateway chat completions.
- Document:
  - Confirm report settings and generation clients only use AI Gateway
    profile-based calls.
  - Clean up labels/docs if they imply provider API key ownership in Document.
- Knowledge runtime:
  - Keep AI Gateway defaults.
  - Add cleanup notes/guards for direct provider factories rather than deleting
    them in this slice.
- Gateway:
  - Do not change Gateway routing unless a direct model/provider call exists.
  - If a direct model/provider call is found there, remove or redirect it to AI
    Gateway and add a focused regression test.

## Step 4: Policy Check

- Add a focused script/test that fails when new direct provider exits appear
  outside `services/ai-gateway` unless explicitly allowlisted with a cleanup
  rationale.
- Wire the check into existing relevant test suites if the repository already
  has a comparable policy-check pattern.

## Step 5: Validation

Run targeted checks based on touched areas:

```bash
cd services/ai-gateway && go test ./... && go build ./cmd/server
cd services/qa && go test ./... && go build ./cmd/server && go build ./cmd/agent
cd services/document && go test ./... && go build ./cmd/server
git diff --check
```

If `services/gateway` code, Gateway OpenAPI, or active route ownership changes:

```bash
cd services/gateway && go test ./... && go build ./cmd/server
python3 scripts/verify_gateway_active_api.py
```

If Knowledge runtime code or tests are touched:

```bash
cd services/knowledge-runtime
PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest test/unit_test/rag/llm test/routes/test_runtime_dependency_check.py -q
```

If config, Docker, Compose, local scripts, local seed, or Docker docs are
touched, also run the repository Docker/config policy checks required by
`AGENTS.md`.

## Risk Notes

- Dropping QA LLM DB fields immediately could break migrations, existing seed
  data, or frontend contracts. Prefer semantic cleanup first.
- Knowledge runtime is vendored and still has broader RAGFlow provider support;
  deleting those factories in one pass is riskier than guarding the default path
  and documenting cleanup criteria.
- Real-provider validation depends on local credentials and should stay
  explicit/env-gated.

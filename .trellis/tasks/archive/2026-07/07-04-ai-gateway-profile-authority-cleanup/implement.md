# Implementation Plan

## Order

1. Record the task requirements/design and start the Trellis task.
2. Inspect current diffs for touched files so existing user/local changes are
   preserved.
3. Update AI Gateway invocation validation:
   - chat payload `model` optional,
   - embedding/rerank input `model` optional,
   - `modelForInvocation` returns `profile.Model` when request model is blank,
   - matching model validation remains.
4. Add or update AI Gateway tests for omitted model on chat, embeddings, and
   rerank, plus mismatch coverage.
5. Update QA and Document AI Gateway clients to send profile-only requests when
   model env is empty and profile is configured.
6. Update Knowledge runtime AI Gateway providers only as needed to allow
   profile-only payloads without destabilizing existing RAGFlow model labels.
7. Clean deploy docs/env/runbook wording so local seed config is clearly not the
   runtime model authority.
8. Run targeted checks and report any smoke gap explicitly.

## Validation

- `cd services/ai-gateway && go test ./...`
- `cd services/qa && go test ./internal/config ./internal/platform/modelclient`
- `cd services/document && go test ./internal/config ./internal/platform/aigateway`
- `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest test/unit_test/rag/llm -q`
- If deploy env/docs or local seed scripts change:
  - `python3 scripts/check_docker_policy.py`
  - `python3 -m pytest scripts/tests -q`
  - `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`

## Risk Points

- Knowledge runtime stores model identifiers for RAGFlow compatibility. Removing
  them entirely may be broader than this task; prefer profile-only request
  support while preserving runtime labels if needed.
- Existing local integration tests may assert exact `.env.example` comments or
  seed contract wording.
- AI Gateway should continue to protect operators from accidental model/profile
  mismatch when callers still send model.

## Validation Result

- `cd services/ai-gateway && go test ./...`
- `cd services/ai-gateway && go build ./cmd/server`
- `cd services/qa && go test ./...`
- `cd services/qa && go build ./cmd/server && go build ./cmd/agent`
- `cd services/document && go test ./...`
- `cd services/document && go build ./cmd/server`
- `cd services/knowledge && go test ./...`
- `cd services/knowledge && go build ./cmd/adapter`
- `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest test/unit_test/rag/llm/test_ai_gateway_provider.py -q`
- `bun run --cwd apps/web api:generate`
- `bun run --cwd apps/web check`
- `bun run --cwd apps/web build`
- `python3 scripts/check_docker_policy.py`
- `uv run --no-project --with pytest --with pyyaml python -m pytest scripts/tests -q`
- `bash -n scripts/local/dev-up.sh scripts/local/run-backend.sh scripts/local/stop-backend.sh`
- `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`
- `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --services`
- `git diff --check`

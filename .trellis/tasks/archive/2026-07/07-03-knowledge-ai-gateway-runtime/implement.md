# Implementation Plan

## Order

1. Finish repository research and record the decisions in this task.
2. Start the Trellis task and load package specs with `trellis-before-dev`.
3. PR A cleanup:
   - Remove `services/knowledge/internal/aigateway`.
   - Remove `answer_from_knowledge` and unpublished write/content MCP handlers/types.
   - Simplify MCP server constructors and adapter startup to no longer create a Knowledge-local AI Gateway chat client.
   - Update Knowledge MCP tests to cover only four published tools.
   - Update Knowledge docs and current capability matrix.
4. PR B provider:
   - Add Knowledge runtime `AI_GATEWAY` embedding provider.
   - Add Knowledge runtime `AI_GATEWAY` rerank provider.
   - Add provider config metadata for `AI_GATEWAY` if needed by runtime model selection.
   - Add unit tests with fake HTTP server for headers, request id, batch order, score mapping, exact model/profile body, and non-2xx errors.
5. PR C / D integration and docs:
   - Update `deploy/.env.example`, `deploy/README.md`, `docs/runbooks/local-integration.md`, runtime README files, and dependency checks.
   - Document explicit direct-provider legacy path.
   - Add or update smoke procedure requiring AI Gateway invocation evidence.
6. Run targeted validation and summarize remaining smoke limits.

## Validation

- `cd services/knowledge && go test ./...`
- `cd services/ai-gateway && go test ./...`
- `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest test/unit_test/rag/llm test/routes -q`
- If deploy env or Docker/local run policy changes:
  - `python3 scripts/check_docker_policy.py`
  - `python3 -m pytest scripts/tests -q`
  - `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`

## Risk Points

- RAGFlow runtime provider constructors have many call sites. Keep the `AI_GATEWAY` provider constructor compatible with existing `key, model_name, base_url` invocation.
- AI Gateway uses exact model/profile matching. Default docs must keep runtime model names aligned with local seed profile model names.
- Batch embedding throughput may be sensitive to batch size and timeout. Start with the existing runtime batch size of 16 and a configurable HTTP timeout.
- Request id may not always be available from runtime call context. Generate a request id instead of omitting it so AI Gateway invocation rows stay traceable.
- Avoid broad refactors in vendored runtime code. Add focused provider classes and tests only.

## Rollback Points

- PR A cleanup can be reverted independently if downstream code unexpectedly depends on the unpublished MCP tools.
- PR B provider is opt-in by factory `AI_GATEWAY`; direct provider factories remain available by explicit config.
- Docs/env defaults can be reverted without changing direct provider implementation.

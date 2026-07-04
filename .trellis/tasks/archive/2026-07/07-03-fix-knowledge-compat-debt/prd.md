# Fix Knowledge compatibility debt

## Goal

Reduce low-quality compatibility behavior in the Knowledge adapter and vendored
Knowledge runtime so Gateway-facing Knowledge APIs are explicit, bounded, and
observable instead of relying on silent fallbacks, synthetic data, or expensive
vendor-shape workarounds.

The user's source request is to fix the nine compatibility/compromise issues
found in the previous read-only review of the Knowledge management module.

## Background

- `services/knowledge` currently runs a contract adapter over the vendored
  RAGFlow runtime (`services/knowledge-runtime`), not the older Go ingestion
  worker.
- A previous cleanup already staged removal of
  `services/knowledge-runtime/common/data_source/`; keep that unrelated staged
  change intact.
- The scope is Knowledge/RAG backend behavior only. Do not add QA agent
  orchestration or frontend behavior.

## Requirements

- R1. Remove the all-KB document lookup workaround in
  `services/knowledge/internal/adapter/handlers.go:281` for document routes that
  require a dataset/knowledge-base context. Require explicit `knowledgeBaseId`
  where the vendor runtime needs it, or use a bounded direct lookup/mapping with
  tests proving no unbounded scan remains.
- R2. Tighten Knowledge runtime auth compatibility in
  `services/knowledge-runtime/api/apps/__init__.py:93`. Legacy auth constants
  may remain only if route decorators still import them, but runtime execution
  must reject routes whose declared auth type does not include Gateway auth.
- R3. Replace implicit runtime tenant/user provisioning in normal request auth
  (`api/utils/gateway_tenant_provisioning.py:23`) with an explicit, bounded
  behavior. At minimum, make auto-provisioning opt-in and return a clear auth or
  provisioning error when disabled.
- R4. Stop silently dropping invalid `chunkStrategy` JSON in
  `services/knowledge/internal/adapter/map.go:363` and `:399`. Invalid JSON
  must return `400 validation_error`.
- R5. Stop returning misleading fixed retrieval trace values in
  `services/knowledge/internal/adapter/map.go:311`. Trace output must either
  reflect configured vendor/runtime values or explicitly mark unavailable
  fields as unavailable.
- R6. Replace fragile vendor error string matching in
  `services/knowledge/internal/adapter/map.go:160` with stable error
  classification based on status/code/type available from `vendorclient`.
- R7. Remove or strictly contain KB-level RAPTOR/GraphRAG fake document IDs
  (`api/db/services/document_service.py:1061` and
  `rag/svr/task_executor.py:832`). Dataset-level tasks must expose an explicit
  task scope or clearly isolated compatibility path that cannot be confused with
  real documents.
- R8. Stop indexing placeholder embedding content `"None"` for empty chunks in
  `api/db/services/llm_service.py:58` and related embedding paths. Empty chunks
  must be skipped or marked non-indexable instead of generating a real vector
  from a placeholder string.
- R9. Bound metadata filter in-memory fallback in
  `common/metadata_utils.py:231` and
  `api/db/services/doc_metadata_service.py:842`. Pushdown failure for large
  candidate sets must return a clear degraded/too-large result instead of
  unbounded in-memory filtering.
- R10. Preserve current public JSON envelope shapes and standard error codes
  unless a requirement above explicitly changes validation behavior.
- R11. Add or update tests for each changed compatibility behavior.
- R12. Update Knowledge docs/spec notes when behavior or configuration changes.

## Acceptance Criteria

- [ ] AC1. Document routes that need vendor dataset context no longer perform an
  unbounded scan across all knowledge bases; tests cover missing and present
  `knowledgeBaseId` behavior.
- [ ] AC2. Runtime auth tests prove a route decorated with non-Gateway legacy
  auth types is rejected even when the service token is present.
- [ ] AC3. Runtime tenant provisioning behavior is explicit and tested for both
  enabled and disabled modes.
- [ ] AC4. Adapter tests prove invalid `chunkStrategy` on create/update returns
  `400 validation_error` and valid config still reaches vendor `parser_config`.
- [ ] AC5. Retrieval trace no longer claims `vendor-default`,
  `EmbeddingDimension: 0`, or `QdrantCollection: elasticsearch` as facts unless
  those values are actually derived from runtime/config.
- [ ] AC6. Vendor 404/not-found mapping uses stable vendorclient
  classification; tests do not depend on `"not found"` string matching.
- [ ] AC7. RAPTOR/GraphRAG dataset-level task behavior is separated from real
  document IDs or guarded by explicit compatibility naming and tests.
- [ ] AC8. Empty/whitespace chunks do not produce real embedding vectors from
  the literal placeholder `"None"`; tests cover empty chunk behavior.
- [ ] AC9. Metadata filter fallback has a configured cap or explicit
  failure/degraded path; tests cover pushdown failure over the cap.
- [ ] AC10. Validation passes:
  `cd services/knowledge && go test ./...` and `go build ./cmd/adapter`.
- [ ] AC11. Runtime targeted validation passes:
  `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --python 3.13 --with pytest --with pytest-asyncio --with filelock --with ruamel-yaml python -m pytest test/routes/test_config_utils.py test/routes/test_route_registry.py test/routes/test_gateway_auth.py test/routes/test_runtime_dependency_check.py test/ci/test_deepdoc_pdf_parser_import.py -q`.
- [ ] AC12. Runtime Python compile check passes:
  `cd services/knowledge-runtime && uv run --no-project --python 3.13 python -m compileall -q api common rag deepdoc deploy`.
- [ ] AC13. `git diff --check` passes.

## Out Of Scope

- Frontend changes.
- QA agent orchestration or answer-generation redesign.
- Restoring removed `services/parser`.
- Reintroducing the removed `common/data_source` connector snapshot.
- Full real-dependency PDF ingestion E2E unless local dependencies are already
  available; if not available, record it as skipped with reason.

## Open Questions

None blocking. Repository evidence is sufficient to implement the requested
compatibility debt cleanup.

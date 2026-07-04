# Implementation Plan

## Checklist

1. Re-load backend specs with `trellis-before-dev`.
2. Adapter document context:
   - Replace `findDocumentByID` route usage with explicit
     `knowledgeBaseId` extraction/validation.
   - Keep bounded helper for routes where context is provided.
   - Update adapter tests.
3. Adapter request validation:
   - Return validation errors for invalid `chunkStrategy`.
   - Add create/update tests.
4. Adapter trace and vendor errors:
   - Extend retrieval trace mapping to avoid hard-coded false values.
   - Add stable HTTP status/code classification in `vendorclient.APIError`.
   - Update error mapping tests.
5. Runtime auth:
   - Enforce `GATEWAY` in declared auth types.
   - Add/extend route auth tests.
6. Runtime tenant provisioning:
   - Add env config for auto-provisioning.
   - Test enabled/disabled behavior.
7. Runtime RAPTOR fake document containment:
   - Rename/contain fake doc path or add explicit task scope checks.
   - Add targeted tests where existing task code is testable without real Redis.
8. Runtime empty embedding:
   - Replace literal `"None"` indexing for empty chunks with skip/error
     behavior.
   - Add unit tests for helper and embedding flow.
9. Runtime metadata fallback:
   - Add fallback cap and explicit error/degraded path.
   - Add tests for pushdown failure over cap.
10. Documentation updates for new env keys/behavior.
11. Validation:
    - `cd services/knowledge && go test ./...`
    - `cd services/knowledge && go build ./cmd/adapter`
    - `cd services/knowledge-runtime && uv run --no-project --python 3.13 python -m compileall -q api common rag deepdoc deploy`
    - `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --python 3.13 --with pytest --with pytest-asyncio --with filelock --with ruamel-yaml python -m pytest test/routes/test_config_utils.py test/routes/test_route_registry.py test/routes/test_gateway_auth.py test/routes/test_runtime_dependency_check.py test/ci/test_deepdoc_pdf_parser_import.py -q`
    - Targeted pytest for newly changed runtime unit tests.
    - `git diff --check`

## Risky Files

- `services/knowledge/internal/adapter/handlers.go`
- `services/knowledge/internal/adapter/map.go`
- `services/knowledge/internal/vendorclient/client.go`
- `services/knowledge-runtime/api/apps/__init__.py`
- `services/knowledge-runtime/api/utils/gateway_tenant_provisioning.py`
- `services/knowledge-runtime/api/db/services/llm_service.py`
- `services/knowledge-runtime/common/metadata_utils.py`
- `services/knowledge-runtime/api/db/services/doc_metadata_service.py`
- `services/knowledge-runtime/api/db/services/document_service.py`
- `services/knowledge-runtime/rag/svr/task_executor.py`

## Existing Worktree Note

The worktree already contains staged deletion of
`services/knowledge-runtime/common/data_source/**` and the matching
`README_zh.md` edit from the previous cleanup. Do not revert it.

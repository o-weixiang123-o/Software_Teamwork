# Reachability Map (Stage A)

Generated 2026-07-03 on branch `L1nggTeam/feat/ragflow-runtime-vendor`.

## Product entry: Go adapter routes (`services/knowledge/internal/adapter/server.go:90-115`)

| Adapter route (`/internal/v1/*`) | Runtime endpoint hit (via `vendorclient/client.go`) |
|---|---|
| GET/POST `knowledge-bases` | `GET/POST /api/v1/datasets` |
| GET/PATCH/DELETE `knowledge-bases/{id}` | `GET/PUT/DELETE /api/v1/datasets/{id}` |
| GET/POST `knowledge-bases/{id}/documents` | `GET/POST /api/v1/datasets/{id}/documents` (upload → `StartDocumentParse` → `POST /api/v1/datasets/{id}/chunks` run) |
| GET/PATCH/DELETE `documents/{id}` | dataset-scoped document endpoints (requires `knowledgeBaseId` query) |
| GET `documents/{id}/chunks` | `GET /api/v1/datasets/{id}/documents/{docId}/chunks` |
| GET `documents/{id}/content` | document download |
| POST `knowledge-queries` | `POST /api/v1/retrieval` (`RetrievalSearch`) |
| GET `knowledge-statistics` | dataset listing aggregation |
| CRUD `parser-configs` | project-owned Postgres (NOT runtime) — `internal/service` + `repository` |

Auth chain: adapter → runtime carries `X-Service-Token` + user context; runtime side `api/utils/gateway_auth.py` + `gateway_tenant_provisioning.py`.

## Reachable runtime API files

- `api/apps/restful_apis/{dataset_api,document_api,chunk_api}.py` — all adapter traffic.
- `api/apps/restful_apis/{task_api,system_api}.py` — parse trigger (`run`), health.
- `api/apps/restful_apis/{models_api,provider_api}.py` — NOT adapter-reachable (runtime-internal model mgmt); in scope only for tenant-scoping spot check.
- `api/apps/services/{dataset_api_service,document_api_service,file_api_service}.py` — business logic for the above.
- `api/db/services/{document_service,task_service,doc_metadata_service,llm_service}.py` — used by the above (queue_tasks at `task_service.py:367`).
- `api/utils/{gateway_auth,gateway_tenant_provisioning}.py` — every request.

## Reachable parser_ids (chunkers)

Dataset `chunk_method` comes from adapter `docType` passthrough (`map.go:372`, `map.go:409` — free string, no enum gate in adapter; runtime validates). `FACTORY` (`rag/svr/task_executor.py:113-129`): general/naive, paper, book, presentation, manual, laws, qa, table, resume, picture, one, audio, email, kg→naive, tag.

- Auto-forced by file type (`document_api.py:515-517`): VISUAL→picture, audio→audio.
- Guard (`document_api.py:213-214`): VISUAL must stay picture; ppt/pptx must stay presentation.
- All FACTORY chunkers are product-reachable because docType is a free string. Review priority: naive (default "general"), paper, book, qa, table, presentation, manual, laws, one, picture. Lower: resume/audio/email/tag (reachable but unlikely in power-industry product; spot-check only).

## Reachable deepdoc parsers

Via chunkers: `pdf_parser` (+`figure_parser`), `docx_parser`, `excel_parser`, `ppt_parser`, `txt_parser`, `markdown_parser`, `html_parser`, `json_parser`. Layout engines chosen by `layout_recognize` (`map.go:442-458`): DeepDOC (builtin), PaddleOCR (local OCR / remote-compatible default), MinerU/Docling/TCADP/OpenDataLoader (only if configured — spot-check config gating only), Plain Text (tika/unstructured backends). `deepdoc/server/adapters/{dla_adapter,tsr_adapter}.py` are project-diverged — in scope.

## Reachable worker paths

- Queueing: `api/db/services/task_service.py:367 queue_tasks` — PDF page-split (page_size 12/22/all), table row-split (3000), else single task; digest-based chunk reuse; RAPTOR/GraphRAG excluded from digest.
- Execution: `rag/svr/task_executor.py` `handle_task:1502` → `do_handle_task:1187` → FACTORY chunker → embed (`task_executor_refactor/{embedding_service,embedding_utils}.py`) → index write (`chunk_service/chunk_builder`).
- Special task types (`task_executor.py:133-135`): raptor, graphrag, mindmap — enqueued when kb parser_config enables them (`DATASET_SCOPE_TASK_DOC_ID = "graph_raptor_x"` sentinel at `task_service.py:38`). Sentinel handling in scope; raptor/graphrag internals out of scope.
- Support: `task_executor_limiter.py`, `task_executor_refactor/*` (all project-diverged or on hot path — in scope).

## Retrieval path

`POST /internal/v1/knowledge-queries` → `buildRetrievalBody` (`map.go:594`) → runtime `POST /api/v1/retrieval` (handled in `chunk_api.py` / `dataset_api_service.py`) → `settings.retriever.retrieval` → optional rerank → `mapRetrievalChunk` (`map.go:336`). Metadata filter: manual conditions (`map.go:635-668`) → runtime `meta_data_filter` → `doc_metadata_service.py` (in-memory fallback bounded by `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT=10000`).

## Out of scope (confirmed dormant or excluded)

- `rag/graphrag/*`, `rag/raptor.py` internals (beyond sentinel/skip logic in task_executor).
- `deepdoc/vision/*` model internals (adapters excepted), `deepdoc/parser/resume/*` internals.
- `api/apps/restful_apis/{models_api,provider_api}.py` deep review (tenant-scope spot check only).
- MCP surface, `common/data_source` (already deleted), retired `services/parser/`.

# Review and optimize knowledge module (document parsing & RAG)

## Goal

Review the knowledge management module for **correctness and robustness** along the **actually-executed pipeline paths**, fix confirmed defects on PR branch `L1nggTeam/feat/ragflow-runtime-vendor` (PR #536), and produce a severity-ranked review report for anything not fixed inline.

## Background (confirmed by repo inspection)

- `services/knowledge-runtime/` is a vendored RAGFlow runtime (upstream commit `45fc7fea`, imported 2026-07-01, Apache-2.0; see `UPSTREAM.md`).
  - Runtime API: `api/ragflow_server.py` on `127.0.0.1:9380`; routes in `api/apps/restful_apis/` (chunk/dataset/document/models/provider/system/task), business logic in `api/apps/services/`.
  - Runtime worker: `rag/svr/task_executor.py` (`FACTORY` at line 113 maps `parser_id` → `rag/app/*` chunkers; `do_handle_task` at line 1187) plus `rag/svr/task_executor_refactor/` (chunk_builder, embedding_service, raptor_service, task_handler, etc.).
  - Parsers: `deepdoc/parser/` (pdf, docx, excel, html, json, markdown, ppt, txt, epub, figure + external engines docling/mineru/paddleocr/tcadp/opendataloader).
- `services/knowledge/` is the project-owned Go service; contract adapter `internal/adapter/` + `internal/vendorclient/` call the runtime HTTP API (`/api/v1/*`). Adapter exposes `/internal/v1/*` routes (knowledge-bases, documents, chunks, knowledge-queries, parser-configs, statistics).
- Project-diverged files (vs upstream): `rag/svr/task_executor.py`, `task_executor_refactor/{constants,embedding_service,embedding_utils}.py`, `deepdoc/server/adapters/*`, `api/apps/services/*`, `api/db/services/*`, `api/utils/gateway_auth.py`, `api/utils/gateway_tenant_provisioning.py`.
- Prior hardening on this branch (PR #440 + #536): explicit `knowledgeBaseId`, chunkStrategy JSON validation, stable vendor error mapping, runtime auth + tenant provisioning guardrails, RAPTOR/GraphRAG doc-id sentinel guards, empty-chunk embedding skip, bounded metadata fallback, `common/data_source` connector removal (−25k lines).

## Requirements

R1. Review the worker ingestion pipeline (`rag/svr/task_executor.py`, `rag/svr/task_executor_refactor/`) for correctness/robustness defects: error handling, task state transitions, partial-failure recovery, resource cleanup, concurrency.

R2. Review the actually-reachable parsing path (`deepdoc/parser/` for formats reachable from the adapter contract: pdf, docx, excel, txt, markdown, html, ppt + `rag/app/naive.py` and other chunkers registered in `FACTORY` that the product surface can select) for crash-prone edge cases: malformed input, empty files, encoding issues, unbounded memory.

R3. Review the retrieval path (`api/apps/restful_apis/{chunk_api,dataset_api}.py`, `api/apps/services/dataset_api_service.py`, retrieval/rerank flow) for correctness: pagination, filtering, tenant scoping, error propagation.

R4. Review the Go adapter integration surface (`services/knowledge/internal/adapter/`, `internal/vendorclient/`) for contract mismatches against the runtime API and the OpenAPI docs (`services/knowledge/api/openapi.yaml`, `docs/services/knowledge/api/*.yaml`).

R5. **(Scope changed 2026-07-03 by user)** Do NOT modify code. For each confirmed P0/P1 finding, produce a concrete fix proposal: root cause, minimal-diff change sketch (file:line + what to change), validation plan (test/command that would prove it), and risk notes.

R6. All findings (confirmed + rejected-with-reason) go into a severity-ranked report in the task directory (`review-report.md`) with file:line anchors and per-finding fix proposals.

## Constraints

- Vendored code not on the executed path stays untouched (UPSTREAM.md refresh policy; upstream diff surface must stay minimal).
- No restore of retired `services/parser/`; no RAGFlow MCP product surface changes.
- Runtime work follows `uv` (Python 3.13, `uv sync --frozen`); Go work follows service-local `go test ./...`.
- Backend spec rules apply: `.trellis/spec/backend/` (error-handling, api-contracts, database-guidelines, logging, quality).

## Acceptance Criteria

- [ ] AC1: Review covers R1–R4 scope; each reviewed area has findings (or an explicit "no findings" note) recorded in `review-report.md` with file:line anchors and severity (P0 blocker / P1 should-fix / P2 nice-to-have).
- [ ] AC2: Every confirmed P0/P1 finding has been adversarially verified against the code (not just reported by one reviewer) and carries a concrete fix proposal: minimal-diff change sketch, validation plan, risk notes.
- [ ] AC3: No source code is modified. Only task-directory artifacts (`research/*.md`, `review-report.md`, planning docs) are produced.
- [ ] AC4: Report separates: confirmed-fix-now candidates vs needs-product-decision vs upstream-refresh-risk items.

## Out of Scope

- Performance optimization work (throughput, latency, memory profiling) — record observations only, no perf-driven changes.
- Deep vendor internals not reachable from the product surface (graphrag, raptor internals beyond sentinel handling, vision model training code, unused parsers like resume/audio/email unless FACTORY-reachable and product-selectable).
- Frontend, gateway routing, QA service changes (except doc sync required by AC4).

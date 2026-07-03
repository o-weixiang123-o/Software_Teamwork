# Implement Knowledge RAG review fixes

## Goal

Implement the directly actionable fixes from the archived Knowledge parsing and RAG review so that the runtime and Go adapter no longer silently drop dataset-scope tasks, leave invalid documents stuck in RUNNING, lose reachable parser data, or return contract-mismatched adapter responses.

Source review: `.trellis/tasks/archive/2026-07/07-03-review-optimize-knowledge-rag/review-report.md`.

## Background

- The review confirmed one P0: dataset-scope RAPTOR/GraphRAG/mindmap tasks use sentinel `Task.doc_id` values and currently fail because `TaskService.get_task(task_id, doc_ids)` ignores `doc_ids` when joining `Task` to `Document`.
- The review confirmed multiple P1 issues in reachable paths:
  - runtime `queue_tasks` can produce zero tasks after setting a document RUNNING, leaving the document stuck;
  - parser/OCR failures can write `progress=-1` and then be overwritten by `No chunk built` success progress;
  - PDF book chunking drops collected tables/figures;
  - Excel vision descriptions can be corrupted when list data is treated as a string;
  - Markdown vision enhancement can crash tasks instead of degrading;
  - retrieval against empty/uncreated indexes becomes a 502 through the adapter instead of an empty result;
  - Go adapter retrieval does not map `topK` to runtime `size`;
  - document download can return a JSON error envelope as file bytes;
  - PATCH document tags can be omitted from the immediate response;
  - statistics and JSON decoding have avoidable boundary issues.
  - business validation failures collapse into runtime body `code=102` and adapter `dependency_error`.
- Existing staged changes are Trellis archival artifacts from the review and must be preserved.

## Requirements

- R1: Fix `TaskService.get_task` so dataset-scope runtime tasks can join through a real source document when `doc_ids` is provided.
- R2: Fix runtime queue/task state handling so a document that produces no parse tasks is marked failed with a clear progress message instead of remaining RUNNING.
- R2a: Fix parser/OCR failure state handling so a task already marked failed is not overwritten as DONE/zero-chunk by the empty-chunk fallback, and PaddleOCR worker-context failures report negative progress.
- R3: Fix reachable parser data-integrity issues with minimal vendor diffs:
  - preserve book PDF tables/figures;
  - prevent vision description list/string corruption;
  - make Markdown vision enhancement fail open and keep original content.
- R4: Fix retrieval empty-index behavior so product-facing search returns a zero-result payload instead of an adapter 502.
- R5: Fix Go adapter contract mismatches:
  - always send runtime `size` from project `topK`;
  - reject JSON error envelopes during download;
  - preserve PATCH tags in the immediate response;
  - remove unbounded statistics N+1 behavior where the required count is already available;
  - cap JSON request bodies and map oversize bodies to validation errors.
- R6: Fix P1-C2 by classifying runtime search business failures and mapping them through the Go adapter/OpenAPI contract:
  - invalid request fields and unsupported dataset combinations -> validation error;
  - missing or hidden datasets -> not found;
  - runtime capacity or filter fallback limits -> validation error unless a stable capacity code already exists;
  - runtime dependency failures remain dependency errors.
- R7: Add or update focused tests for changed behavior where the existing test harness supports it.
- R8: Keep the scope inside Knowledge/RAG backend and runtime. Do not implement QA agent orchestration, frontend changes, or unrelated vendor refreshes.

## Deferred

- Broader P2 parser/retrieval cleanup is deferred except for small localized fixes that naturally share files with the P0/P1 changes.

## Acceptance Criteria

- [ ] AC1: Dataset-scope sentinel tasks using `doc_ids` can be loaded by `TaskService.get_task`, with a focused regression test or equivalent runtime guardrail test.
- [ ] AC2: Zero-task parse scheduling marks the document failed and is covered by a focused test or validated via existing runtime tests.
- [ ] AC2a: Parser/OCR failures that return no chunks preserve `progress=-1`/failed state instead of being overwritten by `No chunk built` success progress.
- [ ] AC3: Book PDF tables/figures, Excel vision descriptions, and Markdown vision degradation are fixed without broad vendor rewrites.
- [ ] AC4: Empty-index retrieval returns an empty result payload through the runtime path.
- [ ] AC5: Go adapter tests cover `topK -> size`, download JSON error-envelope rejection, PATCH tags response preservation, and JSON body size rejection where feasible.
- [ ] AC6: Runtime search business errors produce stable HTTP/body codes, and adapter tests prove they map to `validation_error`, `not_found`, or `dependency_error` instead of blanket 502.
- [ ] AC7: Relevant Python runtime and Go Knowledge checks are run, or any skipped checks are reported with concrete reasons.

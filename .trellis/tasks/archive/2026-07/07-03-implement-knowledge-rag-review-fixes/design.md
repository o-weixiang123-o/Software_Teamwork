# Design

## Scope and Boundaries

This task touches two owned runtime boundaries:

- `services/knowledge-runtime`: vendored RAGFlow runtime code on product-reachable ingestion, parsing, and retrieval paths. Changes must be minimal, localized, and justified by the review report.
- `services/knowledge`: project-owned Go adapter and HTTP handlers. Changes must preserve stable service contracts and existing error-handling patterns.

The task does not change QA agent orchestration, frontend behavior, gateway routing, or vendor modules outside the reachable parsing/RAG paths.

## Runtime Fixes

### Dataset-scope task loading

`TaskService.get_task(task_id, doc_ids)` already receives `doc_ids` from worker collection for sentinel task types. Restore the intended join behavior by using the first real source document id as the join key when `doc_ids` is non-empty. This keeps normal document tasks unchanged and allows sentinel tasks to inherit tenant/KB metadata through the source document.

### Zero-task scheduling

`queue_tasks` currently computes `parse_task_array` and then enqueues rows. When no rows are generated, document state must be flipped to failure immediately, with a stable progress message. This keeps `_sync_progress` from skipping a RUNNING document forever.

### Parser failure progress preservation

Some parser branches mark the current task failed with `progress=-1` and then return an empty chunk list. The task handler must preserve that failure state instead of writing `progress=1.0` with `No chunk built`. PaddleOCR worker-context failures must also call the progress callback with `-1` before returning an empty parser result, so OCR outages and missing models do not become DONE/zero-chunk documents.

### Parser data integrity

Use minimal local fixes:

- book PDF branch: feed collected `tables` into the table-tokenization path instead of leaving `tbls` empty;
- vision descriptions: preserve list values until the final join point, or normalize to a one-element list before joining;
- Markdown vision: wrap enhancement in the same degrade-on-error pattern used by adjacent parser wrappers.

### Empty-index retrieval

Catch the runtime document-engine "index not found" condition in the dataset search path and return the existing zero-result shape.

### Search business error taxonomy

For P1-C2, replace untyped `(False, message)` search failures with a small structured runtime error result that carries both a runtime body code and HTTP status. The mapping for this task is:

| Runtime condition | Runtime HTTP | Runtime body code | Adapter code |
| --- | ---: | ---: | --- |
| invalid `doc_ids`, embedding-model mismatch, metadata filter fallback too large | 400 | `ARGUMENT_ERROR` | `validation_error` |
| dataset missing or hidden from caller | 404 | `NOT_FOUND` | `not_found` |
| runtime/retriever/model infrastructure failure | 502 | `EXCEPTION_ERROR` or HTTP 5xx | `dependency_error` |
| empty/uncreated index | 200 | `SUCCESS` | empty results |

Dataset visibility failures intentionally map to 404 rather than 403 so callers cannot distinguish a hidden dataset from a missing one. Adapter permission-scope failures still return 403 before the runtime call.

## Go Adapter Fixes

### Retrieval request mapping

Set runtime `size` from project `topK` every time. Keep candidate pool fields (`top_k`) compatible with rerank behavior, ensuring `top_k >= size` when rerank topN is configured.

### Download error envelopes

`DownloadDocument` must inspect JSON responses even when HTTP status is 200. If the JSON envelope carries a non-zero runtime code, return an adapter error instead of treating the body as file bytes.

### PATCH tags response

After updating tags, return a document model that includes the effective tags. Prefer re-fetching from runtime if the existing adapter already has a safe method; otherwise merge the requested tags into the immediate mapped response.

### Statistics and body limits

Use dataset-list metadata such as `doc_num` where available to avoid one request per KB. Add a bounded JSON body reader at the shared decode point so all JSON endpoints inherit the same limit and error behavior.

## Compatibility

- Runtime changes keep normal document-task behavior unchanged.
- Adapter changes align responses with documented project semantics.
- OpenAPI must document the corrected error matrix for knowledge queries.

## Rollback

Each fix is local:

- runtime task/parser/retrieval changes can be reverted by file;
- Go adapter changes can be reverted by test-supported commits;
- no database migrations or persisted schema changes are introduced.

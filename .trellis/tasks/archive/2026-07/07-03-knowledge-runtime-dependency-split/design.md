# Knowledge runtime API worker dependency split design

## Architecture

The target runtime shape has two dependency profiles:

- API profile: enough to run `api/ragflow_server.py`, active REST routes,
  runtime status, dataset/document/search APIs, Redis/PostgreSQL/MinIO/doc
  engine clients, and `/documents/parse` queue enqueue.
- Worker profile: API profile plus parser, OCR, deepdoc, browser/crawler,
  GraphRAG/spaCy, and ingestion worker dependencies needed by
  `rag/svr/task_executor.py`.

Knowledge adapter remains outside the Python runtime. It continues to call
`VENDOR_RUNTIME_URL` over HTTP. Upload ingestion still queues work through
`/documents/parse`; worker lifecycle remains deployment-owned.

## Dependency Model

Preferred shape:

- Keep base `dependencies` minimal enough for API-only runtime startup.
- Add a dependency group such as `worker` for parsing and ingestion worker
  packages.
- Keep the existing `test` group for test-only packages.

If a dependency is shared by API and worker, keep it in base. If it is imported
only by deepdoc, task executor, crawler, OCR, optional provider integrations, or
GraphRAG worker paths, move it to `worker`.

High-risk dependencies to classify carefully:

- `litellm`, provider SDKs, and embedding/rerank SDKs may be needed at API time
  if route imports or model metadata initialization touch them.
- `rag.app.tag` is imported by some API paths, so text/NLP dependencies may not
  be cleanly worker-only until imports are made lazy.
- `api.utils.web_utils` imports Selenium directly; if API module import pulls it
  in, add a narrow lazy import before moving Selenium packages.

## Startup Commands

Local API-only startup should use an API dependency sync command and start only
`services/knowledge-runtime/deploy/api/run-local.sh`.

Full ingestion startup should use the worker/full dependency sync command and
start both API and `services/knowledge-runtime/deploy/worker/run-local.sh`.

`run-knowledge-parse-stack.sh` remains the full parse-stack helper. A new
API-only helper or flag is acceptable if it keeps the default backend path
clear:

```text
run-backend.sh                 -> Go backends only
run-knowledge-runtime-api.sh    -> runtime API only
run-knowledge-parse-stack.sh    -> runtime API + worker + adapter
```

## Compatibility

The split should preserve existing behavior when the full worker profile is
installed. Any API import changes should be narrow lazy imports, not functional
rewrites. Existing tests that use `uv run --no-project --with ... pytest` should
continue to work because they bypass the project lock for targeted route tests.

## Rollback

Rollback is straightforward: restore `pyproject.toml`, `uv.lock`, and startup
script sync commands to the single full dependency model. The adapter-side
query-first readiness changes do not need to be rolled back with this task.

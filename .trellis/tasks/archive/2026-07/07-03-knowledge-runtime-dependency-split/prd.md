# Knowledge runtime API worker dependency split

## Goal

Make the Knowledge runtime dependency model match the query-first architecture:
the runtime API should be installable and runnable without forcing the full
parser/OCR/browser/worker dependency set, while the parser worker keeps the
complete dependencies it needs for document parsing, chunking, embedding, and
indexing.

This is a follow-up child task of
`07-03-knowledge-query-only-readiness`. That parent made the Knowledge adapter
query-ready without starting the parser worker. This task removes the next
startup blocker discovered during validation: `uv sync --python 3.13 --frozen`
currently installs the full RAGFlow dependency set even when only the runtime
API is needed.

## Evidence

- `services/knowledge-runtime/api/ragflow_server.py:35-46` starts the runtime
  API from `api.apps`, DB/runtime config, common settings, MCP shutdown, logging,
  and Redis locking.
- `services/knowledge-runtime/api/route_registry.py:4-13` limits active runtime
  REST routes to `chunk_api.py`, `dataset_api.py`, `document_api.py`,
  `models_api.py`, `provider_api.py`, `system_api.py`, and `task_api.py`.
- `services/knowledge-runtime/pyproject.toml:9-90` puts all runtime dependencies
  in base `dependencies`, including provider SDKs, browser/crawler tooling,
  OCR/vision packages, and parser packages.
- Examples of dependencies that are not appropriate to force for a query/API
  startup path include `crawl4ai` (`pyproject.toml:18`), `onnxruntime-gpu`
  (`pyproject.toml:48`), `opencv-python` (`pyproject.toml:49`),
  `selenium-wire` (`pyproject.toml:75`), `spacy` plus
  `en-core-web-sm` (`pyproject.toml:76-77`), and `xgboost`
  (`pyproject.toml:87`).
- `scripts/local/run-knowledge-parse-stack.sh:288-299` currently runs plain
  `uv sync --python 3.13 --frozen`, so local full-stack startup always prepares
  the full base dependency set.
- `scripts/local/run-knowledge-parse-stack.sh:482-484` starts both runtime API
  and runtime worker in `host` mode, while `external` mode skips runtime process
  startup. There is no current helper for "API only, no worker".
- `services/knowledge-runtime/deploy/api/run-local.sh:21-23` and
  `services/knowledge-runtime/deploy/worker/run-local.sh:22-24` both assume the
  same `.venv` and run the same runtime dependency check before starting.

## Requirements

- Split the runtime Python dependency model so the API-only path does not
  install parser/OCR/browser/worker-only dependencies by default.
- Preserve a worker/full dependency path that can still run
  `rag/svr/task_executor.py` for real ingestion.
- Preserve the existing RAGFlow vendored code behavior unless a lazy import or
  import guard is required to keep API startup from importing worker-only
  modules.
- Add a local API-only startup path for validation of query-first runtime
  readiness without starting the parser worker.
- Keep `run-knowledge-parse-stack.sh` as the full ingestion helper that may
  install worker dependencies and start the worker.
- Keep deployment guidance aligned with the architecture:
  runtime API is always-on, parser worker is deployment-managed or KEDA-scaled.
- Update tests or add lightweight verification so dependency grouping drift is
  detectable.

## Acceptance Criteria

- [ ] `services/knowledge-runtime/pyproject.toml` has separate dependency
      groups or extras for API-only and worker/full runtime dependencies.
- [ ] API-only dependency sync does not include known worker/browser/OCR-heavy
      packages such as `onnxruntime-gpu`, `opencv-python`, `crawl4ai`,
      `playwright`/`patchright`, `selenium-wire`, `spacy`, or `xgboost`.
- [ ] Worker/full dependency sync still includes the packages needed by
      `rag/svr/task_executor.py` and deepdoc parsing.
- [ ] There is a documented command or helper to start runtime API only, without
      starting the worker.
- [ ] Existing full parse-stack helper still supports real ingestion smoke and
      starts the worker explicitly.
- [ ] Knowledge adapter `/readyz` can be validated against runtime API without
      requiring a worker heartbeat when `KNOWLEDGE_RUNTIME_READINESS_MODE=query`.
- [ ] Regression checks cover dependency split contracts and relevant startup
      script behavior.
- [ ] Documentation explains which dependency profile is for API-only, which is
      for worker/full ingestion, and why production should keep worker
      dependencies out of the always-on API image/process.

## Out Of Scope

- Changing parser quality, OCR model behavior, chunking, embedding, or indexing
  algorithms.
- Rewriting large RAGFlow modules unless a small lazy import is necessary to
  keep API startup clean.
- Building final production Docker images in this slice.
- Running a real PDF ingestion E2E unless the required provider credentials and
  local runtime dependencies are already available.
- Changing Knowledge adapter API contracts beyond the already-planned
  query-first readiness behavior.

## Open Question

- Should this first slice stop at repo-local dependency groups, startup scripts,
  docs, and contract tests, or should it also introduce separate production
  Docker image definitions for API and worker?

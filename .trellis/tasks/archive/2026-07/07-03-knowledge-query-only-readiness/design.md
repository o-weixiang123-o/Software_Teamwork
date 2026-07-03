# Design

## Architecture

Keep the Knowledge Go adapter as the owner of upload orchestration and keep the
RAGFlow worker as an external runtime process. The adapter does not embed
PaddleOCR, RAGFlow worker code, queue consumers, process supervision, or shell
startup commands.

Upload flow:

1. Upload document through the existing vendor runtime API.
2. If `KNOWLEDGE_AUTO_START_INGESTION=false`, return the uploaded document
   without calling `/documents/parse`.
3. If ingestion is enabled, call runtime `/documents/parse`.
4. If `/documents/parse` fails, delete the uploaded document and return the
   existing sanitized dependency/validation error.
5. If `/documents/parse` succeeds, return the document as `RUNNING`; worker
   pickup is asynchronous and deployment-owned.

Readiness flow:

1. `/healthz` remains process liveness.
2. `/readyz` checks runtime API ping/status.
3. `KNOWLEDGE_RUNTIME_READINESS_MODE=ingestion` keeps requiring a task executor
   heartbeat for ingestion-heavy deployments.
4. `KNOWLEDGE_RUNTIME_READINESS_MODE=query` ignores missing task executor
   heartbeat for readiness but still reports task executor diagnostics.

## Worker Lifecycle

The adapter queues parse work; it does not start workers. Production should use
deployment-native control planes:

- Kubernetes: KEDA `ScaledObject` watches runtime Redis Streams lag and scales
  the `knowledge-runtime-worker` Deployment from 0.
- systemd/supervisor: run the worker as a managed service and use operational
  policy outside the adapter to keep it stopped, started, or scaled.
- Local: run `services/knowledge-runtime/deploy/worker/run-local.sh` manually
  when testing ingestion.

## Queue Contract

The vendored runtime currently enqueues tasks with:

- Redis Streams via `REDIS_CONN.queue_product(... xadd ...)`.
- Queue names from `settings.get_svr_queue_name(priority, suffix)`.
- Current common worker queue names: `te.1.common` and `te.0.common`.
- Consumer group: `rag_flow_svr_task_broker`.

KEDA examples should be treated as deployment examples, not adapter code. If a
runtime upgrade changes queue names or consumer groups, the deployment manifest
must be updated with the runtime.

## Compatibility

- Existing deployments that already run the worker are unaffected.
- Query-only deployments can run without a worker by using
  `KNOWLEDGE_RUNTIME_READINESS_MODE=query`.
- Uploads with auto ingestion enabled will enqueue work even if no worker is
  currently alive; worker startup and scaling are intentionally outside the
  adapter.
- Deployments that do not want upload ingestion can set
  `KNOWLEDGE_AUTO_START_INGESTION=false`.

## Risk Controls

- No shell execution path in the adapter, avoiding process-supervision and
  command-injection risk.
- Runtime status keeps exposing worker heartbeat diagnostics for operators.
- KEDA example is explicit about Redis Streams queue names and consumer group.
- Unit tests prove upload enqueue does not depend on worker heartbeat.

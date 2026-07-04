# Knowledge query-first readiness and queue-driven parser scaling

## Goal

Make Knowledge usable as a query-first RAG service by default while keeping
document parsing available only when upload ingestion actually queues work. The
Knowledge adapter must not start or supervise parser workers. Worker lifecycle
belongs to deployment infrastructure, with production deployments expected to
scale the RAGFlow runtime worker from zero based on Redis Stream queue lag.

## Background

- `services/knowledge-runtime` already has separate host-run processes:
  - `deploy/api/run-local.sh` starts the runtime API used by retrieval, upload,
    status, and parse queue routes.
  - `deploy/worker/run-local.sh` starts the runtime worker / task executor that
    consumes parse, chunk, embedding, and index tasks.
- Query requests call `/api/v1/datasets/search` and do not require
  `/documents/parse` or a live task executor heartbeat.
- Document upload calls `/documents/parse` only when
  `KNOWLEDGE_AUTO_START_INGESTION=true`; that runtime call enqueues Redis Stream
  tasks for the worker.
- RAGFlow runtime task queues are Redis Streams named from
  `settings.get_svr_queue_name`, currently `te.1.common` and `te.0.common` for
  the common worker.

## Requirements

- Default local/backend startup must not start the runtime worker.
- Query-first readiness must allow Knowledge `/readyz` to pass without a task
  executor heartbeat when runtime API and query-time dependencies are healthy.
- Runtime diagnostics must continue exposing `task_executor_ready` and
  `task_executor_count` so ingestion readiness remains visible.
- Upload ingestion remains controlled by `KNOWLEDGE_AUTO_START_INGESTION`.
- When `KNOWLEDGE_AUTO_START_INGESTION=false`, upload must not call
  `/documents/parse`.
- When `KNOWLEDGE_AUTO_START_INGESTION=true`, upload should call
  `/documents/parse` after vendor upload without pre-checking or waiting for a
  worker heartbeat. The deployment layer is responsible for starting workers
  after queue lag appears.
- The Knowledge adapter must not expose worker start command configuration and
  must not execute shell commands.
- Production docs must describe KEDA / queue-driven worker autoscaling as the
  supported on-demand path.
- Local docs may instruct developers to start
  `services/knowledge-runtime/deploy/worker/run-local.sh` manually for ingestion
  smoke tests.

## Acceptance Criteria

- [ ] `scripts/local/run-backend.sh` does not start the Knowledge runtime worker
      or inject a worker start command.
- [ ] `adapterconfig.Config` has no `KNOWLEDGE_RUNTIME_WORKER_*` fields or env
      parsing.
- [ ] `services/knowledge/internal/adapter` has no worker-start shell execution
      path.
- [ ] With `KNOWLEDGE_AUTO_START_INGESTION=false`, upload skips
      `/documents/parse`.
- [ ] With `KNOWLEDGE_AUTO_START_INGESTION=true`, upload calls
      `/documents/parse` even when runtime status has no task executor
      heartbeat.
- [ ] Query readiness remains decoupled from worker heartbeat in
      `KNOWLEDGE_RUNTIME_READINESS_MODE=query`.
- [ ] Deployment docs include a KEDA Redis Streams scaling example for the
      Knowledge runtime worker.
- [ ] Tests cover default readiness, query readiness, skipped ingestion, and
      upload enqueue without worker heartbeat.

## Out Of Scope

- Running a real RAGFlow worker during unit tests.
- Implementing an in-process worker supervisor in the Knowledge adapter.
- Changing RAGFlow parser quality, chunking, embedding, indexing, or queue
  internals.
- Changing QA agent orchestration, MCP tool behavior, or answer generation.

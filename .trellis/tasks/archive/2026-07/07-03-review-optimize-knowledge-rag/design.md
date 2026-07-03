# Design — Review and optimize knowledge module (document parsing & RAG)

## Approach

This is a review-then-fix task, not a feature build. The "architecture" here is the review methodology and the fix boundary policy.

## Review Scope Model

Reviewed surface = union of:

1. **Worker ingestion path** (Python): Redis task dequeue → `rag/svr/task_executor.py::handle_task/do_handle_task` → `FACTORY[parser_id]` chunker (`rag/app/*.py`) → `deepdoc/parser/*` → `task_executor_refactor/` (chunk_builder → embedding_service/embedding_utils → chunk_service write) → doc-engine index write.
2. **Retrieval path** (Python): `api/apps/restful_apis/{dataset_api,chunk_api}.py` → `api/apps/services/dataset_api_service.py` → retrieval/rerank → response envelope.
3. **Adapter contract path** (Go): `services/knowledge/internal/adapter/{server,handlers,map,auth,context}.go` → `internal/vendorclient/client.go` → runtime `/api/v1/*`; plus auth chain `api/utils/gateway_auth.py`, `api/utils/gateway_tenant_provisioning.py`.

Reachability rule: a runtime code path is in scope only if an adapter route (`/internal/v1/*`) or a worker task type actually reaches it. `FACTORY` chunkers count as reachable only if product surface can select the `parser_id` (check adapter `map.go` docType mapping + parser-configs routes).

## Review Dimensions (per area)

- Error handling & propagation: swallowed exceptions, bare `except`, error-to-HTTP mapping stability (spec: `.trellis/spec/backend/error-handling.md`).
- State machine: task status transitions (pending/running/done/failed/cancelled), partial-failure recovery, retry/requeue semantics, idempotency.
- Resource safety: file handles, temp files, memory bounds on large docs, unbounded loops/recursion on malformed input.
- Tenant isolation: every query scoped by tenant/kb id; no cross-tenant leakage via id-only lookups.
- Contract consistency: Go adapter DTO ↔ runtime JSON ↔ OpenAPI docs three-way match.
- Concurrency: shared state in worker (trio/asyncio), Redis queue ack semantics, double-processing.

## Fix Policy

| Finding class | Action |
|---|---|
| P0 (data loss, crash on normal input, tenant leakage, task stuck forever) | Fix on PR branch + regression test |
| P1 (wrong result on edge input, contract mismatch, silent failure) | Fix on PR branch + test or validation command |
| P2 (code smell, perf observation, vendor-wide pattern) | Report only (`review-report.md`) |
| Any fix that would expand upstream diff beyond necessity | Report only, flag as upstream-refresh risk |

Vendor-code fixes must be minimal-diff: patch the defect, no restyling, no refactor-while-here.

## Execution Structure

Multi-agent review (dispatch protocol): parallel `Explore`/review agents per area (R1 worker, R2 parsers/chunkers, R3 retrieval API, R4 Go adapter), findings adversarially verified before fixing, then `trellis-implement` for fixes and `trellis-check` for quality gate. Every dispatch prompt starts with `Active task: .trellis/tasks/07-03-review-optimize-knowledge-rag`.

## Data Flow / Contracts Affected

- Fixes may touch: runtime Python (worker + api services), Go adapter/vendorclient, OpenAPI YAML (`services/knowledge/api/openapi.yaml`, `docs/services/knowledge/api/*.yaml`, `docs/services/gateway/api/public.openapi.yaml` if gateway-visible).
- Contract-affecting fixes require three-way sync (Go DTO, runtime response, OpenAPI) in the same commit.

## Compatibility & Rollback

- All work on `L1nggTeam/feat/ragflow-runtime-vendor`; PR #536 is open — fixes land as additional commits, each independently revertable.
- No DB migrations expected; if a finding requires one, it goes to report (product decision) instead of inline fix.
- Rollback unit = commit; group by area (worker / parser / retrieval / adapter) so a bad fix reverts cleanly.

## Trade-offs

- Executed-path scope (chosen) vs whole-vendor audit: accepts that dormant vendor bugs stay unfound; keeps upstream refresh viable and review depth high where it matters.
- Fix-inline (chosen for P0/P1) vs report-everything: faster convergence on an already-open PR, at the cost of PR #536 growing; mitigated by per-area commits.

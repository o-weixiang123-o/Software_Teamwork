# Implement — Review and optimize knowledge module (document parsing & RAG)

## Preconditions

- [ ] Branch = `L1nggTeam/feat/ragflow-runtime-vendor`, clean tree, synced with `origin`.
- [ ] `task.py start` executed (status in_progress).

## Stage A — Reachability map (foundation for all review areas)

- [ ] A1. From `services/knowledge/internal/adapter/server.go` routes + `map.go` docType/chunkStrategy mapping, list which runtime `/api/v1/*` endpoints and which `parser_id` values are product-reachable.
- [ ] A2. From `rag/svr/task_executor.py` task types (chunk/raptor/graphrag/mindmap…), list which worker paths can be enqueued via the reachable API.
- [ ] A3. Persist result to `research/reachability.md` (in-scope file list per review area R1–R4).

## Stage B — Parallel review (read-only, findings with file:line + severity)

- [ ] B1. R1 worker pipeline review → `research/findings-worker.md`
- [ ] B2. R2 parser/chunker review (reachable formats only) → `research/findings-parsing.md`
- [ ] B3. R3 retrieval API review → `research/findings-retrieval.md`
- [ ] B4. R4 Go adapter contract review (incl. three-way OpenAPI check) → `research/findings-adapter.md`
- [ ] B5. Adversarial verification pass: each P0/P1 finding independently confirmed against code before entering fix list; rejected findings recorded with reason.

## Stage C — Fixes (per-area commits, minimal diff)

- [ ] C1. Fix confirmed P0/P1 in worker pipeline + regression tests (`test/` under knowledge-runtime).
- [ ] C2. Fix confirmed P0/P1 in parsers/chunkers + tests.
- [ ] C3. Fix confirmed P0/P1 in retrieval API + tests.
- [ ] C4. Fix confirmed P0/P1 in Go adapter + tests; sync OpenAPI files in same commit when contract-affecting.
- [ ] C5. Write `review-report.md` in task dir: all findings (fixed + report-only), severity, anchors, fix commit refs.

## Stage D — Validation gates

- [ ] D1. Go: `cd services/knowledge && go build ./... && go vet ./... && go test ./...`
- [ ] D2. Python: `cd services/knowledge-runtime && uv run python run_tests.py` (or documented pytest subset if full suite needs live infra; record what ran vs skipped).
- [ ] D3. `python -m compileall` on touched Python files.
- [ ] D4. Diff audit: `git diff develop...HEAD --stat` — confirm no out-of-scope vendor files modified (AC5).
- [ ] D5. `trellis-check` quality pass (spec compliance: error-handling, api-contracts, logging).

## Stage E — Wrap-up

- [ ] E1. Update spec (`trellis-update-spec`) if review surfaced durable conventions worth capturing.
- [ ] E2. Commit(s) pushed to PR branch; PR #536 description updated with review summary.
- [ ] E3. Journal entry + task archive.

## Risky Files / Rollback Points

- `rag/svr/task_executor.py` — worker core; any fix here needs the empty-chunk/sentinel regression tests from #536 still green.
- `internal/adapter/map.go` — contract mapping; breaking it breaks all knowledge routes → run adapter contract_test.go after every change.
- Rollback unit = per-area commit (C1–C4 are separate commits).

## Validation Commands (quick reference)

```bash
cd services/knowledge && go build ./... && go vet ./... && go test ./...
cd services/knowledge-runtime && uv sync --python 3.13 --frozen && uv run python run_tests.py
git diff develop...HEAD --stat
```

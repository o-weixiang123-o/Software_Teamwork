# Orchestrator Verification Log (Stage B5)

Adversarial verification of P0/P1 findings by the orchestrator before report writing.

## Verified — R1 P0: dataset-scope tasks always dropped (`get_task` ignores `doc_ids`)

**Verdict: CONFIRMED (P0).** Evidence chain personally checked 2026-07-03:
- `api/db/services/task_service.py:82` — `get_task(cls, task_id, doc_ids=[])` accepts param, body never references it; join fixed at `doc_id = cls.model.doc_id` (line 96) → INNER JOIN `Document.id`.
- `rag/svr/task_executor.py:226-232` — sentinel branch calls `TaskService.get_task(msg["id"], msg["doc_ids"])`, expecting substitution.
- `api/db/services/document_service.py:1061-1103` — `queue_raptor_o_graphrag_tasks` inserts Task with `doc_id="graph_raptor_x"`; grep confirms no Document row ever seeded with sentinel id.
- Result: sentinel task → join empty → `None` → collect() acks+drops as "unknown"; Task row stays progress=0 → `dataset_api_service.py:487-493` blocks retries forever.

## Verified — R1 P1 / R2 P0 (same root cause): `queue_tasks` zero-task → doc stuck RUNNING

**Verdict: CONFIRMED (treat as P1, high user impact).** Independently found by two agents (R1 as P1, R2 as P0). Orchestrator read `task_service.py:400-419` during Stage A: `pages=None→0`, `e=min(e-1,0)=0`, `range(0,0,…)` empty → no tasks, no queue message; `begin2parse` still flips doc to RUNNING. Severity call: P1 (stuck document + no error surface, but no data loss/corruption; recoverable via re-parse API which wipes task rows).

## Verified with provenance CORRECTION — R2 P1: book chunker drops tables for PDFs

**Verdict: bug CONFIRMED; provenance claim WRONG.**
- Bug real: `rag/app/book.py:107` PDF branch returns into `tables`; `book.py:176` tokenizes `tbls` which stays `[]` for the PDF branch → all tables/figures silently dropped for book-chunker PDFs.
- R2 claimed "project-introduced regression" — WRONG. Fetched upstream `rag/app/book.py` at import commit `45fc7fea`: line-for-line identical (`tables`/`tbls` mismatch present upstream). Also `git log --follow` shows no project modification since vendor snapshot (dbf5fb98).
- Classification for report: upstream-inherited defect on executed path; fix locally (1-line: pass `tables` into `tokenize_table` merge or `tbls = tables`), consider reporting upstream.

## Cross-checks pending (before report)

- R3/R4 overlap: runtime HTTP-200-with-body-code vs adapter 401/403/404-only mapping — merge into one contract finding after R4 lands.
- R2 P1s to spot-check during report writing: VisionFigureParser list→str mutation (table.py join), layout-failure→"No chunk built" prog=1.0 masking, markdown vision TypeError, by_paddleocr dead callback(-1) branch.

## Verified with severity DOWNGRADE — R3 P1: multi-tenant `break` drops other tenants' indexes

**Verdict: bug real in code, but NOT reachable in this deployment → downgrade to P2 (latent).**
- Code confirmed: `dataset_api_service.py:981-988` and `:1321-1328` — `for tenant in tenants: if …: tenant_ids.append(…); break` — only the first matching joined-tenant's index is searched.
- Reachability checked: tenant membership rows are created ONLY by `gateway_tenant_provisioning.py` / `gateway_tenant_service.py:93` — strictly 1:1 (runtime_id = user_id = tenant_id, OWNER role). No invite/join/add-member route exists in `api/apps/restful_apis/*` (the 'me'/'team' enum is dataset visibility, not membership). Every user therefore has exactly one UserTenant row and the loop is single-iteration.
- Classification: latent defect — becomes P1 the day team membership is wired. Report with explicit activation condition.

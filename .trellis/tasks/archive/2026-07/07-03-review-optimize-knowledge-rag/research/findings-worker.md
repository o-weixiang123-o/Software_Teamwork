# R1 Worker Ingestion Pipeline — Correctness/Robustness Findings

> Reviewed by R1 agent (2026-07-03). Persisted by orchestrator (agent Write was blocked).

**Calibration facts established first (they gate everything):**
- `TE_RUN_MODE` defaults to `"0"` (`task_executor.py:1514`) and is set nowhere in deploy config → **production runs the refactored path** (`TaskManager.run_refactored_task` → `TaskHandler` + `ChunkService`/`EmbeddingService`/`RaptorService`). `do_handle_task` runs only under `TE_RUN_MODE=1` (dry-run) or other explicit values.
- `get_svr_queue_name` hardcodes the `.common` suffix (`settings.py:123`, upstream-identical), so raptor/graphrag/mindmap land on the same queue the `-t common` worker consumes. Delivery works; **collection** is where the P0 bites.
- Upstream baseline: `infiniflow/ragflow@45fc7fea` (per `UPSTREAM.md`), verified against a fresh upstream clone for the diverged functions.

---

## [P0] Dataset-scope (raptor/graphrag/mindmap) tasks are always dropped as "unknown"; KB index slot then locks forever

**Anchor:** `task_executor.py:226-244` (`collect()` sentinel branch); `task_service.py:96,120-129` (`get_task` — `doc_ids` param accepted but **unused**, INNER JOIN `Task.doc_id == Document.id`); `dataset_api_service.py:487-493,512`; `document_service.py:1061-1103`.

**What:** `queue_raptor_o_graphrag_tasks` inserts the Task with `doc_id="graph_raptor_x"` (a sentinel asserted to never be a real doc id; no Document row is ever seeded for it). `collect()` routes all three special task types through `TaskService.get_task(msg["id"], msg["doc_ids"])`. The vendored `get_task` **ignores `doc_ids`** and INNER-JOINs on the sentinel → empty result → returns `None` before any progress/retry update → `collect()` logs "is unknown", increments `FAILED_TASKS`, **acks and drops** the message.

Upstream at the imported commit has the substitution the design depends on:
```python
doc_id = cls.model.doc_id
if doc_id == CANVAS_DEBUG_DOC_ID and doc_ids:   # peewee Expression is always truthy → effectively `if doc_ids:`
    doc_id = doc_ids[0]                          # join Document on the first REAL source doc
```
The vendoring stripped those two lines (canvas removal) but kept the `collect()` call passing `msg["doc_ids"]` — a project regression. Consequences: (1) RAPTOR/GraphRAG/mindmap dataset tasks silently never run, no error surfaced; (2) the Task row stays `progress=0` forever, so `run_index` permanently refuses retries (`"Task … in progress with status 0 … already running"`, `dataset_api_service.py:492`) — one attempt bricks the KB's index slot until manual `DELETE /datasets/{id}/index` or DB surgery. This also makes every raptor checkpoint/cleanup path in the worker dead code.

**Trigger:** `POST /api/v1/datasets/{id}/index?type=raptor|graph|mindmap` on any dataset with docs. 100% reproducible.

**Fix:** Restore the join-key substitution in `get_task`:
```python
doc_id = cls.model.doc_id
if doc_ids:                      # dataset-scope task: resolve KB/tenant via first real source doc
    doc_id = doc_ids[0]
```

---

## [P1] `queue_tasks` can create zero tasks (corrupt PDF / empty table) → document stuck RUNNING forever, no error

**Anchor:** `task_service.py:400-419` (PDF; `pages is None → pages = 0`), `:421-428` (table), `:466-473` (empty bulk-insert + `begin2parse` + nothing queued); `pdf_parser.py:1517-1525` (`total_page_number` swallows and returns `None`); `document_service.py:922-924` (`_sync_progress`: `if not tsks: continue`).

**What:** A PDF whose page count can't be read (corrupt/encrypted/mislabeled) → `pages=0` → `range(0,0,page_size)` yields **no tasks**. `queue_tasks` bulk-inserts `[]`, sets `chunk_num=0`, and `begin2parse` flips the doc to `run=RUNNING, progress≈0.005` with no Task rows and nothing on the queue. `_sync_progress` skips task-less docs, so it sits at "0%" forever; the parse API returned success. Same for the table branch when `row_number` returns 0. This is a stuck-forever state on the **primary adapter parse path** (`POST /datasets/{id}/chunks` → `queue_tasks`) from a merely-corrupt upload. Upstream-identical, but user-visible and easy to hit.

**Trigger:** Upload a corrupt/password-protected PDF (or empty `table` workbook), then parse.

**Fix:** After building `parse_task_array`, if empty, fail the doc instead of RUNNING:
```python
if not parse_task_array:
    DocumentService.update_by_id(doc["id"], {"progress": -1, "run": TaskStatus.FAIL.value,
        "progress_msg": "No parseable pages/rows found (file may be corrupt or empty)."})
    return
```

---

## [P2] `handle_task` finally: unsafe `task["doc_ids"][0]` (project regression) + unguarded log write can skip Redis ack

**Anchor:** `task_executor.py:1554-1563`.

**What:** Upstream guards with `(task.get("doc_ids") or [None])[0]`; vendored indexes `task["doc_ids"][0]` directly → KeyError/IndexError if `doc_ids` ever missing. Worse, **any** exception from `record_pipeline_operation` (transient DB failure) in this `finally` skips `redis_msg.ack()` (line 1563). Since `UNACKED_ITERATOR` is built once at startup with no periodic claim, an unacked message isn't redelivered until **process restart**; on restart, already-completed work re-runs and double-increments `chunk_num/token_num`.

**Trigger:** Latent for the index-arg (producer guarantees non-empty `doc_ids`; P0 keeps these out of `handle_task`); the DB-failure ack-skip is reachable on any parse task during a MySQL hiccup at completion.

**Fix:** Restore `(task.get("doc_ids") or [None])[0]`; wrap the `finally` body in `try/except Exception: logging.exception(...)` so the ack always runs.

---

## [P2] Refactored `_run_raptor` does cleanup + `increment_chunk_num` + "RAPTOR done" even when insert failed

**Anchor:** `task_handler.py:264-292`.

**What:** `do_handle_task` returns early on insert failure (`task_executor.py:1395-1397`), so "remove old summaries after insert" and stats only run on success. `_run_raptor` records `insertion_result` but then **unconditionally** deletes old summaries, calls `increment_chunk_num(len(chunks))`, and emits `prog=1.0` (which recovers a prior `-1`). On a failed insert: good summaries deleted while new ones rolled back, counters inflated, task shown complete. Upstream refactor shares the flaw; realistic trigger is task-row-deleted+cancel (doc/dataset deletion mid-raptor) where `set_progress` swallows `DoesNotExist` and the False path runs. Masked by P0 today. **Fix:** mirror the original early return (`if not insert_result: return`).

---

## [P2] Legacy path: cancelling a RAPTOR task deletes ALL chunks of the first source document

**Anchor:** `task_executor.py:1279-1280` (`task_doc_id = task_doc_ids[0]`) and `:1483-1497` (finally deletes `{"doc_id": task_doc_id}` on cancel).

**What:** The raptor branch reassigns `task_doc_id` to a real source doc; the cancel-cleanup then deletes that document's **entire** content chunks (not raptor summaries) while DB still shows `chunk_num>0, run=DONE`. Retrieval silently loses the doc until re-parse. Upstream-identical, only on non-default `TE_RUN_MODE`, currently unreachable behind P0. **Fix alongside P0:** scope raptor cancel-cleanup to `raptor_kwd` chunks (the refactored `handle_task` uses the sentinel doc_id and avoids this).

---

## [P2] Single-task cancel endpoint tears a multi-task document

**Anchor:** `task_api.py:30-101` (project-added `POST /tasks/{id}/cancel` flags ONE task, sets doc `run=CANCEL`); worker cancel cleanup deletes `{"doc_id": …}` for the whole doc (`task_handler.py:93-110` / `task_executor.py:1483-1497`).

**What:** Doc-level cancels flag all tasks and wipe consistently. The task-level cancel flags one task; its `finally` deletes every chunk of the document while sibling page-range tasks (never flagged) keep running, re-insert later batches, and increment `chunk_num` for earlier batches just wiped. End state: doc `run=CANCEL`, counters out of sync with the index, partial post-wipe chunks. Persists until re-parse.

**Trigger:** Cancel one task of a multi-task PDF (page-split) via `POST /tasks/{id}/cancel` while siblings run.

**Fix:** Flag all tasks of the doc (`cancel_all_task_of`), or gate worker cancel-cleanup on the *document* being cancelled.

---

## [P2] TOC insertion overwrites `Task.chunk_ids` with only the TOC id — breaks reuse bookkeeping and rerun cleanup

**Anchor:** `chunk_service.py:391-399,433-448` / `task_executor.py:1156-1159` (chunk_ids rewritten per `insert_chunks` call); TOC 2nd insert `task_handler.py:460-463` + `post_processor.py:112-147`.

**What:** `insert_chunks` rewrites `Task.chunk_ids` with the current call's ids. The TOC chunk is a second `insert_chunks(…, [toc_chunk])`, so the task ends up recording only the TOC id. `queue_tasks` uses `chunk_ids` to delete superseded chunks and compute reuse — after a TOC run, a rerun-without-delete leaves all old main chunks undeleted (stale/dup if config changed) and reuse reports 1. Immune on the adapter path (`chunk_api.parse` wipes doc + Task rows first); affects `document_api` run with `delete=False` + `toc_extraction`. Upstream-identical. **Fix:** append rather than overwrite for the TOC call, or exclude the TOC insert from `update_chunk_ids`.

---

## [P2] stop_parsing + rerun-without-delete resurrects phantom chunks via digest reuse

**Anchor:** `chunk_api.py:255-272` (deletes all doc chunks, keeps Task rows/`chunk_ids`); `task_service.py:451-473` + `reuse_prev_task_chunks:476-520`.

**What:** `stop_parsing` deletes every chunk and zeroes counters, but completed page-tasks keep `progress=1.0` and their `chunk_ids`. A later `document_api` run with `delete=False` (same digest) reuses them: task marked done, `chunk_ids` pointing at nonexistent chunks, `chunk_num>0` — doc looks parsed but retrieval is empty. Adapter path immune; needs mixed-endpoint usage. Adjacent: counters only reconciled when `rerun_with_delete && run==DONE` (`document_api.py:1418-1420`). **Fix:** have stop_parsing clear `Task.chunk_ids` (or delete Task rows) when it deletes chunks.

---

## [P2] All `@timeout` decorators are no-ops unless `ENABLE_TIMEOUT_ASSERTION` is set

**Anchor:** `connection_utils.py:53-56` (sync `.get()` no timeout), `:69-75` (async plain `await`); consumers `do_handle_task`/`handle` (3h), `build_chunks` (80m), `batch_encode` (60s), `run_raptor_for_kb` (1h).

**What:** The env var is set nowhere in deploy config. A hung chunker/LLM/embedding/storage call never times out; the task holds one of `MAX_CONCURRENT_TASKS=5` slots forever (doc RUNNING, progress frozen); 5 hangs deadlock the worker with a live heartbeat. Upstream-identical gating → observation, but deployment should set `ENABLE_TIMEOUT_ASSERTION=1` for workers or accept the risk knowingly.

---

## [P2] Refactored `_build_toc`: `int(chunk_val or -1)` mis-maps a TOC entry with integer `chunk_id` 0

**Anchor:** `task_handler.py:525-541` (vs correct `int(chunk_val)` in legacy `build_TOC`, `task_executor.py:628`).

**What:** TOC entries come from LLM JSON with integer indices (`gen_toc_from_text` feeds `{idx: text}`). If the model returns `"chunk_id": 0` as a number, `0 or -1 → -1 → docs[-1]`: the first TOC entry anchors to the last chunk and range-fill misbehaves. String `"0"` is fine. Executed (refactored) path; upstream shares it. Silent wrong anchors only with `toc_extraction`. **Fix:** `curr_idx = int(chunk_val)` (the None/empty guard above already covers missing).

---

## [P2] Worker main loop retains every finished task; unacked messages recover only at restart

**Anchor:** `task_executor.py:1704-1716` (`tasks.append(t)`, pruned only at shutdown); `:204-215` (UNACKED_ITERATOR built once, startup-only PEL sweep; no XAUTOCLAIM in `redis_conn.py`).

**What:** Two upstream-identical robustness gaps: (1) `tasks` grows one `asyncio.Task` per handled message and per idle 5s poll per slot, never pruned — slow unbounded memory growth; stored exceptions (finally-block failures) surface only at shutdown. (2) Pending-but-unacked messages are re-consumed only at process start — an ack-skip parks a task until restart. A periodic `get_unacked_iterator` sweep and `tasks[:] = [t for t in tasks if not t.done()]` would close both.

---

## [P2] Legacy-mode metadata aggregation crashes on chunks without `metadata_obj`

**Anchor:** `task_executor.py:516-519` (`update_metadata_to(metadata, doc["metadata_obj"]); del doc["metadata_obj"]`, unguarded).

**What:** If `gen_metadata` returns falsy for any chunk (`if cached:` never sets the key), the loop raises `KeyError: 'metadata_obj'` and the whole task fails after chunking/embedding succeeded. The refactored path already guards with `if "metadata_obj" in doc:` (`chunk_post_processor.py:216`), so this only bites `TE_RUN_MODE=1` / explicit legacy with `enable_metadata`. **Fix:** copy the guard into `build_chunks`.

---

## [P2] Oversized file ends DONE with zero chunks (error → success recovery)

**Anchor:** size check `chunk_service.py:110-115` / `task_executor.py:257-259` (prog −1, return `[]`); follow-up `task_handler.py:403-405` / `task_executor.py:1361-1363` (`progress_cb(1., "No chunk built …")`); ratchet `task_service.py:344-348`; FAIL re-sync `document_service.py:583-585`.

**What:** A `size > DOC_MAXIMUM_SIZE` task writes `-1` (doc FAIL) then immediately `1.0` ("No chunk built"), which the `prog>=1` rule accepts; `_sync_progress` then flips the FAILed doc to DONE with 0 chunks. Normally shadowed by the HTTP upload limit (`MAX_CONTENT_LENGTH == DOC_MAXIMUM_SIZE`). **Fix:** the size branch should return without the later `1.0` write, or mark FAIL and `raise`.

---

## Clean areas (checked, no finding)

- **Tenant isolation**: every worker doc-engine call (`insert/delete/search/chunk_list`, raptor lookups, `queue_tasks` cleanup) is scoped by `search.index_name(tenant_id)` + `kb_id` from the task's DB join. No unscoped query. LLM/tag Redis caches are content-/kb-keyed (vendor design; hit requires already possessing identical content).
- **Chunk-id consistency**: ids are deterministic `xxh64(content+doc_id)`, so at-least-once redelivery and re-parses overwrite rather than duplicate; `chunk_count` dedupes via `set()` consistently; `Task.chunk_ids` is `LongTextField` (no 64KB truncation).
- **Cancel/rollback in insert path**: partial RAPTOR rollback (`_rollback_raptor_chunks`), per-task doc-chunk cleanup, and `set_progress` re-checking the cancel flag (converting late `1.0`→`-1`+exception) close the cancel-vs-complete race on the normal path.
- **Limiters/concurrency**: `LoopLocalSemaphore` binds per-loop (TOC thread's `asyncio.run` gets its own instances); `task_limiter` acquire/release pairing sound; `CURRENT_TASKS`/counters mutated only on the event loop; shared `UNACKED_ITERATOR` generator never re-entered.
- **Refactored empty-chunk filtering** (project change): filter and text-prep use identical predicates, `docs[:]` mutation keeps the insert list consistent, `attach_vectors` hard-fails on length mismatch — no vector/chunk drift.
- **Digest computation**: includes `content_hash`, `embd_id`, page bounds; raptor/graphrag keys stripped deterministically each run; reused ids excluded from the pre-delete list.
- **Dry-run-only modules** (`comparator.py`, `report_generator.py`, `write_operation_interceptor.py`, `RecordingContext`): off the `TE_RUN_MODE=0` path; `NullRecordingContext` is allocation-free; contextvar always set before the `finally` that reads it.

---

**Summary counts — P0: 1, P1: 1, P2: 10.**

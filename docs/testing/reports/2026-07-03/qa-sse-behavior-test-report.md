# QA SSE Behavior Verification Test Report

## 0. Basic Information

| Item | Record |
| --- | --- |
| Report date | `2026-07-03` |
| Test task / Issue | `T-013` / `#498` |
| Owner | `@Jackeyliu37` |
| Scope | QA SSE stream, QA internal replay API, QA public payload redaction, local service logs |
| Tested branch | `Test/test/qa-sse-behavior-verification` |
| Runtime code under test | `801ff74cceb694ac07d26cacb70521b35f44e6e5` |
| Report / evidence commit | `4667cfb5` |
| Base branch | `upstream/develop @ 801ff74cceb694ac07d26cacb70521b35f44e6e5` |
| Environment | Local Windows host, Docker infrastructure, host-run backend services |
| Conclusion | Test failed and was transferred to follow-up issues `#604` and `#605`; Knowledge tool success path is environment-blocked |

## 1. Test Goals

- Verify the real QA SSE path with Gateway/Auth/QA/AI Gateway running locally instead of relying only on fake-backed unit tests.
- Verify event headers, event names, event ordering, replay behavior, error envelopes, disconnect behavior, and redaction of committed public payloads.
- Record environment blockers and transfer product/backend defects to owner issues instead of fixing broad runtime behavior in this test task.

Out of scope:

- Frontend UI rendering.
- Model answer quality and retrieval ranking quality.
- Large behavior fixes in QA, AI Gateway, Gateway, or Knowledge.

## 2. Test Basis

| Type | Link or file | Usage |
| --- | --- | --- |
| Source issue | `#498` | Required scope, acceptance criteria, and reporting rules |
| QA docs | `docs/services/qa/README.md` | SSE event semantics and safety rules |
| QA implementation docs | `docs/services/qa/docs/implementation.md` | Implemented QA behavior and verification guidance |
| Testing strategy | `docs/testing/strategy.md` | Report and evidence expectations |
| Report template | `docs/testing/templates/test-report-template.md` | Report structure |
| Local integration runbook | `docs/runbooks/local-integration.md` | Local service startup and readiness expectations |

## 3. Scope And Preconditions

Tested scope:

- `POST /internal/v1/qa-sessions/{sessionId}/messages` with `Accept: text/event-stream`.
- `GET /internal/v1/qa-sessions/{sessionId}/events?responseRunId=...`.
- `GET /internal/v1/response-runs/{responseRunId}`.
- Invalid session and empty message error paths.
- Raw SSE and selected service-log redaction scans.

Preconditions observed:

- Gateway and QA were ready.
- AI Gateway was degraded because embedding and rerank profiles were missing, but `chat_profile` was ok and the chat path produced model output.
- Knowledge was degraded because the vendor runtime was unavailable, so successful `tool.completed` and `citation.delta` verification was blocked.
- PostgreSQL, Redis, Elasticsearch, and MinIO containers were healthy.

## 4. Test Case Matrix

| ID | Category | Case / Scenario | Expected result | Actual result | Conclusion |
| --- | --- | --- | --- | --- | --- |
| TEST-001 | SSE connectivity | Ask stream returns SSE headers and remains open until terminal event | HTTP 200, `text/event-stream`, no buffering, terminal `answer.completed` | HTTP 200 with `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`; terminal event observed | pass |
| TEST-002 | Event contract | Record documented event types and unknown event types | Expected event types appear; no unknown events | `message.created`, `agent.iteration.started`, `reasoning.step`, `tool.started`, `tool.failed`, `heartbeat`, `answer.delta`, `answer.completed` observed; no unknown events | partial |
| TEST-003 | Event ordering | Persisted non-heartbeat ids strictly increase | `id:` values strictly increasing | Non-heartbeat ids `1..20` strictly increasing | pass |
| TEST-004 | Answer streaming | `answer.delta` is incremental, not one final chunk | Multiple deltas over time | Only one `answer.delta` immediately before `answer.completed` | fail, transferred to `#604` |
| TEST-005 | Knowledge tool behavior | `tool.started` / `tool.completed` pair by `toolCallId` with safe summaries | Started/completed pairs and safe public summaries | Two `tool.started` events and two sanitized `tool.failed` events; no `tool.completed` because Knowledge runtime was unavailable | blocked |
| TEST-006 | Citation behavior | `citation.delta` appears when Knowledge retrieval succeeds | Citation events appear for retrieval hits | No citation event; blocked by Knowledge runtime unavailable | blocked |
| TEST-007 | Replay | Replay returns persisted non-heartbeat events for `responseRunId` | Replay matches original non-heartbeat sequence | Replay returned eventSeq `1..20` for run `7096279d-0729-4938-8a38-d7dab9d3643a` | pass |
| TEST-008 | Validation error | Invalid session id returns stable 404 | HTTP 404 `not_found`, no panic | HTTP 404 with `not_found` envelope | pass |
| TEST-009 | Validation error | Empty message returns stable validation error | HTTP 400 `validation_error` | HTTP 400 with field error for message length | pass |
| TEST-010 | Disconnect | Client abort marks run cancelled | Run status becomes `cancelled` | After `curl --max-time 2`, run `5d19ee38-0e6d-4eb0-9d92-3594b9350a7d` became `completed` | fail, transferred to `#605` |
| TEST-011 | Redaction | Raw SSE/replay/run/tool payloads contain no secrets or raw internals | No API key, auth credential, DB URL, object key, raw prompt, or provider body | Committed evidence had no secret/token hits; log scan had four uncommitted local internal URL diagnostic hits in Knowledge log | pass with noted log advisory |
| TEST-012 | Logs | QA logs show no panic or unexpected error leak | No panic, unexpected `ERROR`, raw prompt, or API key | QA log scan found no `panic`, `ERROR`, or `retrieval_failed` lines for this run | pass |

## 5. Commands And Results

| Time | ID | Command or operation | Result | Evidence / Notes |
| --- | --- | --- | --- | --- |
| 2026-07-03 23:16 +0800 | TEST-001 | `curl --noproxy '*' -D .local/issue498/qa-sse-headers.txt -N -H 'Accept: text/event-stream' .../messages` | pass | `qa-sse-headers.txt`, `qa-sse-raw-knowledge.sse` |
| 2026-07-03 23:17 +0800 | TEST-002/003/004/005 | `python .local/issue498/analyze_sse.py` | partial/fail/blocked | `qa-sse-analysis.json` |
| 2026-07-03 23:17 +0800 | TEST-007 | `curl --noproxy '*' .../events?responseRunId=7096279d-0729-4938-8a38-d7dab9d3643a` | pass | `qa-sse-replay.json` |
| 2026-07-03 23:17 +0800 | TEST-005 | `curl --noproxy '*' .../response-runs/7096279d-0729-4938-8a38-d7dab9d3643a/tool-calls` | blocked | `qa-sse-tool-calls.json`; Knowledge retrieval failed safely |
| 2026-07-03 23:19 +0800 | TEST-008 | `curl --noproxy '*' -i .../qa-sessions/00000000-0000-0000-0000-000000000000/messages` | pass | `qa-invalid-session-http.txt` |
| 2026-07-03 23:19 +0800 | TEST-009 | `curl --noproxy '*' -i .../messages` with empty message JSON | pass | `qa-empty-message-http.txt` |
| 2026-07-03 23:19 +0800 | TEST-010 | `curl --noproxy '*' --max-time 2 -N .../messages` then query response run after 20 seconds | fail | `qa-sse-abort-partial.sse`, `qa-sse-abort-run.json`; transferred to `#605` |
| 2026-07-03 23:25 +0800 | TEST-011 | Redaction scan over committed evidence plus selected `.local/logs/*.log` | pass with advisory | `qa-sse-redaction-scan.txt`; committed artifacts had no token/secret hit |
| 2026-07-03 23:25 +0800 | TEST-012 | `Select-String .local/logs/qa.log -Pattern 'panic|ERROR|error|retrieval_failed'` | pass | No matching QA log lines |

Not run or blocked:

| Test item | Reason | Missing environment | Residual risk | Follow-up |
| --- | --- | --- | --- | --- |
| Successful `tool.completed` pair | Knowledge retrieval failed | Knowledge vendor runtime unavailable | Successful tool result payload and pairing still unverified in real environment | Re-run #498-style verification after Knowledge runtime is ready |
| `citation.delta` success path | Knowledge retrieval failed | Knowledge vendor runtime unavailable | Citation payload redaction still unverified in real environment | Re-run after retrieval succeeds |
| Embedding/rerank profile readiness | AI Gateway readyz degraded | Embedding and rerank profiles missing | Retrieval/rerank integrated path not fully covered | Environment configuration follow-up, not a QA SSE behavior defect |

## 6. Defects And Handling

| Issue | Severity | Handling | Link | Notes |
| --- | --- | --- | --- | --- |
| `answer.delta` is emitted once as a final chunk | Major behavior defect | Transferred to issue | `#604` | Users do not receive meaningful incremental answer text |
| Client disconnect does not cancel response run | Major behavior defect | Transferred to issue | `#605` | Run continued and completed after the SSE client timed out |
| Knowledge runtime unavailable | Environment blocker | Recorded as blocked | `#498` report | Blocks successful tool and citation verification |
| Knowledge log contains local internal URL diagnostics | Low local-log advisory | Not committed as raw log; classified in report | `qa-sse-redaction-scan.txt` | Committed SSE/replay/run payloads did not contain tokens/secrets |

## 7. Evidence List

| Evidence type | Location | Notes |
| --- | --- | --- |
| Environment snapshot | `docs/testing/reports/2026-07-03/qa-sse-environment.txt` | Tooling, readiness, container state, run ids |
| SSE headers | `docs/testing/reports/2026-07-03/qa-sse-headers.txt` | HTTP 200 SSE headers |
| Raw SSE | `docs/testing/reports/2026-07-03/qa-sse-raw-knowledge.sse` | Successful terminal stream with sanitized failed tool calls |
| Parsed analysis | `docs/testing/reports/2026-07-03/qa-sse-analysis.json` | Event counts, ids, unknown events, delta count |
| Replay response | `docs/testing/reports/2026-07-03/qa-sse-replay.json` | Persisted event replay for the completed run |
| Run response | `docs/testing/reports/2026-07-03/qa-sse-run.json` | Completed run metadata |
| Tool calls | `docs/testing/reports/2026-07-03/qa-sse-tool-calls.json` | Sanitized failed Knowledge tool summaries |
| Invalid session | `docs/testing/reports/2026-07-03/qa-invalid-session-http.txt` | HTTP 404 `not_found` envelope |
| Empty message | `docs/testing/reports/2026-07-03/qa-empty-message-http.txt` | HTTP 400 `validation_error` envelope |
| Abort partial stream | `docs/testing/reports/2026-07-03/qa-sse-abort-partial.sse` | Partial stream before client timeout |
| Abort run query | `docs/testing/reports/2026-07-03/qa-sse-abort-run.json` | Run persisted as completed after abort |
| Redaction scan | `docs/testing/reports/2026-07-03/qa-sse-redaction-scan.txt` | Evidence and selected log scan summary |

## 8. Risks And Remaining Gaps

- Successful Knowledge tool completion and citation events are not proven because the local Knowledge runtime was unavailable.
- AI Gateway chat path was usable, but embedding and rerank readiness remained degraded.
- The disconnect behavior is not safe for long-running QA streams until `#605` is fixed.
- The user-visible streaming experience is not truly incremental until `#604` is fixed.

## 9. Final Conclusion

Test failed and was transferred to follow-up issues. The real environment proved SSE connectivity, event ordering, replay, stable validation errors, and committed payload redaction. It also found two major QA SSE defects: non-incremental `answer.delta` streaming (`#604`) and missing cancellation on client disconnect (`#605`). The Knowledge success path remains environment-blocked.

## 10. Review Checklist

- [x] Tests were actually run in a real local environment, not only documented as a checklist.
- [x] Commands, environment, results, and failure evidence were recorded.
- [x] Major issues were separated from the test task and transferred to owner issues.
- [x] Environment-blocked items include missing dependency and residual risk.
- [x] The report and evidence files are linked for the issue and PR.

# Implementation Plan: QA SSE Streaming And Disconnect Cancellation

## Phase 1: TDD For Disconnect Cancellation

- [ ] Update the old service regression test that expected request cancellation not to stop the model run. It should now assert that cancelling the request context cancels the runner/model context.
- [ ] Run:

```bash
cd services/qa
go test ./internal/service -run '^TestAskCancelsModelRunWhenRequestContextIsCancelled$' -count=1
```

Expected red result before implementation: the runner does not observe cancellation or the finalization status is not `cancelled`.

- [ ] Change `QAService.Ask` so the run execution context derives from the request context, while cleanup contexts remain cancellation-independent.
- [ ] Re-run the targeted cancellation tests and existing explicit cancellation tests:

```bash
cd services/qa
go test ./internal/service -run 'TestAskCancelsModelRunWhenRequestContextIsCancelled|TestCancelActiveRunCancelsAgentAndPersistsCancelledMessage|TestAskFinalizesSuccessfulRunAfterRequestContextCancelled|TestAskPersistsCompletedInvocationAfterRequestContextCancelled' -count=1
```

## Phase 2: TDD For Provider Answer Deltas

- [ ] Add a model client test proving streamed `delta.content` invokes an answer-delta observer in order while the final accumulated message still matches the concatenation.
- [ ] Run:

```bash
cd services/qa
go test ./internal/platform/modelclient -run '^TestCompleteEmitsStreamedAnswerDeltas$' -count=1
```

Expected red result before implementation: the answer observer API/event hook is missing or not called.

- [ ] Add a service test with a fake agent runner that emits multiple answer delta events before returning the final answer. Assert `answer.delta` indexes are stable, concatenation equals the final answer, and no duplicate full-answer delta is emitted.
- [ ] Run:

```bash
cd services/qa
go test ./internal/service -run '^TestAskStreamsAnswerDeltasFromAgentEvents$' -count=1
```

Expected red result before implementation: no streamed answer-delta event path exists.

- [ ] Implement answer observer support in `internal/service/agent/types.go`, model client stream parsing, agent runner event projection, and QA service emission/fallback logic.
- [ ] Re-run targeted streaming tests plus existing reasoning and SSE safety tests:

```bash
cd services/qa
go test ./internal/platform/modelclient -run 'TestCompleteEmitsStreamedAnswerDeltas|TestCompleteParsesStreamedReasoningDeltas|TestCompleteRejectsInterruptedStreamWithPartialDelta' -count=1
go test ./internal/service -run 'TestAskStreamsAnswerDeltasFromAgentEvents|TestAskEmitsReasoningDeltaBeforeAnswerDelta|TestAskSSEPayloadsDoNotLeakPromptRawToolOrProviderSecrets|TestAskPersistsReplayRecordsForSSEEvents' -count=1
```

## Phase 3: Documentation And Full Verification

- [ ] Update QA README and Trellis MCP agent runtime spec to describe streaming answer deltas plus final-delta fallback.
- [ ] Run full QA checks:

```bash
cd services/qa
go test ./...
go build ./cmd/server
go build ./cmd/agent
```

- [ ] Run repository whitespace check:

```bash
git diff --check
```

- [ ] Review diff for sensitive data leaks, docs/code drift, and unrelated changes.
- [ ] Commit with Conventional Commit message and close both issues.


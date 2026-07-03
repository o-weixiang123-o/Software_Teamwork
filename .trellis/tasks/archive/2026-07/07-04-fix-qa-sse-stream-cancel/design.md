# Design: QA SSE Streaming And Disconnect Cancellation

## Scope And Boundaries

This change stays inside `services/qa` plus QA documentation/spec sync. Gateway and frontend contracts remain unchanged because the public event names and payload shape are already documented. QA remains the owner of conversation, message, run, replay, tool-call summary, and citation state.

## Root Cause

### #605 Disconnect Cancellation

The HTTP handler passes `r.Context()` into `QAService.Ask`, but `Ask` currently creates its agent execution context from `context.WithoutCancel(ctx)`. This intentionally protects cleanup from request cancellation, but it also protects the long-running model/tool execution. As a result, SSE client disconnects do not cancel the model call and the run can later complete.

### #604 Answer Delta Streaming

The model client already supports event-stream response decoding and accumulates `choice.delta.content`. It only exposes reasoning deltas to the agent layer. The service therefore cannot emit answer chunks while the provider streams them, and instead emits one final `answer.delta` after the runner returns the complete assistant message.

## Target Data Flow

```text
HTTP request context
  -> QAService.Ask run context
  -> agent.Runner
  -> modelclient OpenAI-compatible stream parser
  -> answer delta observer
  -> agent.EventAnswerDelta
  -> QA ProgressEvent answer.delta + response_stream_events
  -> final assistant message/content block + answer.completed
```

Cancellation flow:

```text
SSE client disconnect
  -> net/http cancels request context
  -> QA run context cancels
  -> model/tool calls return context cancellation
  -> classifyRunError maps to cancelled
  -> cleanup uses context.WithoutCancel(ctx) with timeout
  -> response_run and assistant message persist as cancelled
```

## Context Strategy

- Execution contexts derive from the request context so HTTP disconnects cancel model/tool work.
- Cleanup/persistence contexts continue to use `context.WithoutCancel(ctx)` plus bounded timeouts for finalization, model invocation summaries, replay events, and similar durable state.
- The existing active-run cancellation registry remains the path for explicit `CancelResponseRun`.

## Streaming Strategy

- Add an answer-delta observer alongside the existing reasoning-delta observer in `internal/service/agent`.
- `modelclient.decodeStreamCompletion` calls the answer observer whenever an SSE chunk contains non-empty `delta.content`, while still accumulating the final message content.
- `agent.Runner` converts observed answer deltas into a new internal event type, `EventAnswerDelta`.
- `QAService.Ask` emits and persists `answer.delta` events with monotonic `index` values as it receives `EventAnswerDelta`.
- If no streamed answer delta was observed, `QAService.Ask` keeps the existing fallback final `answer.delta`.
- If streamed answer text is a strict prefix of the final assistant answer, the service emits the missing suffix before `answer.completed`. This protects against provider/client edge cases without duplicating the whole final answer.
- If streamed text differs from the final answer in a non-prefix way, the final persisted assistant message remains authoritative and the service avoids emitting a duplicate corrective full answer. Tests cover the normal prefix/equality path required by #604.

## Safety

- Answer deltas are final assistant content text only. They do not include prompts, raw tool arguments/results, provider raw errors, internal URLs, secrets, or full document text.
- Reasoning deltas keep their existing separate observer and event semantics.
- Tool-call planning events and sanitized tool summaries keep the existing code path.
- Public SSE event names and payload field names stay compatible.

## Compatibility

- Non-streaming model responses keep current behavior: one final `answer.delta` followed by `answer.completed`.
- Existing replay APIs continue reading `response_stream_events`; only the number of persisted `answer.delta` events changes when provider streaming is active.
- Explicit run cancellation remains implemented through `ResourceService.CancelResponseRun` and the active-run canceller.

## Docs And Spec Updates

Update QA README and `.trellis/spec/backend/mcp-agent-runtime.md` text that currently says model output is one final delta. The new description should distinguish streaming provider paths from non-streaming fallback behavior.


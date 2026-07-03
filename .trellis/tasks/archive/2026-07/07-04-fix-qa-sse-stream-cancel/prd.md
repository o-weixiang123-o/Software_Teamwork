# Fix QA SSE Streaming And Disconnect Cancellation

## Goal

Complete GitHub issues #604 and #605 so QA SSE answers stream real final-answer deltas when the provider path supports streaming, and so a client disconnect cancels the underlying response run instead of allowing it to complete in the background.

## User Value

Users should see answer text arrive incrementally during long QA generations, and navigating away or aborting an SSE request should stop model/tool work and free backend resources.

## Background And Evidence

- #604 / B-021 reports that response run `7096279d-0729-4938-8a38-d7dab9d3643a` emitted only one `answer.delta`, immediately before `answer.completed`, in `docs/testing/reports/2026-07-03/qa-sse-behavior-test-report.md`.
- #605 / B-022 reports that aborting an SSE request with `curl --max-time 2` left response run `5d19ee38-0e6d-4eb0-9d92-3594b9350a7d` completed after 20 seconds instead of cancelled.
- `docs/services/qa/README.md` defines `answer.delta` as final answer generated text increments and requires public SSE payloads to avoid prompts, raw tool arguments/results, provider raw errors, internal URLs, secrets, and full document text.
- `.trellis/spec/backend/mcp-agent-runtime.md` requires `Client disconnect | Cancel Agent context; bounded cleanup marks the message cancelled/failed`.
- Current code evidence:
  - `services/qa/internal/service/qa.go` derives the agent execution context from `context.WithoutCancel(ctx)`, which strips HTTP/SSE request cancellation.
  - `services/qa/internal/service/qa.go` emits one final `answer.delta` after successful agent completion.
  - `services/qa/internal/platform/modelclient/openai.go` already parses streamed content and reasoning deltas, but only exposes reasoning deltas to the agent/service layer.

## Requirements

### R1: Provider Content Deltas Become QA `answer.delta`

When the AI Gateway/OpenAI-compatible response path uses stream responses and emits content deltas, QA must project those final answer text deltas into ordered `answer.delta` SSE events.

- Preserve existing event names: `message.created`, `agent.iteration.started`, `reasoning.step`, `reasoning.delta`, `tool.started`, `tool.completed`, `tool.failed`, `citation.delta`, `answer.delta`, `answer.completed`, `error`, and `heartbeat`.
- Preserve replay persistence in `response_stream_events`; non-heartbeat replay events remain strictly increasing.
- Preserve non-streaming compatibility: if no provider answer deltas are observed, QA may emit one final `answer.delta` containing the final answer, matching previous behavior.
- Avoid duplicate final full-answer deltas when streaming deltas were already emitted.
- The concatenation of streamed `answer.delta.text` values must match the final assistant answer for normal streaming responses.

### R2: SSE Client Disconnect Cancels The Response Run

When the HTTP request context is cancelled after the response run has been created, QA must cancel the agent/model/tool execution context and finalize the run as `cancelled`.

- Bounded persistence cleanup must still run with cancellation-independent contexts so cancellation is durable.
- Normal complete runs must still finalize as `completed`.
- Explicit response-run cancellation through the existing cancel resource must keep working.
- Public SSE errors and run error summaries must remain sanitized.

### R3: Documentation And Spec Sync

Docs/specs that currently describe QA answer output as a single final `answer.delta` must be updated to reflect the new behavior: provider streaming yields incremental answer deltas; non-streaming remains a one-delta fallback.

## Out Of Scope

- Frontend UI changes.
- Model answer quality, retrieval ranking quality, or Knowledge runtime availability.
- Changing public SSE event names or adding new public routes.
- Replacing AI Gateway/provider contracts.

## Acceptance Criteria

- [ ] For #604, a streaming provider response with multiple content chunks produces multiple ordered `answer.delta` events before `answer.completed`.
- [ ] For #604, concatenated `answer.delta.text` values equal the final assistant answer in the covered streaming path, and replay persists the same ordered non-heartbeat events.
- [ ] For #604, non-streaming or no-delta provider paths still emit a compatible final answer delta.
- [ ] For #604, tool events, reasoning events, citation events, and SSE payload redaction tests do not regress.
- [ ] For #605, cancelling the request context during model execution cancels the agent context and finalizes the run/message as `cancelled`.
- [ ] For #605, cancellation-independent cleanup still persists model invocation/finalization records after request cancellation.
- [ ] Existing explicit response-run cancellation remains covered.
- [ ] QA service checks pass: `cd services/qa && go test ./...`, `cd services/qa && go build ./cmd/server`, and `cd services/qa && go build ./cmd/agent`.
- [ ] `git diff --check` passes.


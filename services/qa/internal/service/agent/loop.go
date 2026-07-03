package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrMaxIterations   = errors.New("agent reached maximum iterations")
	ErrInvalidResponse = errors.New("model returned an invalid response")
)

type EventType string

const (
	EventModelStarted   EventType = "model.started"
	EventModelCompleted EventType = "model.completed"
	EventReasoningDelta EventType = "reasoning.delta"
	EventAnswerDelta    EventType = "answer.delta"
	EventToolStarted    EventType = "tool.started"
	EventToolCompleted  EventType = "tool.completed"
	EventToolFailed     EventType = "tool.failed"
	EventAgentCompleted EventType = "agent.completed"
)

// Event intentionally excludes tool arguments, tool results, prompts, raw
// provider reasoning, and credentials. It is safe to adapt into logs or public
// progress summaries; provider-originated reasoning is emitted only after the
// configured ReasoningFilterFactory returns safe public deltas.
type Event struct {
	Type             EventType
	Iteration        int
	ToolCallID       string
	ToolName         string
	FinishReason     string
	ReasoningContent string
	AnswerContent    string
	Usage            TokenUsage
	Err              error
}

type Observer func(Event)

// ReasoningFilter receives provider-originated reasoning text and returns only
// safe public deltas. Passing final=true flushes any safe pending content.
type ReasoningFilter func(delta string, final bool) []string

// ReasoningFilterFactory creates request/iteration-local reasoning filters.
// Implementations may buffer state and must not be shared across concurrent runs.
type ReasoningFilterFactory func() ReasoningFilter

// ToolObservation carries raw tool input/output for internal persistence and
// citation extraction. It must not be logged or exposed directly.
type ToolObservation struct {
	Type       EventType
	Iteration  int
	ToolCallID string
	ToolName   string
	Arguments  json.RawMessage
	Result     string
	Err        error
}

type ToolObserver func(ToolObservation)

type Config struct {
	MaxIterations          int
	ToolTimeout            time.Duration
	MaxToolResultBytes     int
	Observer               Observer
	ReasoningFilterFactory ReasoningFilterFactory
}

type Runner struct {
	model ModelClient
	tools ToolClient
	cfg   Config
}

type Result struct {
	Messages   []Message
	Final      Message
	Iterations int
}

func NewRunner(model ModelClient, tools ToolClient, cfg Config) (*Runner, error) {
	if model == nil {
		return nil, errors.New("model client is required")
	}
	if tools == nil {
		return nil, errors.New("tool client is required")
	}
	if cfg.MaxIterations <= 0 {
		return nil, errors.New("max iterations must be positive")
	}
	if cfg.ToolTimeout <= 0 {
		return nil, errors.New("tool timeout must be positive")
	}
	if cfg.MaxToolResultBytes <= 0 {
		return nil, errors.New("max tool result bytes must be positive")
	}
	return &Runner{model: model, tools: tools, cfg: cfg}, nil
}

func (r *Runner) Run(ctx context.Context, input []Message) (Result, error) {
	return r.RunWithObserver(ctx, input, r.cfg.Observer)
}

// RunWithObserver executes one agent run with a request-scoped observer. This
// keeps concurrent HTTP streams isolated while preserving Run for CLI users.
func (r *Runner) RunWithObserver(ctx context.Context, input []Message, observer Observer) (Result, error) {
	return r.run(ctx, input, observer, nil)
}

// RunWithToolResultCallback executes one agent run with an observer and an
// additional callback for receiving tool results. The callback is intended for
// internal use only (e.g. citation extraction) and must not expose raw tool
// results to public interfaces or logs.
func (r *Runner) RunWithToolResultCallback(ctx context.Context, input []Message, observer Observer, toolObserver ToolObserver) (Result, error) {
	return r.run(ctx, input, observer, toolObserver)
}

func (r *Runner) run(ctx context.Context, input []Message, observer Observer, toolObserver ToolObserver) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(input) == 0 {
		return Result{}, errors.New("at least one message is required")
	}

	toolDefs, err := r.tools.ListTools(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list MCP tools: %w", err)
	}
	allowed := make(map[string]struct{}, len(toolDefs))
	for _, tool := range toolDefs {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			return Result{}, errors.New("MCP server returned a tool with an empty name")
		}
		if _, exists := allowed[name]; exists {
			return Result{}, fmt.Errorf("MCP server returned duplicate tool %q", name)
		}
		allowed[name] = struct{}{}
	}

	messages := append([]Message(nil), input...)
	// When no tools are exposed, the provider cannot legally return tool calls,
	// so answer chunks are safe to project as they arrive. With tool_choice=auto,
	// a turn is only known to be a final answer after Complete returns.
	streamAnswerDeltas := len(toolDefs) == 0
	for iteration := 1; iteration <= r.cfg.MaxIterations; iteration++ {
		emit(observer, Event{Type: EventModelStarted, Iteration: iteration})
		var answerDeltas []string
		var reasoningFilter ReasoningFilter
		if r.cfg.ReasoningFilterFactory != nil {
			reasoningFilter = r.cfg.ReasoningFilterFactory()
		}
		emitReasoningDelta := func(delta string, final bool) {
			if reasoningFilter == nil {
				return
			}
			for _, safeDelta := range reasoningFilter(delta, final) {
				if safeDelta != "" {
					emit(observer, Event{Type: EventReasoningDelta, Iteration: iteration, ReasoningContent: safeDelta})
				}
			}
		}
		modelCtx := WithReasoningDeltaObserver(ctx, func(delta string) {
			emitReasoningDelta(delta, false)
		})
		modelCtx = WithAnswerDeltaObserver(modelCtx, func(delta string) {
			if delta != "" {
				if streamAnswerDeltas {
					emit(observer, Event{Type: EventAnswerDelta, Iteration: iteration, AnswerContent: delta})
				} else {
					answerDeltas = append(answerDeltas, delta)
				}
			}
		})
		completion, err := r.model.Complete(modelCtx, messages, toolDefs)
		if err != nil {
			return Result{}, fmt.Errorf("complete model iteration %d: %w", iteration, err)
		}
		assistant := completion.Message
		if assistant.Role == "" {
			assistant.Role = RoleAssistant
		}
		if assistant.Role != RoleAssistant {
			return Result{}, fmt.Errorf("%w: expected assistant role, got %q", ErrInvalidResponse, assistant.Role)
		}
		messages = append(messages, assistant)
		if strings.TrimSpace(completion.ReasoningContent) != "" && !completion.ReasoningContentStreamed {
			emitReasoningDelta(completion.ReasoningContent, true)
		} else if completion.ReasoningContentStreamed {
			emitReasoningDelta("", true)
		}
		emit(observer, Event{Type: EventModelCompleted, Iteration: iteration, FinishReason: completion.FinishReason, Usage: completion.Usage})

		if len(assistant.ToolCalls) == 0 {
			if strings.TrimSpace(assistant.Content) == "" {
				return Result{}, fmt.Errorf("%w: empty final assistant message", ErrInvalidResponse)
			}
			if !streamAnswerDeltas {
				for _, delta := range answerDeltas {
					emit(observer, Event{Type: EventAnswerDelta, Iteration: iteration, AnswerContent: delta})
				}
			}
			emit(observer, Event{Type: EventAgentCompleted, Iteration: iteration, FinishReason: completion.FinishReason})
			return Result{Messages: messages, Final: assistant, Iterations: iteration}, nil
		}

		for index, call := range assistant.ToolCalls {
			resultMessage := r.executeTool(ctx, iteration, index, allowed, call, observer, toolObserver)
			messages = append(messages, resultMessage)
		}
	}

	return Result{Messages: messages, Iterations: r.cfg.MaxIterations}, ErrMaxIterations
}

func (r *Runner) executeTool(ctx context.Context, iteration, callIndex int, allowed map[string]struct{}, call ToolCall, observer Observer, toolObserver ToolObserver) Message {
	name := strings.TrimSpace(call.Function.Name)
	base := Message{Role: RoleTool, ToolCallID: call.ID, Name: name}
	toolCallID := strings.TrimSpace(call.ID)
	auditToolCallID := toolCallID
	if auditToolCallID == "" {
		auditToolCallID = fmt.Sprintf("invalid_tool_call_%d_%d", iteration, callIndex+1)
	}
	arguments := normalizedToolArguments(call.Function.Arguments)
	if toolCallID == "" || name == "" {
		base.Content = toolErrorJSON("invalid_tool_call", "model returned an invalid tool call")
		emitTool(toolObserver, ToolObservation{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content, Err: ErrInvalidResponse})
		emit(observer, Event{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Err: ErrInvalidResponse})
		return base
	}
	if _, ok := allowed[name]; !ok {
		base.Content = toolErrorJSON("unknown_tool", "requested tool is not available")
		emitTool(toolObserver, ToolObservation{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content, Err: errors.New("unknown tool")})
		emit(observer, Event{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Err: errors.New("unknown tool")})
		return base
	}

	var object map[string]any
	if err := json.Unmarshal(arguments, &object); err != nil {
		base.Content = toolErrorJSON("invalid_tool_arguments", "tool arguments must be a JSON object")
		emitTool(toolObserver, ToolObservation{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content, Err: err})
		emit(observer, Event{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Err: err})
		return base
	}

	emitTool(toolObserver, ToolObservation{Type: EventToolStarted, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments})
	emit(observer, Event{Type: EventToolStarted, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name})
	toolCtx, cancel := context.WithTimeout(ctx, r.cfg.ToolTimeout)
	defer cancel()
	result, err := r.tools.CallTool(toolCtx, name, arguments)
	if err != nil {
		base.Content = toolErrorJSON("tool_execution_failed", "tool execution failed")
		emitTool(toolObserver, ToolObservation{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content, Err: err})
		emit(observer, Event{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Err: err})
		return base
	}
	content := result.Content
	if result.IsError && strings.TrimSpace(content) == "" {
		content = toolErrorJSON("tool_execution_failed", "tool reported an error")
	}
	base.Content = truncateToolResult(content, r.cfg.MaxToolResultBytes)
	if result.IsError {
		err := errors.New("tool reported an error")
		emitTool(toolObserver, ToolObservation{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content, Err: err})
		emit(observer, Event{Type: EventToolFailed, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Err: err})
	} else {
		emitTool(toolObserver, ToolObservation{Type: EventToolCompleted, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name, Arguments: arguments, Result: base.Content})
		emit(observer, Event{Type: EventToolCompleted, Iteration: iteration, ToolCallID: auditToolCallID, ToolName: name})
	}
	return base
}

func normalizedToolArguments(raw string) json.RawMessage {
	arguments := json.RawMessage(raw)
	if len(arguments) == 0 {
		return json.RawMessage(`{}`)
	}
	return arguments
}

func emit(observer Observer, event Event) {
	if observer != nil {
		observer(event)
	}
}

func emitTool(observer ToolObserver, event ToolObservation) {
	if observer != nil {
		observer(event)
	}
}

func toolErrorJSON(code, message string) string {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
	return string(payload)
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	suffix := "\n...[tool result truncated]"
	limit := maxBytes - len(suffix)
	if limit <= 0 {
		return suffix[:maxBytes]
	}
	for limit > 0 && (value[limit]&0xC0) == 0x80 {
		limit--
	}
	return value[:limit] + suffix
}

func truncateToolResult(content string, maxBytes int) string {
	if len(content) <= maxBytes {
		return content
	}

	if len(content) > 0 && content[0] == '{' {
		var data map[string]any
		if err := json.Unmarshal([]byte(content), &data); err == nil {
			return truncateJSON(data, maxBytes)
		}
	}

	return truncateUTF8(content, maxBytes)
}

func truncateJSON(data map[string]any, maxBytes int) string {
	suffix := map[string]any{"truncated": true}

	for attempt := 0; attempt < 5; attempt++ {
		result := make(map[string]any)
		for k, v := range data {
			if k == "results" {
				if results, ok := v.([]any); ok {
					result[k] = truncateResultsByAttempt(results, attempt)
				} else {
					result[k] = v
				}
			} else {
				result[k] = v
			}
		}
		for k, v := range suffix {
			result[k] = v
		}

		jsonBytes, err := json.Marshal(result)
		if err == nil && len(jsonBytes) <= maxBytes {
			return string(jsonBytes)
		}

		data = result
	}

	minimal := map[string]any{
		"truncated": true,
	}
	if hitCount, ok := data["hit_count"]; ok {
		minimal["hit_count"] = hitCount
	}
	jsonBytes, err := json.Marshal(minimal)
	if err == nil && len(jsonBytes) <= maxBytes {
		return string(jsonBytes)
	}

	return `{"error":"tool_result_too_large","truncated":true}`
}

func truncateResultsByAttempt(results []any, attempt int) []any {
	if len(results) == 0 {
		return results
	}

	switch attempt {
	case 0:
		maxResults := len(results) / 2
		if maxResults < 1 {
			maxResults = 1
		}
		return results[:maxResults]
	case 1:
		maxResults := len(results) / 4
		if maxResults < 1 {
			maxResults = 1
		}
		truncated := make([]any, 0, maxResults)
		for i := 0; i < maxResults; i++ {
			truncated = append(truncated, truncateSingleResult(results[i]))
		}
		return truncated
	case 2:
		return []any{truncateSingleResult(results[0])}
	case 3:
		return []any{truncateToMinimalCitation(results[0])}
	default:
		return []any{}
	}
}

func truncateToMinimalCitation(item any) any {
	if m, ok := item.(map[string]any); ok {
		minimal := make(map[string]any)
		for _, key := range []string{"citation_no", "knowledge_base_id", "document_id", "chunk_id"} {
			if v, exists := m[key]; exists {
				minimal[key] = v
			}
		}
		return minimal
	}
	return item
}

func truncateSingleResult(item any) any {
	if m, ok := item.(map[string]any); ok {
		truncated := make(map[string]any)
		for k, v := range m {
			if str, ok := v.(string); ok {
				truncated[k] = truncateRunes(str, 256)
			} else {
				truncated[k] = v
			}
		}
		return truncated
	}
	return item
}

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

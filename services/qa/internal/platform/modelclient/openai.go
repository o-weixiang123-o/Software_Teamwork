package modelclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/modelendpoint"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/httpclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

const maxResponseBytes = 2 << 20

type Config struct {
	Endpoint    string
	Token       string
	TokenHeader string
	Model       string
	ProfileID   string
	// ParallelToolCalls controls the transport flag sent to AI Gateway. QA
	// still owns tool execution policy and may execute returned calls
	// sequentially even when a provider can return more than one call.
	ParallelToolCalls bool
	MaxTokens         int
	Timeout           time.Duration
	// Stream opts into AI Gateway server-sent events. Function calling itself
	// does not require streaming, so the default request path remains JSON.
	Stream bool

	transport http.RoundTripper
}

type Client struct {
	endpoint  modelendpoint.AIGatewayChatEndpoint
	model     string
	profileID string
	parallel  bool
	maxTokens int
	stream    bool
	http      *http.Client
}

func New(cfg Config) (*Client, error) {
	endpoint, err := modelendpoint.ParseAIGatewayChatEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	if cfg.Model == "" {
		return nil, errors.New("model is required")
	}
	if cfg.MaxTokens <= 0 {
		return nil, errors.New("max tokens must be positive")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("model timeout must be positive")
	}
	return &Client{
		endpoint:  endpoint,
		model:     cfg.Model,
		profileID: cfg.ProfileID,
		parallel:  cfg.ParallelToolCalls,
		maxTokens: cfg.MaxTokens,
		stream:    cfg.Stream,
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: httpclient.HeaderTransport{
				Base:   cfg.transport,
				Header: cfg.TokenHeader,
				Token:  cfg.Token,
			},
		},
	}, nil
}

type completionRequest struct {
	Model             string                 `json:"model"`
	ProfileID         string                 `json:"profile_id,omitempty"`
	Messages          []agent.Message        `json:"messages"`
	Tools             []agent.ToolDefinition `json:"tools,omitempty"`
	ToolChoice        any                    `json:"tool_choice,omitempty"`
	ParallelToolCalls bool                   `json:"parallel_tool_calls"`
	MaxTokens         int                    `json:"max_tokens"`
	Stream            bool                   `json:"stream"`
}

type usagePayload struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	TotalTokens             int `json:"total_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type completionResponse struct {
	Choices []struct {
		Message      messagePayload `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage usagePayload `json:"usage"`
}

type messagePayload struct {
	Message          agent.Message
	ReasoningContent string
}

func (m *messagePayload) UnmarshalJSON(data []byte) error {
	var decoded struct {
		Role             string           `json:"role"`
		Content          *string          `json:"content"`
		ToolCalls        []agent.ToolCall `json:"tool_calls"`
		ToolCallID       string           `json:"tool_call_id"`
		Name             string           `json:"name"`
		ReasoningContent json.RawMessage  `json:"reasoning_content"`
		Reasoning        json.RawMessage  `json:"reasoning"`
		ReasoningCamel   json.RawMessage  `json:"reasoningContent"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	m.Message = agent.Message{
		Role:       decoded.Role,
		ToolCalls:  decoded.ToolCalls,
		ToolCallID: decoded.ToolCallID,
		Name:       decoded.Name,
	}
	if decoded.Content != nil {
		m.Message.Content = *decoded.Content
	}
	m.ReasoningContent = firstNonBlankRawString(decoded.ReasoningContent, decoded.Reasoning, decoded.ReasoningCamel)
	return nil
}

func (c *Client) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Completion, error) {
	payload := completionRequest{
		Model:             c.model,
		ProfileID:         c.profileID,
		Messages:          messages,
		Tools:             tools,
		ParallelToolCalls: c.parallel,
		MaxTokens:         c.maxTokens,
		Stream:            c.stream,
	}
	if len(tools) > 0 {
		payload.ToolChoice = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("marshal completion request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return agent.Completion{}, fmt.Errorf("create completion request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if payload.Stream {
		request.Header.Set("Accept", "text/event-stream")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	request.Header.Set("X-Caller-Service", "qa")
	if requestID := service.RequestIDFromContext(ctx); requestID != "" {
		request.Header.Set("X-Request-Id", requestID)
	}
	if userID := service.UserIDFromContext(ctx); userID != "" {
		request.Header.Set("X-User-Id", userID)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return agent.Completion{}, service.NewError(service.CodeDependency, "AI gateway request failed", fmt.Errorf("call AI gateway: %w", err))
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return agent.Completion{}, normalizeGatewayError(response.StatusCode, response.Body)
	}
	if isEventStream(response.Header.Get("Content-Type")) {
		return decodeStreamCompletion(ctx, response.Body)
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return agent.Completion{}, fmt.Errorf("read completion response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return agent.Completion{}, errors.New("completion response exceeds size limit")
	}
	var decoded completionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return agent.Completion{}, fmt.Errorf("decode completion response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return agent.Completion{}, errors.New("completion response has no choices")
	}
	choice := decoded.Choices[0]
	return agent.Completion{
		Message:          choice.Message.Message,
		FinishReason:     choice.FinishReason,
		Usage:            tokenUsageFromPayload(decoded.Usage),
		ReasoningContent: choice.Message.ReasoningContent,
	}, nil
}

func normalizeGatewayError(statusCode int, body io.Reader) error {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	if statusCode == http.StatusBadRequest {
		return service.NewError(service.CodeValidation, "AI gateway rejected model request", fmt.Errorf("AI gateway returned HTTP %d", statusCode))
	}
	return service.NewError(service.CodeDependency, "AI gateway request failed", fmt.Errorf("AI gateway returned HTTP %d", statusCode))
}

type streamChunk struct {
	Choices []struct {
		Delta        messagePayload `json:"delta"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage usagePayload `json:"usage"`
}

type toolCallAccumulator struct {
	calls   []agent.ToolCall
	byIndex map[int]int
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{byIndex: map[int]int{}}
}

func (a *toolCallAccumulator) apply(deltas []agent.ToolCall) {
	for _, delta := range deltas {
		index := len(a.calls)
		if delta.Index != nil {
			index = *delta.Index
		}
		position, ok := a.byIndex[index]
		if !ok {
			position = len(a.calls)
			a.byIndex[index] = position
			idx := index
			a.calls = append(a.calls, agent.ToolCall{Index: &idx})
		}
		call := &a.calls[position]
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Type != "" {
			call.Type = delta.Type
		}
		call.Function.Name += delta.Function.Name
		call.Function.Arguments += delta.Function.Arguments
	}
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func decodeStreamCompletion(ctx context.Context, body io.Reader) (agent.Completion, error) {
	message := agent.Message{Role: agent.RoleAssistant}
	accumulator := newToolCallAccumulator()
	var finishReason string
	var usage agent.TokenUsage
	var reasoningContent strings.Builder
	reasoningStreamed := false
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	var totalBytes int
	sawDone := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		totalBytes += len(line) + 1
		if totalBytes > maxResponseBytes {
			return agent.Completion{}, errors.New("completion response exceeds size limit")
		}
		if len(line) == 0 || bytes.HasPrefix(line, []byte(":")) {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(payload, []byte("[DONE]")) {
			sawDone = true
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return agent.Completion{}, fmt.Errorf("decode completion stream chunk: %w", err)
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta.Message
			if delta.Role != "" {
				message.Role = delta.Role
			}
			message.Content += delta.Content
			if delta.Content != "" {
				if observer := agent.AnswerDeltaObserverFromContext(ctx); observer != nil {
					observer(delta.Content)
				}
			}
			accumulator.apply(delta.ToolCalls)
			if choice.Delta.ReasoningContent != "" {
				reasoningContent.WriteString(choice.Delta.ReasoningContent)
				reasoningStreamed = true
				if observer := agent.ReasoningDeltaObserverFromContext(ctx); observer != nil {
					observer(choice.Delta.ReasoningContent)
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = tokenUsageFromPayload(chunk.Usage)
		}
	}
	if err := scanner.Err(); err != nil {
		return agent.Completion{}, fmt.Errorf("read completion stream: %w", err)
	}
	if !sawDone {
		return agent.Completion{}, service.NewError(service.CodeDependency, "AI gateway request failed", errors.New("AI gateway stream ended before done"))
	}
	message.ToolCalls = accumulator.calls
	return agent.Completion{
		Message:                  message,
		FinishReason:             finishReason,
		Usage:                    usage,
		ReasoningContent:         reasoningContent.String(),
		ReasoningContentStreamed: reasoningStreamed,
	}, nil
}

func firstNonBlankRawString(values ...json.RawMessage) string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		var decoded string
		if err := json.Unmarshal(value, &decoded); err != nil {
			continue
		}
		if decoded != "" {
			return decoded
		}
	}
	return ""
}

func tokenUsageFromPayload(payload usagePayload) agent.TokenUsage {
	reasoningTokens := payload.CompletionTokensDetails.ReasoningTokens
	completionTokens := payload.CompletionTokens
	if reasoningTokens > 0 && completionTokens >= reasoningTokens {
		completionTokens -= reasoningTokens
	}
	usage := agent.TokenUsage{
		PromptTokens:     payload.PromptTokens,
		CompletionTokens: completionTokens,
		ReasoningTokens:  reasoningTokens,
		TotalTokens:      payload.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens + usage.ReasoningTokens
	}
	return usage
}

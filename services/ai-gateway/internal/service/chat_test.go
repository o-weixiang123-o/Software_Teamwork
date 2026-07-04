package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCreateChatCompletionValidatesToolMessageID(t *testing.T) {
	repo := newMemoryRepository()
	svc := NewWithChatProvider(repo, mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(context.Context, ProviderChatRequest) (ProviderChatResult, error) {
			t.Fatal("provider should not be called for invalid request")
			return ProviderChatResult{}, nil
		},
	})

	_, err := svc.CreateChatCompletion(context.Background(), ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_chat", CallerService: "qa"},
		Payload: map[string]json.RawMessage{
			"model":    json.RawMessage(`"model"`),
			"messages": json.RawMessage(`[{"role":"tool","content":"tool result"}]`),
		},
	})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want validation error")
	}
	openErr, ok := err.(*OpenAIError)
	if !ok || openErr.Type != "invalid_request_error" {
		t.Fatalf("error = %#v, want OpenAI validation error", err)
	}
	if openErr.Param != "messages.0.tool_call_id" {
		t.Fatalf("Param = %q, want messages.0.tool_call_id", openErr.Param)
	}
}

func TestCreateChatCompletionRejectsNonIntegerMaxTokens(t *testing.T) {
	svc := NewWithChatProvider(newMemoryRepository(), mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(context.Context, ProviderChatRequest) (ProviderChatResult, error) {
			t.Fatal("provider should not be called for invalid max_tokens")
			return ProviderChatResult{}, nil
		},
	})

	_, err := svc.CreateChatCompletion(context.Background(), ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_chat", CallerService: "qa"},
		Payload: map[string]json.RawMessage{
			"model":      json.RawMessage(`"model"`),
			"messages":   json.RawMessage(`[{"role":"user","content":"hello"}]`),
			"max_tokens": json.RawMessage(`1.5`),
		},
	})
	openErr, ok := err.(*OpenAIError)
	if !ok || openErr.Param != "max_tokens" {
		t.Fatalf("error = %#v, want max_tokens validation error", err)
	}
}

func TestCreateChatCompletionRecordsOnlySafeInvocationSummary(t *testing.T) {
	repo := newMemoryRepository()
	svc := NewWithChatProvider(repo, mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(ctx context.Context, req ProviderChatRequest) (ProviderChatResult, error) {
			if req.APIKey != "sk-secret-value" {
				t.Fatalf("provider API key = %q", req.APIKey)
			}
			if rawString(req.Payload["model"]) != "provider-model" {
				t.Fatalf("provider model = %q, want profile model", rawString(req.Payload["model"]))
			}
			if _, ok := req.Payload["tools"]; !ok {
				t.Fatalf("tools were not passed through")
			}
			return ProviderChatResult{
				Body:               json.RawMessage(`{"id":"chatcmpl_test","object":"chat.completion","created":1,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"safe answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`),
				Usage:              &TokenUsage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12},
				ProviderStatusCode: 200,
			}, nil
		},
	})
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{UserID: "user_1"}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderOpenAICompatible,
		BaseURL:           "https://provider.example/v1",
		Model:             "provider-model",
		APIKey:            "sk-secret-value",
		IsDefault:         &isDefault,
		SupportsStreaming: &isDefault,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateChatCompletion(context.Background(), ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_chat", CallerService: "qa", UserID: "user_1"},
		Payload: map[string]json.RawMessage{
			"messages": json.RawMessage(`[{"role":"user","content":"full prompt text"}]`),
			"tools":    json.RawMessage(`[{"type":"function","function":{"name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}]`),
		},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if len(repo.invocations) != 1 || len(repo.attempts) != 1 {
		t.Fatalf("recorded invocations=%d attempts=%d, want 1/1", len(repo.invocations), len(repo.attempts))
	}
	invocationBytes, _ := json.Marshal(repo.invocations[0])
	attemptBytes, _ := json.Marshal(repo.attempts[0])
	combined := string(invocationBytes) + string(attemptBytes)
	for _, forbidden := range []string{"sk-secret-value", "full prompt text", "query", "properties", "safe answer"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("invocation summary leaked %q: %s", forbidden, combined)
		}
	}
	if repo.invocations[0].Status != InvocationSucceeded || repo.invocations[0].TotalTokens == nil || *repo.invocations[0].TotalTokens != 12 {
		t.Fatalf("invocation summary = %+v", repo.invocations[0])
	}
	if repo.attempts[0].BaseURLHost != "provider.example" {
		t.Fatalf("BaseURLHost = %q", repo.attempts[0].BaseURLHost)
	}
}

func TestCreateChatCompletionRecordsProviderStatusOnOpenAIError(t *testing.T) {
	repo := newMemoryRepository()
	statusCode := http.StatusTooManyRequests
	svc := NewWithChatProvider(repo, mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(context.Context, ProviderChatRequest) (ProviderChatResult, error) {
			return ProviderChatResult{}, &OpenAIError{
				HTTPStatus:         http.StatusTooManyRequests,
				Message:            "provider rate limited request",
				Type:               "rate_limit_error",
				Code:               "rate_limit_error",
				ProviderStatusCode: &statusCode,
			}
		},
	})
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{UserID: "user_1"}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderOpenAICompatible,
		BaseURL:           "https://provider.example/v1",
		Model:             "provider-model",
		APIKey:            "sk-secret-value",
		IsDefault:         &isDefault,
		SupportsStreaming: &isDefault,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateChatCompletion(context.Background(), ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_rate_limited", CallerService: "qa", UserID: "user_1"},
		Payload: map[string]json.RawMessage{
			"messages": json.RawMessage(`[{"role":"user","content":"full prompt text"}]`),
		},
	})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want provider error")
	}
	if len(repo.invocations) != 1 || len(repo.attempts) != 1 {
		t.Fatalf("recorded invocations=%d attempts=%d, want 1/1", len(repo.invocations), len(repo.attempts))
	}
	if repo.invocations[0].ProviderStatusCode == nil || *repo.invocations[0].ProviderStatusCode != statusCode {
		t.Fatalf("invocation provider status = %v, want %d", repo.invocations[0].ProviderStatusCode, statusCode)
	}
	if repo.attempts[0].ProviderStatusCode == nil || *repo.attempts[0].ProviderStatusCode != statusCode {
		t.Fatalf("attempt provider status = %v, want %d", repo.attempts[0].ProviderStatusCode, statusCode)
	}
}

func TestCreateChatCompletionRecordsCancelledInvocationAfterRequestCancel(t *testing.T) {
	repo := newMemoryRepository()
	ctx, cancel := context.WithCancel(context.Background())
	svc := NewWithChatProvider(repo, mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(providerCtx context.Context, req ProviderChatRequest) (ProviderChatResult, error) {
			cancel()
			<-providerCtx.Done()
			return ProviderChatResult{}, providerCtx.Err()
		},
	})
	isDefault := true
	if _, err := svc.CreateModelProfile(ctx, RequestContext{UserID: "user_1"}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderOpenAICompatible,
		BaseURL:           "https://provider.example/v1",
		Model:             "provider-model",
		APIKey:            "sk-secret-value",
		IsDefault:         &isDefault,
		SupportsStreaming: &isDefault,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateChatCompletion(ctx, ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_cancelled", CallerService: "qa", UserID: "user_1"},
		Payload: map[string]json.RawMessage{
			"messages": json.RawMessage(`[{"role":"user","content":"full prompt text"}]`),
		},
	})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want cancellation error")
	}
	if len(repo.invocations) != 1 || len(repo.attempts) != 1 {
		t.Fatalf("recorded invocations=%d attempts=%d, want 1/1", len(repo.invocations), len(repo.attempts))
	}
	if repo.invocations[0].Status != InvocationCancelled || repo.attempts[0].Status != InvocationCancelled {
		t.Fatalf("status invocation=%s attempt=%s, want cancelled", repo.invocations[0].Status, repo.attempts[0].Status)
	}
}

func TestCreateChatCompletionRejectsModelOutsideProfile(t *testing.T) {
	repo := newMemoryRepository()
	called := false
	svc := NewWithChatProvider(repo, mustEncryptor(t), 60000, fakeChatProvider{
		complete: func(context.Context, ProviderChatRequest) (ProviderChatResult, error) {
			called = true
			return ProviderChatResult{}, nil
		},
	})
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{UserID: "user_1"}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderOpenAICompatible,
		BaseURL:           "https://provider.example/v1",
		Model:             "provider-model",
		APIKey:            "sk-secret-value",
		IsDefault:         &isDefault,
		SupportsStreaming: &isDefault,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateChatCompletion(context.Background(), ChatCompletionInput{
		RequestContext: RequestContext{RequestID: "req_chat_mismatch", CallerService: "qa", UserID: "user_1"},
		Payload: map[string]json.RawMessage{
			"model":    json.RawMessage(`"other-model"`),
			"messages": json.RawMessage(`[{"role":"user","content":"full prompt text"}]`),
		},
	})
	openErr, ok := err.(*OpenAIError)
	if !ok || openErr.Param != "model" || openErr.Type != "invalid_request_error" {
		t.Fatalf("CreateChatCompletion() error = %#v, want model validation error", err)
	}
	if called {
		t.Fatalf("provider was called for mismatched chat model")
	}
	if len(repo.invocations) != 0 || len(repo.attempts) != 0 {
		t.Fatalf("recorded invocations=%d attempts=%d, want 0/0", len(repo.invocations), len(repo.attempts))
	}
}

type fakeChatProvider struct {
	complete func(context.Context, ProviderChatRequest) (ProviderChatResult, error)
	stream   func(context.Context, ProviderChatRequest) (ProviderChatStream, error)
}

func (p fakeChatProvider) CompleteChat(ctx context.Context, req ProviderChatRequest) (ProviderChatResult, error) {
	if p.complete == nil {
		return ProviderChatResult{}, dependencyOpenAIError("not implemented", nil)
	}
	return p.complete(ctx, req)
}

func (p fakeChatProvider) StreamChat(ctx context.Context, req ProviderChatRequest) (ProviderChatStream, error) {
	if p.stream == nil {
		return ProviderChatStream{}, dependencyOpenAIError("not implemented", nil)
	}
	return p.stream(ctx, req)
}

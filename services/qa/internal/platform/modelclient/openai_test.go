package modelclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func newTestTransport(t *testing.T, rawURL string) http.RoundTripper {
	t.Helper()
	target, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	return rewriteTransport{target: target, base: http.DefaultTransport}
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	return t.base.RoundTrip(cloned)
}

func TestCompleteSendsFunctionToolsAndParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Service-Token"); got != "test-token" {
			t.Errorf("X-Service-Token = %q", got)
		}
		if got := r.Header.Get("X-Caller-Service"); got != "qa" {
			t.Errorf("X-Caller-Service = %q", got)
		}
		if got := r.Header.Get("X-Request-Id"); got != "req-model-test" {
			t.Errorf("X-Request-Id = %q", got)
		}
		if got := r.Header.Get("X-User-Id"); got != "user-model-test" {
			t.Errorf("X-User-Id = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Stream {
			t.Errorf("stream = true, want false by default")
		}
		if request.ToolChoice != "auto" || len(request.Tools) != 1 {
			t.Errorf("unexpected tool request: %+v", request)
		}
		if request.ParallelToolCalls {
			t.Errorf("parallel_tool_calls = true, want false by default")
		}
		if request.ProfileID != "profile-chat" {
			t.Errorf("profile_id = %q", request.ProfileID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "choices":[{
            "message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"add","arguments":"{\"a\":1}"}}]},
            "finish_reason":"tool_calls"
          }],
          "usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12,"completion_tokens_details":{"reasoning_tokens":2}}
        }`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", Token: "test-token", TokenHeader: "X-Service-Token", Model: "test", ProfileID: "profile-chat", MaxTokens: 100, Timeout: time.Second, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := service.WithUserID(service.WithRequestID(context.Background(), "req-model-test"), "user-model-test")
	completion, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: "add"}}, []agent.ToolDefinition{{
		Type: "function", Function: agent.FunctionTool{Name: "add", Parameters: map[string]any{"type": "object"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if completion.FinishReason != "tool_calls" || completion.Message.ToolCalls[0].Function.Name != "add" {
		t.Fatalf("unexpected completion: %+v", completion)
	}
	if completion.Message.Content != "" {
		t.Fatalf("content = %q, want empty normalized value", completion.Message.Content)
	}
	if completion.ReasoningContent != "" {
		t.Fatalf("reasoning content = %q, want empty fallback", completion.ReasoningContent)
	}
	if completion.Usage.PromptTokens != 7 || completion.Usage.CompletionTokens != 3 || completion.Usage.ReasoningTokens != 2 || completion.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", completion.Usage)
	}
}

func TestCompleteParsesReasoningContentFromJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "choices":[{
            "message":{"role":"assistant","content":"answer","reasoning_content":"checking safe public facts"},
            "finish_reason":"stop"
          }],
          "usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
        }`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Message.Content != "answer" {
		t.Fatalf("answer = %q", completion.Message.Content)
	}
	if completion.ReasoningContent != "checking safe public facts" || completion.ReasoningContentStreamed {
		t.Fatalf("unexpected reasoning content: %+v", completion)
	}
}

func TestCompleteIgnoresNonStringReasoningContentFromJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
          "choices":[{
            "message":{"role":"assistant","content":"answer","reasoning":{"steps":["ignored"]}},
            "finish_reason":"stop"
          }],
          "usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
        }`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Message.Content != "answer" || completion.ReasoningContent != "" || completion.ReasoningContentStreamed {
		t.Fatalf("unexpected completion: %+v", completion)
	}
}

func TestNewRejectsUntrustedAIGatewayEndpoint(t *testing.T) {
	cases := []string{
		"https://public.example.test/internal/v1/chat/completions",
		"http://169.254.169.254/internal/v1/chat/completions",
		"http://ai-gateway.example.test/internal/v1/chat/completions",
		"http://localhost:18086/internal/v1/chat/completions",
		"http://user:pass@ai-gateway/internal/v1/chat/completions",
		"http://ai-gateway/internal/v1/model-profiles",
		"http://ai-gateway/internal/v1/chat/completions?redirect=http://example.test",
	}
	for _, endpoint := range cases {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := New(Config{Endpoint: endpoint, Model: "test", MaxTokens: 100, Timeout: time.Second}); err == nil {
				t.Fatalf("New() accepted endpoint %q", endpoint)
			}
		})
	}
}

func TestNewAcceptsLocalAndServiceAIGatewayEndpoints(t *testing.T) {
	cases := []string{
		"http://localhost:8086/internal/v1/chat/completions",
		"http://localhost/internal/v1/chat/completions",
		"http://127.0.0.1:8086/internal/v1/chat/completions",
		"http://[::1]:8086/internal/v1/chat/completions",
		"http://ai-gateway:8086/internal/v1/chat/completions",
	}
	for _, endpoint := range cases {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := New(Config{Endpoint: endpoint, Model: "test", MaxTokens: 100, Timeout: time.Second}); err != nil {
				t.Fatalf("New() rejected endpoint %q: %v", endpoint, err)
			}
		})
	}
}

func TestNewRejectsDirectProviderEscapeEndpoints(t *testing.T) {
	cases := []string{
		"https://public.example.test/v1/chat/completions",
		"https://public.example.test/internal/v1/chat/completions",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/internal/v1/chat/completions",
		"http://user:pass@public.example.test/v1/chat/completions",
		"http://ai-gateway/internal/v1/chat/completions?redirect=http://example.test",
		"http://ai-gateway/internal/v1/chat/completions#fragment",
	}
	for _, endpoint := range cases {
		t.Run(endpoint, func(t *testing.T) {
			_, err := New(Config{
				Endpoint:  endpoint,
				Model:     "test",
				MaxTokens: 100,
				Timeout:   time.Second,
			})
			if err == nil {
				t.Fatalf("New() accepted unsafe endpoint %q", endpoint)
			}
		})
	}
}

func TestCompleteRejectsDependencyErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider secret detail api_key=sk-test prompt=hello", http.StatusBadGateway)
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency || appErr.Message != "AI gateway request failed" {
		t.Fatalf("unexpected normalized error: %v", err)
	}
	if strings.Contains(err.Error(), "provider secret") || strings.Contains(err.Error(), "sk-test") || strings.Contains(err.Error(), "prompt=hello") {
		t.Fatalf("error leaked provider body: %v", err)
	}
}

func TestCompleteMapsGatewayValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"full prompt must stay hidden","type":"invalid_request_error","code":"bad_request"}}`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeValidation || appErr.Message != "AI gateway rejected model request" {
		t.Fatalf("unexpected normalized error: %v", err)
	}
	if strings.Contains(err.Error(), "full prompt") {
		t.Fatalf("error leaked gateway body: %v", err)
	}
}

func TestCompleteParsesStreamedToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept = %q", got)
		}
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Stream {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"search_","arguments":"{\"q\""}}]},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"knowledge","arguments":":\"breaker\"}"}}]},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12,"completion_tokens_details":{"reasoning_tokens":2}}}
data: [DONE]
`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, Stream: true, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	completion, err := client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "search"}}, []agent.ToolDefinition{{
		Type: "function", Function: agent.FunctionTool{Name: "search_knowledge", Parameters: map[string]any{"type": "object"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	calls := completion.Message.ToolCalls
	if completion.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q", completion.FinishReason)
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %+v", calls)
	}
	if calls[0].Index == nil || *calls[0].Index != 0 {
		t.Fatalf("tool call index = %+v", calls[0].Index)
	}
	if calls[0].ID != "call-1" || calls[0].Type != "function" || calls[0].Function.Name != "search_knowledge" {
		t.Fatalf("unexpected tool call metadata: %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"q":"breaker"}` {
		t.Fatalf("arguments = %q", calls[0].Function.Arguments)
	}
	if completion.Usage.PromptTokens != 7 || completion.Usage.CompletionTokens != 3 || completion.Usage.ReasoningTokens != 2 || completion.Usage.TotalTokens != 12 {
		t.Fatalf("unexpected usage: %+v", completion.Usage)
	}
}

func TestCompleteParsesStreamedReasoningDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Stream {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"plan"},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{"reasoning_content":" "},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{"reasoning_content":"then answer","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}
data: [DONE]
`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, Stream: true, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	ctx := agent.WithReasoningDeltaObserver(context.Background(), func(delta string) {
		deltas = append(deltas, delta)
	})
	completion, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Message.Content != "done" || completion.ReasoningContent != "plan then answer" || !completion.ReasoningContentStreamed {
		t.Fatalf("unexpected completion: %+v", completion)
	}
	if strings.Join(deltas, "|") != "plan| |then answer" {
		t.Fatalf("reasoning deltas = %v", deltas)
	}
}

func TestCompleteEmitsStreamedAnswerDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Stream {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"first "},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{"content":"second"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}
data: [DONE]
`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, Stream: true, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	ctx := agent.WithAnswerDeltaObserver(context.Background(), func(delta string) {
		deltas = append(deltas, delta)
	})
	completion, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Message.Content != "first second" {
		t.Fatalf("content = %q", completion.Message.Content)
	}
	if strings.Join(deltas, "") != completion.Message.Content {
		t.Fatalf("answer deltas = %v, final content = %q", deltas, completion.Message.Content)
	}
}

func TestCompleteIgnoresNonStringStreamedReasoningDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Stream {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":{"steps":["ignored"]}},"finish_reason":null}]}
data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}
data: [DONE]
`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, Stream: true, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	ctx := agent.WithReasoningDeltaObserver(context.Background(), func(delta string) {
		deltas = append(deltas, delta)
	})
	completion, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completion.Message.Content != "done" || completion.ReasoningContent != "" || completion.ReasoningContentStreamed {
		t.Fatalf("unexpected completion: %+v", completion)
	}
	if len(deltas) != 0 {
		t.Fatalf("non-string reasoning emitted deltas: %v", deltas)
	}
}

func TestCompleteRejectsInterruptedStreamWithPartialDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request completionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if !request.Stream {
			t.Fatal("stream = false, want true")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"partial answer"},"finish_reason":null}]}
`))
	}))
	defer server.Close()

	client, err := New(Config{Endpoint: "http://localhost:8086/internal/v1/chat/completions", TokenHeader: "X-Service-Token", Model: "test", MaxTokens: 100, Timeout: time.Second, Stream: true, transport: newTestTransport(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, []agent.ToolDefinition{{
		Type: "function", Function: agent.FunctionTool{Name: "search_knowledge", Parameters: map[string]any{"type": "object"}},
	}})
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency || appErr.Message != "AI gateway request failed" {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "partial answer") {
		t.Fatalf("interrupted stream leaked partial delta: %v", err)
	}
}

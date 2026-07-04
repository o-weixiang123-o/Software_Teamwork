package aigateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/service"
)

// validChatBody returns a minimal well-formed AI Gateway chat completion response.
func validChatBody(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     5,
			"completion_tokens": 10,
			"total_tokens":      15,
		},
	}
}

// newChatTestClient constructs a ChatClient that talks to server via the
// rewriteTransport helper from profile_client_test.go.
func newChatTestClient(t *testing.T, server *httptest.Server) *ChatClient {
	t.Helper()
	c, err := NewChatClient(
		"http://localhost:8086",
		"service-token",
		"profile-default",
		"model-default",
		newTestHTTPClient(t, server.URL),
	)
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	return c
}

// ── constructor tests ─────────────────────────────────────────────────────────

func TestNewChatClientRejectsInvalidURL(t *testing.T) {
	_, err := NewChatClient("http://external.untrusted.host", "tok", "profile", "model", nil)
	if err == nil {
		t.Fatal("NewChatClient() error = nil, want error for untrusted URL")
	}
}

func TestNewChatClientRequiresProfileID(t *testing.T) {
	_, err := NewChatClient("http://localhost:8086", "svc-token", "", "", nil)
	if err == nil {
		t.Fatal("NewChatClient() error = nil, want error for empty profileID")
	}
}

func TestNewChatClientUsesDefaultHTTPClientWhenNil(t *testing.T) {
	c, err := NewChatClient("http://localhost:8086", "tok", "profile", "model", nil)
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if c == nil {
		t.Fatal("NewChatClient() client = nil, want non-nil")
	}
}

func TestNewChatClientOmitsModelForProfileOnlyConfig(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(validChatBody("ok"))
	}))
	defer server.Close()

	c, err := NewChatClient("http://localhost:8086", "tok", "my-profile", "", newTestHTTPClient(t, server.URL))
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	if _, err := c.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if _, ok := capturedBody["model"]; ok {
		t.Fatalf("request included model: %+v", capturedBody)
	}
	if capturedBody["profile_id"] != "my-profile" {
		t.Fatalf("profile_id = %#v, want my-profile", capturedBody["profile_id"])
	}
}

// ── CreateChatCompletion tests ────────────────────────────────────────────────

func TestChatClientCreatesCompletionWithInternalHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/chat/completions" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Service-Token") != "service-token" {
			t.Fatalf("X-Service-Token = %q", r.Header.Get("X-Service-Token"))
		}
		if r.Header.Get("X-Caller-Service") != "document" {
			t.Fatalf("X-Caller-Service = %q", r.Header.Get("X-Caller-Service"))
		}
		if r.Header.Get("X-Request-Id") != "req-chat" {
			t.Fatalf("X-Request-Id = %q", r.Header.Get("X-Request-Id"))
		}
		if r.Header.Get("X-User-Id") != "user-chat" {
			t.Fatalf("X-User-Id = %q", r.Header.Get("X-User-Id"))
		}
		var body struct {
			Model     string                `json:"model"`
			ProfileID string                `json:"profile_id"`
			Messages  []service.ChatMessage `json:"messages"`
			Stream    bool                  `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "model-default" || body.ProfileID != "profile-default" || body.Stream {
			t.Fatalf("request body = %+v", body)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" || body.Messages[0].Content != "生成大纲" {
			t.Fatalf("messages = %+v", body.Messages)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_1",
			"object":  "chat.completion",
			"created": 1782631200,
			"model":   "model-default",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "{\"sections\":[{\"title\":\"总述\"}]}",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		})
	}))
	defer server.Close()

	client, err := NewChatClient("http://localhost:8086", "service-token", "profile-default", "model-default", newTestHTTPClient(t, server.URL))
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	resp, err := client.CreateChatCompletion(context.Background(), service.RequestContext{
		RequestID: "req-chat",
		UserID:    "user-chat",
	}, service.ChatCompletionRequest{
		Messages: []service.ChatMessage{{Role: "user", Content: "生成大纲"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if resp.Content != "{\"sections\":[{\"title\":\"总述\"}]}" || resp.FinishReason != "stop" || resp.Usage.TotalTokens != 30 {
		t.Fatalf("completion response = %+v", resp)
	}
}

func TestChatClientSanitizesDownstreamError(t *testing.T) {
	rawBody := `{"error":{"message":"provider failed with sk-secret and https://provider.internal/v1","type":"upstream_error"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(rawBody))
	}))
	defer server.Close()

	client, err := NewChatClient("http://localhost:8086", "service-token", "profile-default", "model-default", newTestHTTPClient(t, server.URL))
	if err != nil {
		t.Fatalf("NewChatClient() error = %v", err)
	}
	_, err = client.CreateChatCompletion(context.Background(), service.RequestContext{}, service.ChatCompletionRequest{
		Messages: []service.ChatMessage{{Role: "user", Content: "prompt must stay local"}},
	})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want dependency error")
	}
	if strings.Contains(err.Error(), "sk-secret") || strings.Contains(err.Error(), "provider.internal") || strings.Contains(err.Error(), "prompt must stay local") {
		t.Fatalf("error leaked sensitive data: %v", err)
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error = %#v, want dependency error", err)
	}
}

func TestChatClientRejectsEmptyMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for empty messages")
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want validation error for empty messages")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeValidation {
		t.Fatalf("error code = %v, want %q", err, service.CodeValidation)
	}
}

func TestChatClientMaps400ToValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want validation error for 400")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeValidation {
		t.Fatalf("error code = %v, want %q", err, service.CodeValidation)
	}
}

func TestChatClientMapsNonSuccessStatusToDependencyError(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"unauthorized", http.StatusUnauthorized},
		{"forbidden", http.StatusForbidden},
		{"internal server error", http.StatusInternalServerError},
		{"service unavailable", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			client := newChatTestClient(t, server)
			_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
				service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
			if err == nil {
				t.Fatalf("status %d: error = nil, want dependency error", tc.status)
			}
			appErr, ok := service.Classify(err)
			if !ok || appErr.Code != service.CodeDependency {
				t.Fatalf("status %d: error code = %v, want %q", tc.status, err, service.CodeDependency)
			}
		})
	}
}

func TestChatClientMapsInvalidJSONToDependencyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want error for invalid JSON")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error code = %v, want %q", err, service.CodeDependency)
	}
}

func TestChatClientMapsEmptyChoicesToDependencyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want error for empty choices")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error code = %v, want %q", err, service.CodeDependency)
	}
}

func TestChatClientMapsWhitespaceContentToDependencyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"   \t\n"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want error for whitespace-only content")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error code = %v, want %q", err, service.CodeDependency)
	}
}

func TestChatClientHandlesNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	client := newChatTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so httpClient.Do fails immediately

	_, err := client.CreateChatCompletion(ctx, service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want error for cancelled context")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error code = %v, want %q", err, service.CodeDependency)
	}
}

func TestChatClientHandlesOversizeResponse(t *testing.T) {
	// Construct a syntactically valid JSON response whose total byte length
	// exceeds maxChatResponseBytes. Using valid JSON ensures the test would
	// still catch a regression if the size guard were removed — the body
	// would then reach json.Unmarshal, succeed, and return non-empty content
	// instead of an error, causing the assertion to fire.
	content := strings.Repeat("x", maxChatResponseBytes)
	body := `{"choices":[{"message":{"role":"assistant","content":"` +
		content + `"},"finish_reason":"stop"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{Messages: []service.ChatMessage{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("CreateChatCompletion() error = nil, want error for oversized response")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("error code = %v, want %q", err, service.CodeDependency)
	}
}

func TestChatClientForwardsUserContextHeaders(t *testing.T) {
	var capturedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(validChatBody("ok"))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{
		RequestID:   "req-abc",
		UserID:      "user-xyz",
		Roles:       []string{"admin", "editor"},
		Permissions: []string{"read", "write"},
	}, service.ChatCompletionRequest{
		Messages: []service.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if got := capturedHeaders.Get("X-Request-Id"); got != "req-abc" {
		t.Fatalf("X-Request-Id = %q, want req-abc", got)
	}
	if got := capturedHeaders.Get("X-User-Id"); got != "user-xyz" {
		t.Fatalf("X-User-Id = %q, want user-xyz", got)
	}
	if got := capturedHeaders.Get("X-User-Roles"); got != "admin,editor" {
		t.Fatalf("X-User-Roles = %q, want admin,editor", got)
	}
	if got := capturedHeaders.Get("X-User-Permissions"); got != "read,write" {
		t.Fatalf("X-User-Permissions = %q, want read,write", got)
	}
}

func TestChatClientUsesPerRequestModelAndProfile(t *testing.T) {
	var capturedBody struct {
		Model     string `json:"model"`
		ProfileID string `json:"profile_id"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(validChatBody("ok"))
	}))
	defer server.Close()

	client := newChatTestClient(t, server)
	_, err := client.CreateChatCompletion(context.Background(), service.RequestContext{},
		service.ChatCompletionRequest{
			Model:     "override-model",
			ProfileID: "override-profile",
			Messages:  []service.ChatMessage{{Role: "user", Content: "hi"}},
		})
	if err != nil {
		t.Fatalf("CreateChatCompletion() error = %v", err)
	}
	if capturedBody.Model != "override-model" {
		t.Fatalf("model = %q, want override-model", capturedBody.Model)
	}
	if capturedBody.ProfileID != "override-profile" {
		t.Fatalf("profile_id = %q, want override-profile", capturedBody.ProfileID)
	}
}

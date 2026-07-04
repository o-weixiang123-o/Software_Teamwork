package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/provider"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
)

// chatProfileBody creates a JSON body for registering a chat profile pointing at baseURL.
func chatProfileBody(baseURL string) string {
	return `{"name":"default-chat","purpose":"chat","provider":"openai_compatible","baseUrl":"` + baseURL + `/v1","model":"provider-model","apiKey":"sk-smoke-secret","enabled":true,"isDefault":true,"supportsStreaming":true}`
}

// embeddingProfileBody creates a JSON body for registering an OpenAI-compatible embedding profile.
func embeddingProfileBody(baseURL string) string {
	return `{"name":"default-embedding","purpose":"embedding","provider":"openai_compatible","baseUrl":"` + baseURL + `/v1","model":"BAAI/bge-m3","apiKey":"sk-smoke-secret","enabled":true,"isDefault":true,"dimensions":1024}`
}

// siliconFlowEmbeddingProfileBody creates a JSON body for registering a SiliconFlow embedding profile.
func siliconFlowEmbeddingProfileBody(baseURL string) string {
	return `{"name":"siliconflow-embedding","purpose":"embedding","provider":"siliconflow","baseUrl":"` + baseURL + `/v1","model":"BAAI/bge-m3","apiKey":"sk-smoke-secret","enabled":true,"isDefault":true,"dimensions":1024}`
}

// rerankProfileBody creates a JSON body for registering a rerank profile.
func rerankProfileBody(baseURL string) string {
	return `{"name":"default-rerank","purpose":"rerank","provider":"siliconflow","baseUrl":"` + baseURL + `/v1","model":"BAAI/bge-reranker-v2-m3","apiKey":"sk-smoke-secret","enabled":true,"isDefault":true,"topN":3}`
}

func registerProfile(t *testing.T, server *Server, body string) {
	t.Helper()
	req := authedRequest(http.MethodPost, "/internal/v1/model-profiles", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register profile status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func assertInvocationDoesNotContain(t *testing.T, invocation service.ProviderInvocation, forbidden ...string) {
	t.Helper()
	encoded, err := json.Marshal(invocation)
	if err != nil {
		t.Fatalf("marshal invocation: %v", err)
	}
	for _, item := range forbidden {
		if item == "" {
			continue
		}
		if bytes.Contains(encoded, []byte(item)) {
			t.Fatalf("invocation leaked %q: %s", item, encoded)
		}
	}
}

// TestChatSmoke_Provider401NormalizesError verifies that a 401 from the upstream
// provider is normalized to an authentication_error without leaking the raw provider body.
func TestChatSmoke_Provider401NormalizesError(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-provider-auth-secret","type":"auth","code":"invalid_api_key"}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for provider 401", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-provider-auth-secret") || strings.Contains(body, "invalid_api_key") {
		t.Fatalf("response leaked raw provider error body: %s", body)
	}
	if !strings.Contains(body, `"type"`) || !strings.Contains(body, `"message"`) {
		t.Fatalf("response missing OpenAI-style error fields: %s", body)
	}
	// Invocation must still be recorded with failed status.
	if len(repo.invocations) != 1 || repo.invocations[0].Status != service.InvocationFailed {
		t.Fatalf("invocations = %+v, want 1 failed", repo.invocations)
	}
}

// TestChatSmoke_Provider429NormalizesRateLimit verifies that a 429 from the upstream
// provider is normalized to a rate_limit_error.
func TestChatSmoke_Provider429NormalizesRateLimit(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-provider-rate-secret","type":"tokens","code":"rate_limit_exceeded"}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for provider 429", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-provider-rate-secret") {
		t.Fatalf("response leaked raw provider rate-limit body: %s", body)
	}
	if !strings.Contains(body, `"rate_limit_error"`) && !strings.Contains(body, `"rate_limited"`) {
		t.Fatalf("response missing rate-limit error code: %s", body)
	}
	if len(repo.invocations) != 1 || repo.invocations[0].Status != service.InvocationFailed {
		t.Fatalf("invocations = %+v, want 1 failed", repo.invocations)
	}
}

// TestChatSmoke_Provider5xxNormalizesError verifies that a 503 from the upstream
// provider is normalized to an upstream_error / dependency_error.
func TestChatSmoke_Provider5xxNormalizesError(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-provider-internal-secret"}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for provider 5xx", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-provider-internal-secret") {
		t.Fatalf("response leaked raw provider 5xx body: %s", body)
	}
	if !strings.Contains(body, `"upstream_error"`) || !strings.Contains(body, `"dependency_error"`) {
		t.Fatalf("response missing upstream_error normalization: %s", body)
	}
	if len(repo.invocations) != 1 || repo.invocations[0].Status != service.InvocationFailed {
		t.Fatalf("invocations = %+v, want 1 failed", repo.invocations)
	}
	if repo.invocations[0].ProviderStatusCode == nil || *repo.invocations[0].ProviderStatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ProviderStatusCode = %v, want 503", repo.invocations[0].ProviderStatusCode)
	}
}

// TestChatSmoke_ProviderTimeoutNormalizesError verifies that a provider-level timeout
// is normalized to an upstream_error / timeout and does not leak internal details.
func TestChatSmoke_ProviderTimeoutNormalizesError(t *testing.T) {
	// Use a very short timeout so the test does not hang.
	done := make(chan struct{})
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled (the AI gateway timed out).
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer fakeProvider.Close()
	defer close(done)

	// Create a chat client backed by the fake provider.
	httpClient := fakeProvider.Client()
	chatClient := provider.NewHTTPChatClient(httpClient)

	server, repo := newTestServerWithChatProviderAndRepo(t, chatClient)
	// Register a profile with a very short timeout (1 second minimum allowed).
	body := `{"name":"default-chat","purpose":"chat","provider":"openai_compatible","baseUrl":"` + fakeProvider.URL + `/v1","model":"provider-model","apiKey":"sk-smoke-secret","enabled":true,"isDefault":true,"timeoutMs":1000}`
	registerProfile(t, server, body)

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()

	// This will block until the 1s profile timeout fires.
	start := time.Now()
	server.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("test took too long (%v); timeout may not have fired", elapsed)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for provider timeout", rec.Code)
	}
	respBody := rec.Body.String()
	if !strings.Contains(respBody, `"upstream_error"`) {
		t.Fatalf("response missing upstream_error for timeout: %s", respBody)
	}
	if len(repo.invocations) != 1 || repo.invocations[0].Status != service.InvocationTimeout {
		t.Fatalf("invocations = %+v, want 1 timeout", repo.invocations)
	}
}

// TestChatSmoke_RequestIDForwardedToProvider verifies that the X-Request-Id header
// is forwarded from the AI gateway to the upstream provider on each chat request.
func TestChatSmoke_RequestIDForwardedToProvider(t *testing.T) {
	var receivedRequestID string
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRequestID = r.Header.Get("X-Request-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","object":"chat.completion","created":1,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer fakeProvider.Close()

	server := newTestServerWithChatProvider(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set("X-Request-Id", "client-req-smoke-01")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if receivedRequestID != "client-req-smoke-01" {
		t.Fatalf("provider received X-Request-Id = %q, want client-req-smoke-01", receivedRequestID)
	}
}

// TestChatSmoke_ExplicitProfileIDRoutesToCorrectProfile verifies that a chat request
// carrying an explicit profile_id bypasses the default profile selection.
func TestChatSmoke_ExplicitProfileIDRoutesToCorrectProfile(t *testing.T) {
	var providerCalled bool
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_explicit","object":"chat.completion","created":1,"model":"explicit-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))

	// Create a non-default explicit profile.
	explicitBody := `{"name":"explicit-chat","purpose":"chat","provider":"openai_compatible","baseUrl":"` + fakeProvider.URL + `/v1","model":"explicit-model","apiKey":"sk-explicit-secret","enabled":true,"isDefault":false}`
	createReq := authedRequest(http.MethodPost, "/internal/v1/model-profiles", strings.NewReader(explicitBody))
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create profile status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	// Extract the created profile ID from the response.
	profileID := extractProfileID(t, createRec.Body.Bytes())

	// Request using explicit profile_id; there is no default chat profile.
	chatBody := `{"model":"explicit-model","profile_id":"` + profileID + `","messages":[{"role":"user","content":"hello"}]}`
	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(chatBody))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !providerCalled {
		t.Fatal("provider was not called for explicit profile_id request")
	}
	if len(repo.invocations) != 1 || repo.invocations[0].ProfileID != profileID {
		t.Fatalf("invocation ProfileID = %q, want %q", repo.invocations[0].ProfileID, profileID)
	}
}

// TestChatSmoke_APIKeyNotExposedToProvider verifies that the raw API key from the
// profile is forwarded as a Bearer token but never appears in the response body.
func TestChatSmoke_APIKeyNotExposedToProvider(t *testing.T) {
	var receivedAuth string
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_key","object":"chat.completion","created":1,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer fakeProvider.Close()

	server := newTestServerWithChatProvider(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "qa")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if receivedAuth != "Bearer sk-smoke-secret" {
		t.Fatalf("provider Authorization = %q, want Bearer sk-smoke-secret", receivedAuth)
	}
	if strings.Contains(rec.Body.String(), "sk-smoke-secret") {
		t.Fatalf("response leaked API key: %s", rec.Body.String())
	}
}

// TestEmbeddingSmoke_ControlledProviderOpenAIShapeRecordsSummary verifies the
// downstream Knowledge embedding path against a controlled provider using the
// real HTTP adapter. It documents the reusable fake-provider seed profile and
// expected OpenAI-compatible response shape for issue #287.
func TestEmbeddingSmoke_ControlledProviderOpenAIShapeRecordsSummary(t *testing.T) {
	var providerRequest struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		Dimensions     int      `json:"dimensions"`
		EncodingFormat string   `json:"encoding_format"`
		User           string   `json:"user"`
	}
	var receivedRequestID string
	var receivedAuth string
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		receivedRequestID = r.Header.Get("X-Request-Id")
		receivedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&providerRequest); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.11,0.12]},{"object":"embedding","index":1,"embedding":[0.21,0.22]}],"model":"BAAI/bge-m3","usage":{"prompt_tokens":7,"total_tokens":7}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, embeddingProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/embeddings", strings.NewReader(`{"input":["transformer secret text","second chunk"],"user":"knowledge-smoke"}`))
	req.Header.Set("X-Caller-Service", "knowledge")
	req.Header.Set("X-Request-Id", "embedding-smoke-req-01")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{`"object":"list"`, `"model":"BAAI/bge-m3"`, `"total_tokens":7`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
	for _, forbidden := range []string{"sk-smoke-secret", "transformer secret text"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	if receivedRequestID != "embedding-smoke-req-01" {
		t.Fatalf("provider X-Request-Id = %q, want embedding-smoke-req-01", receivedRequestID)
	}
	if receivedAuth != "Bearer sk-smoke-secret" {
		t.Fatalf("provider Authorization = %q, want Bearer sk-smoke-secret", receivedAuth)
	}
	if providerRequest.Model != "BAAI/bge-m3" {
		t.Fatalf("provider model = %q, want BAAI/bge-m3", providerRequest.Model)
	}
	if len(providerRequest.Input) != 2 || providerRequest.Input[0] != "transformer secret text" {
		t.Fatalf("provider input = %#v, want original input texts", providerRequest.Input)
	}
	if providerRequest.Dimensions != 1024 {
		t.Fatalf("provider dimensions = %d, want profile default 1024", providerRequest.Dimensions)
	}
	if providerRequest.EncodingFormat != "float" {
		t.Fatalf("provider encoding_format = %q, want float default", providerRequest.EncodingFormat)
	}
	if providerRequest.User != "knowledge-smoke" {
		t.Fatalf("provider user = %q, want knowledge-smoke", providerRequest.User)
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Operation != service.OperationEmbedding || invocation.Status != service.InvocationSucceeded {
		t.Fatalf("invocation = %+v, want successful embedding", invocation)
	}
	if invocation.CallerService != "knowledge" || invocation.RequestID != "embedding-smoke-req-01" {
		t.Fatalf("invocation context = %+v, want knowledge request id", invocation)
	}
	if invocation.InputCount == nil || *invocation.InputCount != 2 {
		t.Fatalf("InputCount = %#v, want 2", invocation.InputCount)
	}
	if invocation.EmbeddingDimensions == nil || *invocation.EmbeddingDimensions != 1024 {
		t.Fatalf("EmbeddingDimensions = %#v, want 1024", invocation.EmbeddingDimensions)
	}
	if invocation.TotalTokens == nil || *invocation.TotalTokens != 7 {
		t.Fatalf("TotalTokens = %#v, want 7", invocation.TotalTokens)
	}
	if invocation.ProviderStatusCode == nil || *invocation.ProviderStatusCode != http.StatusOK {
		t.Fatalf("ProviderStatusCode = %#v, want 200", invocation.ProviderStatusCode)
	}
	assertInvocationDoesNotContain(t, invocation, "sk-smoke-secret", "transformer secret text", "0.11", "0.12")
}

// TestEmbeddingSmoke_SiliconFlowOmitsProviderDimensionsRecordsSummary verifies the
// production SiliconFlow adapter shape: AI Gateway keeps configured dimensions in
// invocation records but does not send the unsupported dimensions field upstream.
func TestEmbeddingSmoke_SiliconFlowOmitsProviderDimensionsRecordsSummary(t *testing.T) {
	var providerRequest map[string]any
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&providerRequest); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.31,0.32]}],"model":"BAAI/bge-m3","usage":{"prompt_tokens":3,"total_tokens":3}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, siliconFlowEmbeddingProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/embeddings", strings.NewReader(`{"input":["siliconflow text"]}`))
	req.Header.Set("X-Caller-Service", "knowledge")
	req.Header.Set("X-Request-Id", "embedding-siliconflow-smoke-req-01")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, ok := providerRequest["dimensions"]; ok {
		t.Fatalf("siliconflow provider request included dimensions: %#v", providerRequest)
	}
	if providerRequest["model"] != "BAAI/bge-m3" {
		t.Fatalf("provider model = %#v, want BAAI/bge-m3", providerRequest["model"])
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Provider != service.ProviderSiliconFlow || invocation.Operation != service.OperationEmbedding || invocation.Status != service.InvocationSucceeded {
		t.Fatalf("invocation = %+v, want successful siliconflow embedding", invocation)
	}
	if invocation.EmbeddingDimensions == nil || *invocation.EmbeddingDimensions != 1024 {
		t.Fatalf("EmbeddingDimensions = %#v, want 1024", invocation.EmbeddingDimensions)
	}
	if invocation.ProviderStatusCode == nil || *invocation.ProviderStatusCode != http.StatusOK {
		t.Fatalf("ProviderStatusCode = %#v, want 200", invocation.ProviderStatusCode)
	}
	assertInvocationDoesNotContain(t, invocation, "sk-smoke-secret", "siliconflow text", "0.31", "0.32")
}

// TestEmbeddingSmoke_Provider429NormalizesRateLimit verifies that a 429 from the
// embedding provider HTTP adapter is normalized to a rate_limited error and the raw
// provider body is not leaked. Uses the real provider.HTTPClient so the full production
// path (URL assembly, Authorization header, HTTP status normalisation, body discard) is
// exercised.
func TestEmbeddingSmoke_Provider429NormalizesRateLimit(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("provider missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-embed-rate-secret","type":"tokens"}}`))
	}))
	defer fakeProvider.Close()

	server, _ := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, embeddingProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/embeddings", strings.NewReader(`{"model":"BAAI/bge-m3","input":["text"]}`))
	req.Header.Set("X-Caller-Service", "knowledge")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for embedding 429", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-embed-rate-secret") {
		t.Fatalf("response leaked raw provider rate-limit body: %s", body)
	}
	if !strings.Contains(body, `"rate_limited"`) && !strings.Contains(body, `"rate_limit_error"`) {
		t.Fatalf("response missing rate_limited code: %s", body)
	}
}

// TestEmbeddingSmoke_Provider5xxNormalizesError verifies that a 5xx from the embedding
// provider HTTP adapter is normalized to a dependency_error and the raw provider body is
// not leaked. Uses the real provider.HTTPClient.
func TestEmbeddingSmoke_Provider5xxNormalizesError(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-embed-internal-secret"}}`))
	}))
	defer fakeProvider.Close()

	server, _ := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, embeddingProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/embeddings", strings.NewReader(`{"model":"BAAI/bge-m3","input":["text"]}`))
	req.Header.Set("X-Caller-Service", "knowledge")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for embedding 5xx", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-embed-internal-secret") {
		t.Fatalf("response leaked raw provider 5xx body: %s", body)
	}
	if !strings.Contains(body, `"dependency_error"`) && !strings.Contains(body, `"upstream_error"`) {
		t.Fatalf("response missing dependency_error: %s", body)
	}
}

// TestRerankSmoke_ControlledProviderResultsShapeRecordsSummary verifies the
// downstream Knowledge rerank path against a controlled provider using the real
// HTTP adapter. The fake provider returns the common results[] / relevance_score
// shape so response normalization stays covered.
func TestRerankSmoke_ControlledProviderResultsShapeRecordsSummary(t *testing.T) {
	var providerRequest struct {
		Model           string            `json:"model"`
		Query           string            `json:"query"`
		Documents       []string          `json:"documents"`
		TopN            int               `json:"top_n"`
		ReturnDocuments bool              `json:"return_documents"`
		Metadata        map[string]string `json:"metadata"`
	}
	var receivedRequestID string
	var receivedAuth string
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		receivedRequestID = r.Header.Get("X-Request-Id")
		receivedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&providerRequest); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.91},{"index":0,"relevance_score":0.42},{"index":2,"relevance_score":0.11}],"model":"BAAI/bge-reranker-v2-m3","meta":{"tokens":{"input_tokens":9,"output_tokens":1}}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, rerankProfileBody(fakeProvider.URL))

	reqBody := `{"query":"protection relay settings","documents":[{"id":"chunk-1","text":"first sensitive chunk"},{"id":"chunk-2","text":"best matching transformer chunk"},{"id":"chunk-3","text":"third chunk"}],"top_n":2,"metadata":{"knowledgeBaseId":"kb-smoke"}}`
	req := authedRequest(http.MethodPost, "/internal/v1/rerankings", strings.NewReader(reqBody))
	req.Header.Set("X-Caller-Service", "knowledge")
	req.Header.Set("X-Request-Id", "rerank-smoke-req-01")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{`"object":"list"`, `"document_id":"chunk-2"`, `"document_id":"chunk-1"`, `"total_tokens":10`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
	if strings.Contains(body, `"document_id":"chunk-3"`) {
		t.Fatalf("response was not limited to top_n=2: %s", body)
	}
	for _, forbidden := range []string{"sk-smoke-secret", "best matching transformer chunk", "first sensitive chunk"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	if receivedRequestID != "rerank-smoke-req-01" {
		t.Fatalf("provider X-Request-Id = %q, want rerank-smoke-req-01", receivedRequestID)
	}
	if receivedAuth != "Bearer sk-smoke-secret" {
		t.Fatalf("provider Authorization = %q, want Bearer sk-smoke-secret", receivedAuth)
	}
	if providerRequest.Model != "BAAI/bge-reranker-v2-m3" {
		t.Fatalf("provider model = %q, want BAAI/bge-reranker-v2-m3", providerRequest.Model)
	}
	if providerRequest.Query != "protection relay settings" {
		t.Fatalf("provider query = %q, want original query", providerRequest.Query)
	}
	if len(providerRequest.Documents) != 3 || providerRequest.Documents[1] != "best matching transformer chunk" {
		t.Fatalf("provider documents = %#v, want original document texts", providerRequest.Documents)
	}
	if providerRequest.TopN != 2 {
		t.Fatalf("provider top_n = %d, want request top_n 2", providerRequest.TopN)
	}
	if providerRequest.ReturnDocuments {
		t.Fatalf("provider return_documents = true, want false")
	}
	if providerRequest.Metadata["knowledgeBaseId"] != "kb-smoke" {
		t.Fatalf("provider metadata = %#v, want knowledgeBaseId", providerRequest.Metadata)
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Operation != service.OperationReranking || invocation.Status != service.InvocationSucceeded {
		t.Fatalf("invocation = %+v, want successful reranking", invocation)
	}
	if invocation.CallerService != "knowledge" || invocation.RequestID != "rerank-smoke-req-01" {
		t.Fatalf("invocation context = %+v, want knowledge request id", invocation)
	}
	if invocation.InputCount == nil || *invocation.InputCount != 3 {
		t.Fatalf("InputCount = %#v, want 3", invocation.InputCount)
	}
	if invocation.RerankTopN == nil || *invocation.RerankTopN != 2 {
		t.Fatalf("RerankTopN = %#v, want 2", invocation.RerankTopN)
	}
	if invocation.TotalTokens == nil || *invocation.TotalTokens != 10 {
		t.Fatalf("TotalTokens = %#v, want 10", invocation.TotalTokens)
	}
	if invocation.ProviderStatusCode == nil || *invocation.ProviderStatusCode != http.StatusOK {
		t.Fatalf("ProviderStatusCode = %#v, want 200", invocation.ProviderStatusCode)
	}
	assertInvocationDoesNotContain(t, invocation, "sk-smoke-secret", "best matching transformer chunk", "protection relay settings", "0.91")
}

// TestRerankSmoke_Provider429NormalizesRateLimit verifies that a 429 from the rerank
// provider HTTP adapter is normalized to a rate_limited error and the raw provider body is
// not leaked. Uses the real provider.HTTPClient.
func TestRerankSmoke_Provider429NormalizesRateLimit(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("unexpected provider path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("provider missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"raw-rerank-rate-secret"}}`))
	}))
	defer fakeProvider.Close()

	server, _ := newTestServerWithProvidersAndRepo(t, nil, provider.NewHTTPClient(fakeProvider.Client()))
	registerProfile(t, server, rerankProfileBody(fakeProvider.URL))

	reqBody := `{"model":"BAAI/bge-reranker-v2-m3","query":"query","documents":[{"id":"d1","text":"text"}]}`
	req := authedRequest(http.MethodPost, "/internal/v1/rerankings", strings.NewReader(reqBody))
	req.Header.Set("X-Caller-Service", "knowledge")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-OK for rerank 429", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "raw-rerank-rate-secret") {
		t.Fatalf("response leaked raw provider rate-limit body: %s", body)
	}
	if !strings.Contains(body, `"rate_limited"`) && !strings.Contains(body, `"rate_limit_error"`) {
		t.Fatalf("response missing rate_limited code: %s", body)
	}
}

// TestChatStreamSmoke_ProviderEarlyCloseRecordsNonSuccess verifies that when the
// provider closes the connection before sending [DONE], an invocation is still recorded
// and its status is failed (not succeeded). This exercises the provider-side EOF path
// through the real HTTP chat client and stream handler.
func TestChatStreamSmoke_ProviderEarlyCloseRecordsNonSuccess(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send one valid chunk then close without [DONE].
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"))
			f.Flush()
		}
		// Return immediately without [DONE] to trigger the incomplete-stream path.
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	body := `{"model":"provider-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	if repo.invocations[0].Status == service.InvocationSucceeded {
		t.Fatalf("invocation status = succeeded, want failed for provider early close")
	}
	if repo.invocations[0].Status != service.InvocationFailed {
		t.Fatalf("invocation status = %s, want failed", repo.invocations[0].Status)
	}
}

// extractProfileID parses the profile ID from a successful create-profile response.
func extractProfileID(t *testing.T, body []byte) string {
	t.Helper()
	// Response shape: {"data":{"id":"mp_...","name":...},"requestId":"..."}
	idx := strings.Index(string(body), `"id":"`)
	if idx < 0 {
		t.Fatalf("could not find id in response: %s", body)
	}
	start := idx + len(`"id":"`)
	end := strings.Index(string(body)[start:], `"`)
	if end < 0 {
		t.Fatalf("could not parse id value from response: %s", body)
	}
	return string(body)[start : start+end]
}

// TestChatSmoke_InvocationRecordsCallerService verifies that the caller service header
// is propagated into the invocation summary for audit purposes.
func TestChatSmoke_InvocationRecordsCallerService(t *testing.T) {
	fakeProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_caller","object":"chat.completion","created":1,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer fakeProvider.Close()

	server, repo := newTestServerWithChatProviderAndRepo(t, provider.NewHTTPChatClient(fakeProvider.Client()))
	registerProfile(t, server, chatProfileBody(fakeProvider.URL))

	req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("X-Caller-Service", "document")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	if repo.invocations[0].CallerService != "document" {
		t.Fatalf("CallerService = %q, want document", repo.invocations[0].CallerService)
	}
}

// TestRealProviderSmoke_ExplicitEnvOnly exercises real provider adapters only
// when explicitly requested. Ordinary CI should report this test as skipped.
func TestRealProviderSmoke_ExplicitEnvOnly(t *testing.T) {
	if os.Getenv("AI_GATEWAY_REAL_PROVIDER_SMOKE") != "1" {
		t.Skip("set AI_GATEWAY_REAL_PROVIDER_SMOKE=1 and provider env vars to run real provider smoke")
	}
	baseURL := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_PROVIDER_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_PROVIDER_API_KEY"))
	if baseURL == "" || apiKey == "" {
		t.Fatalf("real provider smoke requires AI_GATEWAY_REAL_PROVIDER_BASE_URL and AI_GATEWAY_REAL_PROVIDER_API_KEY")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	server, repo := newTestServerWithProvidersAndRepo(t, provider.NewHTTPChatClient(httpClient), provider.NewHTTPClient(httpClient))

	if chatModel := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_CHAT_MODEL")); chatModel != "" {
		registerProfile(t, server, realChatProfileBody(baseURL, chatModel, apiKey))
		t.Run("chat", func(t *testing.T) {
			body := `{"model":` + jsonQuote(chatModel) + `,"messages":[{"role":"user","content":"Return the single word ok."}],"temperature":0}`
			req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("X-Caller-Service", "qa")
			req.Header.Set("X-Request-Id", "real-chat-smoke")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("chat status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"object":"chat.completion"`) {
				t.Fatalf("chat response missing chat.completion object: %s", rec.Body.String())
			}
		})
		t.Run("chat_streaming", func(t *testing.T) {
			body := `{"model":` + jsonQuote(chatModel) + `,"stream":true,"messages":[{"role":"user","content":"Return the single word ok."}],"temperature":0}`
			req := authedRequest(http.MethodPost, "/internal/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("X-Caller-Service", "qa")
			req.Header.Set("X-Request-Id", "real-chat-stream-smoke")
			req.Header.Set("Accept", "text/event-stream")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("chat stream status = %d", rec.Code)
			}
			streamBody := rec.Body.String()
			if !strings.Contains(streamBody, "data:") || !strings.Contains(streamBody, "[DONE]") {
				t.Fatalf("chat stream response missing SSE data or done marker")
			}
		})
	} else {
		t.Log("skip chat real smoke: AI_GATEWAY_REAL_CHAT_MODEL is not set")
	}

	if embeddingModel := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_EMBEDDING_MODEL")); embeddingModel != "" {
		t.Run("embeddings", func(t *testing.T) {
			dimensions := 1024
			if raw := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_EMBEDDING_DIMENSIONS")); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil || parsed <= 0 {
					t.Fatalf("AI_GATEWAY_REAL_EMBEDDING_DIMENSIONS must be a positive integer, got %q", raw)
				}
				dimensions = parsed
			}
			registerProfile(t, server, realEmbeddingProfileBody(baseURL, embeddingModel, apiKey, dimensions))
			body := `{"model":` + jsonQuote(embeddingModel) + `,"input":["AI Gateway real provider smoke"]}`
			req := authedRequest(http.MethodPost, "/internal/v1/embeddings", strings.NewReader(body))
			req.Header.Set("X-Caller-Service", "knowledge")
			req.Header.Set("X-Request-Id", "real-embedding-smoke")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("embedding status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"object":"list"`) || !strings.Contains(rec.Body.String(), `"embedding"`) {
				t.Fatalf("embedding response missing OpenAI list shape: %s", rec.Body.String())
			}
		})
	} else {
		t.Log("skip embeddings real smoke: AI_GATEWAY_REAL_EMBEDDING_MODEL is not set")
	}

	if rerankModel := strings.TrimSpace(os.Getenv("AI_GATEWAY_REAL_RERANK_MODEL")); rerankModel != "" {
		t.Run("rerank", func(t *testing.T) {
			registerProfile(t, server, realRerankProfileBody(baseURL, rerankModel, apiKey))
			body := `{"model":` + jsonQuote(rerankModel) + `,"query":"electrical relay protection","documents":[{"id":"doc-a","text":"relay protection settings and fault isolation"},{"id":"doc-b","text":"cafeteria menu"}],"top_n":1}`
			req := authedRequest(http.MethodPost, "/internal/v1/rerankings", strings.NewReader(body))
			req.Header.Set("X-Caller-Service", "knowledge")
			req.Header.Set("X-Request-Id", "real-rerank-smoke")
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("rerank status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"object":"list"`) || !strings.Contains(rec.Body.String(), `"document_id"`) {
				t.Fatalf("rerank response missing OpenAI-style list shape: %s", rec.Body.String())
			}
		})
	} else {
		t.Log("skip rerank real smoke: AI_GATEWAY_REAL_RERANK_MODEL is not set")
	}

	if len(repo.invocations) == 0 {
		t.Fatalf("real provider smoke ran with no operation model env vars set; set at least one of AI_GATEWAY_REAL_CHAT_MODEL, AI_GATEWAY_REAL_EMBEDDING_MODEL, AI_GATEWAY_REAL_RERANK_MODEL")
	}
	for _, invocation := range repo.invocations {
		if invocation.Status != service.InvocationSucceeded {
			t.Fatalf("real provider invocation failed: %+v", invocation)
		}
		assertInvocationDoesNotContain(t, invocation, apiKey)
	}
}

func realChatProfileBody(baseURL, model, apiKey string) string {
	return `{"name":"real-chat-smoke","purpose":"chat","provider":"openai_compatible","baseUrl":` + jsonQuote(baseURL) + `,"model":` + jsonQuote(model) + `,"apiKey":` + jsonQuote(apiKey) + `,"enabled":true,"isDefault":true,"supportsStreaming":true,"timeoutMs":30000}`
}

func realEmbeddingProfileBody(baseURL, model, apiKey string, dimensions int) string {
	return `{"name":"real-embedding-smoke","purpose":"embedding","provider":"openai_compatible","baseUrl":` + jsonQuote(baseURL) + `,"model":` + jsonQuote(model) + `,"apiKey":` + jsonQuote(apiKey) + `,"enabled":true,"isDefault":true,"dimensions":` + strconv.Itoa(dimensions) + `,"timeoutMs":30000}`
}

func realRerankProfileBody(baseURL, model, apiKey string) string {
	return `{"name":"real-rerank-smoke","purpose":"rerank","provider":"openai_compatible","baseUrl":` + jsonQuote(baseURL) + `,"model":` + jsonQuote(model) + `,"apiKey":` + jsonQuote(apiKey) + `,"enabled":true,"isDefault":true,"topN":1,"timeoutMs":30000}`
}

func jsonQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

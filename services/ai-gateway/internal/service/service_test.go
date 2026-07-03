package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type fakeInvoker struct {
	embeddingReq ProviderEmbeddingRequest
	rerankingReq ProviderRerankingRequest
	embeddingFn  func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error)
	rerankingFn  func(context.Context, ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error)
}

func (f *fakeInvoker) CreateEmbeddings(ctx context.Context, req ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
	f.embeddingReq = req
	if f.embeddingFn != nil {
		return f.embeddingFn(ctx, req)
	}
	return EmbeddingResponse{}, ProviderCallMetadata{}, nil
}

func (f *fakeInvoker) CreateReranking(ctx context.Context, req ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error) {
	f.rerankingReq = req
	if f.rerankingFn != nil {
		return f.rerankingFn(ctx, req)
	}
	return RerankingResponse{}, ProviderCallMetadata{}, nil
}

func TestCreateModelProfileRedactsCredential(t *testing.T) {
	repo := newMemoryRepository()
	encryptor, err := NewCredentialEncryptor([]byte("12345678901234567890123456789012"), "local-v1")
	if err != nil {
		t.Fatalf("NewCredentialEncryptor() error = %v", err)
	}
	svc := New(repo, encryptor, 60000)

	profile, err := svc.CreateModelProfile(context.Background(), RequestContext{RequestID: "req-1", CallerService: "gateway"}, CreateModelProfileInput{
		Name:     "default-chat",
		Purpose:  PurposeChat,
		Provider: ProviderSiliconFlow,
		BaseURL:  "https://api.siliconflow.cn/v1",
		Model:    "Qwen/Qwen2.5",
		APIKey:   "sk-secret-value",
	})
	if err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}
	if !profile.APIKeyConfigured {
		t.Fatalf("APIKeyConfigured = false, want true")
	}
	body, _ := json.Marshal(profile)
	if string(body) == "sk-secret-value" || bytes.Contains(body, []byte("sk-secret-value")) {
		t.Fatalf("profile response leaked api key: %s", body)
	}
	if got := repo.credentials[profile.CredentialID]; string(got.Ciphertext) == "sk-secret-value" || len(got.Nonce) == 0 {
		t.Fatalf("credential was not encrypted")
	}
}

func TestCredentialFingerprintUsesKeyedHMAC(t *testing.T) {
	key := []byte("12345678901234567890123456789012")
	encryptor, err := NewCredentialEncryptor(key, "local-v1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := encryptor.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptor.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if first.FingerprintSHA256 != second.FingerprintSHA256 {
		t.Fatalf("fingerprint not deterministic: %q != %q", first.FingerprintSHA256, second.FingerprintSHA256)
	}
	plain := sha256.Sum256([]byte("sk-secret-value"))
	if first.FingerprintSHA256 == hex.EncodeToString(plain[:]) {
		t.Fatal("fingerprint must not be raw SHA-256 of the API key")
	}
	other, err := NewCredentialEncryptor([]byte("abcdefghijklmnopqrstuvwxzy123456"), "other-v1")
	if err != nil {
		t.Fatal(err)
	}
	otherCredential, err := other.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if first.FingerprintSHA256 == otherCredential.FingerprintSHA256 {
		t.Fatal("fingerprint should be separated by encryption key")
	}
}

func TestCreateModelProfileRejectsSensitiveDefaultParameters(t *testing.T) {
	svc := New(newMemoryRepository(), mustEncryptor(t), 60000)
	_, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderSiliconFlow,
		BaseURL:           "https://api.siliconflow.cn/v1",
		Model:             "model",
		APIKey:            "sk-secret",
		DefaultParameters: json.RawMessage(`{"api_key":"nope"}`),
	})
	if err == nil {
		t.Fatalf("CreateModelProfile() error = nil, want validation error")
	}
}

func TestCreateModelProfileRejectsSensitiveDefaultParametersInArray(t *testing.T) {
	svc := New(newMemoryRepository(), mustEncryptor(t), 60000)
	_, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:              "default-chat",
		Purpose:           PurposeChat,
		Provider:          ProviderSiliconFlow,
		BaseURL:           "https://api.siliconflow.cn/v1",
		Model:             "model",
		APIKey:            "sk-secret",
		DefaultParameters: json.RawMessage(`{"headers":[{"authorization":"Bearer secret"}]}`),
	})
	if err == nil {
		t.Fatalf("CreateModelProfile() error = nil, want validation error")
	}
}

func TestDefaultProfileConstraint(t *testing.T) {
	svc := New(newMemoryRepository(), mustEncryptor(t), 60000)
	isDefault := true

	cases := []struct {
		name       string
		purpose    Purpose
		dimensions *int
		topN       *int
	}{
		{name: "chat", purpose: PurposeChat},
		{name: "embedding", purpose: PurposeEmbedding, dimensions: intPtr(1024)},
		{name: "rerank", purpose: PurposeRerank, topN: intPtr(5)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := CreateModelProfileInput{
				Name:       "default-" + tc.name,
				Purpose:    tc.purpose,
				Provider:   ProviderSiliconFlow,
				BaseURL:    "https://api.siliconflow.cn/v1",
				Model:      "model",
				APIKey:     "sk-secret",
				IsDefault:  &isDefault,
				Dimensions: tc.dimensions,
				TopN:       tc.topN,
			}
			if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, input); err != nil {
				t.Fatalf("first CreateModelProfile() error = %v", err)
			}
			input.Name = "second-" + tc.name
			if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, input); err != nil {
				t.Fatalf("second CreateModelProfile() error = %v", err)
			}
			items, err := svc.ListModelProfiles(context.Background(), ListModelProfilesFilter{})
			if err != nil {
				t.Fatalf("ListModelProfiles() error = %v", err)
			}
			defaults := 0
			for _, item := range items {
				if item.Purpose == tc.purpose && item.Enabled && item.IsDefault {
					defaults++
				}
			}
			if defaults != 1 {
				t.Fatalf("enabled %s defaults = %d, want 1", tc.purpose, defaults)
			}
		})
	}
}

func TestCheckReadyReportsAllProfilesOKWhenCredentialsAreConfigured(t *testing.T) {
	repo := newMemoryRepository()
	svc := New(repo, mustEncryptor(t), 60000)
	seedReadyProfile(repo, "ready-chat", PurposeChat, "fingerprint-chat", "chat")
	seedReadyProfile(repo, "ready-embedding", PurposeEmbedding, "fingerprint-embedding", "ding")
	seedReadyProfile(repo, "ready-rerank", PurposeRerank, "fingerprint-rerank", "rank")

	ready, err := svc.CheckReady(context.Background())
	if err != nil {
		t.Fatalf("CheckReady() error = %v", err)
	}
	if ready.Status != "ok" {
		t.Fatalf("ready status = %q, want ok: %#v", ready.Status, ready.Checks)
	}
	for _, name := range []string{"chat_profile", "embedding_profile", "rerank_profile"} {
		if got := readinessStatus(ready, name); got != "ok" {
			t.Fatalf("%s status = %q, want ok: %#v", name, got, ready.Checks)
		}
	}
}

func TestCheckReadyReportsSeedPlaceholderCredential(t *testing.T) {
	repo := newMemoryRepository()
	svc := New(repo, mustEncryptor(t), 60000)
	seedReadyProfile(repo, "default-chat", PurposeChat, "01db0178d97656cc4638023b711d331d4c59cf16a35126e175d9b39bc9c2eb20", "-key")

	ready, err := svc.CheckReady(context.Background())
	if err != nil {
		t.Fatalf("CheckReady() error = %v", err)
	}
	if ready.Status != "degraded" {
		t.Fatalf("ready status = %q, want degraded: %#v", ready.Status, ready.Checks)
	}
	if got := readinessStatus(ready, "chat_profile"); got != "placeholder" {
		t.Fatalf("chat_profile status = %q, want placeholder: %#v", got, ready.Checks)
	}
	if msg := readinessMessage(ready, "chat_profile"); strings.Contains(msg, "01db") || strings.Contains(msg, "-key") {
		t.Fatalf("placeholder readiness message leaked credential metadata: %q", msg)
	}
}

func TestCheckReadyUsesAnyConfiguredProfileForPurpose(t *testing.T) {
	repo := newMemoryRepository()
	svc := New(repo, mustEncryptor(t), 60000)
	seedReadyProfile(repo, "placeholder-chat", PurposeChat, "01db0178d97656cc4638023b711d331d4c59cf16a35126e175d9b39bc9c2eb20", "-key")
	seedReadyProfile(repo, "real-chat", PurposeChat, "fingerprint-chat", "chat")

	ready, err := svc.CheckReady(context.Background())
	if err != nil {
		t.Fatalf("CheckReady() error = %v", err)
	}
	if got := readinessStatus(ready, "chat_profile"); got != "ok" {
		t.Fatalf("chat_profile status = %q, want ok when any chat profile has non-placeholder credential: %#v", got, ready.Checks)
	}
}

func TestCheckReadyReportsMissingCredential(t *testing.T) {
	repo := newMemoryRepository()
	svc := New(repo, mustEncryptor(t), 60000)
	repo.profiles["missing-chat"] = ModelProfile{
		ID:               "missing-chat",
		Name:             "missing chat",
		Purpose:          PurposeChat,
		Enabled:          true,
		APIKeyConfigured: true,
	}

	ready, err := svc.CheckReady(context.Background())
	if err != nil {
		t.Fatalf("CheckReady() error = %v", err)
	}
	if got := readinessStatus(ready, "chat_profile"); got != "missing" {
		t.Fatalf("chat_profile status = %q, want missing: %#v", got, ready.Checks)
	}
}

func TestUpdateModelProfileRejectsEmptyAPIKey(t *testing.T) {
	svc := New(newMemoryRepository(), mustEncryptor(t), 60000)
	profile, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:     "default-chat",
		Purpose:  PurposeChat,
		Provider: ProviderSiliconFlow,
		BaseURL:  "https://api.siliconflow.cn/v1",
		Model:    "model",
		APIKey:   "sk-secret",
	})
	if err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}
	empty := " "
	_, err = svc.UpdateModelProfile(context.Background(), RequestContext{}, UpdateModelProfileInput{
		ID:     profile.ID,
		APIKey: &empty,
	})
	if err == nil {
		t.Fatalf("UpdateModelProfile() error = nil, want validation error")
	}
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeValidation || appErr.Fields["apiKey"] == "" {
		t.Fatalf("UpdateModelProfile() error = %#v, want apiKey validation error", err)
	}
}

func TestCreateEmbeddingsUsesDefaultProfileAndRecordsSafeSummary(t *testing.T) {
	repo := newMemoryRepository()
	invoker := &fakeInvoker{
		embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
			return EmbeddingResponse{
				Object: "list",
				Data: []EmbeddingVector{{
					Object:    "embedding",
					Index:     0,
					Embedding: json.RawMessage(`[0.1,0.2]`),
				}},
				Model: "BAAI/bge-m3",
				Usage: &TokenUsage{PromptTokens: 8, TotalTokens: 8},
			}, ProviderCallMetadata{StatusCode: 200}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	dimensions := 1024
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:       "default-embedding",
		Purpose:    PurposeEmbedding,
		Provider:   ProviderSiliconFlow,
		BaseURL:    "https://api.siliconflow.cn/v1",
		Model:      "BAAI/bge-m3",
		APIKey:     "sk-secret-value",
		IsDefault:  &isDefault,
		Dimensions: &dimensions,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	response, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-1", CallerService: "knowledge", UserID: "user-1"}, EmbeddingInput{
		Input: []string{"sensitive transformer text"},
	})
	if err != nil {
		t.Fatalf("CreateEmbeddings() error = %v", err)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 8 {
		t.Fatalf("response usage = %#v, want total 8", response.Usage)
	}
	if invoker.embeddingReq.Dimensions == nil || *invoker.embeddingReq.Dimensions != dimensions {
		t.Fatalf("provider dimensions = %#v, want %d", invoker.embeddingReq.Dimensions, dimensions)
	}
	if invoker.embeddingReq.Model != "BAAI/bge-m3" {
		t.Fatalf("provider model = %q, want profile model", invoker.embeddingReq.Model)
	}
	if invoker.embeddingReq.APIKey != "sk-secret-value" {
		t.Fatalf("provider API key was not decrypted")
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Operation != OperationEmbedding || invocation.Status != InvocationSucceeded {
		t.Fatalf("invocation = %#v, want successful embedding", invocation)
	}
	if invocation.Model != "BAAI/bge-m3" {
		t.Fatalf("invocation model = %q, want profile model", invocation.Model)
	}
	if invocation.InputCount == nil || *invocation.InputCount != 1 {
		t.Fatalf("InputCount = %#v, want 1", invocation.InputCount)
	}
	if invocation.EmbeddingDimensions == nil || *invocation.EmbeddingDimensions != dimensions {
		t.Fatalf("EmbeddingDimensions = %#v, want %d", invocation.EmbeddingDimensions, dimensions)
	}
	leakCheckInvocation := invocation
	leakCheckInvocation.CreatedAt = time.Time{}
	leakCheckInvocation.FinishedAt = time.Time{}
	body, _ := json.Marshal(leakCheckInvocation)
	for _, forbidden := range []string{"sensitive transformer text", "sk-secret-value", "0.1", "0.2"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("invocation leaked %q: %s", forbidden, body)
		}
	}
}

func TestCreateEmbeddingsRejectsModelOutsideDefaultProfile(t *testing.T) {
	repo := newMemoryRepository()
	called := false
	invoker := &fakeInvoker{
		embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
			called = true
			return EmbeddingResponse{}, ProviderCallMetadata{}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	dimensions := 1024
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:       "default-embedding",
		Purpose:    PurposeEmbedding,
		Provider:   ProviderSiliconFlow,
		BaseURL:    "https://api.siliconflow.cn/v1",
		Model:      "BAAI/bge-m3",
		APIKey:     "sk-secret-value",
		IsDefault:  &isDefault,
		Dimensions: &dimensions,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-model", CallerService: "knowledge"}, EmbeddingInput{
		Model: "other-embedding-model",
		Input: []string{"text"},
	})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeValidation || appErr.Fields["model"] == "" {
		t.Fatalf("CreateEmbeddings() error = %#v, want model validation error", err)
	}
	if called {
		t.Fatalf("provider was called for mismatched embedding model")
	}
	if len(repo.invocations) != 0 {
		t.Fatalf("invocations = %d, want 0", len(repo.invocations))
	}
}

func TestCreateEmbeddingsRejectsInvalidProviderIndexes(t *testing.T) {
	cases := []struct {
		name string
		data []EmbeddingVector
	}{
		{
			name: "missing item",
			data: []EmbeddingVector{embeddingVectorForTest(0)},
		},
		{
			name: "duplicate index",
			data: []EmbeddingVector{embeddingVectorForTest(0), embeddingVectorForTest(0)},
		},
		{
			name: "out of range index",
			data: []EmbeddingVector{embeddingVectorForTest(0), embeddingVectorForTest(2)},
		},
		{
			name: "wrong response order",
			data: []EmbeddingVector{embeddingVectorForTest(1), embeddingVectorForTest(0)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			invoker := &fakeInvoker{
				embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
					return EmbeddingResponse{
						Object: "list",
						Data:   tc.data,
						Model:  "BAAI/bge-m3",
					}, ProviderCallMetadata{StatusCode: 200}, nil
				},
			}
			svc := New(repo, mustEncryptor(t), 60000, invoker)
			dimensions := 1024
			isDefault := true
			if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
				Name:       "default-embedding",
				Purpose:    PurposeEmbedding,
				Provider:   ProviderSiliconFlow,
				BaseURL:    "https://api.siliconflow.cn/v1",
				Model:      "BAAI/bge-m3",
				APIKey:     "sk-secret-value",
				IsDefault:  &isDefault,
				Dimensions: &dimensions,
			}); err != nil {
				t.Fatalf("CreateModelProfile() error = %v", err)
			}

			_, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-invalid", CallerService: "knowledge"}, EmbeddingInput{
				Model: "BAAI/bge-m3",
				Input: []string{"first chunk", "second chunk"},
			})
			appErr, ok := Classify(err)
			if !ok || appErr.Code != CodeDependency {
				t.Fatalf("CreateEmbeddings() error = %#v, want dependency_error", err)
			}
			if len(repo.invocations) != 1 {
				t.Fatalf("invocations = %d, want 1", len(repo.invocations))
			}
			invocation := repo.invocations[0]
			if invocation.Status != InvocationFailed || invocation.NormalizedErrorCode != string(CodeDependency) {
				t.Fatalf("invocation = %#v, want failed dependency_error summary", invocation)
			}
		})
	}
}

func TestCreateEmbeddingsValidatesProviderEmbeddingPayloadShape(t *testing.T) {
	cases := []struct {
		name           string
		encodingFormat string
		payload        json.RawMessage
	}{
		{
			name:           "float rejects null",
			encodingFormat: "float",
			payload:        json.RawMessage(`null`),
		},
		{
			name:           "float rejects object",
			encodingFormat: "float",
			payload:        json.RawMessage(`{"value":0.1}`),
		},
		{
			name:           "float rejects number",
			encodingFormat: "float",
			payload:        json.RawMessage(`0.1`),
		},
		{
			name:           "float rejects string",
			encodingFormat: "float",
			payload:        json.RawMessage(`"AQIDBA=="`),
		},
		{
			name:           "base64 rejects plain string",
			encodingFormat: "base64",
			payload:        json.RawMessage(`"not base64"`),
		},
		{
			name:           "base64 rejects array",
			encodingFormat: "base64",
			payload:        json.RawMessage(`[0.1,0.2]`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			invoker := &fakeInvoker{
				embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
					return EmbeddingResponse{
						Object: "list",
						Data: []EmbeddingVector{{
							Object:    "embedding",
							Index:     0,
							Embedding: tc.payload,
						}},
						Model: "BAAI/bge-m3",
					}, ProviderCallMetadata{StatusCode: 200}, nil
				},
			}
			svc := New(repo, mustEncryptor(t), 60000, invoker)
			dimensions := 1024
			isDefault := true
			if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
				Name:       "default-embedding",
				Purpose:    PurposeEmbedding,
				Provider:   ProviderSiliconFlow,
				BaseURL:    "https://api.siliconflow.cn/v1",
				Model:      "BAAI/bge-m3",
				APIKey:     "sk-secret-value",
				IsDefault:  &isDefault,
				Dimensions: &dimensions,
			}); err != nil {
				t.Fatalf("CreateModelProfile() error = %v", err)
			}

			_, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-payload", CallerService: "knowledge"}, EmbeddingInput{
				Model:          "BAAI/bge-m3",
				Input:          []string{"first chunk"},
				EncodingFormat: tc.encodingFormat,
			})
			appErr, ok := Classify(err)
			if !ok || appErr.Code != CodeDependency {
				t.Fatalf("CreateEmbeddings() error = %#v, want dependency_error", err)
			}
			if invoker.embeddingReq.EncodingFormat != tc.encodingFormat {
				t.Fatalf("provider encoding format = %q, want %q", invoker.embeddingReq.EncodingFormat, tc.encodingFormat)
			}
			if len(repo.invocations) != 1 {
				t.Fatalf("invocations = %d, want 1", len(repo.invocations))
			}
			invocation := repo.invocations[0]
			if invocation.Status != InvocationFailed || invocation.NormalizedErrorCode != string(CodeDependency) {
				t.Fatalf("invocation = %#v, want failed dependency_error summary", invocation)
			}
		})
	}
}

func TestCreateEmbeddingsAcceptsBase64ProviderEmbeddingPayload(t *testing.T) {
	repo := newMemoryRepository()
	invoker := &fakeInvoker{
		embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
			return EmbeddingResponse{
				Object: "list",
				Data: []EmbeddingVector{{
					Object:    "embedding",
					Index:     0,
					Embedding: json.RawMessage(`"AQIDBA=="`),
				}},
				Model: "BAAI/bge-m3",
				Usage: &TokenUsage{PromptTokens: 8, TotalTokens: 8},
			}, ProviderCallMetadata{StatusCode: 200}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	dimensions := 1024
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:       "default-embedding",
		Purpose:    PurposeEmbedding,
		Provider:   ProviderSiliconFlow,
		BaseURL:    "https://api.siliconflow.cn/v1",
		Model:      "BAAI/bge-m3",
		APIKey:     "sk-secret-value",
		IsDefault:  &isDefault,
		Dimensions: &dimensions,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	response, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-base64", CallerService: "knowledge"}, EmbeddingInput{
		Model:          "BAAI/bge-m3",
		Input:          []string{"first chunk"},
		EncodingFormat: "base64",
	})
	if err != nil {
		t.Fatalf("CreateEmbeddings() error = %v", err)
	}
	if invoker.embeddingReq.EncodingFormat != "base64" {
		t.Fatalf("provider encoding format = %q, want base64", invoker.embeddingReq.EncodingFormat)
	}
	if string(response.Data[0].Embedding) != `"AQIDBA=="` {
		t.Fatalf("embedding payload = %s, want base64 string", response.Data[0].Embedding)
	}
	if len(repo.invocations) != 1 || repo.invocations[0].Status != InvocationSucceeded {
		t.Fatalf("invocations = %#v, want successful invocation", repo.invocations)
	}
}

func TestCreateEmbeddingsRejectsWrongPurposeProfile(t *testing.T) {
	svc := New(newMemoryRepository(), mustEncryptor(t), 60000, &fakeInvoker{})
	profile, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:     "default-chat",
		Purpose:  PurposeChat,
		Provider: ProviderSiliconFlow,
		BaseURL:  "https://api.siliconflow.cn/v1",
		Model:    "Qwen/Qwen2.5",
		APIKey:   "sk-secret",
	})
	if err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}
	_, err = svc.CreateEmbeddings(context.Background(), RequestContext{}, EmbeddingInput{
		Model:     "BAAI/bge-m3",
		ProfileID: profile.ID,
		Input:     []string{"text"},
	})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeValidation || appErr.Fields["profile_id"] == "" {
		t.Fatalf("CreateEmbeddings() error = %#v, want profile purpose validation", err)
	}
}

func TestCreateRerankingUsesDefaultTopNAndRecordsSafeSummary(t *testing.T) {
	repo := newMemoryRepository()
	invoker := &fakeInvoker{
		rerankingFn: func(context.Context, ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error) {
			return RerankingResponse{
				Object: "list",
				Data: []RerankingResult{{
					Index:      1,
					DocumentID: "chunk-2",
					Score:      0.91,
				}},
				Model: "BAAI/bge-reranker-v2-m3",
				Usage: &TokenUsage{PromptTokens: 12, TotalTokens: 12},
			}, ProviderCallMetadata{StatusCode: 200}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	topN := 3
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:      "default-rerank",
		Purpose:   PurposeRerank,
		Provider:  ProviderSiliconFlow,
		BaseURL:   "https://api.siliconflow.cn/v1",
		Model:     "BAAI/bge-reranker-v2-m3",
		APIKey:    "sk-secret-value",
		IsDefault: &isDefault,
		TopN:      &topN,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err := svc.CreateReranking(context.Background(), RequestContext{RequestID: "req-2", CallerService: "knowledge"}, RerankingInput{
		Query: "sensitive user query",
		Documents: []RerankingDocument{
			{ID: "chunk-1", Text: "first sensitive document"},
			{ID: "chunk-2", Text: "second sensitive document"},
		},
	})
	if err != nil {
		t.Fatalf("CreateReranking() error = %v", err)
	}
	if invoker.rerankingReq.TopN == nil || *invoker.rerankingReq.TopN != topN {
		t.Fatalf("provider topN = %#v, want %d", invoker.rerankingReq.TopN, topN)
	}
	if invoker.rerankingReq.Model != "BAAI/bge-reranker-v2-m3" {
		t.Fatalf("provider model = %q, want profile model", invoker.rerankingReq.Model)
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Operation != OperationReranking || invocation.RerankTopN == nil || *invocation.RerankTopN != topN {
		t.Fatalf("invocation = %#v, want reranking topN %d", invocation, topN)
	}
	if invocation.Model != "BAAI/bge-reranker-v2-m3" {
		t.Fatalf("invocation model = %q, want profile model", invocation.Model)
	}
	body, _ := json.Marshal(invocation)
	for _, forbidden := range []string{"sensitive user query", "first sensitive document", "second sensitive document", "sk-secret-value"} {
		if bytes.Contains(body, []byte(forbidden)) {
			t.Fatalf("invocation leaked %q: %s", forbidden, body)
		}
	}
}

func TestCreateRerankingRejectsModelOutsideExplicitProfile(t *testing.T) {
	repo := newMemoryRepository()
	called := false
	invoker := &fakeInvoker{
		rerankingFn: func(context.Context, ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error) {
			called = true
			return RerankingResponse{}, ProviderCallMetadata{}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	topN := 2
	profile, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:     "explicit-rerank",
		Purpose:  PurposeRerank,
		Provider: ProviderSiliconFlow,
		BaseURL:  "https://api.siliconflow.cn/v1",
		Model:    "BAAI/bge-reranker-v2-m3",
		APIKey:   "sk-secret-value",
		TopN:     &topN,
	})
	if err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	_, err = svc.CreateReranking(context.Background(), RequestContext{RequestID: "req-rerank-model", CallerService: "knowledge"}, RerankingInput{
		Model:     "other-rerank-model",
		ProfileID: profile.ID,
		Query:     "query",
		Documents: []RerankingDocument{
			{ID: "chunk-1", Text: "first document"},
		},
	})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeValidation || appErr.Fields["model"] == "" {
		t.Fatalf("CreateReranking() error = %#v, want model validation error", err)
	}
	if called {
		t.Fatalf("provider was called for mismatched rerank model")
	}
	if len(repo.invocations) != 0 {
		t.Fatalf("invocations = %d, want 0", len(repo.invocations))
	}
}

func TestCreateRerankingLimitsProviderResultsToTopN(t *testing.T) {
	repo := newMemoryRepository()
	invoker := &fakeInvoker{
		rerankingFn: func(context.Context, ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error) {
			return RerankingResponse{
				Object: "list",
				Data: []RerankingResult{
					{Index: 1, DocumentID: "chunk-2", Score: 0.93},
					{Index: 0, DocumentID: "chunk-1", Score: 0.82},
				},
				Model: "BAAI/bge-reranker-v2-m3",
			}, ProviderCallMetadata{StatusCode: 200}, nil
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	topN := 1
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:      "default-rerank",
		Purpose:   PurposeRerank,
		Provider:  ProviderSiliconFlow,
		BaseURL:   "https://api.siliconflow.cn/v1",
		Model:     "BAAI/bge-reranker-v2-m3",
		APIKey:    "sk-secret-value",
		IsDefault: &isDefault,
		TopN:      &topN,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}

	response, err := svc.CreateReranking(context.Background(), RequestContext{RequestID: "req-rerank-topn", CallerService: "knowledge"}, RerankingInput{
		Model: "BAAI/bge-reranker-v2-m3",
		Query: "query",
		Documents: []RerankingDocument{
			{ID: "chunk-1", Text: "first document"},
			{ID: "chunk-2", Text: "second document"},
		},
	})
	if err != nil {
		t.Fatalf("CreateReranking() error = %v", err)
	}
	if len(response.Data) != topN || response.Data[0].DocumentID != "chunk-2" {
		t.Fatalf("response data = %#v, want only top result", response.Data)
	}
	if len(repo.invocations) != 1 || repo.invocations[0].Status != InvocationSucceeded {
		t.Fatalf("invocations = %#v, want one successful invocation", repo.invocations)
	}
}

func TestCreateRerankingRejectsInvalidProviderDocumentMapping(t *testing.T) {
	cases := []struct {
		name string
		data []RerankingResult
	}{
		{
			name: "out of range index",
			data: []RerankingResult{{Index: 2, DocumentID: "chunk-2", Score: 0.91}},
		},
		{
			name: "duplicate index",
			data: []RerankingResult{
				{Index: 0, DocumentID: "chunk-1", Score: 0.91},
				{Index: 0, DocumentID: "chunk-1", Score: 0.82},
			},
		},
		{
			name: "mismatched document id",
			data: []RerankingResult{{Index: 1, DocumentID: "chunk-1", Score: 0.91}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepository()
			invoker := &fakeInvoker{
				rerankingFn: func(context.Context, ProviderRerankingRequest) (RerankingResponse, ProviderCallMetadata, error) {
					return RerankingResponse{
						Object: "list",
						Data:   tc.data,
						Model:  "BAAI/bge-reranker-v2-m3",
					}, ProviderCallMetadata{StatusCode: 200}, nil
				},
			}
			svc := New(repo, mustEncryptor(t), 60000, invoker)
			topN := 2
			isDefault := true
			if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
				Name:      "default-rerank",
				Purpose:   PurposeRerank,
				Provider:  ProviderSiliconFlow,
				BaseURL:   "https://api.siliconflow.cn/v1",
				Model:     "BAAI/bge-reranker-v2-m3",
				APIKey:    "sk-secret-value",
				IsDefault: &isDefault,
				TopN:      &topN,
			}); err != nil {
				t.Fatalf("CreateModelProfile() error = %v", err)
			}

			_, err := svc.CreateReranking(context.Background(), RequestContext{RequestID: "req-rerank-invalid", CallerService: "knowledge"}, RerankingInput{
				Model: "BAAI/bge-reranker-v2-m3",
				Query: "query",
				Documents: []RerankingDocument{
					{ID: "chunk-1", Text: "first document"},
					{ID: "chunk-2", Text: "second document"},
				},
			})
			appErr, ok := Classify(err)
			if !ok || appErr.Code != CodeDependency {
				t.Fatalf("CreateReranking() error = %#v, want dependency_error", err)
			}
			if len(repo.invocations) != 1 {
				t.Fatalf("invocations = %d, want 1", len(repo.invocations))
			}
			invocation := repo.invocations[0]
			if invocation.Status != InvocationFailed || invocation.NormalizedErrorCode != string(CodeDependency) {
				t.Fatalf("invocation = %#v, want failed dependency_error summary", invocation)
			}
		})
	}
}

func TestCreateEmbeddingsNormalizesProviderRateLimit(t *testing.T) {
	repo := newMemoryRepository()
	statusCode := 429
	invoker := &fakeInvoker{
		embeddingFn: func(context.Context, ProviderEmbeddingRequest) (EmbeddingResponse, ProviderCallMetadata, error) {
			return EmbeddingResponse{}, ProviderCallMetadata{StatusCode: statusCode}, NewProviderError(CodeRateLimited, "provider rate limit exceeded", &statusCode, nil)
		},
	}
	svc := New(repo, mustEncryptor(t), 60000, invoker)
	dimensions := 1024
	isDefault := true
	if _, err := svc.CreateModelProfile(context.Background(), RequestContext{}, CreateModelProfileInput{
		Name:       "default-embedding",
		Purpose:    PurposeEmbedding,
		Provider:   ProviderSiliconFlow,
		BaseURL:    "https://api.siliconflow.cn/v1",
		Model:      "BAAI/bge-m3",
		APIKey:     "sk-secret-value",
		IsDefault:  &isDefault,
		Dimensions: &dimensions,
	}); err != nil {
		t.Fatalf("CreateModelProfile() error = %v", err)
	}
	_, err := svc.CreateEmbeddings(context.Background(), RequestContext{RequestID: "req-rate", CallerService: "knowledge"}, EmbeddingInput{
		Model: "BAAI/bge-m3",
		Input: []string{"text"},
	})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeRateLimited {
		t.Fatalf("CreateEmbeddings() error = %#v, want rate_limited", err)
	}
	if len(repo.invocations) != 1 {
		t.Fatalf("invocations = %d, want 1", len(repo.invocations))
	}
	invocation := repo.invocations[0]
	if invocation.Status != InvocationFailed || invocation.NormalizedErrorCode != string(CodeRateLimited) {
		t.Fatalf("invocation = %#v, want failed rate_limited summary", invocation)
	}
	if invocation.ProviderStatusCode == nil || *invocation.ProviderStatusCode != statusCode {
		t.Fatalf("ProviderStatusCode = %#v, want %d", invocation.ProviderStatusCode, statusCode)
	}
}

func embeddingVectorForTest(index int) EmbeddingVector {
	return EmbeddingVector{
		Object:    "embedding",
		Index:     index,
		Embedding: json.RawMessage(`[0.1,0.2]`),
	}
}

func seedReadyProfile(repo *memoryRepository, id string, purpose Purpose, fingerprint, keyLast4 string) {
	repo.profiles[id] = ModelProfile{
		ID:               id,
		Name:             id,
		Purpose:          purpose,
		Enabled:          true,
		APIKeyConfigured: true,
		CredentialID:     "cred-" + id,
	}
	repo.credentials["cred-"+id] = ProviderCredential{
		ID:                "cred-" + id,
		ProfileID:         id,
		FingerprintSHA256: fingerprint,
		KeyLast4:          keyLast4,
		Status:            CredentialActive,
	}
}

func readinessStatus(ready Readiness, name string) string {
	for _, check := range ready.Checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}

func readinessMessage(ready Readiness, name string) string {
	for _, check := range ready.Checks {
		if check.Name == name {
			return check.Message
		}
	}
	return ""
}

func mustEncryptor(t *testing.T) *CredentialEncryptor {
	t.Helper()
	encryptor, err := NewCredentialEncryptor([]byte("12345678901234567890123456789012"), "local-v1")
	if err != nil {
		t.Fatalf("NewCredentialEncryptor() error = %v", err)
	}
	return encryptor
}

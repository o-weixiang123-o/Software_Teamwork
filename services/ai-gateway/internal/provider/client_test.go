package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
)

func TestCreateEmbeddingsSendsBatchDimensionsAndBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != embeddingsPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, embeddingsPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-secret-value" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "BAAI/bge-m3" || body["encoding_format"] != "float" {
			t.Fatalf("request body = %#v", body)
		}
		if got := int(body["dimensions"].(float64)); got != 1024 {
			t.Fatalf("dimensions = %d, want 1024", got)
		}
		inputs := body["input"].([]any)
		if len(inputs) != 2 {
			t.Fatalf("input len = %d, want 2", len(inputs))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"BAAI/bge-m3","usage":{"prompt_tokens":4,"total_tokens":4}}`))
	}))
	defer server.Close()

	dimensions := 1024
	client := NewHTTPClient(server.Client())
	response, metadata, err := client.CreateEmbeddings(t.Context(), service.ProviderEmbeddingRequest{
		RequestID:      "req-1",
		BaseURL:        server.URL,
		APIKey:         "sk-secret-value",
		TimeoutMS:      1000,
		Model:          "BAAI/bge-m3",
		Input:          []string{"a", "b"},
		Dimensions:     &dimensions,
		EncodingFormat: "float",
	})
	if err != nil {
		t.Fatalf("CreateEmbeddings() error = %v", err)
	}
	if metadata.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", metadata.StatusCode)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 4 {
		t.Fatalf("response usage = %#v, want total 4", response.Usage)
	}
}

func TestCreateEmbeddingsOmitsDimensionsForSiliconFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["dimensions"]; ok {
			t.Fatalf("siliconflow embedding request included dimensions: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"BAAI/bge-m3","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer server.Close()

	dimensions := 1024
	client := NewHTTPClient(server.Client())
	response, metadata, err := client.CreateEmbeddings(t.Context(), service.ProviderEmbeddingRequest{
		Provider:       service.ProviderSiliconFlow,
		BaseURL:        server.URL,
		APIKey:         "sk-secret-value",
		TimeoutMS:      1000,
		Model:          "BAAI/bge-m3",
		Input:          []string{"a"},
		Dimensions:     &dimensions,
		EncodingFormat: "float",
	})
	if err != nil {
		t.Fatalf("CreateEmbeddings() error = %v", err)
	}
	if metadata.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", metadata.StatusCode)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 2 {
		t.Fatalf("response usage = %#v, want total 2", response.Usage)
	}
}

func TestCreateRerankingSendsTextOnlyAndDisablesDocumentReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != rerankPath {
			t.Fatalf("path = %s, want %s", r.URL.Path, rerankPath)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["return_documents"] != false {
			t.Fatalf("return_documents = %#v, want false", body["return_documents"])
		}
		documents := body["documents"].([]any)
		if len(documents) != 2 || documents[0] != "first text" || documents[1] != "second text" {
			t.Fatalf("documents = %#v", documents)
		}
		for _, document := range documents {
			if strings.Contains(document.(string), "chunk-") {
				t.Fatalf("provider request leaked document IDs in documents: %#v", documents)
			}
		}
		if got := int(body["top_n"].(float64)); got != 1 {
			t.Fatalf("top_n = %d, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":0.93}],"model":"BAAI/bge-reranker-v2-m3","meta":{"tokens":{"input_tokens":9,"output_tokens":0}}}`))
	}))
	defer server.Close()

	topN := 1
	client := NewHTTPClient(server.Client())
	response, _, err := client.CreateReranking(t.Context(), service.ProviderRerankingRequest{
		BaseURL:   server.URL,
		APIKey:    "sk-secret-value",
		TimeoutMS: 1000,
		Model:     "BAAI/bge-reranker-v2-m3",
		Query:     "query",
		Documents: []service.RerankingDocument{
			{ID: "chunk-1", Text: "first text"},
			{ID: "chunk-2", Text: "second text"},
		},
		TopN: &topN,
	})
	if err != nil {
		t.Fatalf("CreateReranking() error = %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].DocumentID != "chunk-2" || response.Data[0].Score != 0.93 {
		t.Fatalf("response data = %#v", response.Data)
	}
	if response.Usage == nil || response.Usage.TotalTokens != 9 {
		t.Fatalf("usage = %#v, want total 9", response.Usage)
	}
}

func TestCreateRerankingDataBranchMapsDocumentIDByIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":1,"score":0.88}],"model":"BAAI/bge-reranker-v2-m3"}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.Client())
	response, _, err := client.CreateReranking(t.Context(), service.ProviderRerankingRequest{
		BaseURL:   server.URL,
		APIKey:    "sk-secret-value",
		TimeoutMS: 1000,
		Model:     "BAAI/bge-reranker-v2-m3",
		Query:     "query",
		Documents: []service.RerankingDocument{
			{ID: "chunk-1", Text: "first text"},
			{ID: "chunk-2", Text: "second text"},
		},
	})
	if err != nil {
		t.Fatalf("CreateReranking() error = %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].Index != 1 || response.Data[0].DocumentID != "chunk-2" || response.Data[0].Score != 0.88 {
		t.Fatalf("response data = %#v", response.Data)
	}
}

func TestCreateRerankingRejectsInvalidDataBranchIndexes(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "out of range index",
			body: `{"data":[{"index":2,"document_id":"chunk-2","score":0.91}],"model":"BAAI/bge-reranker-v2-m3"}`,
		},
		{
			name: "mismatched document id",
			body: `{"data":[{"index":1,"document_id":"chunk-1","score":0.91}],"model":"BAAI/bge-reranker-v2-m3"}`,
		},
		{
			name: "duplicate index",
			body: `{"data":[{"index":0,"score":0.91},{"index":0,"score":0.82}],"model":"BAAI/bge-reranker-v2-m3"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewHTTPClient(server.Client())
			_, _, err := client.CreateReranking(t.Context(), service.ProviderRerankingRequest{
				BaseURL:   server.URL,
				APIKey:    "sk-secret-value",
				TimeoutMS: 1000,
				Model:     "BAAI/bge-reranker-v2-m3",
				Query:     "query",
				Documents: []service.RerankingDocument{
					{ID: "chunk-1", Text: "first text"},
					{ID: "chunk-2", Text: "second text"},
				},
			})
			if err == nil {
				t.Fatalf("CreateReranking() error = nil, want invalid provider response")
			}
		})
	}
}

func TestProviderErrorDoesNotExposeRawBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `raw provider body with sk-secret-value and prompt text`, http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewHTTPClient(server.Client())
	_, _, err := client.CreateEmbeddings(t.Context(), service.ProviderEmbeddingRequest{
		BaseURL:   server.URL,
		APIKey:    "sk-secret-value",
		TimeoutMS: 1000,
		Model:     "model",
		Input:     []string{"prompt text"},
	})
	if err == nil {
		t.Fatalf("CreateEmbeddings() error = nil, want provider error")
	}
	if strings.Contains(err.Error(), "sk-secret-value") || strings.Contains(err.Error(), "prompt text") || strings.Contains(err.Error(), "raw provider body") {
		t.Fatalf("provider error leaked sensitive data: %v", err)
	}
}

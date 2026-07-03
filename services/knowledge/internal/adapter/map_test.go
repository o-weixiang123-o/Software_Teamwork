package adapter

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/vendorclient"
)

func TestBuildCreateDatasetBodyUsesDefaultParserConfigWhenChunkStrategyMissing(t *testing.T) {
	parserConfig := map[string]any{
		"layout_recognize": ragflowLayoutPaddleOCR,
		"chunk_token_num":  float64(1024),
	}
	body, err := buildCreateDatasetBody(createKnowledgeBaseRequest{Name: "Manuals"}, parserConfig, createDatasetOptions{})
	if err != nil {
		t.Fatalf("buildCreateDatasetBody: %v", err)
	}
	payload := decodeMap(t, body)
	cfg, ok := payload["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", payload["parser_config"])
	}
	if cfg["layout_recognize"] != ragflowLayoutPaddleOCR {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
}

func TestBuildCreateDatasetBodyPreservesExplicitChunkStrategy(t *testing.T) {
	explicit := json.RawMessage(`{"layout_recognize":"DeepDOC","chunk_token_num":256}`)
	body, err := buildCreateDatasetBody(createKnowledgeBaseRequest{
		Name:          "Manuals",
		ChunkStrategy: &explicit,
	}, map[string]any{"layout_recognize": ragflowLayoutPaddleOCR}, createDatasetOptions{})
	if err != nil {
		t.Fatalf("buildCreateDatasetBody: %v", err)
	}
	payload := decodeMap(t, body)
	cfg, ok := payload["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", payload["parser_config"])
	}
	if cfg["layout_recognize"] != ragflowLayoutDeepDOC {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
}

func TestBuildCreateDatasetBodyRejectsInvalidChunkStrategy(t *testing.T) {
	explicit := json.RawMessage(`"not-an-object"`)
	_, err := buildCreateDatasetBody(createKnowledgeBaseRequest{
		Name:          "Manuals",
		ChunkStrategy: &explicit,
	}, nil, createDatasetOptions{})
	if err == nil {
		t.Fatal("buildCreateDatasetBody returned nil error")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeValidation {
		t.Fatalf("error=%v, want validation_error", err)
	}
	if appErr.Fields["chunkStrategy"] == "" {
		t.Fatalf("fields=%v", appErr.Fields)
	}
}

func TestBuildCreateDatasetBodyIncludesVendorEmbeddingID(t *testing.T) {
	body, err := buildCreateDatasetBody(
		createKnowledgeBaseRequest{Name: "Manuals"},
		nil,
		createDatasetOptions{VendorEmbeddingID: "BAAI/bge-m3@SILICONFLOW"},
	)
	if err != nil {
		t.Fatalf("buildCreateDatasetBody: %v", err)
	}
	payload := decodeMap(t, body)
	if payload["embedding_model"] != "BAAI/bge-m3@SILICONFLOW" {
		t.Fatalf("embedding_model=%v", payload["embedding_model"])
	}
}

func TestBuildUpdateDatasetBodyPreservesExplicitChunkStrategy(t *testing.T) {
	explicit := json.RawMessage(`{"layout_recognize":"OpenDataLoader"}`)
	body, err := buildUpdateDatasetBody(updateKnowledgeBaseRequest{ChunkStrategy: &explicit})
	if err != nil {
		t.Fatalf("buildUpdateDatasetBody: %v", err)
	}
	payload := decodeMap(t, body)
	cfg, ok := payload["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", payload["parser_config"])
	}
	if cfg["layout_recognize"] != ragflowLayoutOpenDataLoader {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
}

func TestBuildUpdateDatasetBodyRejectsInvalidChunkStrategy(t *testing.T) {
	explicit := json.RawMessage(`[]`)
	_, err := buildUpdateDatasetBody(updateKnowledgeBaseRequest{ChunkStrategy: &explicit})
	if err == nil {
		t.Fatal("buildUpdateDatasetBody returned nil error")
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeValidation {
		t.Fatalf("error=%v, want validation_error", err)
	}
	if appErr.Fields["chunkStrategy"] == "" {
		t.Fatalf("fields=%v", appErr.Fields)
	}
}

func TestRAGFlowParserConfigFromSnapshotMapsBackends(t *testing.T) {
	endpoint := "https://parser.internal/v1"
	tests := []struct {
		name              string
		snapshot          service.ParserConfigSnapshot
		wantLayout        string
		wantTokenFiltered bool
	}{
		{
			name: "builtin uses deepdoc",
			snapshot: service.ParserConfigSnapshot{
				ParserConfigID:        "parser_builtin",
				Backend:               service.ParserBackendBuiltin,
				Concurrency:           4,
				SupportedContentTypes: []string{"application/pdf"},
				DefaultParameters:     json.RawMessage(`{"chunk_token_num":1024}`),
			},
			wantLayout: ragflowLayoutDeepDOC,
		},
		{
			name: "local ocr uses paddleocr",
			snapshot: service.ParserConfigSnapshot{
				ParserConfigID:    "parser_local",
				Backend:           service.ParserBackendLocalOCR,
				Concurrency:       2,
				DefaultParameters: json.RawMessage(`{"chunk_token_num":768}`),
			},
			wantLayout: ragflowLayoutPaddleOCR,
		},
		{
			name: "remote compatible respects layoutRecognize parameter",
			snapshot: service.ParserConfigSnapshot{
				ParserConfigID:    "parser_remote",
				Backend:           service.ParserBackendRemoteCompatible,
				Concurrency:       8,
				EndpointURL:       &endpoint,
				DefaultParameters: json.RawMessage(`{"layoutRecognize":"MinerU","accessToken":"secret","chunk_token_num":2048}`),
			},
			wantLayout:        ragflowLayoutMinerU,
			wantTokenFiltered: true,
		},
		{
			name: "remote compatible defaults to paddleocr",
			snapshot: service.ParserConfigSnapshot{
				ParserConfigID:    "parser_remote_default",
				Backend:           service.ParserBackendRemoteCompatible,
				Concurrency:       4,
				DefaultParameters: json.RawMessage(`{"delimiter":"\n"}`),
			},
			wantLayout: ragflowLayoutPaddleOCR,
		},
		{
			name: "tika uses plain text",
			snapshot: service.ParserConfigSnapshot{
				ParserConfigID:    "parser_tika",
				Backend:           service.ParserBackendTika,
				Concurrency:       1,
				DefaultParameters: json.RawMessage(`{}`),
			},
			wantLayout: ragflowLayoutPlainText,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ragflowParserConfigFromSnapshot(tc.snapshot)
			if cfg["layout_recognize"] != tc.wantLayout {
				t.Fatalf("layout_recognize=%v want %s", cfg["layout_recognize"], tc.wantLayout)
			}
			trace, ok := cfg[parserConfigTraceKey].(map[string]any)
			if !ok {
				t.Fatalf("%s=%v", parserConfigTraceKey, cfg[parserConfigTraceKey])
			}
			if trace["backend"] != string(tc.snapshot.Backend) {
				t.Fatalf("trace backend=%v", trace["backend"])
			}
			if tc.snapshot.ParserConfigID != "" && trace["parserConfigId"] != tc.snapshot.ParserConfigID {
				t.Fatalf("trace parserConfigId=%v", trace["parserConfigId"])
			}
			if tc.wantTokenFiltered {
				if _, ok := cfg["accessToken"]; ok {
					t.Fatalf("sensitive accessToken should be filtered: %v", cfg)
				}
				if cfg["chunk_token_num"].(float64) != 2048 {
					t.Fatalf("chunk_token_num=%v", cfg["chunk_token_num"])
				}
				if trace["endpointUrl"] != endpoint {
					t.Fatalf("trace endpointUrl=%v", trace["endpointUrl"])
				}
			}
		})
	}
}

func TestRAGFlowParserConfigFromSnapshotSplitsPaddleOCRCloudCredentials(t *testing.T) {
	cfg := ragflowParserConfigFromSnapshot(service.ParserConfigSnapshot{
		ParserConfigID:    "parser_paddleocr",
		Backend:           service.ParserBackendPaddleOCRCloud,
		Concurrency:       4,
		DefaultParameters: json.RawMessage(`{"paddleocr_base_url":"https://paddleocr.example.com/api","paddleocr_access_token":"sk-secret","paddleocr_algorithm":"PaddleOCR-VL-1.6"}`),
	})

	if cfg["layout_recognize"] != ragflowLayoutPaddleOCR {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
	for _, key := range []string{"paddleocr_base_url", "paddleocr_access_token", "paddleocr_algorithm"} {
		if _, ok := cfg[key]; ok {
			t.Fatalf("ordinary parser config should not include PaddleOCR cloud key %q", key)
		}
	}
	credentials, ok := cfg[parserConfigCredentialsKey].(map[string]any)
	if !ok {
		t.Fatalf("%s missing", parserConfigCredentialsKey)
	}
	paddleOCR, ok := credentials["paddleocr_cloud"].(map[string]any)
	if !ok {
		t.Fatalf("paddleocr_cloud credentials missing")
	}
	if paddleOCR["paddleocr_base_url"] != "https://paddleocr.example.com/api" {
		t.Fatalf("paddleocr_base_url=%v", paddleOCR["paddleocr_base_url"])
	}
	if paddleOCR["paddleocr_access_token"] != "sk-secret" {
		t.Fatalf("paddleocr_access_token was not preserved in protected credentials")
	}
	if paddleOCR["paddleocr_algorithm"] != "PaddleOCR-VL-1.6" {
		t.Fatalf("paddleocr_algorithm=%v", paddleOCR["paddleocr_algorithm"])
	}
	trace, ok := cfg[parserConfigTraceKey].(map[string]any)
	if !ok {
		t.Fatalf("%s=%v", parserConfigTraceKey, cfg[parserConfigTraceKey])
	}
	if _, ok := trace["paddleocr_access_token"]; ok {
		t.Fatalf("trace leaked token: %v", trace)
	}
}

func TestBuildCreateDatasetBodySendsPaddleOCRCredentialsOutsideParserConfig(t *testing.T) {
	cfg := ragflowParserConfigFromSnapshot(service.ParserConfigSnapshot{
		ParserConfigID:    "parser_paddleocr",
		Backend:           service.ParserBackendPaddleOCRCloud,
		Concurrency:       4,
		DefaultParameters: json.RawMessage(`{"paddleocr_base_url":"https://paddleocr.example.com/api","paddleocr_access_token":"sk-secret","paddleocr_algorithm":"PaddleOCR-VL-1.6"}`),
	})

	body, err := buildCreateDatasetBody(createKnowledgeBaseRequest{Name: "Manuals"}, cfg, createDatasetOptions{})
	if err != nil {
		t.Fatalf("buildCreateDatasetBody: %v", err)
	}
	payload := decodeMap(t, body)
	parserConfig, ok := payload["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", payload["parser_config"])
	}
	if parserConfig["layout_recognize"] != ragflowLayoutPaddleOCR {
		t.Fatalf("layout_recognize=%v", parserConfig["layout_recognize"])
	}
	for _, key := range []string{"paddleocr_base_url", "paddleocr_access_token", "paddleocr_algorithm", parserConfigCredentialsKey} {
		if _, ok := parserConfig[key]; ok {
			t.Fatalf("parser_config leaked protected PaddleOCR key %q: %v", key, parserConfig)
		}
	}

	credentials, ok := payload["parser_config_credentials"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config_credentials missing")
	}
	paddleOCR, ok := credentials["paddleocr_cloud"].(map[string]any)
	if !ok {
		t.Fatalf("paddleocr_cloud credentials missing")
	}
	if paddleOCR["paddleocr_access_token"] != "sk-secret" {
		t.Fatalf("paddleocr_access_token was not preserved in protected credentials")
	}
	if paddleOCR["paddleocr_base_url"] != "https://paddleocr.example.com/api" {
		t.Fatalf("paddleocr_base_url=%v", paddleOCR["paddleocr_base_url"])
	}
	if paddleOCR["paddleocr_algorithm"] != "PaddleOCR-VL-1.6" {
		t.Fatalf("paddleocr_algorithm=%v", paddleOCR["paddleocr_algorithm"])
	}
}

func TestParserConfigResponseRedactsPaddleOCRToken(t *testing.T) {
	response := parserConfigFromDomain(service.ParserConfig{
		ID:                "parser_paddleocr",
		Name:              "PaddleOCR Cloud",
		Backend:           service.ParserBackendPaddleOCRCloud,
		Enabled:           true,
		Concurrency:       4,
		DefaultParameters: json.RawMessage(`{"paddleocr_base_url":"https://paddleocr.example.com/api","paddleocr_access_token":"sk-secret","paddleocr_algorithm":"PaddleOCR-VL"}`),
	})
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !response.PaddleOCRAccessTokenConfigured {
		t.Fatal("paddleocr token should be reported as configured")
	}
	if string(body) == "" || strings.Contains(string(body), "sk-secret") || strings.Contains(string(body), "paddleocr_access_token") {
		t.Fatalf("response leaked token: %s", body)
	}
}

func TestBuildRetrievalBodyForwardsSearchParams(t *testing.T) {
	rerankTopN := 5
	body, err := buildRetrievalBody(knowledgeQueryRequest{
		Query:            "maintenance",
		KnowledgeBaseIDs: []string{"kb_1"},
		DocumentIDs:      []string{"doc_1", "doc_2"},
		TopK:             8,
		ScoreThreshold:   ptrFloat64(0.4),
		Tags:             []string{"锅炉"},
		MetadataFilter:   map[string]string{"专业": "锅炉"},
		Rerank:           true,
		RerankTopN:       &rerankTopN,
	}, retrievalBuildOptions{VendorRerankID: "BAAI/bge-reranker-v2-m3@default@SILICONFLOW"})
	if err != nil {
		t.Fatalf("buildRetrievalBody: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["question"] != "maintenance" {
		t.Fatalf("question=%v", payload["question"])
	}
	if got, _ := payload["dataset_ids"].([]any); len(got) != 1 || got[0] != "kb_1" {
		t.Fatalf("dataset_ids=%v", payload["dataset_ids"])
	}
	if got, _ := payload["doc_ids"].([]any); len(got) != 2 {
		t.Fatalf("doc_ids=%v", payload["doc_ids"])
	}
	if payload["top_k"].(float64) != 8 {
		t.Fatalf("top_k=%v", payload["top_k"])
	}
	if payload["similarity_threshold"].(float64) != 0.4 {
		t.Fatalf("similarity_threshold=%v", payload["similarity_threshold"])
	}
	if payload["rerank_id"] != "BAAI/bge-reranker-v2-m3@default@SILICONFLOW" {
		t.Fatalf("rerank_id=%v", payload["rerank_id"])
	}
	if payload["size"].(float64) != 5 {
		t.Fatalf("size=%v", payload["size"])
	}

	filter, ok := payload["meta_data_filter"].(map[string]any)
	if !ok {
		t.Fatalf("meta_data_filter=%v", payload["meta_data_filter"])
	}
	if filter["method"] != "manual" {
		t.Fatalf("method=%v", filter["method"])
	}
	manual, ok := filter["manual"].([]any)
	if !ok || len(manual) != 2 {
		t.Fatalf("manual=%v", filter["manual"])
	}
}

func TestBuildRetrievalBodyMapsTopKToRuntimeSizeWithoutRerank(t *testing.T) {
	body, err := buildRetrievalBody(knowledgeQueryRequest{
		Query:            "maintenance",
		KnowledgeBaseIDs: []string{"kb_1"},
		TopK:             7,
	}, retrievalBuildOptions{})
	if err != nil {
		t.Fatalf("buildRetrievalBody: %v", err)
	}
	payload := decodeMap(t, body)
	if payload["top_k"].(float64) != 7 {
		t.Fatalf("top_k=%v", payload["top_k"])
	}
	if payload["size"].(float64) != 7 {
		t.Fatalf("size=%v", payload["size"])
	}
}

func TestBuildRetrievalBodyOmitsRerankWithoutVendorModel(t *testing.T) {
	body, err := buildRetrievalBody(knowledgeQueryRequest{
		Query:            "q",
		KnowledgeBaseIDs: []string{"kb_1"},
		Rerank:           true,
	}, retrievalBuildOptions{})
	if err != nil {
		t.Fatalf("buildRetrievalBody: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["rerank_id"]; ok {
		t.Fatalf("rerank_id should be omitted without vendor rerank id: %v", payload)
	}
}

func TestKnowledgeQueryTraceUsesConfiguredRuntimeValues(t *testing.T) {
	summary := knowledgeQueryFromVendor(
		"kq_test",
		"query",
		&vendorclient.RetrievalData{Total: 1, Chunks: []map[string]interface{}{{"id": "chunk_1"}}},
		8,
		0.4,
		true,
		ptrInt(5),
		knowledgeQueryTraceOptions{VendorEmbeddingID: "BAAI/bge-m3@SILICONFLOW"},
	)

	if summary.Trace.EmbeddingProvider != "runtime" {
		t.Fatalf("embeddingProvider=%q", summary.Trace.EmbeddingProvider)
	}
	if summary.Trace.EmbeddingModel != "BAAI/bge-m3@SILICONFLOW" {
		t.Fatalf("embeddingModel=%q", summary.Trace.EmbeddingModel)
	}
	if summary.Trace.EmbeddingModel == "vendor-default" {
		t.Fatalf("embeddingModel must not use fake default")
	}
	if summary.Trace.EmbeddingDimension == 0 {
		t.Fatalf("embeddingDimension must not claim zero as a runtime fact")
	}
	if summary.Trace.QdrantCollection == "elasticsearch" {
		t.Fatalf("qdrantCollection must not claim elasticsearch as a fact")
	}
	if summary.Trace.QdrantCollection != runtimeManagedTraceValue {
		t.Fatalf("qdrantCollection=%q", summary.Trace.QdrantCollection)
	}
}

func TestMapRetrievalChunkOmitsMissingChunkIndex(t *testing.T) {
	result := mapRetrievalChunk(map[string]interface{}{
		"id":                  "chunk_1",
		"content_with_weight": "content",
	})

	if result.ChunkIndex != nil {
		t.Fatalf("ChunkIndex=%v, want nil when vendor payload has no chunk index", *result.ChunkIndex)
	}
}

func TestMapRetrievalChunkKeepsContentPreviewUTF8Valid(t *testing.T) {
	result := mapRetrievalChunk(map[string]interface{}{
		"id":                  "chunk_1",
		"content_with_weight": strings.Repeat("电力A", 80),
	})

	if !utf8.ValidString(result.ContentPreview) {
		t.Fatalf("ContentPreview is not valid UTF-8: %q", result.ContentPreview)
	}
	if strings.ContainsRune(result.ContentPreview, utf8.RuneError) {
		t.Fatalf("ContentPreview contains replacement rune: %q", result.ContentPreview)
	}
	if len(result.ContentPreview) > 240 {
		t.Fatalf("ContentPreview length=%d, want <= 240 bytes", len(result.ContentPreview))
	}
}

func TestMapVendorErrorUsesStatusInsteadOfMessageMatching(t *testing.T) {
	notFound := mapVendorError(&vendorclient.APIError{
		HTTPStatus: 404,
		Message:    "vendor hid the details",
	})
	appErr, ok := service.Classify(notFound)
	if !ok || appErr.Code != service.CodeNotFound {
		t.Fatalf("status 404 mapped to %v, want not_found", notFound)
	}

	dependency := mapVendorError(&vendorclient.APIError{
		Code:    102,
		Message: "not found text inside a non-404 vendor error",
	})
	appErr, ok = service.Classify(dependency)
	if !ok || appErr.Code != service.CodeDependency {
		t.Fatalf("message-matched error mapped to %v, want dependency_error", dependency)
	}
}

func TestMapVendorErrorClassifiesRuntimeBodyCodes(t *testing.T) {
	validation := mapVendorError(&vendorclient.APIError{
		HTTPStatus: http.StatusOK,
		Code:       101,
		Message:    "Datasets use different embedding models.",
	})
	appErr, ok := service.Classify(validation)
	if !ok || appErr.Code != service.CodeValidation {
		t.Fatalf("body code 101 mapped to %v, want validation_error", validation)
	}

	notFound := mapVendorError(&vendorclient.APIError{
		HTTPStatus: http.StatusOK,
		Code:       404,
		Message:    "Dataset not found.",
	})
	appErr, ok = service.Classify(notFound)
	if !ok || appErr.Code != service.CodeNotFound {
		t.Fatalf("body code 404 mapped to %v, want not_found", notFound)
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func ptrInt(v int) *int {
	return &v
}

func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

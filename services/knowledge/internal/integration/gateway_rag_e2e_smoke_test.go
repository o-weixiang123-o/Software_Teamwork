package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

const gatewayRAGSmokeGate = "GATEWAY_RAG_E2E_SMOKE"

const (
	ragSmokeFixtureFilename = "gateway-rag-e2e-smoke.md"
	ragSmokeExpectedHit     = "calibrate relay RAG-E2E-304"
	ragSmokeQuestion        = "What calibration marker must be checked for the RAG E2E smoke?"
)

func TestGatewayRAGE2ESmoke(t *testing.T) {
	if os.Getenv(gatewayRAGSmokeGate) != "1" {
		t.Skip("set GATEWAY_RAG_E2E_SMOKE=1 to run the Gateway -> Knowledge -> QA RAG smoke")
	}

	cfg := loadGatewayRAGSmokeConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	assertGatewayRAGPrechecks(t, ctx, cfg)
	requestID := "req_gateway_rag_e2e_smoke_" + safeIdentifierSuffix(newSmokeRunID(t))
	session := createGatewaySession(t, ctx, cfg.gatewayOwnerSmokeConfig(), requestID+"_session")

	requestedKnowledgeBaseID := "kb_gateway_rag_smoke_" + safeIdentifierSuffix(newSmokeRunID(t))
	setCleanupIDs := cleanupGatewayRAGSmokeResources(t, cfg, session)
	createdKB := createGatewayKnowledgeBase(t, ctx, cfg.gatewayOwnerSmokeConfig(), session, requestID+"_kb", requestedKnowledgeBaseID)
	knowledgeBaseID := createdKB.ID
	setCleanupIDs(knowledgeBaseID, "")

	doc := uploadGatewayRAGDocument(t, ctx, cfg, session, requestID+"_upload", knowledgeBaseID)
	setCleanupIDs(knowledgeBaseID, doc.ID)
	readyDoc := waitForGatewayDocumentReady(t, ctx, cfg, session, requestID+"_document_ready", doc.ID)
	if readyDoc.ChunkCount <= 0 {
		t.Fatalf("Knowledge ingestion stage: document chunkCount = %d, want > 0", readyDoc.ChunkCount)
	}
	if strings.TrimSpace(readyDoc.ParserBackend) == "" {
		t.Fatal("Runtime stage: ready document did not record parserBackend")
	}

	query := createGatewayKnowledgeQuery(t, ctx, cfg, session, requestID+"_query", knowledgeBaseID)
	assertGatewayRAGQueryHit(t, query, knowledgeBaseID, readyDoc.ID)

	configureGatewayQAForRAG(t, ctx, cfg, session, requestID+"_qa_config", knowledgeBaseID)
	qaSession := createGatewayQASession(t, ctx, cfg, session, requestID+"_qa_session")
	answer := createGatewayQAMessage(t, ctx, cfg, session, requestID+"_qa_answer", qaSession.ID, knowledgeBaseID)
	assertGatewayQAAnswer(t, answer, knowledgeBaseID, readyDoc.ID)

	citations := listGatewayMessageCitations(t, ctx, cfg, session, requestID+"_citations", answer.AssistantMessage.ID)
	assertGatewayQACitations(t, citations, knowledgeBaseID, readyDoc.ID)
}

func cleanupGatewayRAGSmokeResources(t *testing.T, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession) func(string, string) {
	t.Helper()
	var knowledgeBaseID string
	var documentID string
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if strings.TrimSpace(documentID) != "" {
			deleteGatewayRAGDocument(cleanupCtx, t, cfg, session, "req_gateway_rag_e2e_cleanup_"+knowledgeBaseID, documentID)
		}
		if strings.TrimSpace(knowledgeBaseID) == "" {
			return
		}
		deleteGatewayKnowledgeBase(cleanupCtx, t, cfg.gatewayOwnerSmokeConfig(), session, "req_gateway_rag_e2e_kb_cleanup_"+knowledgeBaseID, knowledgeBaseID)
		if err := deleteGatewaySmokeKnowledgeBaseRows(cleanupCtx, cfg.gatewayOwnerSmokeConfig(), knowledgeBaseID); err != nil {
			t.Errorf("cleanup Gateway RAG smoke knowledge base %q: %v", knowledgeBaseID, err)
		}
	})
	return func(kbID string, docID string) {
		knowledgeBaseID = strings.TrimSpace(kbID)
		documentID = strings.TrimSpace(docID)
	}
}

type gatewayRAGSmokeConfig struct {
	gatewayBaseURL          string
	vendorRuntimeURL        string
	knowledgeServiceBaseURL string
	qaServiceBaseURL        string
	aiGatewayBaseURL        string
	knowledgeDatabaseURL    string
	redisAddr               string
	username                string
	password                string
	qaChatProfileID         string
	qaChatModel             string
	timeout                 time.Duration
}

func loadGatewayRAGSmokeConfig(t *testing.T) gatewayRAGSmokeConfig {
	t.Helper()
	required := map[string]string{
		"GATEWAY_BASE_URL":                               os.Getenv("GATEWAY_BASE_URL"),
		"VENDOR_RUNTIME_URL":                             os.Getenv("VENDOR_RUNTIME_URL"),
		"KNOWLEDGE_SERVICE_BASE_URL":                     os.Getenv("KNOWLEDGE_SERVICE_BASE_URL"),
		"QA_SERVICE_BASE_URL":                            os.Getenv("QA_SERVICE_BASE_URL"),
		"AI_GATEWAY_BASE_URL":                            os.Getenv("AI_GATEWAY_BASE_URL"),
		"KNOWLEDGE_TEST_DATABASE_URL":                    os.Getenv("KNOWLEDGE_TEST_DATABASE_URL"),
		"KNOWLEDGE_REDIS_ADDR":                           firstNonEmptyEnv("KNOWLEDGE_REDIS_ADDR", "GATEWAY_REDIS_ADDR"),
		"GATEWAY_SMOKE_USERNAME or LOCAL_ADMIN_USERNAME": firstNonEmptyEnv("GATEWAY_SMOKE_USERNAME", "LOCAL_ADMIN_USERNAME"),
		"GATEWAY_SMOKE_PASSWORD or LOCAL_ADMIN_PASSWORD": firstNonEmptyEnv("GATEWAY_SMOKE_PASSWORD", "LOCAL_ADMIN_PASSWORD"),
		"QA_SMOKE_CHAT_PROFILE_ID":                       os.Getenv("QA_SMOKE_CHAT_PROFILE_ID"),
		"QA_SMOKE_CHAT_MODEL":                            os.Getenv("QA_SMOKE_CHAT_MODEL"),
	}
	var missing []string
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("GATEWAY_RAG_E2E_SMOKE=1 requires %s", strings.Join(missing, ", "))
	}
	timeout := 2 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("GATEWAY_RAG_SMOKE_TIMEOUT")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			t.Fatalf("GATEWAY_RAG_SMOKE_TIMEOUT must be a positive duration")
		}
		timeout = value
	}
	return gatewayRAGSmokeConfig{
		gatewayBaseURL:          trimHTTPBaseURL(t, "GATEWAY_BASE_URL", required["GATEWAY_BASE_URL"]),
		vendorRuntimeURL:        trimHTTPBaseURL(t, "VENDOR_RUNTIME_URL", required["VENDOR_RUNTIME_URL"]),
		knowledgeServiceBaseURL: trimHTTPBaseURL(t, "KNOWLEDGE_SERVICE_BASE_URL", required["KNOWLEDGE_SERVICE_BASE_URL"]),
		qaServiceBaseURL:        trimHTTPBaseURL(t, "QA_SERVICE_BASE_URL", required["QA_SERVICE_BASE_URL"]),
		aiGatewayBaseURL:        trimHTTPBaseURL(t, "AI_GATEWAY_BASE_URL", required["AI_GATEWAY_BASE_URL"]),
		knowledgeDatabaseURL:    strings.TrimSpace(required["KNOWLEDGE_TEST_DATABASE_URL"]),
		redisAddr:               normalizeRedisAddr(t, required["KNOWLEDGE_REDIS_ADDR"]),
		username:                strings.TrimSpace(required["GATEWAY_SMOKE_USERNAME or LOCAL_ADMIN_USERNAME"]),
		password:                strings.TrimSpace(required["GATEWAY_SMOKE_PASSWORD or LOCAL_ADMIN_PASSWORD"]),
		qaChatProfileID:         strings.TrimSpace(required["QA_SMOKE_CHAT_PROFILE_ID"]),
		qaChatModel:             strings.TrimSpace(required["QA_SMOKE_CHAT_MODEL"]),
		timeout:                 timeout,
	}
}

func (cfg gatewayRAGSmokeConfig) gatewayOwnerSmokeConfig() gatewayOwnerSmokeConfig {
	return gatewayOwnerSmokeConfig{
		gatewayBaseURL:          cfg.gatewayBaseURL,
		knowledgeServiceBaseURL: cfg.knowledgeServiceBaseURL,
		knowledgeDatabaseURL:    cfg.knowledgeDatabaseURL,
		redisAddr:               cfg.redisAddr,
		username:                cfg.username,
		password:                cfg.password,
	}
}

func assertGatewayRAGPrechecks(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig) {
	t.Helper()
	assertRuntimeHealthy(t, ctx, cfg.vendorRuntimeURL)
	assertPostgresReady(t, ctx, cfg.knowledgeDatabaseURL)
	assertRedisReady(t, ctx, cfg.redisAddr)
	assertHTTPReady(t, ctx, "knowledge", cfg.knowledgeServiceBaseURL)
	assertHTTPReady(t, ctx, "qa", cfg.qaServiceBaseURL)
	assertHTTPReady(t, ctx, "ai-gateway", cfg.aiGatewayBaseURL)
	assertHTTPReady(t, ctx, "gateway", cfg.gatewayBaseURL)
}

func assertRuntimeHealthy(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/system/healthz", nil)
	if err != nil {
		t.Fatalf("build knowledge runtime health request: %v", err)
	}
	req.Header.Set("X-Request-Id", "req_gateway_rag_runtime_precheck")
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("knowledge runtime healthz request failed: %v", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		t.Fatalf("knowledge runtime healthz returned HTTP %d", res.StatusCode)
	}
}

func deleteGatewayRAGDocument(ctx context.Context, t *testing.T, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, documentID string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, cfg.gatewayBaseURL+"/api/v1/documents/"+url.PathEscape(documentID), nil)
	if err != nil {
		t.Errorf("cleanup Gateway RAG document: build delete request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Errorf("cleanup Gateway RAG document %q: request failed: %v", documentID, err)
		return
	}
	defer res.Body.Close()
	discardResponse(res.Body)
	switch res.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
		return
	default:
		t.Errorf("cleanup Gateway RAG document %q: DELETE returned HTTP %d", documentID, res.StatusCode)
	}
}

type gatewayRAGDocument struct {
	ID            string
	Status        string
	ChunkCount    int64
	ParserBackend string
}

func uploadGatewayRAGDocument(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, knowledgeBaseID string) gatewayRAGDocument {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", ragSmokeFixtureFilename)
	if err != nil {
		t.Fatalf("Knowledge upload stage: create multipart file part: %v", err)
	}
	if _, err := part.Write([]byte(gatewayRAGFixtureText())); err != nil {
		t.Fatalf("Knowledge upload stage: write multipart fixture: %v", err)
	}
	for _, tag := range []string{"rag-smoke", "issue-304"} {
		if err := writer.WriteField("tags", tag); err != nil {
			t.Fatalf("Knowledge upload stage: write tags field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Knowledge upload stage: close multipart writer: %v", err)
	}

	endpoint := cfg.gatewayBaseURL + "/api/v1/knowledge-bases/" + url.PathEscape(knowledgeBaseID) + "/documents"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		t.Fatalf("Knowledge upload stage: build upload request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Request-Id", requestID)

	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge upload stage: gateway document upload request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		discardResponse(res.Body)
		t.Fatalf("Knowledge upload stage: gateway document upload returned HTTP %d", res.StatusCode)
	}
	return decodeGatewayDocumentResponse(t, res.Body, requestID)
}

func gatewayRAGFixtureText() string {
	return "# Gateway RAG E2E smoke\n\n" +
		"Operators must calibrate relay RAG-E2E-304 before energizing the bay. " +
		"The expected citation should point at this paragraph and no provider secrets are part of the sample.\n"
}

func waitForGatewayDocumentReady(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, documentID string) gatewayRAGDocument {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Minute)
	}
	var last gatewayRAGDocument
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		doc := getGatewayDocument(t, ctx, cfg, session, fmt.Sprintf("%s_%02d", requestID, attempt), documentID)
		last = doc
		switch doc.Status {
		case "ready":
			return doc
		case "failed":
			t.Fatalf("Knowledge ingestion stage: document %s failed during ingestion", documentID)
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Knowledge ingestion stage: document %s did not become ready before timeout; last status=%q chunkCount=%d", documentID, last.Status, last.ChunkCount)
	return gatewayRAGDocument{}
}

func getGatewayDocument(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, documentID string) gatewayRAGDocument {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.gatewayBaseURL+"/api/v1/documents/"+url.PathEscape(documentID), nil)
	if err != nil {
		t.Fatalf("Knowledge ingestion stage: build document get request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge ingestion stage: gateway document get request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		discardResponse(res.Body)
		t.Fatalf("Knowledge ingestion stage: gateway document get returned HTTP %d", res.StatusCode)
	}
	return decodeGatewayDocumentResponse(t, res.Body, requestID)
}

func decodeGatewayDocumentResponse(t *testing.T, body io.Reader, requestID string) gatewayRAGDocument {
	t.Helper()
	var decoded struct {
		Data struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			ChunkCount    int64  `json:"chunkCount"`
			ParserBackend string `json:"parserBackend"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&decoded); err != nil {
		t.Fatalf("Knowledge stage: decode document response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge stage: document response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	if strings.TrimSpace(decoded.Data.ID) == "" {
		t.Fatal("Knowledge stage: document response id is empty")
	}
	return gatewayRAGDocument{
		ID:            strings.TrimSpace(decoded.Data.ID),
		Status:        strings.TrimSpace(decoded.Data.Status),
		ChunkCount:    decoded.Data.ChunkCount,
		ParserBackend: strings.TrimSpace(decoded.Data.ParserBackend),
	}
}

type gatewayKnowledgeQuery struct {
	Results []gatewayKnowledgeQueryResult
	Trace   gatewayKnowledgeQueryTrace
}

type gatewayKnowledgeQueryResult struct {
	KnowledgeBaseID string  `json:"knowledgeBaseId"`
	DocumentID      string  `json:"documentId"`
	ChunkID         string  `json:"chunkId"`
	DocumentName    string  `json:"documentName"`
	ContentPreview  string  `json:"contentPreview"`
	Score           float64 `json:"score"`
}

type gatewayKnowledgeQueryTrace struct {
	HitCount           int    `json:"hitCount"`
	Rerank             bool   `json:"rerank"`
	EmbeddingProvider  string `json:"embeddingProvider"`
	EmbeddingModel     string `json:"embeddingModel"`
	EmbeddingDimension int    `json:"embeddingDimension"`
}

func createGatewayKnowledgeQuery(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, knowledgeBaseID string) gatewayKnowledgeQuery {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"query":            ragSmokeQuestion,
		"knowledgeBaseIds": []string{knowledgeBaseID},
		"topK":             3,
		"scoreThreshold":   0,
		"rerank":           true,
		"rerankTopN":       1,
	})
	if err != nil {
		t.Fatalf("Knowledge retrieval stage: encode query request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.gatewayBaseURL+"/api/v1/knowledge-queries", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Knowledge retrieval stage: build query request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)

	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Knowledge retrieval stage: gateway knowledge query request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		discardResponse(res.Body)
		t.Fatalf("Knowledge retrieval stage: gateway knowledge query returned HTTP %d", res.StatusCode)
	}
	var decoded struct {
		Data struct {
			Results []gatewayKnowledgeQueryResult `json:"results"`
			Trace   gatewayKnowledgeQueryTrace    `json:"trace"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&decoded); err != nil {
		t.Fatalf("Knowledge retrieval stage: decode query response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("Knowledge retrieval stage: query response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	return gatewayKnowledgeQuery{Results: decoded.Data.Results, Trace: decoded.Data.Trace}
}

func assertGatewayRAGQueryHit(t *testing.T, query gatewayKnowledgeQuery, knowledgeBaseID string, documentID string) {
	t.Helper()
	if query.Trace.HitCount == 0 || len(query.Results) == 0 {
		t.Fatalf("Knowledge retrieval stage: hitCount=%d len(results)=%d, want at least one hit", query.Trace.HitCount, len(query.Results))
	}
	if !query.Trace.Rerank {
		t.Fatal("Knowledge retrieval stage: rerank trace is false, want true to prove rerank/fallback path")
	}
	if strings.TrimSpace(query.Trace.EmbeddingProvider) == "" || strings.TrimSpace(query.Trace.EmbeddingModel) == "" || query.Trace.EmbeddingDimension <= 0 {
		t.Fatalf("Knowledge retrieval stage: invalid embedding trace: %+v", query.Trace)
	}
	for _, result := range query.Results {
		if result.KnowledgeBaseID == knowledgeBaseID && result.DocumentID == documentID && strings.Contains(result.ContentPreview, ragSmokeExpectedHit) {
			if strings.TrimSpace(result.ChunkID) == "" {
				t.Fatal("Knowledge retrieval stage: expected hit has empty chunkId")
			}
			return
		}
	}
	t.Fatalf("Knowledge retrieval stage: expected hit %q for kb=%s doc=%s was not returned", ragSmokeExpectedHit, knowledgeBaseID, documentID)
}

func configureGatewayQAForRAG(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, knowledgeBaseID string) {
	t.Helper()
	activeQAConfigs := snapshotGatewayQAActiveConfigs(t, ctx, cfg, session, requestID+"_snapshot")
	createGatewayQALLMConfig(t, ctx, cfg, session, requestID+"_llm")
	cleanupGatewayQAActiveConfigs(t, cfg, session, requestID+"_restore", activeQAConfigs)
	createGatewayQAConfig(t, ctx, cfg, session, requestID+"_retrieval", knowledgeBaseID)
}

type gatewayQAActiveConfigs struct {
	LLM gatewayQALLMConfigVersion
	QA  gatewayQAConfigVersion
}

type gatewayQALLMConfigVersion struct {
	Provider       string  `json:"provider"`
	ProfileID      string  `json:"profileId"`
	ModelName      string  `json:"modelName"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"maxTokens"`
}

type gatewayQAConfigVersion struct {
	DefaultKnowledgeBaseIDs []string                       `json:"defaultKnowledgeBaseIds"`
	KnowledgeBases          []gatewayQAConfigKnowledgeBase `json:"knowledgeBases"`
	Retrieval               gatewayQARetrievalSettings     `json:"retrieval"`
	Agent                   gatewayQAAgentConfig           `json:"agent"`
}

type gatewayQAConfigKnowledgeBase struct {
	ID          string `json:"id"`
	Type        string `json:"type,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	SortOrder   int    `json:"sortOrder,omitempty"`
}

type gatewayQARetrievalSettings struct {
	TopK            int     `json:"topK"`
	ScoreThreshold  float64 `json:"scoreThreshold"`
	EnableRerank    bool    `json:"enableRerank"`
	RerankThreshold float64 `json:"rerankThreshold"`
	RerankTopN      int     `json:"rerankTopN"`
}

type gatewayQAAgentConfig struct {
	MaxIterations         int      `json:"maxIterations"`
	ToolTimeoutSeconds    int      `json:"toolTimeoutSeconds"`
	ModelTimeoutSeconds   int      `json:"modelTimeoutSeconds"`
	OverallTimeoutSeconds int      `json:"overallTimeoutSeconds"`
	EnabledToolNames      []string `json:"enabledToolNames"`
}

func snapshotGatewayQAActiveConfigs(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string) gatewayQAActiveConfigs {
	t.Helper()
	llm := getGatewayQALLMConfig(t, ctx, cfg, session, requestID+"_llm")
	qa := getGatewayQAConfig(t, ctx, cfg, session, requestID+"_qa")
	return gatewayQAActiveConfigs{LLM: llm, QA: qa}
}

func cleanupGatewayQAActiveConfigs(t *testing.T, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, configs gatewayQAActiveConfigs) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := restoreGatewayQALLMConfig(cleanupCtx, cfg, session, requestID+"_llm", configs.LLM); err != nil {
			t.Errorf("cleanup Gateway RAG smoke LLM config: %v", err)
		}
		if err := restoreGatewayQAConfig(cleanupCtx, cfg, session, requestID+"_qa", configs.QA); err != nil {
			t.Errorf("cleanup Gateway RAG smoke QA config: %v", err)
		}
	})
}

func getGatewayQALLMConfig(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string) gatewayQALLMConfigVersion {
	t.Helper()
	var decoded struct {
		Data      gatewayQALLMConfigVersion `json:"data"`
		RequestID string                    `json:"requestId"`
	}
	getGatewayJSON(t, ctx, cfg, session, requestID, "/api/v1/llm-config-versions/current", &decoded, "QA stage")
	if strings.TrimSpace(decoded.Data.Provider) == "" || strings.TrimSpace(decoded.Data.ModelName) == "" {
		t.Fatal("QA stage: active LLM config response is missing provider or modelName")
	}
	return decoded.Data
}

func getGatewayQAConfig(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string) gatewayQAConfigVersion {
	t.Helper()
	var decoded struct {
		Data      gatewayQAConfigVersion `json:"data"`
		RequestID string                 `json:"requestId"`
	}
	getGatewayJSON(t, ctx, cfg, session, requestID, "/api/v1/qa-config-versions/current", &decoded, "QA stage")
	if decoded.Data.Retrieval.TopK <= 0 {
		t.Fatal("QA stage: active QA config response is missing retrieval.topK; run the local QA seed or create an active QA config before this smoke")
	}
	return decoded.Data
}

func getGatewayJSON(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, path string, target any, stage string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.gatewayBaseURL+path, nil)
	if err != nil {
		t.Fatalf("%s: build GET %s request: %v", stage, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("%s: gateway GET %s request failed: %v", stage, path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		discardResponse(res.Body)
		t.Fatalf("%s: gateway GET %s returned HTTP %d, want 200; run the local QA seed or create active QA/LLM config before this smoke", stage, path, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(target); err != nil {
		t.Fatalf("%s: decode GET %s response: %v", stage, path, err)
	}
	requestIDValue := extractGatewayResponseRequestID(t, target)
	if requestIDValue != requestID {
		t.Fatalf("%s: GET %s response requestId = %q, want %q", stage, path, requestIDValue, requestID)
	}
}

func extractGatewayResponseRequestID(t *testing.T, target any) string {
	t.Helper()
	data, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("QA stage: re-encode gateway response envelope: %v", err)
	}
	var envelope struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("QA stage: decode gateway response requestId: %v", err)
	}
	return strings.TrimSpace(envelope.RequestID)
}

func createGatewayQALLMConfig(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"provider":       "ai-gateway",
		"profileId":      cfg.qaChatProfileID,
		"modelName":      cfg.qaChatModel,
		"timeoutSeconds": 60,
		"maxTokens":      512,
		"activate":       true,
	})
	if err != nil {
		t.Fatalf("QA stage: encode LLM config request: %v", err)
	}
	postGatewayJSON(t, ctx, cfg, session, requestID, "/api/v1/llm-config-versions", payload, http.StatusCreated, "QA stage")
}

func createGatewayQAConfig(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, knowledgeBaseID string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"defaultKnowledgeBaseIds": []string{knowledgeBaseID},
		"knowledgeBases": []map[string]any{{
			"id":          knowledgeBaseID,
			"type":        "smoke",
			"displayName": "Gateway RAG E2E smoke",
			"sortOrder":   1,
		}},
		"retrieval": map[string]any{
			"topK":           3,
			"scoreThreshold": 0,
			"enableRerank":   true,
			"rerankTopN":     1,
		},
		"agent": map[string]any{
			"enabledToolNames": []string{"search_knowledge"},
			"maxIterations":    4,
		},
		"maxIterations":         4,
		"toolTimeoutSeconds":    30,
		"modelTimeoutSeconds":   60,
		"overallTimeoutSeconds": 90,
		"enabledToolNames":      []string{"search_knowledge"},
		"activate":              true,
	})
	if err != nil {
		t.Fatalf("QA stage: encode QA config request: %v", err)
	}
	postGatewayJSON(t, ctx, cfg, session, requestID, "/api/v1/qa-config-versions", payload, http.StatusCreated, "QA stage")
}

func restoreGatewayQALLMConfig(ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, config gatewayQALLMConfigVersion) error {
	payload, err := json.Marshal(map[string]any{
		"provider":       config.Provider,
		"profileId":      config.ProfileID,
		"modelName":      config.ModelName,
		"timeoutSeconds": config.TimeoutSeconds,
		"temperature":    config.Temperature,
		"maxTokens":      config.MaxTokens,
		"activate":       true,
	})
	if err != nil {
		return fmt.Errorf("encode LLM config restore request: %w", err)
	}
	return postGatewayJSONRequest(ctx, cfg, session, requestID, "/api/v1/llm-config-versions", payload, http.StatusCreated, "QA cleanup")
}

func restoreGatewayQAConfig(ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, config gatewayQAConfigVersion) error {
	payload, err := json.Marshal(map[string]any{
		"defaultKnowledgeBaseIds": config.DefaultKnowledgeBaseIDs,
		"knowledgeBases":          config.KnowledgeBases,
		"retrieval":               config.Retrieval,
		"agent":                   config.Agent,
		"activate":                true,
	})
	if err != nil {
		return fmt.Errorf("encode QA config restore request: %w", err)
	}
	return postGatewayJSONRequest(ctx, cfg, session, requestID, "/api/v1/qa-config-versions", payload, http.StatusCreated, "QA cleanup")
}

func postGatewayJSON(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, path string, payload []byte, wantStatus int, stage string) {
	t.Helper()
	if err := postGatewayJSONRequest(ctx, cfg, session, requestID, path, payload, wantStatus, stage); err != nil {
		t.Fatal(err)
	}
}

func postGatewayJSONRequest(ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, path string, payload []byte, wantStatus int, stage string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.gatewayBaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%s: build POST %s request: %w", stage, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("%s: gateway POST %s request failed: %w", stage, path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != wantStatus {
		discardResponse(res.Body)
		return fmt.Errorf("%s: gateway POST %s returned HTTP %d, want %d; check QA_SETTINGS_OPEN or qa:settings permissions and AI Gateway profile/model exact-match", stage, path, res.StatusCode, wantStatus)
	}
	discardResponse(res.Body)
	return nil
}

type gatewayQASession struct {
	ID string
}

func createGatewayQASession(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string) gatewayQASession {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"title": "Gateway RAG E2E smoke"})
	if err != nil {
		t.Fatalf("QA stage: encode session request: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.gatewayBaseURL+"/api/v1/qa-sessions", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("QA stage: build session request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("QA stage: gateway QA session request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		discardResponse(res.Body)
		t.Fatalf("QA stage: gateway QA session returned HTTP %d", res.StatusCode)
	}
	var decoded struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&decoded); err != nil {
		t.Fatalf("QA stage: decode session response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("QA stage: session response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	if strings.TrimSpace(decoded.Data.ID) == "" {
		t.Fatal("QA stage: session response id is empty")
	}
	return gatewayQASession{ID: strings.TrimSpace(decoded.Data.ID)}
}

type gatewayQAAnswer struct {
	AssistantMessage gatewayQAMessage
	Citations        []gatewayQACitation
	ResponseRun      gatewayQAResponseRun
}

type gatewayQAMessage struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
	Role    string `json:"role"`
}

type gatewayQAResponseRun struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type gatewayQACitation struct {
	ID              string  `json:"id"`
	MessageID       string  `json:"messageId"`
	CitationNo      int     `json:"citationNo"`
	KnowledgeBaseID string  `json:"knowledgeBaseId"`
	DocumentID      string  `json:"documentId"`
	ChunkID         string  `json:"chunkId"`
	DocumentName    string  `json:"documentName"`
	ContentPreview  string  `json:"contentPreview"`
	Text            string  `json:"text"`
	Score           float64 `json:"score"`
}

func createGatewayQAMessage(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, qaSessionID string, knowledgeBaseID string) gatewayQAAnswer {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"message":          ragSmokeQuestion,
		"mode":             "knowledge_qa",
		"knowledgeBaseIds": []string{knowledgeBaseID},
		"retrieval": map[string]any{
			"topK":           3,
			"scoreThreshold": 0,
			"enableRerank":   true,
			"rerankTopN":     1,
		},
	})
	if err != nil {
		t.Fatalf("QA stage: encode message request: %v", err)
	}
	endpoint := cfg.gatewayBaseURL + "/api/v1/qa-sessions/" + url.PathEscape(qaSessionID) + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("QA stage: build message request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("QA stage: gateway QA message request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		discardResponse(res.Body)
		t.Fatalf("QA stage: gateway QA message returned HTTP %d; check AI Gateway profile/provider availability and QA runtime config", res.StatusCode)
	}
	var decoded struct {
		Data struct {
			AssistantMessage gatewayQAMessage     `json:"assistantMessage"`
			Citations        []gatewayQACitation  `json:"citations"`
			ResponseRun      gatewayQAResponseRun `json:"responseRun"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&decoded); err != nil {
		t.Fatalf("QA stage: decode answer response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("QA stage: answer response requestId = %q, want %q", decoded.RequestID, requestID)
	}
	return gatewayQAAnswer{
		AssistantMessage: decoded.Data.AssistantMessage,
		Citations:        decoded.Data.Citations,
		ResponseRun:      decoded.Data.ResponseRun,
	}
}

func assertGatewayQAAnswer(t *testing.T, answer gatewayQAAnswer, knowledgeBaseID string, documentID string) {
	t.Helper()
	if answer.ResponseRun.Status != "completed" {
		t.Fatalf("QA stage: response run status = %q, want completed", answer.ResponseRun.Status)
	}
	if answer.AssistantMessage.Status != "completed" {
		t.Fatalf("QA stage: assistant message status = %q, want completed", answer.AssistantMessage.Status)
	}
	if strings.TrimSpace(answer.AssistantMessage.Content) == "" {
		t.Fatal("QA stage: assistant answer is empty")
	}
	if !strings.Contains(strings.ToLower(answer.AssistantMessage.Content), "rag-e2e-304") {
		t.Fatalf("QA stage: assistant answer did not mention expected marker %q", "RAG-E2E-304")
	}
	assertGatewayQACitations(t, answer.Citations, knowledgeBaseID, documentID)
}

func listGatewayMessageCitations(t *testing.T, ctx context.Context, cfg gatewayRAGSmokeConfig, session gatewaySmokeSession, requestID string, messageID string) []gatewayQACitation {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.gatewayBaseURL+"/api/v1/messages/"+url.PathEscape(messageID)+"/citations", nil)
	if err != nil {
		t.Fatalf("QA citation stage: build citation list request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	req.Header.Set("X-Request-Id", requestID)
	res, err := smokeHTTPClient().Do(req)
	if err != nil {
		t.Fatalf("QA citation stage: gateway citation list request failed: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		discardResponse(res.Body)
		t.Fatalf("QA citation stage: gateway citation list returned HTTP %d", res.StatusCode)
	}
	var decoded struct {
		Data      []gatewayQACitation `json:"data"`
		RequestID string              `json:"requestId"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&decoded); err != nil {
		t.Fatalf("QA citation stage: decode citation list response: %v", err)
	}
	if strings.TrimSpace(decoded.RequestID) != requestID {
		t.Fatalf("QA citation stage: citation list requestId = %q, want %q", decoded.RequestID, requestID)
	}
	return decoded.Data
}

func assertGatewayQACitations(t *testing.T, citations []gatewayQACitation, knowledgeBaseID string, documentID string) {
	t.Helper()
	if len(citations) == 0 {
		t.Fatal("QA citation stage: no citations returned")
	}
	for _, citation := range citations {
		if citation.KnowledgeBaseID == knowledgeBaseID && citation.DocumentID == documentID {
			if strings.TrimSpace(citation.ChunkID) == "" {
				t.Fatal("QA citation stage: matching citation has empty chunkId")
			}
			if !strings.Contains(citation.ContentPreview+":"+citation.Text, ragSmokeExpectedHit) {
				t.Fatalf("QA citation stage: matching citation does not contain expected hit %q", ragSmokeExpectedHit)
			}
			return
		}
	}
	t.Fatalf("QA citation stage: no citation matched kb=%s doc=%s", knowledgeBaseID, documentID)
}

func discardResponse(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1024))
}

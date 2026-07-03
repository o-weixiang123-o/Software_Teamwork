package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapter"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapterconfig"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/aigateway"
	kmcp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/mcp"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

const testServiceToken = "test-service-token"

type fakeVendorState struct {
	mu         sync.Mutex
	chunks     []map[string]any
	datasets   map[string]map[string]any
	documents  map[string]map[string]any
	docContent map[string][]byte
	parseCalls []string
	searchBody []byte
}

func newFakeVendorState() *fakeVendorState {
	return &fakeVendorState{
		datasets:   map[string]map[string]any{},
		documents:  map[string]map[string]any{},
		docContent: map[string][]byte{},
	}
}

func startFakeVendor(t *testing.T, state *fakeVendorState) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/ping":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("pong"))
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/datasets":
			items := make([]map[string]any, 0, len(state.datasets))
			for _, item := range state.datasets {
				items = append(items, item)
			}
			sort.Slice(items, func(i, j int) bool {
				return stringField(items[i], "id") < stringField(items[j], "id")
			})
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": items, "total_datasets": len(items)})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := "kb_fake_" + itoa(len(state.datasets)+1)
			item := map[string]any{
				"id": id, "name": body["name"], "description": body["description"],
				"chunk_method": "naive", "document_count": 0, "chunk_count": 0,
				"create_time": float64(1700000000000),
			}
			state.datasets[id] = item
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": item})
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && !strings.Contains(r.URL.Path, "/documents"):
			kbID := strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/")
			item, ok := state.datasets[kbID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": item})
			return
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/documents/parse"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ids := documentIDsFromParseBody(body)
			for _, id := range ids {
				state.parseCalls = append(state.parseCalls, id)
				if doc, ok := state.documents[id]; ok {
					doc["run"] = "RUNNING"
				}
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"started": len(ids)}})
			return
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/documents") && strings.Contains(r.URL.RawQuery, "type=local"):
			kbID := strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/")
			kbID = strings.TrimSuffix(kbID, "/documents")
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			content, _ := io.ReadAll(file)
			_ = file.Close()
			docID := "doc_fake_" + itoa(len(state.documents)+1)
			doc := map[string]any{
				"id": docID, "kb_id": kbID, "dataset_id": kbID, "name": header.Filename,
				"type": "txt", "size": header.Size, "run": "UNSTART", "chunk_count": 0,
			}
			state.documents[docID] = doc
			state.docContent[docID] = content
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": doc})
			return
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/documents") && strings.Contains(r.URL.Path, "/chunks"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/"), "/")
			if len(parts) == 4 && parts[1] == "documents" && parts[3] == "chunks" {
				kbID := parts[0]
				docID := parts[2]
				items := make([]map[string]any, 0)
				for _, chunk := range state.chunks {
					if stringField(chunk, "kb_id", "dataset_id") == kbID && stringField(chunk, "doc_id", "document_id") == docID {
						items = append(items, chunk)
					}
				}
				writeVendorJSON(w, http.StatusOK, map[string]any{
					"code": 0,
					"data": map[string]any{
						"total":  len(items),
						"chunks": items,
					},
				})
				return
			}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/documents") && !strings.Contains(r.URL.Path, "/chunks"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/"), "/")
			if len(parts) == 2 && parts[1] == "documents" {
				kbID := parts[0]
				items := make([]map[string]any, 0)
				for _, doc := range state.documents {
					if stringField(doc, "kb_id", "dataset_id") == kbID {
						items = append(items, doc)
					}
				}
				writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"total": len(items), "docs": items}})
				return
			}
			docID := strings.TrimPrefix(r.URL.Path, "/api/v1/documents/")
			doc, ok := state.documents[docID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": doc})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets/search":
			state.searchBody, _ = io.ReadAll(r.Body)
			writeVendorJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{
					"total":  len(state.chunks),
					"chunks": state.chunks,
				},
			})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func stringField(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func documentIDsFromParseBody(body map[string]any) []string {
	if raw, ok := body["document_ids"].([]any); ok {
		return anyStrings(raw)
	}
	if raw, ok := body["documents"].([]any); ok {
		return anyStrings(raw)
	}
	return nil
}

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func writeVendorJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func connectInMemory(t *testing.T, adapterServer *adapter.Server, caller kmcp.CallerContext, chatClient *aigateway.ChatClient) *sdkmcp.ClientSession {
	t.Helper()
	if caller.ServiceToken == "" {
		caller.ServiceToken = testServiceToken
	}
	server := kmcp.NewInMemoryServer(adapterServer, caller, chatClient)
	transport1, transport2 := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), transport1, nil); err != nil {
		t.Fatalf("connect MCP server: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	session, err := client.Connect(ctx, transport2, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestToolsListReturnsV1Catalog(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		RequestID:   "req_test",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("Tools(): %v", err)
		}
		names = append(names, tool.Name)
		if tool.InputSchema == nil {
			t.Fatalf("tool %q missing input schema", tool.Name)
		}
	}

	want := kmcp.ToolCatalog()
	if len(names) != len(want) {
		t.Fatalf("tool count=%d want %d: %v", len(names), len(want), names)
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, name := range want {
		wantSet[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := wantSet[name]; !ok {
			t.Fatalf("unexpected tool %q in %v", name, names)
		}
	}
}

func TestSearchKnowledgeReturnsAdapterResults(t *testing.T) {
	state := newFakeVendorState()
	state.chunks = []map[string]any{
		{
			"similarity":          0.91,
			"kb_id":               "kb_test",
			"doc_id":              "doc_test",
			"chunk_id":            "chunk_test",
			"docnm_kwd":           "Manual.pdf",
			"content_with_weight": "Transformer maintenance checklist",
		},
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		RequestID:   "req_search",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "search",
		Arguments: map[string]any{
			"query":            "maintenance checklist",
			"knowledgeBaseIds": []any{"kb_test"},
			"topK":             5,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}

	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var output struct {
		QueryID string `json:"queryId"`
		Results []struct {
			Score           float64 `json:"score"`
			KnowledgeBaseID string  `json:"knowledgeBaseId"`
			DocumentID      string  `json:"documentId"`
			ChunkID         string  `json:"chunkId"`
			DocumentName    string  `json:"documentName"`
			ContentPreview  string  `json:"contentPreview"`
			Content         string  `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("decode output: %v body=%s", err, string(payload))
	}
	if !strings.HasPrefix(output.QueryID, "kq_") {
		t.Fatalf("queryId=%q", output.QueryID)
	}
	if len(output.Results) != 1 {
		t.Fatalf("results=%+v", output.Results)
	}
	got := output.Results[0]
	if got.Score != 0.91 || got.KnowledgeBaseID != "kb_test" || got.DocumentID != "doc_test" || got.ChunkID != "chunk_test" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if got.DocumentName != "Manual.pdf" {
		t.Fatalf("documentName=%q", got.DocumentName)
	}
	if !strings.Contains(got.ContentPreview, "Transformer maintenance") {
		t.Fatalf("contentPreview=%q", got.ContentPreview)
	}
	if got.Content != got.ContentPreview {
		t.Fatalf("content=%q preview=%q", got.Content, got.ContentPreview)
	}
}

func TestSearchKnowledgeExpandsAccessibleKnowledgeBasesWhenOmitted(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_test"] = map[string]any{"id": "kb_test", "name": "Safety KB"}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "search",
		Arguments: map[string]any{
			"query": "search all accessible knowledge",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(state.searchBody, &payload); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	datasetIDs, _ := payload["dataset_ids"].([]any)
	if len(datasetIDs) != 1 || datasetIDs[0] != "kb_test" {
		t.Fatalf("dataset_ids=%v", payload["dataset_ids"])
	}
}

func TestSearchKnowledgeWithDocumentIDs(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		RequestID:   "req_search_docids",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "search",
		Arguments: map[string]any{
			"query":            "maintenance checklist",
			"knowledgeBaseIds": []any{"kb_test"},
			"documentIds":      []any{"doc_test"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
}

func TestGetChunkReturnsSingleChunkByDocumentAndChunkID(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_test"] = map[string]any{"id": "kb_test", "name": "Safety KB"}
	state.documents["doc_test"] = map[string]any{
		"id": "doc_test", "kb_id": "kb_test", "dataset_id": "kb_test", "name": "Manual.pdf", "run": "DONE",
	}
	state.chunks = []map[string]any{
		{
			"id":                  "chunk_other",
			"kb_id":               "kb_test",
			"doc_id":              "doc_test",
			"content_with_weight": "Other chunk",
		},
		{
			"id":                  "chunk_test",
			"kb_id":               "kb_test",
			"doc_id":              "doc_test",
			"content_with_weight": "Transformer maintenance full context",
			"chunk_index":         2,
		},
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		RequestID:   "req_get_chunk",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "get_chunk",
		Arguments: map[string]any{
			"knowledgeBaseId": "kb_test",
			"documentId":      "doc_test",
			"chunkId":         "chunk_test",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var output struct {
		ID              string `json:"id"`
		ChunkID         string `json:"chunkId"`
		DocumentID      string `json:"documentId"`
		KnowledgeBaseID string `json:"knowledgeBaseId"`
		ChunkIndex      int    `json:"chunkIndex"`
		Content         string `json:"content"`
	}
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("decode output: %v body=%s", err, string(payload))
	}
	if output.ID != "chunk_test" || output.ChunkID != "chunk_test" || output.DocumentID != "doc_test" || output.KnowledgeBaseID != "kb_test" {
		t.Fatalf("unexpected output: %+v", output)
	}
	if output.ChunkIndex != 2 || !strings.Contains(output.Content, "full context") {
		t.Fatalf("unexpected content output: %+v", output)
	}
}

func TestCreateKnowledgeBaseRequiresWritePermission(t *testing.T) {
	t.Skip("create_knowledge_base is not published in the v1 MCP read-only contract")
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "create_knowledge_base",
		Arguments: map[string]any{
			"name": "Should Fail",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got %+v", result)
	}
}

func TestCreateAndListKnowledgeBases(t *testing.T) {
	t.Skip("knowledge base CRUD is not published in the v1 MCP read-only contract")
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		Permissions: service.PermissionKnowledgeWrite,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	createResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "create_knowledge_base",
		Arguments: map[string]any{
			"name":        "Manuals",
			"description": "Test KB",
		},
	})
	if err != nil {
		t.Fatalf("CallTool create: %v", err)
	}
	if createResult.IsError {
		t.Fatalf("unexpected create error: %+v", createResult)
	}
	createPayload, _ := json.Marshal(createResult.StructuredContent)
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(createPayload, &created); err != nil {
		t.Fatalf("decode create output: %v", err)
	}
	if created.ID == "" || created.Name != "Manuals" {
		t.Fatalf("created=%+v", created)
	}

	listResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "list_knowledge_bases",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool list: %v", err)
	}
	if listResult.IsError {
		t.Fatalf("unexpected list error: %+v", listResult)
	}
	listPayload, _ := json.Marshal(listResult.StructuredContent)
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(listPayload, &listed); err != nil {
		t.Fatalf("decode list output: %v body=%s", err, string(listPayload))
	}
	if len(listed.Data) != 1 {
		t.Fatalf("listed data=%+v", listed.Data)
	}
}

func TestCreateDocumentUpload(t *testing.T) {
	t.Skip("create_document is not published in the v1 MCP read-only contract")
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: true,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		Permissions: service.PermissionKnowledgeWrite,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	kbResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "create_knowledge_base",
		Arguments: map[string]any{
			"name": "Upload KB",
		},
	})
	if err != nil || kbResult.IsError {
		t.Fatalf("create kb: err=%v result=%+v", err, kbResult)
	}
	kbPayload, _ := json.Marshal(kbResult.StructuredContent)
	var kb struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(kbPayload, &kb)

	content := base64.StdEncoding.EncodeToString([]byte("hello ingestion"))
	uploadResult, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "create_document",
		Arguments: map[string]any{
			"knowledgeBaseId":   kb.ID,
			"fileName":          "notes.txt",
			"fileContentBase64": content,
		},
	})
	if err != nil {
		t.Fatalf("CallTool upload: %v", err)
	}
	if uploadResult.IsError {
		t.Fatalf("unexpected upload error: %+v", uploadResult)
	}
	uploadPayload, _ := json.Marshal(uploadResult.StructuredContent)
	var uploaded struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(uploadPayload, &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if uploaded.ID == "" || uploaded.Name != "notes.txt" {
		t.Fatalf("uploaded=%+v", uploaded)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
}

func TestAnswerFromKnowledgeReturnsAnswerAndCitations(t *testing.T) {
	t.Skip("answer_from_knowledge is not published in the v1 MCP read-only contract")
	state := newFakeVendorState()
	state.chunks = []map[string]any{
		{
			"similarity":          0.88,
			"kb_id":               "kb_rag",
			"doc_id":              "doc_rag",
			"chunk_id":            "chunk_rag",
			"docnm_kwd":           "Policy.pdf",
			"content_with_weight": "Employees must reset passwords every 90 days.",
		},
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Passwords must be reset every 90 days [1].",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer gateway.Close()

	chatClient, err := aigateway.NewChatClient(gateway.URL, "test-token", gateway.Client())
	if err != nil {
		t.Fatalf("NewChatClient: %v", err)
	}

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		RequestID:   "req_answer",
		Permissions: service.PermissionKnowledgeRead,
	}, chatClient)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "answer_from_knowledge",
		Arguments: map[string]any{
			"question":         "How often must passwords be reset?",
			"knowledgeBaseIds": []any{"kb_rag"},
			"modelProfileId":   "profile-test",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}

	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var output struct {
		Answer    string `json:"answer"`
		Citations []struct {
			Index           int    `json:"index"`
			KnowledgeBaseID string `json:"knowledgeBaseId"`
			DocumentID      string `json:"documentId"`
			ChunkID         string `json:"chunkId"`
			Excerpt         string `json:"excerpt"`
		} `json:"citations"`
		Retrieval struct {
			QueryID     string `json:"queryId"`
			ResultCount int    `json:"resultCount"`
		} `json:"retrieval"`
	}
	if err := json.Unmarshal(payload, &output); err != nil {
		t.Fatalf("decode: %v body=%s", err, string(payload))
	}
	if !strings.Contains(output.Answer, "90 days") {
		t.Fatalf("answer=%q", output.Answer)
	}
	if len(output.Citations) != 1 || output.Citations[0].Index != 1 {
		t.Fatalf("citations=%+v", output.Citations)
	}
	if output.Retrieval.ResultCount != 1 || !strings.HasPrefix(output.Retrieval.QueryID, "kq_") {
		t.Fatalf("retrieval=%+v", output.Retrieval)
	}
}

func TestAnswerFromKnowledgeRequiresGatewayClient(t *testing.T) {
	t.Skip("answer_from_knowledge is not published in the v1 MCP read-only contract")
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	session := connectInMemory(t, adapterServer, kmcp.CallerContext{
		UserID:      "usr_test",
		Permissions: service.PermissionKnowledgeRead,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "answer_from_knowledge",
		Arguments: map[string]any{
			"question":         "test",
			"knowledgeBaseIds": []any{"kb_test"},
			"modelProfileId":   "profile-test",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got %+v", result)
	}
}

func TestStreamableHTTPHandlerUsesTrustedCallerContext(t *testing.T) {
	state := newFakeVendorState()
	state.chunks = []map[string]any{
		{
			"similarity":          0.75,
			"kb_id":               "kb_http",
			"doc_id":              "doc_http",
			"chunk_id":            "chunk_http",
			"docnm_kwd":           "Guide.txt",
			"content_with_weight": "HTTP bridge works",
		},
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	httpServer := httptest.NewServer(kmcp.NewStreamableHTTPHandler(adapterServer, kmcp.CallerContext{
		UserID:       "usr_trusted_mcp",
		ServiceToken: testServiceToken,
		Permissions:  service.PermissionKnowledgeRead,
	}, nil))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("X-User-Id", "usr_forged")
				r.Header.Set("X-Request-Id", "req_http")
				r.Header.Set("X-Service-Token", testServiceToken)
				r.Header.Set("X-User-Permissions", service.PermissionKnowledgeWrite)
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "search",
		Arguments: map[string]any{
			"query":            "bridge",
			"knowledgeBaseIds": []any{"kb_http"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
}

func TestStreamableHTTPHandlerDoesNotTrustForgedWritePermission(t *testing.T) {
	t.Skip("write tools are not published in the v1 MCP read-only contract — forged write permission test is no longer applicable")
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	httpServer := httptest.NewServer(kmcp.NewStreamableHTTPHandler(adapterServer, kmcp.CallerContext{
		UserID:       "usr_trusted_mcp",
		ServiceToken: testServiceToken,
		Permissions:  service.PermissionKnowledgeRead,
	}, nil))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("X-User-Id", "usr_attacker")
				r.Header.Set("X-Service-Token", testServiceToken)
				r.Header.Set("X-User-Permissions", service.PermissionKnowledgeWrite)
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "create_knowledge_base",
		Arguments: map[string]any{
			"name": "Should Fail",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected forged write permission to be ignored, got %+v", result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.datasets) != 0 {
		t.Fatalf("datasets=%v, want none created by forged MCP caller", state.datasets)
	}
}

func TestStreamableHTTPHandlerRejectsMissingServiceToken(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	adapterServer := adapter.NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)
	httpServer := httptest.NewServer(kmcp.NewStreamableHTTPHandler(adapterServer, kmcp.CallerContext{
		UserID:       "usr_trusted_mcp",
		ServiceToken: testServiceToken,
		Permissions:  service.PermissionKnowledgeRead,
	}, nil))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: httpServer.URL,
		HTTPClient: &http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("X-User-Id", "usr_http")
				r.Header.Set("X-User-Permissions", service.PermissionKnowledgeRead)
				return http.DefaultTransport.RoundTrip(r)
			}),
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err == nil {
		defer session.Close()
		for _, toolErr := range session.Tools(ctx, nil) {
			if toolErr == nil {
				t.Fatal("Tools() succeeded without X-Service-Token")
			}
			return
		}
		t.Fatal("Tools() returned no error without X-Service-Token")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		t.Fatalf("connect error = %v, want unauthorized", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

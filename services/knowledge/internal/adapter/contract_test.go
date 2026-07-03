package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapterconfig"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

const testServiceToken = "test-service-token"

type fakeVendorState struct {
	mu                 sync.Mutex
	datasets           map[string]map[string]any
	documents          map[string]map[string]any
	parseCalls         []string
	deleteCalls        []deleteCall
	listDatasetsCalls  int
	listDocumentsCalls int
	taskExecutorReady  bool
	failParse          bool
	searchStatus       int
	searchResponse     map[string]any
	searchBody         []byte
	searchUserID       string
	listUserIDs        []string
	documentUserIDs    []string
	createUserIDs      []string
	createBody         []byte
	createPath         string
}

type deleteCall struct {
	datasetID  string
	documentID string
}

type fakeRuntimeWorkerStarter struct {
	mu      sync.Mutex
	calls   int
	err     error
	onStart func()
}

func (s *fakeRuntimeWorkerStarter) Start(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.onStart != nil {
		s.onStart()
	}
	return s.err
}

func (s *fakeRuntimeWorkerStarter) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func waitForRuntimeWorkerStarterCalls(t *testing.T, starter *fakeRuntimeWorkerStarter, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if starter.callCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("worker starter calls=%d, want %d", starter.callCount(), want)
}

func newFakeVendorState() *fakeVendorState {
	return &fakeVendorState{
		datasets:          map[string]map[string]any{},
		documents:         map[string]map[string]any{},
		taskExecutorReady: true,
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			heartbeats := map[string]any{}
			if state.taskExecutorReady {
				heartbeats["task_executor_common_1"] = []any{map[string]any{"now": time.Now().Format(time.RFC3339Nano)}}
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{
					"doc_engine":               map[string]any{"status": "green"},
					"storage":                  map[string]any{"status": "green"},
					"database":                 map[string]any{"status": "green"},
					"redis":                    map[string]any{"status": "green"},
					"task_executor_heartbeats": heartbeats,
				},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/datasets":
			state.listDatasetsCalls++
			state.listUserIDs = append(state.listUserIDs, r.Header.Get("X-User-Id"))
			items := make([]map[string]any, 0, len(state.datasets))
			for _, item := range state.datasets {
				items = append(items, item)
			}
			sort.Slice(items, func(i, j int) bool {
				left, _ := items[i]["id"].(string)
				right, _ := items[j]["id"].(string)
				return left < right
			})
			total := len(items)
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			if page <= 0 {
				page = 1
			}
			pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
			if pageSize <= 0 {
				pageSize = total
			}
			start := (page - 1) * pageSize
			if start >= total {
				items = []map[string]any{}
			} else {
				end := start + pageSize
				if end > total {
					end = total
				}
				items = items[start:end]
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": items, "total_datasets": total})
			return
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/datasets" || r.URL.Path == "/api/v1/internal/datasets"):
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			state.createUserIDs = append(state.createUserIDs, r.Header.Get("X-User-Id"))
			state.createPath = r.URL.Path
			state.createBody = append([]byte(nil), raw...)
			_ = json.Unmarshal(raw, &body)
			id := "kb_fake_1"
			item := map[string]any{
				"id": id, "name": body["name"], "description": body["description"],
				"chunk_method": "naive", "document_count": 0, "chunk_count": 0,
				"create_time": float64(1700000000000),
			}
			if cfg := body["parser_config"]; cfg != nil {
				item["parser_config"] = cfg
			}
			state.datasets[id] = item
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": item})
			return
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/documents/parse"):
			if state.failParse {
				writeVendorJSON(w, http.StatusBadRequest, map[string]any{"code": 100, "message": "parse failed"})
				return
			}
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
			defer file.Close()
			docID := "doc_fake_1"
			doc := map[string]any{
				"id": docID, "kb_id": kbID, "dataset_id": kbID, "name": header.Filename,
				"type": "txt", "size": header.Size, "run": "UNSTART", "chunk_count": 0,
			}
			state.documents[docID] = doc
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": doc})
			return
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.HasSuffix(r.URL.Path, "/documents"):
			state.listDocumentsCalls++
			state.documentUserIDs = append(state.documentUserIDs, r.Header.Get("X-User-Id"))
			kbID := strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/")
			kbID = strings.TrimSuffix(kbID, "/documents")
			docs := make([]map[string]any, 0)
			for _, doc := range state.documents {
				if doc["kb_id"] == kbID || doc["dataset_id"] == kbID {
					docs = append(docs, doc)
				}
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"total": len(docs), "docs": docs}})
			return
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.Contains(r.URL.Path, "/documents/"):
			trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/")
			parts := strings.Split(trimmed, "/documents/")
			if len(parts) != 2 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			docID := parts[1]
			doc, ok := state.documents[docID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if meta, ok := body["meta_fields"].(map[string]any); ok {
				doc["meta_fields"] = meta
			}
			response := map[string]any{}
			for key, value := range doc {
				if key == "meta_fields" {
					continue
				}
				response[key] = value
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": response})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets/search":
			raw, _ := io.ReadAll(r.Body)
			state.searchBody = append([]byte(nil), raw...)
			state.searchUserID = r.Header.Get("X-User-Id")
			if state.searchStatus != 0 {
				writeVendorJSON(w, state.searchStatus, state.searchResponse)
				return
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{
				"code": 0,
				"data": map[string]any{"total": 0, "chunks": []any{}},
			})
			return
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/datasets/") && strings.HasSuffix(r.URL.Path, "/documents"):
			kbID := strings.TrimPrefix(r.URL.Path, "/api/v1/datasets/")
			kbID = strings.TrimSuffix(kbID, "/documents")
			var body struct {
				IDs []string `json:"ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode delete body: %v", err)
			}
			for _, docID := range body.IDs {
				state.deleteCalls = append(state.deleteCalls, deleteCall{datasetID: kbID, documentID: docID})
				delete(state.documents, docID)
			}
			writeVendorJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"deleted": len(body.IDs)}})
			return
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeVendorJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
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

func TestAdapterCreateKnowledgeBaseAppliesDefaultParserConfig(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	repo := repository.NewMemoryRepository()
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	repo.SeedParserConfig(service.ParserConfig{
		ID:                    "parser_default_ocr",
		Name:                  "Default OCR",
		Backend:               service.ParserBackendLocalOCR,
		Enabled:               true,
		IsDefault:             true,
		Concurrency:           2,
		SupportedContentTypes: []string{"application/pdf"},
		DefaultParameters:     json.RawMessage(`{"chunk_token_num":768}`),
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	server := NewServer(adapterconfig.Config{
		ServiceVersion:    "test",
		VendorRuntimeURL:  vendor.URL,
		ServiceToken:      testServiceToken,
		VendorEmbeddingID: "BAAI/bge-m3@SILICONFLOW",
	}, nil, WithParserConfigService(service.New(repo)))

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases", strings.NewReader(`{"name":"Manuals"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	createBody := decodeMap(t, state.createBody)
	cfg, ok := createBody["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", createBody["parser_config"])
	}
	if cfg["layout_recognize"] != ragflowLayoutPaddleOCR {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
	if createBody["embedding_model"] != "BAAI/bge-m3@SILICONFLOW" {
		t.Fatalf("embedding_model=%v", createBody["embedding_model"])
	}
	if _, ok := cfg[parserConfigTraceKey]; ok {
		t.Fatalf("parser config trace must not be sent to vendor payload: %v", cfg[parserConfigTraceKey])
	}
	if state.createPath != "/api/v1/datasets" {
		t.Fatalf("create path=%s", state.createPath)
	}
}

func TestAdapterKnowledgeBasesUseProjectRuntimeScope(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler"}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	listReq := httptest.NewRequest(http.MethodGet, "/internal/v1/knowledge-bases", nil)
	listReq.Header.Set("X-User-Id", "usr_standard")
	listReq.Header.Set("X-Service-Token", testServiceToken)
	listReq.Header.Set("X-User-Permissions", "knowledge:read")
	listRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases", strings.NewReader(`{"name":"Manuals"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-User-Id", "usr_admin")
	createReq.Header.Set("X-Service-Token", testServiceToken)
	createReq.Header.Set("X-User-Permissions", "knowledge:write")
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.listUserIDs) == 0 || state.listUserIDs[0] != "project_runtime" {
		t.Fatalf("list user ids=%v", state.listUserIDs)
	}
	if len(state.createUserIDs) == 0 || state.createUserIDs[0] != "project_runtime" {
		t.Fatalf("create user ids=%v", state.createUserIDs)
	}
}

func TestAdapterCreateKnowledgeBaseUsesInternalDatasetPathForPaddleOCRCredentials(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	repo := repository.NewMemoryRepository()
	now := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	repo.SeedParserConfig(service.ParserConfig{
		ID:                    "parser_default_paddleocr",
		Name:                  "Default PaddleOCR",
		Backend:               service.ParserBackendPaddleOCRCloud,
		Enabled:               true,
		IsDefault:             true,
		Concurrency:           2,
		SupportedContentTypes: []string{"application/pdf"},
		DefaultParameters:     json.RawMessage(`{"paddleocr_base_url":"https://paddleocr.example.com/api","paddleocr_access_token":"sk-secret","paddleocr_algorithm":"PaddleOCR-VL-1.6"}`),
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	server := NewServer(adapterconfig.Config{
		ServiceVersion:    "test",
		VendorRuntimeURL:  vendor.URL,
		ServiceToken:      testServiceToken,
		VendorEmbeddingID: "BAAI/bge-m3@SILICONFLOW",
	}, nil, WithParserConfigService(service.New(repo)))

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases", strings.NewReader(`{"name":"Manuals"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.createPath != "/api/v1/internal/datasets" {
		t.Fatalf("create path=%s", state.createPath)
	}
	createBody := decodeMap(t, state.createBody)
	cfg, ok := createBody["parser_config"].(map[string]any)
	if !ok {
		t.Fatalf("parser_config=%v", createBody["parser_config"])
	}
	if cfg["layout_recognize"] != ragflowLayoutPaddleOCR {
		t.Fatalf("layout_recognize=%v", cfg["layout_recognize"])
	}
	if _, ok := cfg["paddleocr_access_token"]; ok {
		t.Fatalf("parser_config leaked PaddleOCR access token")
	}
	if _, ok := createBody["parser_config_credentials"].(map[string]any); !ok {
		t.Fatalf("parser_config_credentials missing")
	}
}

func TestAdapterDocumentUploadStartsVendorIngestion(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: true,
	}, nil)

	kbReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases", strings.NewReader(`{"name":"Manuals"}`))
	kbReq.Header.Set("Content-Type", "application/json")
	kbReq.Header.Set("X-User-Id", "usr_test")
	kbReq.Header.Set("X-Service-Token", testServiceToken)
	kbReq.Header.Set("X-User-Permissions", "knowledge:write")
	kbRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(kbRec, kbReq)
	if kbRec.Code != http.StatusCreated {
		t.Fatalf("create kb status=%d body=%s", kbRec.Code, kbRec.Body.String())
	}

	var kbBody struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(kbRec.Body.Bytes(), &kbBody); err != nil {
		t.Fatalf("decode kb: %v", err)
	}
	kbID, _ := kbBody.Data["id"].(string)
	if kbID == "" {
		t.Fatalf("kb id missing: %v", kbBody.Data)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "hello ingestion"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/"+kbID+"/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 || state.parseCalls[0] != "doc_fake_1" {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
}

func TestAdapterDocumentUploadQueuesIngestionWithoutRuntimeWorkerHeartbeat(t *testing.T) {
	state := newFakeVendorState()
	state.taskExecutorReady = false
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: true,
	}, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 || state.parseCalls[0] != "doc_fake_1" {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("deleteCalls=%v want none", state.deleteCalls)
	}
}

func TestAdapterDocumentUploadStartsConfiguredRuntimeWorkerWhenHeartbeatMissing(t *testing.T) {
	state := newFakeVendorState()
	state.taskExecutorReady = false
	vendor := startFakeVendor(t, state)
	defer vendor.Close()
	starter := &fakeRuntimeWorkerStarter{onStart: func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.taskExecutorReady = true
	}}

	server := NewServer(adapterconfig.Config{
		ServiceVersion:            "test",
		VendorRuntimeURL:          vendor.URL,
		ServiceToken:              testServiceToken,
		AutoStartIngestion:        true,
		RuntimeWorkerStartTimeout: time.Second,
	}, nil, WithRuntimeWorkerStarter(starter))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	waitForRuntimeWorkerStarterCalls(t, starter, 1)

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 || state.parseCalls[0] != "doc_fake_1" {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("deleteCalls=%v want none", state.deleteCalls)
	}
}

func TestAdapterDocumentUploadDoesNotRollbackWhenRuntimeWorkerStartFailsAfterParseEnqueue(t *testing.T) {
	state := newFakeVendorState()
	state.taskExecutorReady = false
	vendor := startFakeVendor(t, state)
	defer vendor.Close()
	starter := &fakeRuntimeWorkerStarter{err: errors.New("boom")}

	server := NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: true,
	}, nil, WithRuntimeWorkerStarter(starter))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	waitForRuntimeWorkerStarterCalls(t, starter, 1)

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 || state.parseCalls[0] != "doc_fake_1" {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("deleteCalls=%v want none", state.deleteCalls)
	}
}

func TestAdapterDocumentUploadDoesNotWaitForRuntimeWorkerHeartbeatAfterParseEnqueue(t *testing.T) {
	state := newFakeVendorState()
	state.taskExecutorReady = false
	vendor := startFakeVendor(t, state)
	defer vendor.Close()
	starter := &fakeRuntimeWorkerStarter{}

	server := NewServer(adapterconfig.Config{
		ServiceVersion:            "test",
		VendorRuntimeURL:          vendor.URL,
		ServiceToken:              testServiceToken,
		AutoStartIngestion:        true,
		RuntimeWorkerStartTimeout: 25 * time.Millisecond,
	}, nil, WithRuntimeWorkerStarter(starter))

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	waitForRuntimeWorkerStarterCalls(t, starter, 1)

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.parseCalls) != 1 || state.parseCalls[0] != "doc_fake_1" {
		t.Fatalf("parseCalls=%v", state.parseCalls)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("deleteCalls=%v want none", state.deleteCalls)
	}
}

func TestAdapterDocumentUploadSkipsIngestionWhenDisabled(t *testing.T) {
	state := newFakeVendorState()
	state.taskExecutorReady = false
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: false,
	}, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	if len(state.parseCalls) != 0 {
		t.Fatalf("parseCalls=%v want none", state.parseCalls)
	}
}

func TestAdapterKnowledgeQueryForwardsDocumentIDs(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
		VendorRerankID:   "rerank-model",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-queries", strings.NewReader(`{"query":"maintenance","knowledgeBaseIds":["kb_fake_1"],"documentIds":["doc_1"],"tags":["锅炉"],"metadataFilter":{"专业":"锅炉"},"rerank":true,"rerankTopN":5,"topK":8,"scoreThreshold":0.4}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:read")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("query status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.searchBody) == 0 {
		t.Fatal("vendor search body missing")
	}
	var payload map[string]any
	if err := json.Unmarshal(state.searchBody, &payload); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	docIDs, _ := payload["doc_ids"].([]any)
	if len(docIDs) != 1 || docIDs[0] != "doc_1" {
		t.Fatalf("doc_ids=%v", payload["doc_ids"])
	}
	if payload["rerank_id"] != "rerank-model" {
		t.Fatalf("rerank_id=%v", payload["rerank_id"])
	}
}

func TestAdapterKnowledgeQueryMapsRuntimeValidationError(t *testing.T) {
	state := newFakeVendorState()
	state.searchStatus = http.StatusBadRequest
	state.searchResponse = map[string]any{
		"code":    101,
		"message": "Datasets use different embedding models.",
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-queries", strings.NewReader(`{"query":"maintenance","knowledgeBaseIds":["kb_1","kb_2"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:read")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != string(service.CodeValidation) {
		t.Fatalf("error code=%q", body.Error.Code)
	}
}

func TestAdapterKnowledgeQueryMapsRuntimeNotFoundError(t *testing.T) {
	state := newFakeVendorState()
	state.searchStatus = http.StatusNotFound
	state.searchResponse = map[string]any{
		"code":    404,
		"message": "Dataset not found.",
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-queries", strings.NewReader(`{"query":"maintenance","knowledgeBaseIds":["kb_missing"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:read")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != string(service.CodeNotFound) {
		t.Fatalf("error code=%q", body.Error.Code)
	}
}

func TestAdapterKnowledgeQueryExpandsAccessibleKnowledgeBasesWhenOmitted(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler", "chunk_count": 1}
	state.datasets["kb_fake_2"] = map[string]any{"id": "kb_fake_2", "name": "Transformer", "chunk_count": 1}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-queries", strings.NewReader(`{"query":"maintenance"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:read")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("query status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(state.searchBody, &payload); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	datasetIDs, _ := payload["dataset_ids"].([]any)
	if len(datasetIDs) != 2 || datasetIDs[0] != "kb_fake_1" || datasetIDs[1] != "kb_fake_2" {
		t.Fatalf("dataset_ids=%v", payload["dataset_ids"])
	}
	if len(state.listUserIDs) == 0 || state.listUserIDs[0] != "project_runtime" {
		t.Fatalf("list user ids=%v", state.listUserIDs)
	}
	if state.searchUserID != "project_runtime" {
		t.Fatalf("search user id=%q", state.searchUserID)
	}
}

func TestAdapterTrustedQAKnowledgeQueryUsesProjectRuntimeScopeWithoutKnowledgeRead(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler", "chunk_count": 1}
	state.datasets["kb_fake_2"] = map[string]any{"id": "kb_fake_2", "name": "Transformer", "chunk_count": 1}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-queries", strings.NewReader(`{"query":"maintenance"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_without_knowledge_permission")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set(retrievalScopeHeader, retrievalScopeProject)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("query status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(state.searchBody, &payload); err != nil {
		t.Fatalf("decode search body: %v", err)
	}
	datasetIDs, _ := payload["dataset_ids"].([]any)
	if len(datasetIDs) != 2 || datasetIDs[0] != "kb_fake_1" || datasetIDs[1] != "kb_fake_2" {
		t.Fatalf("dataset_ids=%v", payload["dataset_ids"])
	}
	if len(state.listUserIDs) == 0 || state.listUserIDs[0] != "project_runtime" {
		t.Fatalf("list user ids=%v", state.listUserIDs)
	}
	if state.searchUserID != "project_runtime" {
		t.Fatalf("search user id=%q", state.searchUserID)
	}
}

func TestAdapterTrustedQAGetDocumentStillRequiresKnowledgeRead(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler"}
	state.documents["doc_fake_1"] = map[string]any{"id": "doc_fake_1", "kb_id": "kb_fake_1", "dataset_id": "kb_fake_1", "name": "guide.md"}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:       "test",
		VendorRuntimeURL:     vendor.URL,
		ServiceToken:         testServiceToken,
		ProjectRuntimeUserID: "project_runtime",
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/documents/doc_fake_1?knowledgeBaseId=kb_fake_1", nil)
	req.Header.Set("X-User-Id", "usr_without_knowledge_permission")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set(retrievalScopeHeader, retrievalScopeProject)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.listUserIDs) != 0 {
		t.Fatalf("list user ids=%v, want none", state.listUserIDs)
	}
	if len(state.documentUserIDs) != 0 {
		t.Fatalf("document user ids=%v, want none", state.documentUserIDs)
	}
}

func TestAdapterReadRoutesRequireKnowledgeReadPermission(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "list knowledge bases",
			method: http.MethodGet,
			path:   "/internal/v1/knowledge-bases",
		},
		{
			name:   "create knowledge query",
			method: http.MethodPost,
			path:   "/internal/v1/knowledge-queries",
			body:   `{"query":"maintenance","knowledgeBaseIds":["kb_fake_1"]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.Header.Set("X-User-Id", "usr_without_knowledge_permission")
			req.Header.Set("X-Service-Token", testServiceToken)
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if body.Error.Code != string(service.CodeForbidden) {
				t.Fatalf("error code=%q", body.Error.Code)
			}
		})
	}
}

func TestAdapterKnowledgeStatisticsUsesTenantScopedRuntimeTotals(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler", "doc_num": 2}
	state.datasets["kb_fake_2"] = map[string]any{"id": "kb_fake_2", "name": "Transformer", "doc_num": 1}
	state.documents["doc_fake_1"] = map[string]any{"id": "doc_fake_1", "kb_id": "kb_fake_1", "dataset_id": "kb_fake_1"}
	state.documents["doc_fake_2"] = map[string]any{"id": "doc_fake_2", "kb_id": "kb_fake_1", "dataset_id": "kb_fake_1"}
	state.documents["doc_fake_3"] = map[string]any{"id": "doc_fake_3", "kb_id": "kb_fake_2", "dataset_id": "kb_fake_2"}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/knowledge-statistics", nil)
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Id", "usr_stats")
	req.Header.Set("X-User-Permissions", "knowledge:read")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			KnowledgeBaseCount int64 `json:"knowledgeBaseCount"`
			DocumentCount      int64 `json:"documentCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if body.Data.KnowledgeBaseCount != 2 || body.Data.DocumentCount != 3 {
		t.Fatalf("stats=(%d,%d), want (2,3)", body.Data.KnowledgeBaseCount, body.Data.DocumentCount)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.listDocumentsCalls != 0 {
		t.Fatalf("listDocumentsCalls=%d, want no per-KB document scan", state.listDocumentsCalls)
	}
}

func TestAdapterUpdateDocumentPreservesTagsInImmediateResponse(t *testing.T) {
	state := newFakeVendorState()
	state.documents["doc_fake_1"] = map[string]any{
		"id": "doc_fake_1", "kb_id": "kb_fake_1", "dataset_id": "kb_fake_1", "name": "notes.txt",
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodPatch, "/internal/v1/documents/doc_fake_1?knowledgeBaseId=kb_fake_1", strings.NewReader(`{"tags":["锅炉","检修"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Tags []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Tags) != 2 || body.Data.Tags[0] != "锅炉" || body.Data.Tags[1] != "检修" {
		t.Fatalf("tags=%v", body.Data.Tags)
	}
}

func TestAdapterRejectsOversizedJSONBody(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	oversizedName := strings.Repeat("a", int(defaultMaxJSONBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases", strings.NewReader(`{"name":"`+oversizedName+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != string(service.CodeValidation) || body.Error.Fields["body"] != "exceeds maximum JSON body size" {
		t.Fatalf("error=%+v", body.Error)
	}
}

func TestAdapterKnowledgeStatisticsWithoutReadPermissionReturnsForbidden(t *testing.T) {
	state := newFakeVendorState()
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/knowledge-statistics", nil)
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Id", "usr_stats")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdapterKnowledgeStatisticsWithoutUserReturnsZeroCounts(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler"}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/knowledge-statistics", nil)
	req.Header.Set("X-Service-Token", testServiceToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			KnowledgeBaseCount int64 `json:"knowledgeBaseCount"`
			DocumentCount      int64 `json:"documentCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if body.Data.KnowledgeBaseCount != 0 || body.Data.DocumentCount != 0 {
		t.Fatalf("stats=(%d,%d), want zero counts", body.Data.KnowledgeBaseCount, body.Data.DocumentCount)
	}
}

func TestAdapterDeleteDocumentUsesDatasetScopedRuntimeRoute(t *testing.T) {
	state := newFakeVendorState()
	state.datasets["kb_fake_1"] = map[string]any{"id": "kb_fake_1", "name": "Boiler"}
	state.documents["doc_fake_1"] = map[string]any{
		"id": "doc_fake_1", "kb_id": "kb_fake_1", "dataset_id": "kb_fake_1", "name": "notes.txt",
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodDelete, "/internal/v1/documents/doc_fake_1?knowledgeBaseId=kb_fake_1", nil)
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.deleteCalls) != 1 || state.deleteCalls[0] != (deleteCall{datasetID: "kb_fake_1", documentID: "doc_fake_1"}) {
		t.Fatalf("deleteCalls=%v", state.deleteCalls)
	}
	if _, ok := state.documents["doc_fake_1"]; ok {
		t.Fatal("document should be removed after delete")
	}
}

func TestAdapterDocumentRoutesRequireKnowledgeBaseIDWithoutScanningDatasets(t *testing.T) {
	state := newFakeVendorState()
	for i := 1; i <= 101; i++ {
		kbID := fmt.Sprintf("kb_%03d", i)
		state.datasets[kbID] = map[string]any{"id": kbID, "name": fmt.Sprintf("KB %03d", i)}
	}
	state.documents["doc_late_page"] = map[string]any{
		"id": "doc_late_page", "kb_id": "kb_101", "dataset_id": "kb_101", "name": "late.txt",
	}
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:   "test",
		VendorRuntimeURL: vendor.URL,
		ServiceToken:     testServiceToken,
	}, nil)

	req := httptest.NewRequest(http.MethodDelete, "/internal/v1/documents/doc_late_page", nil)
	req.Header.Set("X-User-Id", "usr_test")
	req.Header.Set("X-Service-Token", testServiceToken)
	req.Header.Set("X-User-Permissions", "knowledge:write")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != string(service.CodeValidation) || body.Error.Fields["knowledgeBaseId"] == "" {
		t.Fatalf("error=%+v", body.Error)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.listDatasetsCalls != 0 {
		t.Fatalf("listDatasetsCalls=%d, want no all-KB scan", state.listDatasetsCalls)
	}
	if len(state.deleteCalls) != 0 {
		t.Fatalf("deleteCalls=%v", state.deleteCalls)
	}
}

func TestAdapterDocumentUploadRollsBackWhenParseFails(t *testing.T) {
	state := newFakeVendorState()
	state.failParse = true
	vendor := startFakeVendor(t, state)
	defer vendor.Close()

	server := NewServer(adapterconfig.Config{
		ServiceVersion:     "test",
		VendorRuntimeURL:   vendor.URL,
		ServiceToken:       testServiceToken,
		AutoStartIngestion: true,
	}, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = io.WriteString(part, "hello")
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/internal/v1/knowledge-bases/kb_fake_1/documents", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", "usr_test")
	uploadReq.Header.Set("X-Service-Token", testServiceToken)
	uploadReq.Header.Set("X-User-Permissions", "knowledge:write")
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code == http.StatusCreated {
		t.Fatalf("expected upload failure, got status=%d body=%s", uploadRec.Code, uploadRec.Body.String())
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.deleteCalls) != 1 || state.deleteCalls[0] != (deleteCall{datasetID: "kb_fake_1", documentID: "doc_fake_1"}) {
		t.Fatalf("deleteCalls=%v", state.deleteCalls)
	}
	if _, ok := state.documents["doc_fake_1"]; ok {
		t.Fatal("document should be removed after parse failure")
	}
}

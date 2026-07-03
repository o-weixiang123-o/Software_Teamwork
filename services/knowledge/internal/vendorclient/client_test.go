package vendorclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientUsesKnowledgeRuntimeContractPaths(t *testing.T) {
	type call struct {
		method       string
		path         string
		query        string
		tenant       string
		serviceToken string
	}

	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{
			method:       r.Method,
			path:         r.URL.Path,
			query:        r.URL.RawQuery,
			tenant:       r.Header.Get("X-Tenant-Id"),
			serviceToken: r.Header.Get("X-Service-Token"),
		})

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/ping":
			_, _ = w.Write([]byte("pong"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			writeTestVendorJSON(w, `{"code":0,"data":{"redis":{"status":"green"},"task_executor_heartbeats":{"task_executor_common_1":[{"ts":1700000000}]}}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/datasets":
			writeTestVendorJSON(w, `{"code":0,"data":[],"total_datasets":0}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets":
			writeTestVendorJSON(w, `{"code":0,"data":{"id":"kb_plain","name":"Plain"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/internal/datasets":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode internal create body: %v", err)
			}
			if _, ok := body["parser_config_credentials"]; !ok {
				t.Fatalf("internal create missing parser_config_credentials")
			}
			writeTestVendorJSON(w, `{"code":0,"data":{"id":"kb_internal","name":"Internal"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets/kb_1/documents" && r.URL.Query().Get("type") == "local":
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatalf("parse multipart form: %v", err)
			}
			writeTestVendorJSON(w, `{"code":0,"data":{"id":"doc_1","name":"notes.txt"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/datasets/kb_1/documents":
			writeTestVendorJSON(w, `{"code":0,"data":{"total":1,"docs":[{"id":"doc_1","name":"notes.txt","kb_id":"kb_1"}]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets/kb_1/documents/parse":
			writeTestVendorJSON(w, `{"code":0,"data":{}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/datasets/kb_1/documents/doc_1/chunks":
			writeTestVendorJSON(w, `{"code":0,"data":{"total":0,"chunks":[]}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/datasets/search":
			writeTestVendorJSON(w, `{"code":0,"data":{"total":0,"chunks":[]}}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/datasets/kb_1/documents":
			var body struct {
				IDs []string `json:"ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode delete body: %v", err)
			}
			if len(body.IDs) != 1 || body.IDs[0] != "doc_1" {
				t.Fatalf("delete body ids=%v, want [doc_1]", body.IDs)
			}
			writeTestVendorJSON(w, `{"code":0,"data":{"deleted":1}}`)
		default:
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	ctx := context.Background()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if status, err := client.RuntimeStatus(ctx, "tenant_1"); err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	} else if status["task_executor_heartbeats"] == nil {
		t.Fatalf("RuntimeStatus missing task_executor_heartbeats: %#v", status)
	}
	if _, _, err := client.ListDatasets(ctx, "tenant_1", 2, 10); err != nil {
		t.Fatalf("ListDatasets: %v", err)
	}
	if _, err := client.CreateDataset(ctx, "tenant_1", []byte(`{"name":"Plain"}`)); err != nil {
		t.Fatalf("CreateDataset plain: %v", err)
	}
	if _, err := client.CreateDataset(ctx, "tenant_1", []byte(`{"name":"Internal","parser_config_credentials":{"paddleocr_cloud":{"paddleocr_access_token":"sk-secret"}}}`)); err != nil {
		t.Fatalf("CreateDataset internal credentials: %v", err)
	}
	if _, err := client.UploadDocument(ctx, "tenant_1", "kb_1", "notes.txt", "text/plain", strings.NewReader("hello")); err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}
	if err := client.StartDocumentParse(ctx, "tenant_1", "kb_1", []string{"doc_1"}); err != nil {
		t.Fatalf("StartDocumentParse: %v", err)
	}
	if _, err := client.GetDatasetDocument(ctx, "tenant_1", "kb_1", "doc_1"); err != nil {
		t.Fatalf("GetDatasetDocument: %v", err)
	}
	if _, _, err := client.ListChunks(ctx, "tenant_1", "kb_1", "doc_1", 1, 20); err != nil {
		t.Fatalf("ListChunks: %v", err)
	}
	if _, err := client.RetrievalSearch(ctx, "tenant_1", []byte(`{"question":"hello","dataset_ids":["kb_1"]}`)); err != nil {
		t.Fatalf("RetrievalSearch: %v", err)
	}
	if err := client.DeleteDocument(ctx, "tenant_1", "kb_1", "doc_1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	expected := []call{
		{method: http.MethodGet, path: "/api/v1/system/ping"},
		{method: http.MethodGet, path: "/api/v1/system/status", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodGet, path: "/api/v1/datasets", query: "page=2&page_size=10", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodPost, path: "/api/v1/datasets", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodPost, path: "/api/v1/internal/datasets", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodPost, path: "/api/v1/datasets/kb_1/documents", query: "type=local", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodPost, path: "/api/v1/datasets/kb_1/documents/parse", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodGet, path: "/api/v1/datasets/kb_1/documents", query: "page=1&page_size=100", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodGet, path: "/api/v1/datasets/kb_1/documents/doc_1/chunks", query: "page=1&page_size=20", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodPost, path: "/api/v1/datasets/search", tenant: "tenant_1", serviceToken: "runtime-token"},
		{method: http.MethodDelete, path: "/api/v1/datasets/kb_1/documents", tenant: "tenant_1", serviceToken: "runtime-token"},
	}

	if len(calls) != len(expected) {
		t.Fatalf("call count = %d, want %d: %#v", len(calls), len(expected), calls)
	}
	for i := range expected {
		if calls[i] != expected[i] {
			t.Fatalf("call[%d] = %#v, want %#v", i, calls[i], expected[i])
		}
	}
}

func TestGetDatasetDocumentScansAllPages(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/datasets/kb_1/documents" {
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		requested = append(requested, r.URL.RawQuery)
		switch r.URL.Query().Get("page") {
		case "1":
			writeTestVendorJSON(w, `{"code":0,"data":{"total":101,"docs":[{"id":"doc_001","name":"first.txt","kb_id":"kb_1"}]}}`)
		case "2":
			writeTestVendorJSON(w, `{"code":0,"data":{"total":101,"docs":[{"id":"doc_101","name":"target.txt","kb_id":"kb_1"}]}}`)
		default:
			t.Fatalf("unexpected page query: %s", r.URL.RawQuery)
		}
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	doc, err := client.GetDatasetDocument(context.Background(), "tenant_1", "kb_1", "doc_101")
	if err != nil {
		t.Fatalf("GetDatasetDocument: %v", err)
	}
	if doc["name"] != "target.txt" {
		t.Fatalf("document name = %v, want target.txt", doc["name"])
	}
	want := []string{"page=1&page_size=100", "page=2&page_size=100"}
	if len(requested) != len(want) {
		t.Fatalf("queries = %#v, want %#v", requested, want)
	}
	for i := range want {
		if requested[i] != want[i] {
			t.Fatalf("query[%d] = %q, want %q", i, requested[i], want[i])
		}
	}
}

func TestClientPropagatesRequestIDFromContext(t *testing.T) {
	var gotRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/datasets" {
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		gotRequestID = r.Header.Get("X-Request-Id")
		writeTestVendorJSON(w, `{"code":0,"data":[],"total_datasets":0}`)
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	ctx := ContextWithRequestID(context.Background(), "req_trace")
	if _, _, err := client.ListDatasets(ctx, "tenant_1", 1, 20); err != nil {
		t.Fatalf("ListDatasets: %v", err)
	}
	if gotRequestID != "req_trace" {
		t.Fatalf("X-Request-Id = %q, want req_trace", gotRequestID)
	}
}

func TestDeleteDocumentRejectsVendorEnvelopeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/datasets/kb_1/documents" {
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		writeTestVendorJSON(w, `{"code":102,"message":"document is locked","data":{}}`)
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	err := client.DeleteDocument(context.Background(), "tenant_1", "kb_1", "doc_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("DeleteDocument error = %v, want APIError", err)
	}
	if apiErr.Code != 102 || apiErr.Message != "document is locked" {
		t.Fatalf("APIError=(%d,%q), want (102, document is locked)", apiErr.Code, apiErr.Message)
	}
}

func TestClientPreservesHTTPStatusOnVendorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/datasets/missing" {
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusNotFound)
		writeTestVendorJSON(w, `{"code":1002,"message":"dataset disappeared","data":{}}`)
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	_, err := client.GetDataset(context.Background(), "tenant_1", "missing")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetDataset error = %v, want APIError", err)
	}
	if apiErr.HTTPStatus != http.StatusNotFound || !apiErr.MatchesHTTPStatus(http.StatusNotFound) {
		t.Fatalf("APIError status=%d code=%d", apiErr.HTTPStatus, apiErr.Code)
	}
	if apiErr.Code != 1002 {
		t.Fatalf("APIError code=%d", apiErr.Code)
	}
}

func TestDownloadDocumentRejectsJSONErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/datasets/kb_1/documents/doc_1" {
			t.Fatalf("unexpected vendor request: %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		writeTestVendorJSON(w, `{"code":102,"message":"document is locked","data":{}}`)
	}))
	defer server.Close()

	client := New(server.URL, time.Second, "runtime-token")
	_, _, err := client.DownloadDocument(context.Background(), "tenant_1", "kb_1", "doc_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("DownloadDocument error = %v, want APIError", err)
	}
	if apiErr.Code != 102 || apiErr.Message != "document is locked" {
		t.Fatalf("APIError=(%d,%q), want (102, document is locked)", apiErr.Code, apiErr.Message)
	}
}

func writeTestVendorJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

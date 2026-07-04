package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	filehttp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/platform/storage"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/service"
)

func TestHealthReturnsEnvelope(t *testing.T) {
	server := newHTTPTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-Id", "req_health")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	var body successBody
	decodeJSON(t, res.Body, &body)
	if body.RequestID != "req_health" {
		t.Fatalf("requestId = %q", body.RequestID)
	}
}

func TestFileRoutesCreateGetContentAndDelete(t *testing.T) {
	server := newHTTPTestServer(t)
	content := "content"
	checksum := sha256Hex(content)
	create := newMultipartUploadRequest(t, "/internal/v1/files", "..\\policy.txt", "text/plain", content, nil, checksum)
	setInternalCaller(create)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, create)
	if res.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", res.Code, res.Body.String())
	}

	bodyText := res.Body.String()
	assertNotContainsSensitiveStorage(t, bodyText)
	var created fileResponseBody
	decodeJSON(t, strings.NewReader(bodyText), &created)
	if created.RequestID != "req_test" {
		t.Fatalf("requestId = %q", created.RequestID)
	}
	if created.Data.ID == "" || !strings.HasPrefix(created.Data.ID, "file_") {
		t.Fatalf("file id = %q", created.Data.ID)
	}
	if created.Data.Filename != "policy.txt" {
		t.Fatalf("filename = %q", created.Data.Filename)
	}
	if created.Data.ContentType != "text/plain" || created.Data.SizeBytes != int64(len(content)) || created.Data.ChecksumSHA256 == nil || *created.Data.ChecksumSHA256 != checksum {
		t.Fatalf("file metadata = %+v", created.Data)
	}
	if created.Data.DeletedAt != nil {
		t.Fatalf("deletedAt = %v", *created.Data.DeletedAt)
	}

	getReq := internalRequest(http.MethodGet, "/internal/v1/files/"+created.Data.ID, nil)
	getRes := httptest.NewRecorder()
	server.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRes.Code, getRes.Body.String())
	}
	assertNotContainsSensitiveStorage(t, getRes.Body.String())

	contentReq := internalRequest(http.MethodGet, "/internal/v1/files/"+created.Data.ID+"/content", nil)
	contentRes := httptest.NewRecorder()
	server.ServeHTTP(contentRes, contentReq)
	if contentRes.Code != http.StatusOK {
		t.Fatalf("content status = %d, body = %s", contentRes.Code, contentRes.Body.String())
	}
	if got := contentRes.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("content type = %q", got)
	}
	if got := contentRes.Header().Get("Content-Length"); got != "7" {
		t.Fatalf("content length = %q", got)
	}
	contentDisposition := contentRes.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment") || strings.ContainsAny(contentDisposition, "\r\n") || strings.Contains(contentDisposition, "..") {
		t.Fatalf("content disposition = %q", contentDisposition)
	}
	if got := contentRes.Body.String(); got != content {
		t.Fatalf("content body = %q", got)
	}

	deleteReq := internalRequest(http.MethodDelete, "/internal/v1/files/"+created.Data.ID, nil)
	deleteRes := httptest.NewRecorder()
	server.ServeHTTP(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRes.Code, deleteRes.Body.String())
	}

	getAfterDelete := internalRequest(http.MethodGet, "/internal/v1/files/"+created.Data.ID, nil)
	getAfterDeleteRes := httptest.NewRecorder()
	server.ServeHTTP(getAfterDeleteRes, getAfterDelete)
	if getAfterDeleteRes.Code != http.StatusNotFound {
		t.Fatalf("get after delete status = %d", getAfterDeleteRes.Code)
	}

	contentAfterDelete := internalRequest(http.MethodGet, "/internal/v1/files/"+created.Data.ID+"/content", nil)
	contentAfterDeleteRes := httptest.NewRecorder()
	server.ServeHTTP(contentAfterDeleteRes, contentAfterDelete)
	if contentAfterDeleteRes.Code != http.StatusNotFound {
		t.Fatalf("content after delete status = %d", contentAfterDeleteRes.Code)
	}
}

func TestFileCreateRejectsChecksumMismatch(t *testing.T) {
	server := newHTTPTestServer(t)
	req := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, strings.Repeat("0", 64))
	setInternalCaller(req)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var errBody errorResponseBody
	decodeJSON(t, res.Body, &errBody)
	if errBody.Error.Code != "validation_error" || errBody.Error.Fields["checksumSha256"] == "" {
		t.Fatalf("error body = %+v", errBody)
	}
}

func TestFileCreateRejectsInvalidChecksum(t *testing.T) {
	server := newHTTPTestServer(t)
	req := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, "not-a-checksum")
	setInternalCaller(req)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var errBody errorResponseBody
	decodeJSON(t, res.Body, &errBody)
	if errBody.Error.Fields["checksumSha256"] == "" {
		t.Fatalf("error body = %+v", errBody)
	}
}

func TestFileCreateRejectsEmptyFile(t *testing.T) {
	server := newHTTPTestServer(t)
	req := newMultipartUploadRequest(t, "/internal/v1/files", "empty.txt", "text/plain", "", nil, "")
	setInternalCaller(req)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var errBody errorResponseBody
	decodeJSON(t, res.Body, &errBody)
	if errBody.Error.Fields["file"] == "" {
		t.Fatalf("error body = %+v", errBody)
	}
}

func TestFileCreateRejectsOversizedMultipart(t *testing.T) {
	server := newLimitedHTTPTestServer(t, 4)
	req := newMultipartUploadRequest(t, "/internal/v1/files", "large.txt", "text/plain", "too large", nil, "")
	setInternalCaller(req)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var errBody errorResponseBody
	decodeJSON(t, res.Body, &errBody)
	if !strings.Contains(errBody.Error.Fields["file"], "exceeds") {
		t.Fatalf("error body = %+v", errBody)
	}
}

func TestFileCreateRejectsMalformedMultipart(t *testing.T) {
	server := newHTTPTestServer(t)
	req := internalRequest(http.MethodPost, "/internal/v1/files", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var errBody errorResponseBody
	decodeJSON(t, res.Body, &errBody)
	if errBody.Error.Code != "validation_error" || errBody.Error.Fields["file"] == "" {
		t.Fatalf("error body = %+v", errBody)
	}
}

func TestFileCreateRequiresInternalCaller(t *testing.T) {
	server := newHTTPTestServer(t)
	req := unauthenticatedMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestFileRoutesRequireConfiguredServiceToken(t *testing.T) {
	server := newTokenHTTPTestServer(t, "expected-token")

	missing := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, "")
	missing.Header.Set("X-Caller-Service", "knowledge")
	missing.Header.Del("X-Service-Token")
	missingRes := httptest.NewRecorder()
	server.ServeHTTP(missingRes, missing)
	if missingRes.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, body = %s", missingRes.Code, missingRes.Body.String())
	}
	if strings.Contains(missingRes.Body.String(), "expected-token") {
		t.Fatalf("error response leaked service token: %s", missingRes.Body.String())
	}

	wrong := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, "")
	wrong.Header.Set("X-Caller-Service", "knowledge")
	wrong.Header.Set("X-Service-Token", "wrong-token")
	wrongRes := httptest.NewRecorder()
	server.ServeHTTP(wrongRes, wrong)
	if wrongRes.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, body = %s", wrongRes.Code, wrongRes.Body.String())
	}
	if strings.Contains(wrongRes.Body.String(), "wrong-token") {
		t.Fatalf("error response leaked submitted service token: %s", wrongRes.Body.String())
	}

	ownerWithoutToken := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, "")
	ownerWithoutToken.Header.Set("X-Caller-Service", "document")
	ownerWithoutToken.Header.Set("X-Request-Id", "req_owner_no_token")
	ownerWithoutToken.Header.Del("X-Service-Token")
	ownerWithoutTokenRes := httptest.NewRecorder()
	server.ServeHTTP(ownerWithoutTokenRes, ownerWithoutToken)
	if ownerWithoutTokenRes.Code != http.StatusUnauthorized {
		t.Fatalf("owner without token status = %d, body = %s", ownerWithoutTokenRes.Code, ownerWithoutTokenRes.Body.String())
	}
	var ownerErr errorResponseBody
	decodeJSON(t, ownerWithoutTokenRes.Body, &ownerErr)
	if ownerErr.Error.Code != "unauthorized" || ownerErr.Error.RequestID != "req_owner_no_token" {
		t.Fatalf("owner without token error = %+v", ownerErr)
	}

	valid := newMultipartUploadRequest(t, "/internal/v1/files", "policy.pdf", "application/pdf", "content", nil, "")
	valid.Header.Set("X-Caller-Service", "knowledge")
	valid.Header.Set("X-Service-Token", "expected-token")
	validRes := httptest.NewRecorder()
	server.ServeHTTP(validRes, valid)
	if validRes.Code != http.StatusCreated {
		t.Fatalf("valid token status = %d, body = %s", validRes.Code, validRes.Body.String())
	}
}

func TestFileRoutesEnforceCallerPolicyByOperation(t *testing.T) {
	server := newReadyHTTPTestServer(t, filehttp.Config{
		MaxUploadBytes:       1024 * 1024,
		AllowedCreateCallers: []string{"document"},
		AllowedReadCallers:   []string{"qa"},
		AllowedDeleteCallers: []string{"document"},
	})

	create := newMultipartUploadRequest(t, "/internal/v1/files", "policy.txt", "text/plain", "content", nil, "")
	setCaller(create, "document")
	createRes := httptest.NewRecorder()
	server.ServeHTTP(createRes, create)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRes.Code, createRes.Body.String())
	}
	var created fileResponseBody
	decodeJSON(t, createRes.Body, &created)

	getAsDocument := internalRequestWithCaller(http.MethodGet, "/internal/v1/files/"+created.Data.ID, nil, "document")
	getAsDocumentRes := httptest.NewRecorder()
	server.ServeHTTP(getAsDocumentRes, getAsDocument)
	if getAsDocumentRes.Code != http.StatusForbidden {
		t.Fatalf("get as document status = %d, body = %s", getAsDocumentRes.Code, getAsDocumentRes.Body.String())
	}

	getAsQA := internalRequestWithCaller(http.MethodGet, "/internal/v1/files/"+created.Data.ID, nil, "qa")
	getAsQARes := httptest.NewRecorder()
	server.ServeHTTP(getAsQARes, getAsQA)
	if getAsQARes.Code != http.StatusOK {
		t.Fatalf("get as qa status = %d, body = %s", getAsQARes.Code, getAsQARes.Body.String())
	}

	contentAsQA := internalRequestWithCaller(http.MethodGet, "/internal/v1/files/"+created.Data.ID+"/content", nil, "qa")
	contentAsQARes := httptest.NewRecorder()
	server.ServeHTTP(contentAsQARes, contentAsQA)
	if contentAsQARes.Code != http.StatusOK {
		t.Fatalf("content as qa status = %d, body = %s", contentAsQARes.Code, contentAsQARes.Body.String())
	}

	deleteAsQA := internalRequestWithCaller(http.MethodDelete, "/internal/v1/files/"+created.Data.ID, nil, "qa")
	deleteAsQARes := httptest.NewRecorder()
	server.ServeHTTP(deleteAsQARes, deleteAsQA)
	if deleteAsQARes.Code != http.StatusForbidden {
		t.Fatalf("delete as qa status = %d, body = %s", deleteAsQARes.Code, deleteAsQARes.Body.String())
	}

	deleteAsDocument := internalRequestWithCaller(http.MethodDelete, "/internal/v1/files/"+created.Data.ID, nil, "document")
	deleteAsDocumentRes := httptest.NewRecorder()
	server.ServeHTTP(deleteAsDocumentRes, deleteAsDocument)
	if deleteAsDocumentRes.Code != http.StatusNoContent {
		t.Fatalf("delete as document status = %d, body = %s", deleteAsDocumentRes.Code, deleteAsDocumentRes.Body.String())
	}
}

func TestFileRoutesRequireCallerWhenPolicyConfigured(t *testing.T) {
	server := newReadyHTTPTestServer(t, filehttp.Config{
		MaxUploadBytes:       1024 * 1024,
		AllowedCreateCallers: []string{"document"},
	})
	req := newMultipartUploadRequest(t, "/internal/v1/files", "policy.txt", "text/plain", "content", nil, "")
	req.Header.Del("X-Caller-Service")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestReadyReportsMemoryMode(t *testing.T) {
	server := newHTTPTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.Header.Set("X-Request-Id", "req_ready")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body readyResponseBody
	decodeJSON(t, res.Body, &body)
	if body.RequestID != "req_ready" || body.Data.Status != "ready" || body.Data.MetadataBackend != "memory" {
		t.Fatalf("ready body = %+v", body)
	}
	if len(body.Data.Dependencies) != 1 || body.Data.Dependencies[0].Name != "postgres" || body.Data.Dependencies[0].Status != "not_configured" {
		t.Fatalf("dependencies = %+v", body.Data.Dependencies)
	}
}

func TestReadyReportsPostgresDependencyFailure(t *testing.T) {
	server := newReadyHTTPTestServer(t, filehttp.Config{
		MetadataBackend:  "postgres",
		StorageBackend:   "local",
		ReadinessChecker: errReadyChecker{err: errors.New("db down")},
		ReadinessTimeout: time.Second,
	})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req.Header.Set("X-Request-Id", "req_ready")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body readyResponseBody
	decodeJSON(t, res.Body, &body)
	if body.Data.Status != "not_ready" || body.Data.MetadataBackend != "postgres" || body.Data.StorageBackend != "local" {
		t.Fatalf("ready body = %+v", body)
	}
	if len(body.Data.Dependencies) != 1 || body.Data.Dependencies[0].Status != "unavailable" {
		t.Fatalf("dependencies = %+v", body.Data.Dependencies)
	}
	if strings.Contains(res.Body.String(), "db down") {
		t.Fatalf("ready response leaked dependency error: %s", res.Body.String())
	}
}

func TestReadyReportsPostgresDependencySuccess(t *testing.T) {
	server := newReadyHTTPTestServer(t, filehttp.Config{
		ServiceToken:     "token",
		MetadataBackend:  "postgres",
		StorageBackend:   "local",
		ReadinessChecker: okReadyChecker{},
		ReadinessTimeout: time.Second,
	})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body readyResponseBody
	decodeJSON(t, res.Body, &body)
	if body.Data.Status != "ready" || !body.Data.ServiceTokenConfigured {
		t.Fatalf("ready body = %+v", body)
	}
	if len(body.Data.Dependencies) != 1 || body.Data.Dependencies[0].Status != "ready" {
		t.Fatalf("dependencies = %+v", body.Data.Dependencies)
	}
}

func TestLegacyKnowledgeDocumentRoutesReturnNotFound(t *testing.T) {
	server := newHTTPTestServer(t)
	cases := []struct {
		method string
		path   string
		body   io.Reader
	}{
		{method: http.MethodPost, path: "/internal/v1/knowledge-bases/kb_123/documents", body: strings.NewReader("")},
		{method: http.MethodGet, path: "/internal/v1/documents/doc_123"},
		{method: http.MethodPatch, path: "/internal/v1/documents/doc_123", body: strings.NewReader(`{"tags":["updated"]}`)},
		{method: http.MethodDelete, path: "/internal/v1/documents/doc_123"},
		{method: http.MethodGet, path: "/internal/v1/documents/doc_123/content"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, tc.body)
		req.Header.Set("X-Request-Id", "req_legacy")
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, body = %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

func newHTTPTestServer(t *testing.T) http.Handler {
	t.Helper()
	return newLimitedHTTPTestServer(t, 1024*1024)
}

func newLimitedHTTPTestServer(t *testing.T, maxUploadBytes int64) http.Handler {
	t.Helper()
	repo := repository.NewMemoryRepository()
	store := storage.NewMemoryStore()
	files := service.New(repo, store)
	return filehttp.NewServer(files, filehttp.Config{MaxUploadBytes: maxUploadBytes})
}

func newTokenHTTPTestServer(t *testing.T, token string) http.Handler {
	t.Helper()
	return newReadyHTTPTestServer(t, filehttp.Config{MaxUploadBytes: 1024 * 1024, ServiceToken: token})
}

func newReadyHTTPTestServer(t *testing.T, cfg filehttp.Config) http.Handler {
	t.Helper()
	repo := repository.NewMemoryRepository()
	store := storage.NewMemoryStore()
	files := service.New(repo, store)
	if cfg.MaxUploadBytes == 0 {
		cfg.MaxUploadBytes = 1024 * 1024
	}
	return filehttp.NewServer(files, cfg)
}

func newMultipartUploadRequest(t *testing.T, target string, filename string, contentType string, content string, tags []string, checksum string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart() error = %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader(content)); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if checksum != "" {
		if err := writer.WriteField("checksumSha256", checksum); err != nil {
			t.Fatalf("WriteField(checksumSha256) error = %v", err)
		}
	}
	for _, tag := range tags {
		if err := writer.WriteField("tags", tag); err != nil {
			t.Fatalf("WriteField() error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := authorizedRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func unauthenticatedMultipartUploadRequest(t *testing.T, target string, filename string, contentType string, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename}))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		t.Fatalf("CreatePart() error = %v", err)
	}
	if _, err := io.Copy(part, strings.NewReader(content)); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("X-Request-Id", "req_test")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
func authorizedRequest(method string, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-Request-Id", "req_test")
	req.Header.Set("X-User-Id", "usr_123")
	req.Header.Set("X-User-Roles", "admin")
	req.Header.Set("X-User-Permissions", "document:read,document:upload,document:update,document:delete")
	return req
}

func internalRequest(method string, target string, body io.Reader) *http.Request {
	return internalRequestWithCaller(method, target, body, "knowledge")
}

func internalRequestWithCaller(method string, target string, body io.Reader, caller string) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-Request-Id", "req_test")
	setCaller(req, caller)
	return req
}

func setInternalCaller(req *http.Request) {
	setCaller(req, "knowledge")
}

func setCaller(req *http.Request, caller string) {
	req.Header.Set("X-Caller-Service", caller)
	req.Header.Set("X-Service-Token", "test-token")
}

func decodeJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func assertNotContainsSensitiveStorage(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"objectKey", "storageObjectKey", "storageBucket", "bucket", "files/", "minio", "accessKey", "secretKey"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
}

type successBody struct {
	Data      map[string]string `json:"data"`
	RequestID string            `json:"requestId"`
}

type fileResponseBody struct {
	Data struct {
		ID             string  `json:"id"`
		Filename       string  `json:"filename"`
		ContentType    string  `json:"contentType"`
		SizeBytes      int64   `json:"sizeBytes"`
		ChecksumSHA256 *string `json:"checksumSha256"`
		CreatedAt      string  `json:"createdAt"`
		DeletedAt      *string `json:"deletedAt"`
	} `json:"data"`
	RequestID string `json:"requestId"`
}

type errorResponseBody struct {
	Error struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		RequestID string            `json:"requestId"`
		Fields    map[string]string `json:"fields"`
	} `json:"error"`
}

type readyResponseBody struct {
	Data struct {
		Service                string `json:"service"`
		Status                 string `json:"status"`
		MetadataBackend        string `json:"metadataBackend"`
		StorageBackend         string `json:"storageBackend"`
		ServiceTokenConfigured bool   `json:"serviceTokenConfigured"`
		Dependencies           []struct {
			Name    string `json:"name"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"dependencies"`
	} `json:"data"`
	RequestID string `json:"requestId"`
}

type okReadyChecker struct{}

func (okReadyChecker) CheckReady(ctx context.Context) error {
	return nil
}

type errReadyChecker struct {
	err error
}

func (c errReadyChecker) CheckReady(ctx context.Context) error {
	return c.err
}

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/service"
)

type routeContract struct {
	Method      string
	Pattern     string
	Owner       string
	OperationID string
}

func TestActiveRouteMatrixCoversGatewayOwnerMap(t *testing.T) {
	if got, want := activeOperationCount(), 112; got != want {
		t.Fatalf("active operations = %d, want %d", got, want)
	}
	openAPIRoutes := gatewayOpenAPIRoutes(t, gatewayOpenAPIPath(t))
	if got, want := activeOperationCount(), len(openAPIRoutes); got != want {
		t.Fatalf("active operations = %d, want %d from gateway OpenAPI", got, want)
	}
	ownerCounts := map[string]int{}
	seen := map[string]bool{}
	for _, route := range append(activeDirectRoutes, activeProxyRoutes...) {
		ownerCounts[route.Owner]++
		key := route.Method + " " + route.Pattern
		if seen[key] {
			t.Fatalf("duplicate route %s", key)
		}
		seen[key] = true
		contract, ok := openAPIRoutes[key]
		if !ok {
			t.Fatalf("route %s is missing from gateway OpenAPI", key)
		}
		if route.Owner != contract.Owner {
			t.Fatalf("route %s owner = %q, want %q", key, route.Owner, contract.Owner)
		}
		if route.OperationID != contract.OperationID {
			t.Fatalf("route %s operationId = %q, want %q", key, route.OperationID, contract.OperationID)
		}
	}
	expected := map[string]int{
		"gateway":    5,
		"auth":       11,
		"knowledge":  18,
		"ai-gateway": 5,
		"document":   44,
		"qa":         29,
	}
	for owner, want := range expected {
		if got := ownerCounts[owner]; got != want {
			t.Fatalf("%s routes = %d, want %d", owner, got, want)
		}
	}
	for key := range openAPIRoutes {
		if !seen[key] {
			t.Fatalf("gateway OpenAPI route %s is missing from activeProxyRoutes", key)
		}
	}
}

func TestValidateOwnerBaseURLsAcceptsSafeURLsAndEmptyValues(t *testing.T) {
	values := map[string]string{
		"auth":       "http://auth:8001",
		"knowledge":  "http://knowledge:8083",
		"qa":         "https://localhost:8084/base",
		"document":   "http://127.0.0.1:8085",
		"ai-gateway": "http://[::1]:8086",
		"empty":      "",
		"blank":      "   ",
	}

	if err := ValidateOwnerBaseURLs(values); err != nil {
		t.Fatalf("ValidateOwnerBaseURLs() error = %v", err)
	}
}

func TestValidateOwnerBaseURLsRejectsUnsafeURLsWithoutEchoingRawURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "missing host", raw: "knowledge:8082"},
		{name: "unsupported scheme", raw: "ftp://knowledge:8082"},
		{name: "credentials", raw: "http://user:pass@knowledge:8082"},
		{name: "query", raw: "http://knowledge:8082?token=secret"},
		{name: "empty query marker", raw: "http://knowledge:8082?"},
		{name: "fragment", raw: "http://knowledge:8082#readyz"},
		{name: "empty fragment marker", raw: "http://knowledge:8082#"},
		{name: "public host", raw: "https://public.example.test"},
		{name: "private ip", raw: "http://10.0.0.5:8083"},
		{name: "link local ip", raw: "http://169.254.169.254"},
		{name: "wrong owner service host", raw: "http://qa:8084"},
		{name: "parse error", raw: "http://knowledge/%zz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOwnerBaseURLs(map[string]string{"knowledge": tt.raw})
			if err == nil {
				t.Fatal("ValidateOwnerBaseURLs() error = nil")
			}
			if strings.Contains(err.Error(), tt.raw) {
				t.Fatalf("error leaked raw URL: %v", err)
			}
		})
	}
}

func TestNotImplementedRoutesReturnStableGatewayError(t *testing.T) {
	hasher, err := service.NewTokenHasher("test-secret", "v1")
	if err != nil {
		t.Fatalf("NewTokenHasher() error = %v", err)
	}
	token := "valid-token"
	tokenHash, err := hasher.Hash(token)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	server := NewServer(Config{
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceVersion:     "test",
		Environment:        "test",
		RequestTimeout:     time.Second,
		MaxBodyBytes:       1024 * 1024,
		CORSAllowedOrigins: []string{"*"},
		SessionStore: fixedSessionStore{entry: service.SessionCacheEntry{
			SessionID:       "sess_1",
			UserID:          "usr_1",
			Username:        "alice",
			Roles:           []string{"admin"},
			Permissions:     []string{"knowledge:read", "system:admin"},
			TokenType:       "Bearer",
			AccessTokenHash: tokenHash,
			IssuedAt:        time.Now().Add(-time.Minute).UTC(),
			ExpiresAt:       time.Now().Add(time.Hour).UTC(),
			CachedAt:        time.Now().UTC(),
		}},
		TokenHasher: hasher,
		HTTPClient: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("not implemented route should not call downstream")
			return nil, nil
		}).client(),
	})

	tested := 0
	for _, route := range activeProxyRoutes {
		if !route.NotImplemented {
			continue
		}
		tested++
		t.Run(route.Method+" "+route.Pattern, func(t *testing.T) {
			req := httptest.NewRequest(route.Method, samplePath(route.Pattern), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Request-Id", "req_not_implemented")
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			if res.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			var body struct {
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					RequestID string `json:"requestId"`
				} `json:"error"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if body.Error.Code != "not_implemented" ||
				body.Error.Message != "route is not implemented" ||
				body.Error.RequestID != "req_not_implemented" {
				t.Fatalf("error = %+v", body.Error)
			}
		})
	}
	if tested == 0 {
		t.Skip("no not implemented proxy routes are currently registered")
	}
}

func TestGatewayDoesNotImportBusinessInfrastructureClients(t *testing.T) {
	root := gatewayServiceRoot(t)
	forbidden := []string{
		"database/sql",
		"github.com/jackc/pgx",
		"github.com/minio/",
		"github.com/qdrant/",
		"github.com/openai/",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			for _, forbiddenPrefix := range forbidden {
				if strings.HasPrefix(importPath, forbiddenPrefix) {
					t.Fatalf("gateway must not import %q in %s", importPath, path)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk gateway service root: %v", err)
	}
}

func gatewayServiceRoot(t *testing.T) string {
	t.Helper()
	dir := filepath.Dir(gatewayOpenAPIPath(t))
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "services", "gateway", "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Dir(candidate)
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("services/gateway/go.mod not found")
	return ""
}

func gatewayOpenAPIRoutes(t *testing.T, yamlPath string) map[string]routeContract {
	t.Helper()
	f, err := os.Open(yamlPath)
	if err != nil {
		t.Fatalf("open %s: %v", yamlPath, err)
	}
	defer f.Close()

	routes := map[string]routeContract{}
	var currentPath string
	var current *routeContract
	finalize := func() {
		if current != nil && current.Owner == "" && isGatewayOperationalPath(current.Pattern) {
			current.Owner = "gateway"
		}
		if current == nil || !isActiveOwner(current.Owner) {
			return
		}
		if current.Method == "" || current.Pattern == "" || current.OperationID == "" {
			t.Fatalf("incomplete OpenAPI operation: %+v", current)
		}
		routes[current.Method+" "+current.Pattern] = *current
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(line, "    ") {
			finalize()
			currentPath = strings.TrimSuffix(trimmed, ":")
			current = nil
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":") {
			method := strings.TrimSuffix(trimmed, ":")
			if isHTTPMethod(method) {
				finalize()
				current = &routeContract{
					Method:  strings.ToUpper(method),
					Pattern: currentPath,
				}
			}
			continue
		}
		if current == nil {
			continue
		}
		if value, ok := strings.CutPrefix(trimmed, "operationId:"); ok {
			current.OperationID = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(trimmed, "x-owner-service:"); ok {
			current.Owner = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", yamlPath, err)
	}
	finalize()
	return routes
}

func isActiveOwner(owner string) bool {
	switch owner {
	case "gateway", "auth", "knowledge", "ai-gateway", "document", "qa":
		return true
	default:
		return false
	}
}

func isGatewayOperationalPath(path string) bool {
	return path == "/healthz" || path == "/readyz"
}

func isHTTPMethod(method string) bool {
	switch method {
	case "get", "post", "patch", "delete":
		return true
	default:
		return false
	}
}

func samplePath(pattern string) string {
	replacer := strings.NewReplacer(
		"{knowledgeBaseId}", "kb_1",
		"{documentId}", "doc_1",
		"{profileId}", "mp_1",
		"{parserConfigId}", "pc_1",
		"{reportTemplateId}", "rt_1",
		"{materialId}", "mat_1",
		"{reportId}", "rep_1",
		"{outlineId}", "outline_1",
		"{sectionId}", "sec_1",
		"{jobId}", "job_1",
		"{reportFileId}", "file_1",
		"{sessionId}", "sess_1",
		"{responseRunId}", "run_1",
		"{messageId}", "msg_1",
		"{citationId}", "cit_1",
		"{testRunId}", "test_1",
	)
	return replacer.Replace(pattern)
}

type fixedSessionStore struct {
	entry service.SessionCacheEntry
}

func (s fixedSessionStore) Put(context.Context, service.SessionCacheEntry, time.Duration) error {
	return nil
}

func (s fixedSessionStore) Get(_ context.Context, accessTokenHash string) (service.SessionCacheEntry, error) {
	if accessTokenHash != s.entry.AccessTokenHash {
		return service.SessionCacheEntry{}, service.ErrSessionNotFound
	}
	return s.entry, nil
}

func (s fixedSessionStore) Delete(context.Context, string) error {
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func (f roundTripFunc) client() *http.Client {
	return &http.Client{Transport: f}
}

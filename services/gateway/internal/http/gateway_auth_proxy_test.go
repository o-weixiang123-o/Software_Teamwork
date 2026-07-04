package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gatewayhttp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/platform/authclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/service"
)

func TestCreateSessionCachesSessionWithoutRawToken(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	rawToken := "opaque-token-value-that-must-not-be-cached"
	auth := &fakeAuthClient{
		createSessionResult: service.SessionResponse{
			User: service.UserSummary{
				ID:          "usr_1",
				Username:    "alice",
				Roles:       []string{"admin"},
				Permissions: []string{"knowledge:read"},
			},
			Session: service.SessionSummary{
				SessionID:   "sess_1",
				AccessToken: rawToken,
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(time.Hour).UTC(),
			},
		},
	}
	server := newGatewayTestServer(t, gatewayDeps{auth: auth, store: store, hasher: hasher})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("X-Request-Id", "req_session")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if len(store.entries) != 1 {
		t.Fatalf("cached entries = %d", len(store.entries))
	}
	for key, entry := range store.entries {
		if strings.Contains(key, rawToken) || strings.Contains(entry.AccessTokenHash, rawToken) {
			t.Fatalf("raw token leaked into cache key or entry: key=%q entry=%+v", key, entry)
		}
		if entry.UserID != "usr_1" || entry.SessionID != "sess_1" || entry.RequestID != "req_session" {
			t.Fatalf("cache entry = %+v", entry)
		}
	}
	var body service.SessionEnvelope
	decodeJSON(t, res.Body, &body)
	if body.Data.Session.AccessToken != rawToken {
		t.Fatalf("access token response = %q", body.Data.Session.AccessToken)
	}
}

func TestProtectedRouteMissingTokenReturnsUnauthorized(t *testing.T) {
	server := newGatewayTestServer(t, gatewayDeps{store: newMemorySessionStore(), hasher: testHasher(t)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("X-Request-Id", "req_missing_token")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "unauthorized" || body.Error.RequestID != "req_missing_token" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestProtectedRouteSessionStoreFailureReturnsDependencyError(t *testing.T) {
	server := newGatewayTestServer(t, gatewayDeps{store: failingSessionStore{}, hasher: testHasher(t)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("X-Request-Id", "req_redis_down")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "dependency_error" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestProxyTreatsUnsafeOwnerBaseURLAsNotConfigured(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": "ftp://knowledge.internal"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_unsafe_owner_url")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "dependency_error" || body.Error.RequestID != "req_unsafe_owner_url" {
		t.Fatalf("error = %+v", body.Error)
	}
	if strings.Contains(res.Body.String(), "ftp://") || strings.Contains(res.Body.String(), "knowledge.internal") {
		t.Fatalf("response leaked unsafe owner URL: %s", res.Body.String())
	}
}

func TestProxyInjectsAuthenticatedContextHeaders(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin", "operator"},
		Permissions: []string{"knowledge:read", "document:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	var captured http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/knowledge-bases" {
			t.Fatalf("downstream path = %q", r.URL.Path)
		}
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"requestId":"req_proxy"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         hasherStore{SessionStore: store},
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
		serviceToken:  "svc-token",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases?page=1", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_proxy")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if captured.Get("X-Request-Id") != "req_proxy" ||
		captured.Get("X-User-Id") != "usr_1" ||
		captured.Get("X-User-Roles") != "admin,operator" ||
		captured.Get("X-User-Permissions") != "knowledge:read,document:write" ||
		captured.Get("X-Caller-Service") != "gateway" ||
		captured.Get("X-Service-Token") != "svc-token" {
		t.Fatalf("downstream headers = %#v", captured)
	}
	if captured.Get("Authorization") != "" {
		t.Fatalf("authorization leaked to downstream: %q", captured.Get("Authorization"))
	}
}

func TestKnowledgeQueriesRouteProxiesToKnowledge(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"analyst"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	var capturedPath string
	var capturedHeader http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"kq_1","query":"breaker policy","results":[]},"requestId":"req_query"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
		serviceToken:  "svc-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-queries", strings.NewReader(`{"query":"breaker policy","topK":3}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_query")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code == http.StatusNotImplemented {
		t.Fatalf("status = %d, route should proxy instead of returning 501", res.Code)
	}
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if capturedPath != "/internal/v1/knowledge-queries" {
		t.Fatalf("downstream path = %q", capturedPath)
	}
	if capturedHeader.Get("X-User-Id") != "usr_1" ||
		capturedHeader.Get("X-User-Roles") != "analyst" ||
		capturedHeader.Get("X-User-Permissions") != "knowledge:read" ||
		capturedHeader.Get("X-Request-Id") != "req_query" ||
		capturedHeader.Get("X-Caller-Service") != "gateway" ||
		capturedHeader.Get("X-Service-Token") != "svc-token" {
		t.Fatalf("downstream headers = %#v", capturedHeader)
	}
	if !strings.Contains(res.Body.String(), `"id":"kq_1"`) {
		t.Fatalf("response body was not proxied: %s", res.Body.String())
	}
}

func TestKnowledgeWriteRouteRejectsReadOnlyUserBeforeProxy(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_reader",
		Username:    "reader",
		Roles:       []string{"standard"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called for a read-only knowledge user")
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
		serviceToken:  "svc-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases", strings.NewReader(`{"name":"Docs"}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_kb_write_readonly")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestAdminUsersRouteProxiesToAuthWithActorContext(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_admin",
		Username:    "admin",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	var capturedPath string
	var capturedHeader http.Header
	authDownstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"page":{"page":1,"pageSize":20,"total":0},"requestId":"req_users"}`))
	}))
	defer authDownstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:                 store,
		hasher:                hasher,
		ownerBaseURLs:         map[string]string{"auth": authDownstream.URL},
		serviceToken:          "svc-token",
		authAdminServiceToken: "auth-admin-token",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_users")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if capturedPath != "/internal/v1/admin/users" ||
		capturedHeader.Get("X-User-Id") != "usr_admin" ||
		capturedHeader.Get("X-User-Roles") != "admin" ||
		capturedHeader.Get("X-Caller-Service") != "gateway" ||
		capturedHeader.Get("X-Service-Token") != "auth-admin-token" {
		t.Fatalf("downstream path/header = %s %#v", capturedPath, capturedHeader)
	}
}

func TestAdminUsersRouteUsesDedicatedAuthAdminServiceToken(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_admin",
		Username:    "admin",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	authDownstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Service-Token"); got != "auth-admin-token" {
			t.Fatalf("X-Service-Token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"page":{"page":1,"pageSize":20,"total":0},"requestId":"req_users"}`))
	}))
	defer authDownstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:                 store,
		hasher:                hasher,
		ownerBaseURLs:         map[string]string{"auth": authDownstream.URL},
		serviceToken:          "svc-token",
		authAdminServiceToken: "auth-admin-token",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_users")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestAdminUsersRouteRejectsBareSystemAdminPermission(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_system_admin",
		Username:    "system-admin",
		Roles:       []string{"standard"},
		Permissions: []string{"system:admin"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	authDownstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("bare system:admin must not reach auth user management")
	}))
	defer authDownstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"auth": authDownstream.URL},
		serviceToken:  "svc-token",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_users_forbidden")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestMustChangePasswordBlocksProtectedRoutesButAllowsPasswordChange(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:          "sess_1",
		UserID:             "usr_1",
		Username:           "alice",
		Roles:              []string{"standard"},
		Permissions:        []string{"knowledge:read"},
		TokenType:          "Bearer",
		ExpiresAt:          time.Now().Add(time.Hour).UTC(),
		MustChangePassword: true,
		Status:             "active",
	})
	auth := &fakeAuthClient{
		getUserResult: service.UserRecord{
			ID:                 "usr_1",
			Username:           "alice",
			Roles:              []string{"standard"},
			Permissions:        []string{"knowledge:read"},
			Status:             "active",
			MustChangePassword: true,
		},
		changePasswordResult: service.UserRecord{
			ID:          "usr_1",
			Username:    "alice",
			Roles:       []string{"standard"},
			Permissions: []string{"knowledge:read"},
			Status:      "active",
		},
	}
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("protected downstream should not be called before password change")
	}))
	defer downstream.Close()
	server := newGatewayTestServer(t, gatewayDeps{
		auth:          auth,
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})

	blockedReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	blockedReq.Header.Set("Authorization", "Bearer "+accessToken)
	blockedRes := httptest.NewRecorder()
	server.ServeHTTP(blockedRes, blockedReq)
	if blockedRes.Code != http.StatusForbidden {
		t.Fatalf("blocked status = %d, body = %s", blockedRes.Code, blockedRes.Body.String())
	}

	profilePatchReq := httptest.NewRequest(http.MethodPatch, "/api/v1/users/me/profile", strings.NewReader(`{"displayName":"Alice"}`))
	profilePatchReq.Header.Set("Authorization", "Bearer "+accessToken)
	profilePatchRes := httptest.NewRecorder()
	server.ServeHTTP(profilePatchRes, profilePatchReq)
	if profilePatchRes.Code != http.StatusForbidden {
		t.Fatalf("profile patch status = %d, body = %s", profilePatchRes.Code, profilePatchRes.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/current", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	logoutRes := httptest.NewRecorder()
	server.ServeHTTP(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logoutRes.Code, logoutRes.Body.String())
	}

	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:          "sess_1",
		UserID:             "usr_1",
		Username:           "alice",
		Roles:              []string{"standard"},
		Permissions:        []string{"knowledge:read"},
		TokenType:          "Bearer",
		ExpiresAt:          time.Now().Add(time.Hour).UTC(),
		MustChangePassword: true,
		Status:             "active",
	})

	changeReq := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/password-changes", strings.NewReader(`{"currentPassword":"temporary","newPassword":"new-password","newPasswordConfirmation":"new-password"}`))
	changeReq.Header.Set("Authorization", "Bearer "+accessToken)
	changeRes := httptest.NewRecorder()
	server.ServeHTTP(changeRes, changeReq)
	if changeRes.Code != http.StatusOK {
		t.Fatalf("change status = %d, body = %s", changeRes.Code, changeRes.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+accessToken)
	meRes := httptest.NewRecorder()
	server.ServeHTTP(meRes, meReq)
	if meRes.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meRes.Code, meRes.Body.String())
	}
	var meBody service.UserEnvelope
	decodeJSON(t, meRes.Body, &meBody)
	if meBody.Data.MustChangePassword {
		t.Fatalf("mustChangePassword = true after password change")
	}
}

func TestKnowledgeDocumentChunkAndContentRoutesProxyToKnowledge(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"analyst"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	captured := map[string]http.Header{}
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured[r.URL.Path] = r.Header.Clone()
		switch r.URL.Path {
		case "/internal/v1/documents/doc_1/chunks":
			if r.URL.RawQuery != "page=1&pageSize=10" {
				t.Fatalf("chunks query = %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"chunk_1"}],"page":{"page":1,"pageSize":10,"total":1},"requestId":"req_chunks"}`))
		case "/internal/v1/documents/doc_1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0, 1, 2, 3})
		default:
			t.Fatalf("downstream path = %q", r.URL.Path)
		}
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
		serviceToken:  "svc-token",
	})

	chunksReq := httptest.NewRequest(http.MethodGet, "/api/v1/documents/doc_1/chunks?page=1&pageSize=10", nil)
	chunksReq.Header.Set("Authorization", "Bearer "+accessToken)
	chunksReq.Header.Set("X-Request-Id", "req_chunks")
	chunksRes := httptest.NewRecorder()
	server.ServeHTTP(chunksRes, chunksReq)
	if chunksRes.Code == http.StatusNotImplemented {
		t.Fatalf("chunks status = %d, route should proxy instead of returning 501", chunksRes.Code)
	}
	if chunksRes.Code != http.StatusOK || !strings.Contains(chunksRes.Body.String(), `"chunk_1"`) {
		t.Fatalf("chunks status = %d, body = %s", chunksRes.Code, chunksRes.Body.String())
	}

	contentReq := httptest.NewRequest(http.MethodGet, "/api/v1/documents/doc_1/content", nil)
	contentReq.Header.Set("Authorization", "Bearer "+accessToken)
	contentReq.Header.Set("X-Request-Id", "req_content")
	contentRes := httptest.NewRecorder()
	server.ServeHTTP(contentRes, contentReq)
	if contentRes.Code == http.StatusNotImplemented {
		t.Fatalf("content status = %d, route should proxy instead of returning 501", contentRes.Code)
	}
	if contentRes.Code != http.StatusOK {
		t.Fatalf("content status = %d, body = %q", contentRes.Code, contentRes.Body.String())
	}
	if got := contentRes.Body.Bytes(); !bytes.Equal(got, []byte{0, 1, 2, 3}) {
		t.Fatalf("content body = %#v", got)
	}

	for path, requestID := range map[string]string{
		"/internal/v1/documents/doc_1/chunks":  "req_chunks",
		"/internal/v1/documents/doc_1/content": "req_content",
	} {
		header := captured[path]
		if header.Get("X-User-Id") != "usr_1" ||
			header.Get("X-User-Roles") != "analyst" ||
			header.Get("X-User-Permissions") != "knowledge:read" ||
			header.Get("X-Request-Id") != requestID ||
			header.Get("X-Caller-Service") != "gateway" ||
			header.Get("X-Service-Token") != "svc-token" {
			t.Fatalf("downstream headers for %s = %#v", path, header)
		}
	}
}

func TestProxyOverwritesSpoofedForwardingHeaders(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	var captured http.Header
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"requestId":"req_proxy"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_proxy")
	req.Header.Set("Forwarded", "for=203.0.113.9;proto=https")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if captured.Get("X-Forwarded-For") != "198.51.100.10" {
		t.Fatalf("x-forwarded-for = %q", captured.Get("X-Forwarded-For"))
	}
	if captured.Get("X-Forwarded-Proto") != "http" {
		t.Fatalf("x-forwarded-proto = %q", captured.Get("X-Forwarded-Proto"))
	}
	if captured.Get("Forwarded") != "" || captured.Get("X-Forwarded-Host") != "" {
		t.Fatalf("spoofed forwarding headers leaked: %#v", captured)
	}
}

func TestAuthClientErrorIsSanitized(t *testing.T) {
	auth := &fakeAuthClient{
		createSessionErr: &authclient.RemoteError{
			Status: http.StatusBadRequest,
			Detail: authclient.ErrorDetail{
				Code:    "internal_sql_error",
				Message: "select * from auth_credentials",
				Fields:  map[string]string{"password_hash": "secret"},
			},
		},
	}
	server := newGatewayTestServer(t, gatewayDeps{
		auth:   auth,
		store:  newMemorySessionStore(),
		hasher: testHasher(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("X-Request-Id", "req_auth_sanitized")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	raw := res.Body.String()
	for _, sensitive := range []string{"internal_sql_error", "auth_credentials", "password_hash", "select *"} {
		if strings.Contains(raw, sensitive) {
			t.Fatalf("auth internal detail leaked in response: %s", raw)
		}
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "validation_error" ||
		body.Error.Message != "request validation failed" ||
		body.Error.RequestID != "req_auth_sanitized" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestCreateSessionRateLimitedPropagatesRetryAfter(t *testing.T) {
	auth := &fakeAuthClient{
		createSessionErr: &authclient.RemoteError{
			Status:     http.StatusTooManyRequests,
			RetryAfter: "90",
			Detail: authclient.ErrorDetail{
				Code:    "rate_limited",
				Message: "rate limited",
			},
		},
	}
	server := newGatewayTestServer(t, gatewayDeps{
		auth:   auth,
		store:  newMemorySessionStore(),
		hasher: testHasher(t),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("X-Request-Id", "req_auth_rate_limited")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Retry-After"); got != "90" {
		t.Fatalf("Retry-After = %q", got)
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "rate_limited" || body.Error.RequestID != "req_auth_rate_limited" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestProtectedRouteRejectsRevokedAuthoritativeSession(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	revokedAt := time.Now().UTC()
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called for a revoked session")
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		auth: &fakeAuthClient{
			getSessionResult: service.SessionIdentity{
				SessionID: "sess_1",
				User: service.UserSummary{
					ID:          "usr_1",
					Username:    "alice",
					Roles:       []string{"admin"},
					Permissions: []string{"knowledge:read"},
				},
				TokenType: "Bearer",
				Status:    "active",
				ExpiresAt: time.Now().Add(time.Hour).UTC(),
				IssuedAt:  time.Now().Add(-time.Minute).UTC(),
				RevokedAt: &revokedAt,
			},
			getUserResult: service.UserRecord{
				ID:          "usr_1",
				Username:    "alice",
				Roles:       []string{"admin"},
				Permissions: []string{"knowledge:read"},
				Status:      "active",
			},
		},
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_revoked")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	hash, err := hasher.Hash(accessToken)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if _, err := store.Get(context.Background(), hash); !errors.Is(err, service.ErrSessionNotFound) {
		t.Fatalf("cache lookup error = %v, want ErrSessionNotFound", err)
	}
}

func TestProtectedRouteRejectsRevokedSessionStatus(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called for a revoked session")
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		auth: &fakeAuthClient{
			getSessionResult: service.SessionIdentity{
				SessionID: "sess_1",
				User: service.UserSummary{
					ID:          "usr_1",
					Username:    "alice",
					Roles:       []string{"admin"},
					Permissions: []string{"knowledge:read"},
				},
				TokenType: "Bearer",
				Status:    "revoked",
				ExpiresAt: time.Now().Add(time.Hour).UTC(),
				IssuedAt:  time.Now().Add(-time.Minute).UTC(),
			},
			getUserResult: service.UserRecord{
				ID:          "usr_1",
				Username:    "alice",
				Roles:       []string{"admin"},
				Permissions: []string{"knowledge:read"},
				Status:      "active",
			},
		},
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_revoked_status")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	hash, err := hasher.Hash(accessToken)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if _, err := store.Get(context.Background(), hash); !errors.Is(err, service.ErrSessionNotFound) {
		t.Fatalf("cache lookup error = %v, want ErrSessionNotFound", err)
	}
}

func TestProtectedRouteTreatsAuthAuthorityUnauthorizedAsDependencyError(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called when auth authority is unavailable")
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		auth: &fakeAuthClient{
			getSessionErr: &authclient.RemoteError{
				Status: http.StatusUnauthorized,
				Detail: authclient.ErrorDetail{Code: "unauthorized", Message: "service authentication required"},
			},
		},
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_auth_dependency")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "dependency_error" {
		t.Fatalf("error = %+v", body.Error)
	}
	hash, err := hasher.Hash(accessToken)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if _, err := store.Get(context.Background(), hash); err != nil {
		t.Fatalf("cache lookup error = %v, want cached entry retained", err)
	}
}

func TestProtectedRouteAuthRefreshLimiterReturnsRateLimitedWhenSaturated(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	auth := newBlockingAuthRefreshClient()
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"requestId":"req_refresh_first"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		auth:                   auth,
		store:                  store,
		hasher:                 hasher,
		ownerBaseURLs:          map[string]string{"knowledge": downstream.URL},
		authRefreshMaxInFlight: 1,
	})

	firstReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	firstReq.Header.Set("Authorization", "Bearer "+accessToken)
	firstReq.Header.Set("X-Request-Id", "req_refresh_first")
	firstRes := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		server.ServeHTTP(firstRes, firstReq)
	}()

	auth.waitEntered(t)

	secondReq := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	secondReq.Header.Set("Authorization", "Bearer "+accessToken)
	secondReq.Header.Set("X-Request-Id", "req_refresh_second")
	secondRes := httptest.NewRecorder()
	server.ServeHTTP(secondRes, secondReq)

	if secondRes.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", secondRes.Code, secondRes.Body.String())
	}
	var body errorBody
	decodeJSON(t, secondRes.Body, &body)
	if body.Error.Code != "rate_limited" || body.Error.RequestID != "req_refresh_second" {
		t.Fatalf("error = %+v", body.Error)
	}
	if calls := auth.sessionCalls(); calls != 1 {
		t.Fatalf("GetSession calls = %d, want 1", calls)
	}

	auth.release()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first request did not finish")
	}
	if firstRes.Code != http.StatusOK {
		t.Fatalf("first status = %d, body = %s", firstRes.Code, firstRes.Body.String())
	}
	if max := auth.maxConcurrentSessions(); max != 1 {
		t.Fatalf("max concurrent GetSession calls = %d, want 1", max)
	}
}

func TestProtectedRouteAuthRefreshLimiterDisabledAllowsRefresh(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	auth := &fakeAuthClient{}
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"requestId":"req_refresh_disabled"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		auth:                   auth,
		store:                  store,
		hasher:                 hasher,
		ownerBaseURLs:          map[string]string{"knowledge": downstream.URL},
		authRefreshMaxInFlight: 0,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_refresh_disabled")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestProxyMapsGatewayPathsToOwnerServiceNamespaces(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read", "qa:read", "document:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	cases := []struct {
		name     string
		owner    string
		method   string
		path     string
		expected string
	}{
		{
			name:     "knowledge internal namespace",
			owner:    "knowledge",
			method:   http.MethodGet,
			path:     "/api/v1/knowledge-bases/kb_1",
			expected: "/internal/v1/knowledge-bases/kb_1",
		},
		{
			name:     "document service root namespace",
			owner:    "document",
			method:   http.MethodGet,
			path:     "/api/v1/report-types",
			expected: "/report-types",
		},
		{
			name:     "qa internal namespace",
			owner:    "qa",
			method:   http.MethodGet,
			path:     "/api/v1/qa-sessions/sess_1/messages",
			expected: "/internal/v1/qa-sessions/sess_1/messages",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedPath string
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{},"requestId":"req_proxy"}`))
			}))
			defer downstream.Close()

			server := newGatewayTestServer(t, gatewayDeps{
				store:         store,
				hasher:        hasher,
				ownerBaseURLs: map[string]string{tc.owner: downstream.URL},
			})
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("X-Request-Id", "req_proxy")
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			if capturedPath != tc.expected {
				t.Fatalf("downstream path = %q, want %q", capturedPath, tc.expected)
			}
		})
	}
}

func TestProxyUsesDownstreamPathTemplate(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"admin:model-profile:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})

	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/model-profiles/mp_1" {
			t.Fatalf("downstream path = %q", r.URL.Path)
		}
		if r.URL.RawQuery != "includeDisabled=true" {
			t.Fatalf("downstream query = %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"mp_1"},"requestId":"req_model_profile"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"ai-gateway": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/model-profiles/mp_1?includeDisabled=true", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_model_profile")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestProxyStreamsBinaryContentWithoutJSONEnvelope(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 2, 3})
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"document": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/report-files/file_1/content", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", res.Code, res.Body.String())
	}
	if got := res.Body.Bytes(); !bytes.Equal(got, []byte{0, 1, 2, 3}) {
		t.Fatalf("body = %#v", got)
	}
}

func TestProxyStreamsSSEWithoutFixedTimeout(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"qa:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/qa-sessions/sess_1/messages" {
			t.Fatalf("downstream path = %q", r.URL.Path)
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: ok\n\n"))
	}))
	defer downstream.Close()

	server := gatewayhttp.NewServer(gatewayhttp.Config{
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceVersion:     "test",
		Environment:        "test",
		RequestTimeout:     10 * time.Millisecond,
		MaxBodyBytes:       1024 * 1024,
		CORSAllowedOrigins: []string{"*"},
		DownstreamTimeout:  10 * time.Millisecond,
		SessionStore:       store,
		TokenHasher:        hasher,
		OwnerBaseURLs:      map[string]string{"qa": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/qa-sessions/sess_1/messages", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "text/event-stream")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Body.String(); !strings.Contains(got, "data: ok") {
		t.Fatalf("body = %q", got)
	}
}

func TestDocumentPatchRouteProxiesToKnowledge(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"knowledge:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	var capturedMethod string
	var capturedPath string
	var capturedBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read downstream body: %v", err)
		}
		capturedBody = string(body)
		if r.Header.Get("X-User-Id") != "usr_1" ||
			r.Header.Get("X-User-Permissions") != "knowledge:write" ||
			r.Header.Get("X-Request-Id") != "req_patch" {
			t.Fatalf("downstream headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"doc_1"},"requestId":"req_patch"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/documents/doc_1", strings.NewReader(`{"tags":["锅炉"]}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_patch")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code == http.StatusNotImplemented {
		t.Fatalf("status = %d, route should proxy instead of returning 501", res.Code)
	}
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if capturedMethod != http.MethodPatch || capturedPath != "/internal/v1/documents/doc_1" {
		t.Fatalf("downstream method/path = %s %s", capturedMethod, capturedPath)
	}
	if !strings.Contains(capturedBody, `"tags"`) {
		t.Fatalf("downstream body = %q", capturedBody)
	}
}

func TestReportJobCancelPatchRouteProxiesToDocument(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"document:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	var capturedMethod string
	var capturedPath string
	var capturedBody string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read downstream body: %v", err)
		}
		capturedBody = string(body)
		if r.Header.Get("X-User-Id") != "usr_1" ||
			r.Header.Get("X-User-Permissions") != "document:write" ||
			r.Header.Get("X-Request-Id") != "req_cancel_job" {
			t.Fatalf("downstream headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"job_cancel","reportId":"rpt_1","jobType":"content_generation","status":"canceled","createdAt":"2026-07-03T00:00:00Z"},"requestId":"req_cancel_job"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"document": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/report-jobs/job_cancel", strings.NewReader(`{"status":"canceled"}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req_cancel_job")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code == http.StatusNotFound && strings.Contains(res.Body.String(), "route not found") {
		t.Fatalf("report job cancellation route returned gateway route not found: %s", res.Body.String())
	}
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if capturedMethod != http.MethodPatch || capturedPath != "/report-jobs/job_cancel" {
		t.Fatalf("downstream method/path = %s %s", capturedMethod, capturedPath)
	}
	if !strings.Contains(capturedBody, `"status":"canceled"`) {
		t.Fatalf("downstream body = %q", capturedBody)
	}
}

func TestProxyNormalizesDownstreamErrorBody(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"knowledge:read"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"sql":"select * from private_table","internalUrl":"http://knowledge.internal"}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases/kb_1", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_downstream_404")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	raw := res.Body.String()
	if strings.Contains(raw, "private_table") || strings.Contains(raw, "knowledge.internal") {
		t.Fatalf("downstream raw body leaked: %s", raw)
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "not_found" || body.Error.RequestID != "req_downstream_404" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestProxySanitizesDownstreamErrorEnvelope(t *testing.T) {
	hasher := testHasher(t)
	store := newMemorySessionStore()
	accessToken := "valid-token"
	store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
		SessionID:   "sess_1",
		UserID:      "usr_1",
		Username:    "alice",
		Roles:       []string{},
		Permissions: []string{"knowledge:write"},
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour).UTC(),
	})
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error","message":"select * from private_table","requestId":"downstream","fields":{"password_hash":"secret","internalUrl":"http://knowledge.internal"}}}`))
	}))
	defer downstream.Close()

	server := newGatewayTestServer(t, gatewayDeps{
		store:         store,
		hasher:        hasher,
		ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases", strings.NewReader(`{"name":""}`))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Request-Id", "req_downstream_400")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	raw := res.Body.String()
	for _, sensitive := range []string{"private_table", "password_hash", "knowledge.internal", "select *", "fields"} {
		if strings.Contains(raw, sensitive) {
			t.Fatalf("downstream detail leaked: %s", raw)
		}
	}
	var body errorBody
	decodeJSON(t, res.Body, &body)
	if body.Error.Code != "validation_error" ||
		body.Error.Message != "request validation failed" ||
		body.Error.RequestID != "req_downstream_400" {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestProxyMapsDownstreamErrorCodes(t *testing.T) {
	cases := []struct {
		name           string
		downstreamCode int
		wantCode       int
		wantErrorCode  string
		wantRetryAfter string
		bodyMustAbsent string // non-empty: assert gateway body does NOT contain this string
	}{
		{
			name:           "downstream 500 yields 502 dependency_error",
			downstreamCode: http.StatusInternalServerError,
			wantCode:       http.StatusBadGateway,
			wantErrorCode:  "dependency_error",
			bodyMustAbsent: "internal server error from upstream",
		},
		{
			name:           "downstream 503 yields 502 dependency_error",
			downstreamCode: http.StatusServiceUnavailable,
			wantCode:       http.StatusBadGateway,
			wantErrorCode:  "dependency_error",
			bodyMustAbsent: "service unavailable from upstream",
		},
		{
			name:           "downstream 403 yields 403 forbidden",
			downstreamCode: http.StatusForbidden,
			wantCode:       http.StatusForbidden,
			wantErrorCode:  "forbidden",
		},
		{
			name:           "downstream 409 yields 409 conflict",
			downstreamCode: http.StatusConflict,
			wantCode:       http.StatusConflict,
			wantErrorCode:  "conflict",
		},
		{
			name:           "downstream 429 yields 429 rate_limited",
			downstreamCode: http.StatusTooManyRequests,
			wantCode:       http.StatusTooManyRequests,
			wantErrorCode:  "rate_limited",
			wantRetryAfter: "60",
		},
		{
			// covers the 401 branch in downstreamErrorCode when a business service (not auth)
			// returns 401 — gateway transparently passes it through as unauthorized.
			name:           "downstream 401 yields 401 unauthorized",
			downstreamCode: http.StatusUnauthorized,
			wantCode:       http.StatusUnauthorized,
			wantErrorCode:  "unauthorized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hasher := testHasher(t)
			store := newMemorySessionStore()
			accessToken := "valid-token"
			store.putToken(t, hasher, accessToken, service.SessionCacheEntry{
				SessionID:   "sess_1",
				UserID:      "usr_1",
				Username:    "alice",
				Roles:       []string{"analyst"},
				Permissions: []string{"knowledge:read"},
				TokenType:   "Bearer",
				ExpiresAt:   time.Now().Add(time.Hour).UTC(),
			})

			downstreamBody := "internal server error from upstream"
			if tc.downstreamCode == http.StatusServiceUnavailable {
				downstreamBody = "service unavailable from upstream"
			}
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wantRetryAfter != "" {
					w.Header().Set("Retry-After", tc.wantRetryAfter)
				}
				w.WriteHeader(tc.downstreamCode)
				_, _ = io.WriteString(w, downstreamBody)
			}))
			defer downstream.Close()

			server := newGatewayTestServer(t, gatewayDeps{
				store:         store,
				hasher:        hasher,
				ownerBaseURLs: map[string]string{"knowledge": downstream.URL},
			})
			req := httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil)
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("X-Request-Id", "req_error_map")
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			if res.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d, body = %s", res.Code, tc.wantCode, res.Body.String())
			}
			raw := res.Body.String()
			if tc.bodyMustAbsent != "" && strings.Contains(raw, tc.bodyMustAbsent) {
				t.Fatalf("gateway response body leaked downstream content %q: %s", tc.bodyMustAbsent, raw)
			}
			var body errorBody
			decodeJSON(t, res.Body, &body)
			if body.Error.Code != tc.wantErrorCode {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, tc.wantErrorCode)
			}
			if got := res.Header().Get("Retry-After"); got != tc.wantRetryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, tc.wantRetryAfter)
			}
		})
	}
}

type gatewayDeps struct {
	auth                   gatewayhttp.AuthClient
	store                  service.SessionStore
	hasher                 service.TokenHasher
	ownerBaseURLs          map[string]string
	serviceToken           string
	authAdminServiceToken  string
	maxInFlight            int
	authRefreshMaxInFlight int
}

func newGatewayTestServer(t *testing.T, deps gatewayDeps) http.Handler {
	t.Helper()
	if deps.ownerBaseURLs == nil {
		deps.ownerBaseURLs = map[string]string{}
	}
	return gatewayhttp.NewServer(gatewayhttp.Config{
		Logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceVersion:         "test",
		Environment:            "test",
		RequestTimeout:         time.Second,
		MaxBodyBytes:           1024 * 1024,
		MaxInFlight:            deps.maxInFlight,
		AuthRefreshMaxInFlight: deps.authRefreshMaxInFlight,
		CORSAllowedOrigins:     []string{"*"},
		DownstreamTimeout:      time.Second,
		AuthClient:             deps.auth,
		SessionStore:           deps.store,
		TokenHasher:            deps.hasher,
		OwnerBaseURLs:          deps.ownerBaseURLs,
		InternalServiceToken:   deps.serviceToken,
		AuthAdminServiceToken:  deps.authAdminServiceToken,
	})
}

func testHasher(t *testing.T) service.TokenHasher {
	t.Helper()
	hasher, err := service.NewTokenHasher("test-secret", "v1")
	if err != nil {
		t.Fatalf("NewTokenHasher() error = %v", err)
	}
	return hasher
}

type fakeAuthClient struct {
	createUserResult     service.SessionResponse
	createUserErr        error
	createSessionResult  service.SessionResponse
	createSessionErr     error
	getUserResult        service.UserRecord
	getUserErr           error
	getSessionResult     service.SessionIdentity
	getSessionErr        error
	deleteSessionID      string
	deleteSessionErr     error
	updateProfileResult  service.UserRecord
	updateProfileErr     error
	changePasswordResult service.UserRecord
	changePasswordErr    error
}

type blockingAuthRefreshClient struct {
	fakeAuthClient

	entered     chan struct{}
	releaseOnce sync.Once
	released    chan struct{}
	enteredOnce sync.Once

	mu             sync.Mutex
	activeSessions int
	maxSessions    int
	sessionCount   int
}

func newBlockingAuthRefreshClient() *blockingAuthRefreshClient {
	return &blockingAuthRefreshClient{
		fakeAuthClient: fakeAuthClient{
			getUserResult: service.UserRecord{
				ID:          "usr_1",
				Username:    "alice",
				Roles:       []string{"admin"},
				Permissions: []string{"knowledge:read"},
				Status:      "active",
			},
		},
		entered:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (c *blockingAuthRefreshClient) GetSession(ctx context.Context, requestID string, sessionID string, forwarding authclient.ForwardingContext) (service.SessionIdentity, error) {
	c.mu.Lock()
	c.sessionCount++
	c.activeSessions++
	if c.activeSessions > c.maxSessions {
		c.maxSessions = c.activeSessions
	}
	c.mu.Unlock()
	c.enteredOnce.Do(func() { close(c.entered) })
	defer func() {
		c.mu.Lock()
		c.activeSessions--
		c.mu.Unlock()
	}()

	select {
	case <-c.released:
	case <-ctx.Done():
		return service.SessionIdentity{}, ctx.Err()
	}
	return c.fakeAuthClient.GetSession(ctx, requestID, sessionID, forwarding)
}

func (c *blockingAuthRefreshClient) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("auth refresh was not entered")
	}
}

func (c *blockingAuthRefreshClient) release() {
	c.releaseOnce.Do(func() { close(c.released) })
}

func (c *blockingAuthRefreshClient) sessionCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionCount
}

func (c *blockingAuthRefreshClient) maxConcurrentSessions() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxSessions
}

func (c *fakeAuthClient) CreateUser(context.Context, string, []byte, authclient.ForwardingContext) (service.SessionResponse, error) {
	if c.createUserErr != nil {
		return service.SessionResponse{}, c.createUserErr
	}
	return c.createUserResult, nil
}

func (c *fakeAuthClient) CreateSession(context.Context, string, []byte, authclient.ForwardingContext) (service.SessionResponse, error) {
	if c.createSessionErr != nil {
		return service.SessionResponse{}, c.createSessionErr
	}
	return c.createSessionResult, nil
}

func (c *fakeAuthClient) GetUser(context.Context, string, string, authclient.ForwardingContext) (service.UserRecord, error) {
	if c.getUserErr != nil {
		return service.UserRecord{}, c.getUserErr
	}
	if strings.TrimSpace(c.getUserResult.ID) != "" {
		return c.getUserResult, nil
	}
	return service.UserRecord{
		ID:          "usr_1",
		Username:    "alice",
		Roles:       []string{"admin"},
		Permissions: []string{"knowledge:read"},
		Status:      "active",
	}, nil
}

func (c *fakeAuthClient) GetSession(_ context.Context, _ string, sessionID string, _ authclient.ForwardingContext) (service.SessionIdentity, error) {
	if c.getSessionErr != nil {
		return service.SessionIdentity{}, c.getSessionErr
	}
	if strings.TrimSpace(c.getSessionResult.SessionID) != "" {
		return c.getSessionResult, nil
	}
	return service.SessionIdentity{
		SessionID: sessionID,
		User: service.UserSummary{
			ID:          "usr_1",
			Username:    "alice",
			Roles:       []string{"admin"},
			Permissions: []string{"knowledge:read"},
		},
		TokenType: "Bearer",
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		IssuedAt:  time.Now().Add(-time.Minute).UTC(),
	}, nil
}

func (c *fakeAuthClient) DeleteSession(_ context.Context, _ string, sessionID string, _ authclient.ForwardingContext) error {
	if c.deleteSessionErr != nil {
		return c.deleteSessionErr
	}
	c.deleteSessionID = sessionID
	return nil
}

func (c *fakeAuthClient) UpdateUserProfile(context.Context, string, string, []byte, authclient.ForwardingContext) (service.UserRecord, error) {
	if c.updateProfileErr != nil {
		return service.UserRecord{}, c.updateProfileErr
	}
	if strings.TrimSpace(c.updateProfileResult.ID) != "" {
		return c.updateProfileResult, nil
	}
	return c.GetUser(context.Background(), "", "", authclient.ForwardingContext{})
}

func (c *fakeAuthClient) ChangeUserPassword(context.Context, string, string, []byte, authclient.ForwardingContext) (service.UserRecord, error) {
	if c.changePasswordErr != nil {
		return service.UserRecord{}, c.changePasswordErr
	}
	if strings.TrimSpace(c.changePasswordResult.ID) != "" {
		if c.changePasswordResult.ID == c.getUserResult.ID {
			c.getUserResult = c.changePasswordResult
		}
		return c.changePasswordResult, nil
	}
	return c.GetUser(context.Background(), "", "", authclient.ForwardingContext{})
}

type memorySessionStore struct {
	mu      sync.Mutex
	entries map[string]service.SessionCacheEntry
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{entries: map[string]service.SessionCacheEntry{}}
}

func (s *memorySessionStore) Put(_ context.Context, entry service.SessionCacheEntry, ttl time.Duration) error {
	if ttl <= 0 {
		return service.ErrSessionInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.AccessTokenHash] = entry
	return nil
}

func (s *memorySessionStore) Get(_ context.Context, accessTokenHash string) (service.SessionCacheEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[accessTokenHash]
	if !ok {
		return service.SessionCacheEntry{}, service.ErrSessionNotFound
	}
	return entry, nil
}

func (s *memorySessionStore) Delete(_ context.Context, accessTokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, accessTokenHash)
	return nil
}

func (s *memorySessionStore) putToken(t *testing.T, hasher service.TokenHasher, accessToken string, entry service.SessionCacheEntry) {
	t.Helper()
	hash, err := hasher.Hash(accessToken)
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	entry.AccessTokenHash = hash
	entry.CachedAt = time.Now().UTC()
	if entry.IssuedAt.IsZero() {
		entry.IssuedAt = entry.CachedAt
	}
	if err := s.Put(context.Background(), entry, time.Until(entry.ExpiresAt)); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
}

type failingSessionStore struct{}

func (failingSessionStore) Put(context.Context, service.SessionCacheEntry, time.Duration) error {
	return errors.New("unexpected put")
}

func (failingSessionStore) Get(context.Context, string) (service.SessionCacheEntry, error) {
	return service.SessionCacheEntry{}, service.ErrSessionStoreUnavailable
}

func (failingSessionStore) Delete(context.Context, string) error {
	return service.ErrSessionStoreUnavailable
}

type hasherStore struct {
	service.SessionStore
}

var _ service.SessionStore = (*memorySessionStore)(nil)

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppVersionFreshnessReturnsCurrentWhenBuildIncludesDevelop(t *testing.T) {
	latestSHA := strings.Repeat("a", 40)
	currentSHA := strings.Repeat("b", 40)
	var capturedAuthorization string
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthorization = r.Header.Get("Authorization")
		if r.URL.Path != "/compare/"+currentSHA+"...develop" {
			t.Fatalf("github path = %q", r.URL.Path)
		}
		writeAppVersionCompareResponse(t, w, currentSHA, latestSHA, nil, 0, 1)
	}))
	defer github.Close()

	server := newAppVersionTestServer(t, github.URL, "backend-github-token", currentSHA)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/app-version/freshness?currentSha="+currentSHA, nil)
	req.Header.Set("X-Request-Id", "req_app_version")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if capturedAuthorization != "Bearer backend-github-token" {
		t.Fatalf("GitHub Authorization header = %q", capturedAuthorization)
	}
	if strings.Contains(res.Body.String(), "backend-github-token") {
		t.Fatalf("backend token leaked into response: %s", res.Body.String())
	}
	var body appVersionEnvelope
	decodeAppVersionJSON(t, res.Body, &body)
	if body.RequestID != "req_app_version" {
		t.Fatalf("requestId = %q", body.RequestID)
	}
	if body.Data.Status != appFreshnessCurrent ||
		body.Data.CurrentSHA != currentSHA ||
		body.Data.LatestSHA != latestSHA ||
		body.Data.LatestURL == "" {
		t.Fatalf("freshness = %+v", body.Data)
	}
}

func TestAppVersionFreshnessReturnsDifferentWhenCurrentSHAIsDevelopAncestor(t *testing.T) {
	latestSHA := strings.Repeat("a", 40)
	currentSHA := strings.Repeat("1", 40)
	intermediateSHA := strings.Repeat("2", 40)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compare/"+currentSHA+"...develop" {
			t.Fatalf("github path = %q", r.URL.Path)
		}
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, []string{intermediateSHA, latestSHA}, 2, 0)
	}))
	defer github.Close()

	server := newAppVersionTestServer(t, github.URL, "", currentSHA)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/app-version/freshness?currentSha="+currentSHA, nil)
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body appVersionEnvelope
	decodeAppVersionJSON(t, res.Body, &body)
	if body.Data.Status != appFreshnessDifferent ||
		body.Data.CurrentSHA != currentSHA ||
		body.Data.LatestSHA != latestSHA {
		t.Fatalf("freshness = %+v", body.Data)
	}
}

func TestAppVersionFreshnessFallsBackToUnknownOnGitHubForbidden(t *testing.T) {
	currentSHA := strings.Repeat("a", 40)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer github.Close()

	server := newAppVersionTestServer(t, github.URL, "", currentSHA)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/app-version/freshness?currentSha="+currentSHA, nil)
	req.Header.Set("X-Request-Id", "req_app_version_403")
	res := httptest.NewRecorder()

	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var body appVersionEnvelope
	decodeAppVersionJSON(t, res.Body, &body)
	if body.Data.Status != appFreshnessUnknown ||
		body.Data.Reason != appFreshnessReasonGitHub403 ||
		body.Data.CurrentSHA != currentSHA ||
		body.Data.LatestSHA != "" {
		t.Fatalf("freshness = %+v", body.Data)
	}
}

func TestAppVersionFreshnessCachesFreshnessByCurrentSHA(t *testing.T) {
	latestSHA := strings.Repeat("a", 40)
	currentSHA := strings.Repeat("1", 40)
	calls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, []string{latestSHA}, 1, 0)
	}))
	defer github.Close()

	server := newAppVersionTestServer(t, github.URL, "", currentSHA)
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/app-version/freshness?currentSha="+currentSHA, nil)
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("GitHub calls = %d, want 1", calls)
	}
}

func TestAppVersionFreshnessChecksConfiguredCurrentSHAWhenAllowlistIsEmpty(t *testing.T) {
	latestSHA := strings.Repeat("a", 40)
	currentSHA := strings.Repeat("b", 40)
	calls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/compare/"+currentSHA+"...develop" {
			t.Fatalf("github path = %q", r.URL.Path)
		}
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, []string{latestSHA}, 1, 0)
	}))
	defer github.Close()

	checker := newAppVersionTestCheckerWithCurrentSHA(t, github.URL, "", currentSHA)

	freshness := checker.CheckFreshness(context.Background(), currentSHA)

	if freshness.Status != appFreshnessDifferent ||
		freshness.CurrentSHA != currentSHA ||
		freshness.LatestSHA != latestSHA {
		t.Fatalf("freshness = %+v", freshness)
	}
	if calls != 1 {
		t.Fatalf("GitHub calls = %d, want 1", calls)
	}
}

func TestAppVersionFreshnessCoalescesConcurrentGitHubRequests(t *testing.T) {
	latestSHA := strings.Repeat("a", 40)
	currentSHA := strings.Repeat("1", 40)
	var calls int64
	started := make(chan struct{})
	release := make(chan struct{})
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compare/"+currentSHA+"...develop" {
			t.Fatalf("github path = %q", r.URL.Path)
		}
		if atomic.AddInt64(&calls, 1) == 1 {
			close(started)
		}
		<-release
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, []string{latestSHA}, 1, 0)
	}))
	defer github.Close()

	checker := newAppVersionTestChecker(t, github.URL, "", currentSHA)
	var wg sync.WaitGroup
	begin := make(chan struct{})
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-begin
			freshness := checker.CheckFreshness(context.Background(), currentSHA)
			if freshness.Status != appFreshnessDifferent {
				t.Errorf("status = %q, want %q", freshness.Status, appFreshnessDifferent)
			}
		}()
	}

	close(begin)
	<-started
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls != 1 {
		t.Fatalf("GitHub calls = %d, want 1", calls)
	}
}

func TestAppVersionFreshnessRejectsInvalidCurrentSHAWithoutGitHubCall(t *testing.T) {
	githubCalls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalls++
		t.Fatalf("GitHub should not be called for invalid currentSha")
	}))
	defer github.Close()

	server := newAppVersionTestServer(t, github.URL, "")
	tests := []struct {
		name       string
		currentSHA string
	}{
		{name: "short", currentSHA: strings.Repeat("a", 39)},
		{name: "long", currentSHA: strings.Repeat("a", 41)},
		{name: "non hex", currentSHA: strings.Repeat("a", 39) + "g"},
		{name: "punctuation", currentSHA: strings.Repeat("a", 39) + "!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/app-version/freshness?currentSha="+tt.currentSHA, nil)
			req.Header.Set("X-Request-Id", "req_app_version_validation")
			res := httptest.NewRecorder()

			server.ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
			}
			var body struct {
				Error struct {
					Code      string            `json:"code"`
					RequestID string            `json:"requestId"`
					Fields    map[string]string `json:"fields"`
				} `json:"error"`
			}
			decodeAppVersionJSON(t, res.Body, &body)
			if body.Error.Code != "validation_error" ||
				body.Error.RequestID != "req_app_version_validation" ||
				body.Error.Fields["currentSha"] == "" {
				t.Fatalf("error = %+v", body.Error)
			}
		})
	}

	if githubCalls != 0 {
		t.Fatalf("GitHub calls = %d, want 0", githubCalls)
	}
}

func TestAppVersionFreshnessReturnsUnknownWhenNoTrustedSHAsAreConfiguredWithoutGitHubOrCache(t *testing.T) {
	currentSHA := strings.Repeat("b", 40)
	githubCalls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalls++
		t.Fatalf("GitHub should not be called when allowlist is empty")
	}))
	defer github.Close()

	checker := newAppVersionTestChecker(t, github.URL, "")

	freshness := checker.CheckFreshness(context.Background(), currentSHA)

	if freshness.Status != appFreshnessUnknown ||
		freshness.Reason != appFreshnessReasonUntrustedCurrentSHA ||
		freshness.CurrentSHA != currentSHA {
		t.Fatalf("freshness = %+v", freshness)
	}
	checker.cacheLock.Lock()
	cacheLen := len(checker.cache)
	inFlightLen := len(checker.inFlight)
	checker.cacheLock.Unlock()
	if cacheLen != 0 || inFlightLen != 0 {
		t.Fatalf("cache entries = %d, in-flight entries = %d, want 0", cacheLen, inFlightLen)
	}
	if githubCalls != 0 {
		t.Fatalf("GitHub calls = %d, want 0", githubCalls)
	}
}

func TestAppVersionFreshnessReturnsUnknownForSHAOutsideConfiguredAllowlistWithoutGitHubOrCache(t *testing.T) {
	githubCalls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalls++
		t.Fatalf("GitHub should not be called for untrusted currentSha")
	}))
	defer github.Close()

	currentSHA := strings.Repeat("b", 40)
	checker := newAppVersionTestChecker(t, github.URL, "", strings.Repeat("a", 40))

	freshness := checker.CheckFreshness(context.Background(), currentSHA)

	if freshness.Status != appFreshnessUnknown ||
		freshness.Reason != appFreshnessReasonUntrustedCurrentSHA ||
		freshness.CurrentSHA != currentSHA {
		t.Fatalf("freshness = %+v", freshness)
	}
	checker.cacheLock.Lock()
	cacheLen := len(checker.cache)
	inFlightLen := len(checker.inFlight)
	checker.cacheLock.Unlock()
	if cacheLen != 0 || inFlightLen != 0 {
		t.Fatalf("cache entries = %d, in-flight entries = %d, want 0", cacheLen, inFlightLen)
	}
	if githubCalls != 0 {
		t.Fatalf("GitHub calls = %d, want 0", githubCalls)
	}
}

func TestAppVersionFreshnessEvictsOldestCacheEntryWhenCapacityExceeded(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	calls := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		currentSHA := strings.TrimPrefix(r.URL.Path, "/compare/")
		currentSHA = strings.TrimSuffix(currentSHA, "...develop")
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, nil, 0, 0)
	}))
	defer github.Close()

	shas := []string{
		strings.Repeat("1", 40),
		strings.Repeat("2", 40),
		strings.Repeat("3", 40),
	}
	checker := newAppVersionTestChecker(t, github.URL, "", shas...)
	checker.cacheMaxEntries = 2
	checker.now = func() time.Time {
		return now
	}

	for _, sha := range shas {
		now = now.Add(time.Second)
		freshness := checker.CheckFreshness(context.Background(), sha)
		if freshness.Status != appFreshnessCurrent {
			t.Fatalf("freshness = %+v", freshness)
		}
	}

	checker.cacheLock.Lock()
	cacheLen := len(checker.cache)
	_, firstCached := checker.cache[shas[0]]
	checker.cacheLock.Unlock()
	if cacheLen != 2 {
		t.Fatalf("cache entries = %d, want 2", cacheLen)
	}
	if firstCached {
		t.Fatalf("oldest cache entry was not evicted")
	}

	now = now.Add(time.Second)
	_ = checker.CheckFreshness(context.Background(), shas[0])
	if calls != 4 {
		t.Fatalf("GitHub calls = %d, want 4", calls)
	}
}

func TestAppVersionFreshnessPrunesExpiredCacheEntries(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentSHA := strings.TrimPrefix(r.URL.Path, "/compare/")
		currentSHA = strings.TrimSuffix(currentSHA, "...develop")
		writeAppVersionCompareResponse(t, w, currentSHA, currentSHA, nil, 0, 0)
	}))
	defer github.Close()

	expiredSHA := strings.Repeat("4", 40)
	currentSHA := strings.Repeat("5", 40)
	checker := newAppVersionTestChecker(t, github.URL, "", expiredSHA, currentSHA)
	checker.cacheTTL = time.Minute
	checker.cacheMaxEntries = 10
	checker.now = func() time.Time {
		return now
	}

	_ = checker.CheckFreshness(context.Background(), expiredSHA)
	now = now.Add(2 * time.Minute)
	_ = checker.CheckFreshness(context.Background(), currentSHA)

	checker.cacheLock.Lock()
	_, expiredCached := checker.cache[expiredSHA]
	_, currentCached := checker.cache[currentSHA]
	cacheLen := len(checker.cache)
	checker.cacheLock.Unlock()

	if expiredCached || !currentCached || cacheLen != 1 {
		t.Fatalf("cache expired=%t current=%t entries=%d", expiredCached, currentCached, cacheLen)
	}
}

func newAppVersionTestServer(t *testing.T, githubURL string, githubToken string, allowedSHAs ...string) http.Handler {
	t.Helper()
	checker := newAppVersionTestChecker(t, githubURL, githubToken, allowedSHAs...)
	return NewServer(Config{
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceVersion:     "test",
		Environment:        "test",
		RequestTimeout:     time.Second,
		MaxBodyBytes:       1024,
		CORSAllowedOrigins: []string{"*"},
		AppVersionChecker:  checker,
	})
}

func newAppVersionTestChecker(t *testing.T, githubURL string, githubToken string, allowedSHAs ...string) *gitHubAppVersionChecker {
	t.Helper()
	return newAppVersionTestCheckerWithCurrentSHA(t, githubURL, githubToken, "", allowedSHAs...)
}

func newAppVersionTestCheckerWithCurrentSHA(t *testing.T, githubURL string, githubToken string, currentSHA string, allowedSHAs ...string) *gitHubAppVersionChecker {
	t.Helper()
	checker := newGitHubAppVersionChecker(http.DefaultClient, slog.New(slog.NewTextHandler(io.Discard, nil)), githubToken, currentSHA, allowedSHAs)
	checker.apiURL = strings.TrimRight(githubURL, "/") + "/compare"
	checker.now = func() time.Time {
		return time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	}
	return checker
}

type appVersionEnvelope struct {
	Data      AppVersionFreshness `json:"data"`
	RequestID string              `json:"requestId"`
}

func decodeAppVersionJSON(t *testing.T, r io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(target); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
}

func writeAppVersionCompareResponse(t *testing.T, w http.ResponseWriter, baseSHA string, mergeBaseSHA string, commitSHAs []string, aheadBy int, behindBy int) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	commits := make([]map[string]string, 0, len(commitSHAs))
	for _, sha := range commitSHAs {
		commits = append(commits, map[string]string{
			"sha":      sha,
			"html_url": "https://github.test/commit/" + sha,
		})
	}
	body := map[string]any{
		"ahead_by":  aheadBy,
		"behind_by": behindBy,
		"base_commit": map[string]string{
			"sha":      baseSHA,
			"html_url": "https://github.test/commit/" + baseSHA,
		},
		"merge_base_commit": map[string]string{
			"sha":      mergeBaseSHA,
			"html_url": "https://github.test/commit/" + mergeBaseSHA,
		},
		"commits": commits,
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
}

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/middleware"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/response"
)

const (
	githubDevelopCompareAPI   = "https://api.github.com/repos/Sakayori-Iroha-168/Software_Teamwork/compare/"
	appVersionCacheTTL        = 5 * time.Minute
	appVersionCacheMaxEntries = 128

	appFreshnessCurrent   = "current"
	appFreshnessDifferent = "different"
	appFreshnessUnknown   = "unknown"

	appFreshnessReasonMissingCurrentSHA   = "missing_current_sha"
	appFreshnessReasonUntrustedCurrentSHA = "untrusted_current_sha"
	appFreshnessReasonGitHub403           = "github_403"
	appFreshnessReasonGitHub404           = "github_404"
	appFreshnessReasonGitHub429           = "github_429"
	appFreshnessReasonGitHubStatus        = "github_status"
	appFreshnessReasonNetworkError        = "network_error"
	appFreshnessReasonInvalidResponse     = "invalid_response"
)

type AppVersionChecker interface {
	CheckFreshness(ctx context.Context, currentSHA string) AppVersionFreshness
}

type AppVersionFreshness struct {
	Status     string    `json:"status"`
	CurrentSHA string    `json:"currentSha,omitempty"`
	LatestSHA  string    `json:"latestSha,omitempty"`
	LatestURL  string    `json:"latestUrl,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
	Reason     string    `json:"reason,omitempty"`
}

type gitHubAppVersionChecker struct {
	apiURL          string
	token           string
	allowedSHAs     map[string]struct{}
	client          *http.Client
	logger          *slog.Logger
	cacheTTL        time.Duration
	cacheMaxEntries int
	now             func() time.Time
	cacheLock       sync.Mutex
	cache           map[string]gitHubFreshnessCacheEntry
	inFlight        map[string]*gitHubFreshnessCall
}

type gitHubFreshnessCacheEntry struct {
	freshness AppVersionFreshness
	expiresAt time.Time
}

type gitHubFreshnessCall struct {
	done      chan struct{}
	freshness AppVersionFreshness
}

type gitHubCommitResponse struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
}

type gitHubCompareResponse struct {
	AheadBy         int                    `json:"ahead_by"`
	BehindBy        int                    `json:"behind_by"`
	BaseCommit      gitHubCommitResponse   `json:"base_commit"`
	MergeBaseCommit gitHubCommitResponse   `json:"merge_base_commit"`
	Commits         []gitHubCommitResponse `json:"commits"`
}

func newGitHubAppVersionChecker(client *http.Client, logger *slog.Logger, token string, currentSHA string, allowedSHAs []string) *gitHubAppVersionChecker {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	trustedSHAs := commitSHASet(allowedSHAs)
	if sha := normalizeCommitSHA(currentSHA); isFullCommitSHA(sha) {
		trustedSHAs[sha] = struct{}{}
	}
	return &gitHubAppVersionChecker{
		apiURL:          githubDevelopCompareAPI,
		token:           strings.TrimSpace(token),
		allowedSHAs:     trustedSHAs,
		client:          client,
		logger:          logger,
		cacheTTL:        appVersionCacheTTL,
		cacheMaxEntries: appVersionCacheMaxEntries,
		now:             time.Now,
		cache:           make(map[string]gitHubFreshnessCacheEntry),
		inFlight:        make(map[string]*gitHubFreshnessCall),
	}
}

func (s *Server) handleAppVersionFreshness(w http.ResponseWriter, r *http.Request) {
	currentSHA := normalizeCommitSHA(r.URL.Query().Get("currentSha"))
	if currentSHA != "" && !isFullCommitSHA(currentSHA) {
		response.WriteError(w, http.StatusBadRequest, response.ErrorDetail{
			Code:      response.CodeValidation,
			Message:   "currentSha must be a 40 character hexadecimal Git SHA",
			RequestID: middleware.RequestIDFromContext(r.Context()),
			Fields: map[string]string{
				"currentSha": "must be a 40 character hexadecimal Git SHA",
			},
		})
		return
	}

	checker := s.appVersionChecker
	if checker == nil {
		checker = newGitHubAppVersionChecker(s.httpClient, s.logger, "", "", nil)
	}
	freshness := checker.CheckFreshness(r.Context(), currentSHA)
	response.WriteJSON(w, http.StatusOK, freshness, middleware.RequestIDFromContext(r.Context()))
}

func (c *gitHubAppVersionChecker) CheckFreshness(ctx context.Context, currentSHA string) AppVersionFreshness {
	currentSHA = normalizeCommitSHA(currentSHA)
	checkedAt := c.now().UTC()
	if currentSHA == "" {
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonMissingCurrentSHA)
	}
	if !c.isAllowedCurrentSHA(currentSHA) {
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonUntrustedCurrentSHA)
	}

	return c.cachedFreshness(ctx, currentSHA, checkedAt)
}

func (c *gitHubAppVersionChecker) isAllowedCurrentSHA(currentSHA string) bool {
	_, ok := c.allowedSHAs[currentSHA]
	return ok
}

func (c *gitHubAppVersionChecker) cachedFreshness(ctx context.Context, currentSHA string, checkedAt time.Time) AppVersionFreshness {
	c.cacheLock.Lock()
	if entry, ok := c.cache[currentSHA]; ok {
		if checkedAt.Before(entry.expiresAt) {
			freshness := entry.freshness
			c.cacheLock.Unlock()
			return freshness
		}
		delete(c.cache, currentSHA)
	}
	if call := c.inFlight[currentSHA]; call != nil {
		c.cacheLock.Unlock()
		select {
		case <-call.done:
			return call.freshness
		case <-ctx.Done():
			return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonNetworkError)
		}
	}
	call := &gitHubFreshnessCall{done: make(chan struct{})}
	if c.inFlight == nil {
		c.inFlight = make(map[string]*gitHubFreshnessCall)
	}
	c.inFlight[currentSHA] = call
	c.cacheLock.Unlock()

	freshness := c.fetchCompareFreshness(ctx, currentSHA, checkedAt)

	c.cacheLock.Lock()
	c.storeFreshnessLocked(currentSHA, freshness, checkedAt)
	call.freshness = freshness
	close(call.done)
	delete(c.inFlight, currentSHA)
	c.cacheLock.Unlock()

	return freshness
}

func (c *gitHubAppVersionChecker) storeFreshnessLocked(currentSHA string, freshness AppVersionFreshness, checkedAt time.Time) {
	if c.cache == nil {
		c.cache = make(map[string]gitHubFreshnessCacheEntry)
	}
	c.cache[currentSHA] = gitHubFreshnessCacheEntry{
		freshness: freshness,
		expiresAt: checkedAt.Add(c.cacheTTL),
	}
	c.pruneFreshnessCacheLocked(checkedAt)
}

func (c *gitHubAppVersionChecker) pruneFreshnessCacheLocked(now time.Time) {
	for key, entry := range c.cache {
		if !now.Before(entry.expiresAt) {
			delete(c.cache, key)
		}
	}

	maxEntries := c.cacheMaxEntries
	if maxEntries <= 0 {
		maxEntries = appVersionCacheMaxEntries
	}
	for len(c.cache) > maxEntries {
		var oldestKey string
		var oldestExpiresAt time.Time
		first := true
		for key, entry := range c.cache {
			if first || entry.expiresAt.Before(oldestExpiresAt) ||
				(entry.expiresAt.Equal(oldestExpiresAt) && key < oldestKey) {
				oldestKey = key
				oldestExpiresAt = entry.expiresAt
				first = false
			}
		}
		delete(c.cache, oldestKey)
	}
}

func (c *gitHubAppVersionChecker) fetchCompareFreshness(ctx context.Context, currentSHA string, checkedAt time.Time) AppVersionFreshness {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.compareURL(currentSHA), nil)
	if err != nil {
		c.warnGitHubFallback(ctx, checkedAt, appFreshnessReasonInvalidResponse, 0)
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonInvalidResponse)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Software-Teamwork-Gateway")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	res, err := c.client.Do(req)
	if err != nil {
		c.warnGitHubFallback(ctx, checkedAt, appFreshnessReasonNetworkError, 0)
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonNetworkError)
	}
	defer res.Body.Close()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		reason := gitHubStatusReason(res.StatusCode)
		c.warnGitHubFallback(ctx, checkedAt, reason, res.StatusCode)
		return unknownAppVersionFreshness(currentSHA, checkedAt, reason)
	}

	var body gitHubCompareResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		c.warnGitHubFallback(ctx, checkedAt, appFreshnessReasonInvalidResponse, 0)
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonInvalidResponse)
	}

	latestCommit := latestDevelopCommit(body)
	latestSHA := normalizeCommitSHA(latestCommit.SHA)
	if latestSHA == "" {
		c.warnGitHubFallback(ctx, checkedAt, appFreshnessReasonInvalidResponse, 0)
		return unknownAppVersionFreshness(currentSHA, checkedAt, appFreshnessReasonInvalidResponse)
	}

	status := appFreshnessCurrent
	if body.AheadBy > 0 {
		status = appFreshnessDifferent
	}
	return AppVersionFreshness{
		Status:     status,
		CurrentSHA: currentSHA,
		LatestSHA:  latestSHA,
		LatestURL:  strings.TrimSpace(latestCommit.HTMLURL),
		CheckedAt:  checkedAt,
	}
}

func (c *gitHubAppVersionChecker) compareURL(currentSHA string) string {
	return strings.TrimRight(c.apiURL, "/") + "/" + url.PathEscape(currentSHA) + "...develop"
}

func latestDevelopCommit(body gitHubCompareResponse) gitHubCommitResponse {
	if body.AheadBy > 0 && len(body.Commits) > 0 {
		return body.Commits[len(body.Commits)-1]
	}
	if body.BehindBy > 0 {
		return body.MergeBaseCommit
	}
	return body.BaseCommit
}

func (c *gitHubAppVersionChecker) warnGitHubFallback(ctx context.Context, checkedAt time.Time, reason string, statusCode int) {
	args := []any{
		"service", "gateway",
		"operation", "app_version_freshness",
		"dependency", "github",
		"status", "unknown",
		"reason", reason,
		"checked_at", checkedAt.Format(time.RFC3339),
	}
	if statusCode > 0 {
		args = append(args, "status_code", statusCode)
	}
	c.logger.WarnContext(ctx, "github app version freshness check fell back to unknown", args...)
}

func unknownAppVersionFreshness(currentSHA string, checkedAt time.Time, reason string) AppVersionFreshness {
	return AppVersionFreshness{
		Status:     appFreshnessUnknown,
		CurrentSHA: normalizeCommitSHA(currentSHA),
		CheckedAt:  checkedAt,
		Reason:     reason,
	}
}

func normalizeCommitSHA(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func commitSHASet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		sha := normalizeCommitSHA(value)
		if !isFullCommitSHA(sha) {
			continue
		}
		set[sha] = struct{}{}
	}
	return set
}

func isFullCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func gitHubStatusReason(statusCode int) string {
	switch statusCode {
	case http.StatusForbidden:
		return appFreshnessReasonGitHub403
	case http.StatusNotFound:
		return appFreshnessReasonGitHub404
	case http.StatusTooManyRequests:
		return appFreshnessReasonGitHub429
	default:
		return appFreshnessReasonGitHubStatus
	}
}

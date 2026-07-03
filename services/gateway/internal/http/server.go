package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/metrics"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/middleware"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/response"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/service"
)

type Config struct {
	Logger                 *slog.Logger
	ServiceVersion         string
	Environment            string
	RequestTimeout         time.Duration
	MaxBodyBytes           int64
	MaxInFlight            int
	AuthRefreshMaxInFlight int
	CORSAllowedOrigins     []string
	CORSAllowedMethods     []string
	CORSAllowedHeaders     []string
	CORSAllowCredentials   bool
	DownstreamTimeout      time.Duration
	InternalServiceToken   string
	AuthAdminServiceToken  string
	OwnerBaseURLs          map[string]string
	AuthClient             AuthClient
	AppVersionChecker      AppVersionChecker
	SessionStore           service.SessionStore
	TokenHasher            service.TokenHasher
	HTTPClient             *http.Client
	GitHubToken            string
	AppVersionCurrentSHA   string
	AppVersionAllowedSHAs  []string
	ReadyCheck             func(context.Context) error
	MetricsReg             *metrics.Registry
}

type Server struct {
	logger                *slog.Logger
	serviceVersion        string
	environment           string
	internalServiceToken  string
	authAdminServiceToken string
	authClient            AuthClient
	authRefreshLimiter    *middleware.InFlightLimiter
	appVersionChecker     AppVersionChecker
	sessionStore          service.SessionStore
	tokenHasher           service.TokenHasher
	ownerBaseURLs         map[string]*url.URL
	httpClient            *http.Client
	streamHTTPClient      *http.Client
	readyCheck            func(context.Context) error
	mux                   *http.ServeMux
	handler               http.Handler
}

func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DownstreamTimeout <= 0 {
		cfg.DownstreamTimeout = 10 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.DownstreamTimeout}
	}
	if cfg.AppVersionChecker == nil {
		cfg.AppVersionChecker = newGitHubAppVersionChecker(
			cfg.HTTPClient,
			cfg.Logger,
			cfg.GitHubToken,
			cfg.AppVersionCurrentSHA,
			cfg.AppVersionAllowedSHAs,
		)
	}
	s := &Server{
		logger:                cfg.Logger,
		serviceVersion:        cfg.ServiceVersion,
		environment:           cfg.Environment,
		internalServiceToken:  strings.TrimSpace(cfg.InternalServiceToken),
		authAdminServiceToken: strings.TrimSpace(cfg.AuthAdminServiceToken),
		authClient:            cfg.AuthClient,
		authRefreshLimiter:    middleware.NewInFlightLimiter(cfg.AuthRefreshMaxInFlight),
		appVersionChecker:     cfg.AppVersionChecker,
		sessionStore:          cfg.SessionStore,
		tokenHasher:           cfg.TokenHasher,
		ownerBaseURLs:         parseOwnerBaseURLs(cfg.OwnerBaseURLs),
		httpClient:            cfg.HTTPClient,
		streamHTTPClient:      cloneHTTPClientWithoutTimeout(cfg.HTTPClient),
		readyCheck:            cfg.ReadyCheck,
		mux:                   http.NewServeMux(),
	}
	s.routes()
	s.handler = middleware.Chain(
		s.mux,
		middleware.RequestID(),
		middleware.Metrics(cfg.MetricsReg, s.mux),
		middleware.Recover(cfg.Logger),
		middleware.TimeoutWithSkip(cfg.RequestTimeout, skipsFixedRequestTimeout),
		middleware.CORS(middleware.CORSConfig{
			AllowedOrigins:   cfg.CORSAllowedOrigins,
			AllowedMethods:   cfg.CORSAllowedMethods,
			AllowedHeaders:   cfg.CORSAllowedHeaders,
			AllowCredentials: cfg.CORSAllowCredentials,
		}),
		middleware.InFlight(cfg.MaxInFlight),
		middleware.BodyLimitForRequest(cfg.MaxBodyBytes, bodyLimitForRequest),
	)
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/app-version/freshness", s.handleAppVersionFreshness)
	s.mux.HandleFunc("POST /api/v1/users", s.handleCreateUser)
	s.mux.HandleFunc("POST /api/v1/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/v1/users/me", s.handleCurrentUser)
	s.mux.HandleFunc("GET /api/v1/users/me/profile", s.handleCurrentUserProfile)
	s.mux.HandleFunc("PATCH /api/v1/users/me/profile", s.handleUpdateCurrentUserProfile)
	s.mux.HandleFunc("POST /api/v1/users/me/password-changes", s.handleChangeCurrentUserPassword)
	s.mux.HandleFunc("DELETE /api/v1/sessions/current", s.handleDeleteCurrentSession)
	for _, route := range activeProxyRoutes {
		route := route
		s.mux.HandleFunc(route.Method+" "+route.Pattern, s.handleProxy(route))
	}
	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

const (
	qaAttachmentMaxFileBytes       = int64(20 << 20)
	qaAttachmentMultipartOverhead  = int64(1 << 20)
	qaAttachmentUploadMaxBodyBytes = qaAttachmentMaxFileBytes + qaAttachmentMultipartOverhead
)

func bodyLimitForRequest(r *http.Request) int64 {
	if r.Method != http.MethodPost {
		return 0
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 5 &&
		parts[0] == "api" &&
		parts[1] == "v1" &&
		parts[2] == "qa-sessions" &&
		parts[4] == "attachments" {
		return qaAttachmentUploadMaxBodyBytes
	}
	return 0
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, healthResponse{
		Status:      "ok",
		Service:     "gateway",
		Version:     s.serviceVersion,
		Environment: s.environment,
	}, middleware.RequestIDFromContext(r.Context()))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.readyCheck != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.readyCheck(ctx); err != nil {
			s.logger.WarnContext(r.Context(), "gateway dependencies are not ready",
				"service", "gateway",
				"request_id", middleware.RequestIDFromContext(r.Context()),
				"operation", "readyz",
				"status", "failed",
				"error", err,
			)
			response.WriteError(w, http.StatusServiceUnavailable, response.ErrorDetail{
				Code:      response.CodeDependency,
				Message:   "gateway dependencies are not ready",
				RequestID: middleware.RequestIDFromContext(r.Context()),
			})
			return
		}
	}
	response.WriteJSON(w, http.StatusOK, healthResponse{
		Status:      "ready",
		Service:     "gateway",
		Version:     s.serviceVersion,
		Environment: s.environment,
	}, middleware.RequestIDFromContext(r.Context()))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	response.WriteError(w, http.StatusNotFound, response.ErrorDetail{
		Code:      response.CodeNotFound,
		Message:   "route not found",
		RequestID: middleware.RequestIDFromContext(r.Context()),
	})
}

func ValidateOwnerBaseURLs(values map[string]string) error {
	for owner, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := parseOwnerBaseURL(owner, raw); err != nil {
			return fmt.Errorf("%s owner base URL is invalid: %w", owner, err)
		}
	}
	return nil
}

type healthResponse struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	Version     string `json:"version,omitempty"`
	Environment string `json:"environment,omitempty"`
}

func parseOwnerBaseURLs(values map[string]string) map[string]*url.URL {
	parsed := make(map[string]*url.URL, len(values))
	for owner, raw := range values {
		u, err := parseOwnerBaseURL(owner, raw)
		if err != nil || u == nil {
			continue
		}
		parsed[owner] = u
	}
	return parsed
}

func parseOwnerBaseURL(owner string, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("must be a valid absolute URL")
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("must include scheme and host")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("must use http or https scheme")
	}
	if u.User != nil {
		return nil, fmt.Errorf("must not include credentials")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return nil, fmt.Errorf("must not include query parameters")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, fmt.Errorf("must not include fragment")
	}
	if !trustedOwnerBaseURLHost(owner, u.Hostname()) {
		return nil, fmt.Errorf("host is not trusted")
	}
	return u, nil
}

var trustedOwnerServiceHosts = map[string][]string{
	"auth":       {"auth"},
	"knowledge":  {"knowledge"},
	"qa":         {"qa"},
	"document":   {"document"},
	"ai-gateway": {"ai-gateway"},
}

func trustedOwnerBaseURLHost(owner string, host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	for _, trusted := range trustedOwnerServiceHosts[owner] {
		if host == trusted {
			return true
		}
	}
	return false
}

func cloneHTTPClientWithoutTimeout(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	cloned := *client
	cloned.Timeout = 0
	return &cloned
}

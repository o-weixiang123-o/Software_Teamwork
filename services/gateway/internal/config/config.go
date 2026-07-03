package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHTTPAddr               = ":8080"
	DefaultMetricsAddr            = ":9091"
	DefaultServiceVersion         = "0.1.0"
	DefaultEnvironment            = "local"
	DefaultMaxBodyBytes           = int64(10 << 20)
	DefaultMaxInFlight            = 128
	DefaultAuthRefreshMaxInFlight = 32
	DefaultRequestTimeout         = 30 * time.Second
	DefaultShutdownTimeout        = 10 * time.Second
	DefaultDownstreamTimeout      = 10 * time.Second
	DefaultRedisAddr              = "localhost:6379"
	DefaultTokenHashSecret        = "local-dev-token-hash-secret"
	DefaultTokenKeyVersion        = "v1"
)

type Config struct {
	HTTPAddr               string
	MetricsAddr            string
	ServiceVersion         string
	Environment            string
	MaxBodyBytes           int64
	MaxInFlight            int
	AuthRefreshMaxInFlight int
	RequestTimeout         time.Duration
	ShutdownTimeout        time.Duration
	DownstreamTimeout      time.Duration
	CORSAllowedOrigins     []string
	CORSAllowedMethods     []string
	CORSAllowedHeaders     []string
	CORSAllowCredentials   bool
	RedisAddr              string
	RedisPassword          string
	RedisDB                int
	TokenHashSecret        string
	TokenHashKeyVersion    string
	InternalServiceToken   string
	AuthAdminServiceToken  string
	GitHubToken            string
	AppVersionCurrentSHA   string
	AppVersionAllowedSHAs  []string
	AuthBaseURL            string
	KnowledgeBaseURL       string
	QABaseURL              string
	DocumentBaseURL        string
	AIGatewayBaseURL       string
}

func Load() (Config, error) {
	appVersionCurrentSHA, err := optionalCommitSHA("GATEWAY_APP_VERSION_CURRENT_SHA")
	if err != nil {
		return Config{}, err
	}
	appVersionAllowedSHAs, err := commitSHAList("GATEWAY_APP_VERSION_ALLOWED_SHAS")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:               stringValue("GATEWAY_HTTP_ADDR", DefaultHTTPAddr),
		MetricsAddr:            stringValue("GATEWAY_METRICS_ADDR", DefaultMetricsAddr),
		ServiceVersion:         stringValue("GATEWAY_SERVICE_VERSION", DefaultServiceVersion),
		Environment:            stringValue("GATEWAY_ENV", DefaultEnvironment),
		MaxBodyBytes:           DefaultMaxBodyBytes,
		MaxInFlight:            DefaultMaxInFlight,
		AuthRefreshMaxInFlight: DefaultAuthRefreshMaxInFlight,
		RequestTimeout:         DefaultRequestTimeout,
		ShutdownTimeout:        DefaultShutdownTimeout,
		DownstreamTimeout:      DefaultDownstreamTimeout,
		CORSAllowedOrigins:     csvValue("GATEWAY_CORS_ALLOWED_ORIGINS", []string{"*"}),
		CORSAllowedMethods:     csvValue("GATEWAY_CORS_ALLOWED_METHODS", []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}),
		CORSAllowedHeaders:     csvValue("GATEWAY_CORS_ALLOWED_HEADERS", []string{"Authorization", "Content-Type", "X-Request-Id"}),
		RedisAddr:              stringValue("GATEWAY_REDIS_ADDR", DefaultRedisAddr),
		RedisPassword:          os.Getenv("GATEWAY_REDIS_PASSWORD"),
		TokenHashSecret:        stringValueFromKeys([]string{"GATEWAY_TOKEN_HASH_SECRET", "TOKEN_HASH_SECRET"}, DefaultTokenHashSecret),
		TokenHashKeyVersion:    stringValue("GATEWAY_TOKEN_HASH_KEY_VERSION", DefaultTokenKeyVersion),
		InternalServiceToken:   firstNonEmptyEnv("GATEWAY_INTERNAL_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"),
		AuthAdminServiceToken:  strings.TrimSpace(os.Getenv("GATEWAY_AUTH_ADMIN_SERVICE_TOKEN")),
		GitHubToken:            strings.TrimSpace(os.Getenv("GATEWAY_GITHUB_TOKEN")),
		AppVersionCurrentSHA:   appVersionCurrentSHA,
		AppVersionAllowedSHAs:  appVersionAllowedSHAs,
		AuthBaseURL:            stringValue("GATEWAY_AUTH_BASE_URL", "http://localhost:8001"),
		KnowledgeBaseURL:       strings.TrimSpace(os.Getenv("GATEWAY_KNOWLEDGE_BASE_URL")),
		QABaseURL:              strings.TrimSpace(os.Getenv("GATEWAY_QA_BASE_URL")),
		DocumentBaseURL:        strings.TrimSpace(os.Getenv("GATEWAY_DOCUMENT_BASE_URL")),
		AIGatewayBaseURL:       strings.TrimSpace(os.Getenv("GATEWAY_AI_GATEWAY_BASE_URL")),
	}

	if raw := os.Getenv("GATEWAY_MAX_BODY_BYTES"); raw != "" {
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("GATEWAY_MAX_BODY_BYTES must be a positive integer")
		}
		cfg.MaxBodyBytes = value
	}

	if raw := os.Getenv("GATEWAY_MAX_IN_FLIGHT"); raw != "" {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("GATEWAY_MAX_IN_FLIGHT must be a non-negative integer")
		}
		cfg.MaxInFlight = value
	}

	if raw := os.Getenv("GATEWAY_AUTH_REFRESH_MAX_IN_FLIGHT"); raw != "" {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("GATEWAY_AUTH_REFRESH_MAX_IN_FLIGHT must be a non-negative integer")
		}
		cfg.AuthRefreshMaxInFlight = value
	}

	if raw := os.Getenv("GATEWAY_REQUEST_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("GATEWAY_REQUEST_TIMEOUT must be a positive duration")
		}
		cfg.RequestTimeout = value
	}

	if raw := os.Getenv("GATEWAY_SHUTDOWN_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("GATEWAY_SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = value
	}

	if raw := os.Getenv("GATEWAY_DOWNSTREAM_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("GATEWAY_DOWNSTREAM_TIMEOUT must be a positive duration")
		}
		cfg.DownstreamTimeout = value
	}

	if raw := os.Getenv("GATEWAY_REDIS_DB"); raw != "" {
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value < 0 {
			return Config{}, fmt.Errorf("GATEWAY_REDIS_DB must be a non-negative integer")
		}
		cfg.RedisDB = value
	}

	if raw := os.Getenv("GATEWAY_CORS_ALLOW_CREDENTIALS"); raw != "" {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return Config{}, fmt.Errorf("GATEWAY_CORS_ALLOW_CREDENTIALS must be a boolean")
		}
		cfg.CORSAllowCredentials = value
	}

	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return Config{}, fmt.Errorf("GATEWAY_HTTP_ADDR must not be empty")
	}
	if len(cfg.CORSAllowedOrigins) == 0 {
		return Config{}, fmt.Errorf("GATEWAY_CORS_ALLOWED_ORIGINS must not be empty")
	}
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		return Config{}, fmt.Errorf("GATEWAY_REDIS_ADDR must not be empty")
	}
	if strings.TrimSpace(cfg.TokenHashSecret) == "" {
		return Config{}, fmt.Errorf("GATEWAY_TOKEN_HASH_SECRET must not be empty")
	}
	if strings.TrimSpace(cfg.TokenHashKeyVersion) == "" {
		return Config{}, fmt.Errorf("GATEWAY_TOKEN_HASH_KEY_VERSION must not be empty")
	}
	if strings.TrimSpace(cfg.AuthAdminServiceToken) == "" {
		return Config{}, fmt.Errorf("GATEWAY_AUTH_ADMIN_SERVICE_TOKEN must not be empty")
	}
	if strings.TrimSpace(cfg.AuthAdminServiceToken) != "" && strings.TrimSpace(cfg.AuthAdminServiceToken) == strings.TrimSpace(cfg.InternalServiceToken) {
		return Config{}, fmt.Errorf("GATEWAY_AUTH_ADMIN_SERVICE_TOKEN must differ from GATEWAY_INTERNAL_SERVICE_TOKEN")
	}

	return cfg, nil
}

func stringValue(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func stringValueFromKeys(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func csvValue(key string, fallback []string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return append([]string(nil), fallback...)
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func commitSHAList(key string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		sha := strings.ToLower(strings.TrimSpace(part))
		if sha == "" {
			continue
		}
		if !isFullCommitSHA(sha) {
			return nil, fmt.Errorf("%s entries must be 40 character hexadecimal Git SHAs", key)
		}
		if _, ok := seen[sha]; ok {
			continue
		}
		seen[sha] = struct{}{}
		values = append(values, sha)
	}
	return values, nil
}

func optionalCommitSHA(key string) (string, error) {
	sha := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if sha == "" {
		return "", nil
	}
	if !isFullCommitSHA(sha) {
		return "", fmt.Errorf("%s must be a 40 character hexadecimal Git SHA", key)
	}
	return sha, nil
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

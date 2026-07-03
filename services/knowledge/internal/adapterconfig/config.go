package adapterconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHTTPAddr           = ":8083"
	DefaultServiceVersion     = "dev"
	DefaultEnvironment        = "local"
	DefaultVendorRuntimeURL   = "http://127.0.0.1:9380"
	DefaultShutdownTimeout    = 10 * time.Second
	DefaultWorkerStartTimeout = 30 * time.Second
)

type RuntimeReadinessMode string

const (
	RuntimeReadinessModeIngestion RuntimeReadinessMode = "ingestion"
	RuntimeReadinessModeQuery     RuntimeReadinessMode = "query"
)

type Config struct {
	HTTPAddr                  string
	MCPAddr                   string
	MCPCallerID               string
	MCPRoles                  string
	MCPPermissions            string
	ServiceVersion            string
	Environment               string
	ServiceToken              string
	VendorRuntimeToken        string
	VendorRuntimeURL          string
	VendorEmbeddingID         string
	VendorRerankID            string
	DatabaseURL               string
	AutoStartIngestion        bool
	RuntimeReadinessMode      RuntimeReadinessMode
	RuntimeWorkerStartCommand string
	RuntimeWorkerStartTimeout time.Duration
	ShutdownTimeout           time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:                  stringValue("KNOWLEDGE_HTTP_ADDR", DefaultHTTPAddr),
		MCPAddr:                   strings.TrimSpace(os.Getenv("KNOWLEDGE_MCP_ADDR")),
		MCPCallerID:               stringValue("KNOWLEDGE_MCP_CALLER_ID", "knowledge_mcp"),
		MCPRoles:                  strings.TrimSpace(os.Getenv("KNOWLEDGE_MCP_ROLES")),
		MCPPermissions:            stringValue("KNOWLEDGE_MCP_PERMISSIONS", "knowledge:read"),
		ServiceVersion:            stringValue("KNOWLEDGE_SERVICE_VERSION", DefaultServiceVersion),
		Environment:               stringValue("KNOWLEDGE_ENV", DefaultEnvironment),
		RuntimeReadinessMode:      RuntimeReadinessModeIngestion,
		RuntimeWorkerStartTimeout: DefaultWorkerStartTimeout,
		ShutdownTimeout:           DefaultShutdownTimeout,
	}
	cfg.VendorRuntimeURL = trimTrailingSlash(stringValue("VENDOR_RUNTIME_URL", DefaultVendorRuntimeURL))
	cfg.VendorEmbeddingID = strings.TrimSpace(os.Getenv("KNOWLEDGE_VENDOR_EMBEDDING_ID"))
	cfg.VendorRerankID = strings.TrimSpace(os.Getenv("KNOWLEDGE_VENDOR_RERANK_ID"))
	cfg.DatabaseURL = firstEnv("DATABASE_URL", "KNOWLEDGE_DATABASE_URL")
	cfg.ServiceToken = firstEnv("KNOWLEDGE_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN")
	cfg.VendorRuntimeToken = strings.TrimSpace(os.Getenv("VENDOR_RUNTIME_SERVICE_TOKEN"))
	cfg.AutoStartIngestion = boolValue("KNOWLEDGE_AUTO_START_INGESTION", true)
	cfg.RuntimeWorkerStartCommand = strings.TrimSpace(os.Getenv("KNOWLEDGE_RUNTIME_WORKER_START_COMMAND"))
	if raw := strings.TrimSpace(os.Getenv("KNOWLEDGE_RUNTIME_READINESS_MODE")); raw != "" {
		mode, err := parseRuntimeReadinessMode(raw)
		if err != nil {
			return Config{}, err
		}
		cfg.RuntimeReadinessMode = mode
	}
	if raw := os.Getenv("KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT"); raw != "" {
		value, err := parsePositiveDuration("KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.RuntimeWorkerStartTimeout = value
	}

	if raw := os.Getenv("KNOWLEDGE_SHUTDOWN_TIMEOUT"); raw != "" {
		value, err := parsePositiveDuration("KNOWLEDGE_SHUTDOWN_TIMEOUT", raw)
		if err != nil {
			return Config{}, err
		}
		cfg.ShutdownTimeout = value
	}

	if strings.TrimSpace(cfg.VendorRuntimeURL) == "" {
		return Config{}, fmt.Errorf("VENDOR_RUNTIME_URL is required")
	}
	if strings.TrimSpace(cfg.ServiceToken) == "" {
		return Config{}, fmt.Errorf("KNOWLEDGE_SERVICE_TOKEN or INTERNAL_SERVICE_TOKEN is required")
	}
	if strings.TrimSpace(cfg.VendorRuntimeToken) == "" {
		return Config{}, fmt.Errorf("VENDOR_RUNTIME_SERVICE_TOKEN is required")
	}
	return cfg, nil
}

func parseRuntimeReadinessMode(raw string) (RuntimeReadinessMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(RuntimeReadinessModeIngestion):
		return RuntimeReadinessModeIngestion, nil
	case string(RuntimeReadinessModeQuery):
		return RuntimeReadinessModeQuery, nil
	default:
		return "", fmt.Errorf("KNOWLEDGE_RUNTIME_READINESS_MODE must be one of ingestion, query")
	}
}

func parsePositiveDuration(key, raw string) (time.Duration, error) {
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func trimTrailingSlash(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func boolValue(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

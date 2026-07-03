package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultHTTPAddr        = ":8085"
	DefaultMCPPath         = "/mcp"
	DefaultMCPTokenHeader  = "Authorization"
	DefaultShutdownTimeout = 10 * time.Second
	DefaultPandocPath      = "pandoc"
	DefaultLibreOfficePath = "soffice"
)

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	RedisAddr             string
	FileServiceURL        string
	FileServiceToken      string
	AIGatewayURL          string
	AIGatewayProfileID    string
	AIGatewayModel        string
	AIGatewayServiceToken string
	KnowledgeServiceURL   string
	KnowledgeServiceToken string
	MCPPath               string
	MCPServiceToken       string
	MCPTokenHeader        string
	PandocPath            string
	LibreOfficePath       string
	ShutdownTimeout       time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:              envOr("DOCUMENT_HTTP_ADDR", DefaultHTTPAddr),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DOCUMENT_DATABASE_URL")),
		RedisAddr:             strings.TrimSpace(os.Getenv("DOCUMENT_REDIS_ADDR")),
		FileServiceURL:        strings.TrimSpace(os.Getenv("DOCUMENT_FILE_SERVICE_URL")),
		FileServiceToken:      firstEnv("DOCUMENT_FILE_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"),
		AIGatewayURL:          strings.TrimSpace(os.Getenv("DOCUMENT_AI_GATEWAY_URL")),
		AIGatewayProfileID:    strings.TrimSpace(os.Getenv("DOCUMENT_AI_GATEWAY_PROFILE_ID")),
		AIGatewayModel:        strings.TrimSpace(os.Getenv("DOCUMENT_AI_GATEWAY_MODEL")),
		AIGatewayServiceToken: firstEnv("DOCUMENT_AI_GATEWAY_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"),
		KnowledgeServiceURL:   strings.TrimSpace(os.Getenv("DOCUMENT_KNOWLEDGE_SERVICE_URL")),
		KnowledgeServiceToken: firstEnv("DOCUMENT_KNOWLEDGE_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"),
		MCPPath:               envOr("DOCUMENT_MCP_PATH", DefaultMCPPath),
		MCPServiceToken:       firstEnv("DOCUMENT_MCP_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN"),
		MCPTokenHeader:        envOr("DOCUMENT_MCP_TOKEN_HEADER", DefaultMCPTokenHeader),
		PandocPath:            envOr("DOCUMENT_PANDOC_PATH", DefaultPandocPath),
		LibreOfficePath:       envOr("DOCUMENT_LIBREOFFICE_PATH", DefaultLibreOfficePath),
		ShutdownTimeout:       DefaultShutdownTimeout,
	}

	if raw := strings.TrimSpace(os.Getenv("DOCUMENT_SHUTDOWN_TIMEOUT")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return Config{}, errors.New("DOCUMENT_SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = value
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return errors.New("DOCUMENT_HTTP_ADDR is required")
	}
	if err := validatePath("DOCUMENT_MCP_PATH", c.MCPPath); err != nil {
		return err
	}
	if strings.TrimSpace(c.MCPServiceToken) == "" {
		return errors.New("DOCUMENT_MCP_SERVICE_TOKEN or INTERNAL_SERVICE_TOKEN is required")
	}
	if !validHeaderName(c.MCPTokenHeader) {
		return errors.New("DOCUMENT_MCP_TOKEN_HEADER is invalid")
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return errors.New("DOCUMENT_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.RedisAddr) == "" {
		return errors.New("DOCUMENT_REDIS_ADDR is required")
	}
	if err := validateHTTPURL("DOCUMENT_FILE_SERVICE_URL", c.FileServiceURL); err != nil {
		return err
	}
	if err := validateHTTPURL("DOCUMENT_AI_GATEWAY_URL", c.AIGatewayURL); err != nil {
		return err
	}
	if strings.TrimSpace(c.AIGatewayProfileID) == "" {
		return errors.New("DOCUMENT_AI_GATEWAY_PROFILE_ID is required")
	}
	if strings.TrimSpace(c.KnowledgeServiceURL) != "" {
		if err := validateHTTPURL("DOCUMENT_KNOWLEDGE_SERVICE_URL", c.KnowledgeServiceURL); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.PandocPath) == "" {
		return errors.New("DOCUMENT_PANDOC_PATH is required")
	}
	if strings.TrimSpace(c.LibreOfficePath) == "" {
		return errors.New("DOCUMENT_LIBREOFFICE_PATH is required")
	}
	if c.ShutdownTimeout <= 0 {
		return errors.New("DOCUMENT_SHUTDOWN_TIMEOUT must be a positive duration")
	}
	return nil
}

func validateHTTPURL(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http(s) URL", name)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not contain credentials", name)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not contain query or fragment", name)
	}
	if name == "DOCUMENT_AI_GATEWAY_URL" {
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		if path != "" && path != "/internal/v1" {
			return fmt.Errorf("%s must be an AI Gateway service base URL", name)
		}
		if !trustedInternalHost(parsed.Hostname()) {
			return fmt.Errorf("%s host is not trusted", name)
		}
		if port := parsed.Port(); port != "" && port != "8086" {
			return fmt.Errorf("%s port is not trusted", name)
		}
	}
	return nil
}

func trustedInternalHost(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "" {
		return false
	}
	switch host {
	case "localhost", "ai-gateway":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func envOr(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func validatePath(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return fmt.Errorf("%s must be an absolute HTTP path", name)
	}
	return nil
}

func validHeaderName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

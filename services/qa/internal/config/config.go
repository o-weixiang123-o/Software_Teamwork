package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/modelendpoint"
)

const (
	TransportDisabled         = "disabled"
	TransportStdio            = "stdio"
	TransportStreamableHTTP   = "streamable_http"
	maxSessionAttachmentBytes = int64(20 << 20)

	defaultAIGatewayURL         = "http://localhost:8086/internal/v1/chat/completions"
	defaultAIGatewayTokenHeader = "X-Service-Token"
	defaultSystemPrompt         = `You are a QA agent for a power-industry knowledge system.
When the user asks about facts, standards, policies, domain knowledge, uploaded knowledge-base content, or requests citations, call knowledge__search first. Use the user's question as the query, set topK to 5 unless the user asks otherwise, and leave knowledgeBaseIds empty to search all indexed knowledge bases.
After knowledge__search or knowledge__get_chunk returns relevant results, stop calling retrieval tools and write the final answer with citations from those results. Do not repeat similar searches unless the retrieved results are empty or clearly unrelated.
If knowledge__search is unavailable, use search_knowledge as the fallback retrieval tool. Answer knowledge questions only from retrieved tool results; if retrieval finds no relevant content, say that clearly instead of inventing sources.
Use search_session_attachments only for files bound to the current message. Use document__ tools only for report generation tasks.`
)

type Config struct {
	HTTPAddr                string
	ShutdownTimeout         time.Duration
	MaxRequestBytes         int64
	DatabaseURL             string
	EncryptionKey           string
	AdminUserIDs            []string
	SettingsOpen            bool
	ServiceToken            string
	KnowledgeURL            string
	KnowledgeMCPURL         string
	KnowledgeMCPToken       string
	KnowledgeMCPTokenHeader string
	KnowledgeMCPAlias       string
	KnowledgeMCPTimeout     time.Duration

	AIGatewayURL         string
	AIGatewayToken       string
	AIGatewayTokenHeader string
	AIGatewayProfileID   string
	ModelID              string
	ModelTimeout         time.Duration
	MaxTokens            int
	AIGatewayStream      bool

	MCPTransport         string
	MCPServerCommand     string
	MCPServerArgs        []string
	MCPServerURL         string
	MCPServerAlias       string
	MCPServerToken       string
	MCPServerTokenHeader string
	MCPToolTimeout       time.Duration

	SystemPrompt             string
	MaxIterations            int
	MaxToolResultBytes       int
	WorkDir                  string
	MaxFileBytes             int
	EnableCommandTool        bool
	CommandTimeout           time.Duration
	AttachmentTTL            time.Duration
	AttachmentMaxBytes       int64
	AttachmentMaxPerSession  int
	AttachmentProcessTimeout time.Duration
	FileServiceURL           string
}

func Load() (Config, error) {
	serviceToken := strings.TrimSpace(os.Getenv("INTERNAL_SERVICE_TOKEN"))
	knowledgeMCPToken := strings.TrimSpace(os.Getenv("KNOWLEDGE_MCP_TOKEN"))
	if knowledgeMCPToken == "" {
		knowledgeMCPToken = serviceToken
	}
	aiGatewayToken := strings.TrimSpace(os.Getenv("AI_GATEWAY_TOKEN"))
	if aiGatewayToken == "" {
		aiGatewayToken = serviceToken
	}
	cfg := Config{
		HTTPAddr:                envOr("QA_HTTP_ADDR", ":8084"),
		DatabaseURL:             strings.TrimSpace(os.Getenv("QA_DATABASE_URL")),
		EncryptionKey:           envOr("QA_CONFIG_ENCRYPTION_KEY", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"),
		AdminUserIDs:            splitCSV(os.Getenv("QA_ADMIN_USER_IDS")),
		ServiceToken:            serviceToken,
		KnowledgeURL:            envOr("KNOWLEDGE_SERVICE_URL", "http://localhost:8083"),
		KnowledgeMCPURL:         strings.TrimSpace(os.Getenv("KNOWLEDGE_MCP_URL")),
		KnowledgeMCPToken:       knowledgeMCPToken,
		KnowledgeMCPTokenHeader: envOr("KNOWLEDGE_MCP_TOKEN_HEADER", "X-Service-Token"),
		KnowledgeMCPAlias:       envOr("KNOWLEDGE_MCP_ALIAS", "knowledge"),
		AIGatewayURL:            envOr("AI_GATEWAY_URL", defaultAIGatewayURL),
		AIGatewayToken:          aiGatewayToken,
		AIGatewayTokenHeader:    envOr("AI_GATEWAY_TOKEN_HEADER", defaultAIGatewayTokenHeader),
		AIGatewayProfileID:      strings.TrimSpace(os.Getenv("AI_GATEWAY_PROFILE_ID")),
		ModelID:                 envOr("MODEL_ID", "deepseek-chat"),
		MCPTransport:            strings.ToLower(envOr("MCP_TRANSPORT", TransportDisabled)),
		MCPServerCommand:        strings.TrimSpace(os.Getenv("MCP_SERVER_COMMAND")),
		MCPServerURL:            strings.TrimSpace(os.Getenv("MCP_SERVER_URL")),
		MCPServerAlias:          envOr("MCP_SERVER_ALIAS", "env_default"),
		MCPServerToken:          os.Getenv("MCP_SERVER_TOKEN"),
		MCPServerTokenHeader:    envOr("MCP_SERVER_TOKEN_HEADER", "Authorization"),
		SystemPrompt:            envOr("AGENT_SYSTEM_PROMPT", defaultSystemPrompt),
		WorkDir:                 strings.TrimSpace(os.Getenv("AGENT_WORKDIR")),
	}

	var err error
	if cfg.WorkDir == "" {
		if cfg.WorkDir, err = os.Getwd(); err != nil {
			return Config{}, fmt.Errorf("resolve current working directory: %w", err)
		}
	}
	if cfg.ModelTimeout, err = durationEnv("AI_GATEWAY_TIMEOUT", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("QA_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.MaxRequestBytes, err = positiveInt64Env("QA_MAX_REQUEST_BYTES", 1<<20); err != nil {
		return Config{}, err
	}
	if cfg.MCPToolTimeout, err = durationEnv("MCP_TOOL_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.KnowledgeMCPTimeout, err = durationEnv("KNOWLEDGE_MCP_TIMEOUT", cfg.MCPToolTimeout); err != nil {
		return Config{}, err
	}
	if cfg.MaxTokens, err = positiveIntEnv("AGENT_MAX_TOKENS", 4096); err != nil {
		return Config{}, err
	}
	if cfg.MaxIterations, err = positiveIntEnv("AGENT_MAX_ITERATIONS", 8); err != nil {
		return Config{}, err
	}
	if cfg.MaxIterations > 10 {
		return Config{}, errors.New("AGENT_MAX_ITERATIONS must not exceed 10")
	}
	if cfg.MaxToolResultBytes, err = positiveIntEnv("MCP_MAX_RESULT_BYTES", 50000); err != nil {
		return Config{}, err
	}
	if cfg.MaxToolResultBytes < 100 {
		return Config{}, errors.New("MCP_MAX_RESULT_BYTES must be at least 100")
	}
	if cfg.MaxFileBytes, err = positiveIntEnv("AGENT_MAX_FILE_BYTES", 1<<20); err != nil {
		return Config{}, err
	}
	if cfg.CommandTimeout, err = durationEnv("AGENT_COMMAND_TIMEOUT", 120*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.EnableCommandTool, err = boolEnv("AGENT_ENABLE_COMMAND_TOOL", false); err != nil {
		return Config{}, err
	}
	if cfg.AttachmentTTL, err = hoursDurationEnv("QA_SESSION_ATTACHMENT_TTL_HOURS", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.AttachmentMaxBytes, err = positiveInt64Env("QA_SESSION_ATTACHMENT_MAX_BYTES", 20<<20); err != nil {
		return Config{}, err
	}
	if cfg.AttachmentMaxPerSession, err = positiveIntEnv("QA_SESSION_ATTACHMENT_MAX_PER_SESSION", 10); err != nil {
		return Config{}, err
	}
	if cfg.AttachmentProcessTimeout, err = secondsDurationEnv("QA_SESSION_ATTACHMENT_PROCESS_TIMEOUT_SECONDS", 60*time.Second); err != nil {
		return Config{}, err
	}
	cfg.FileServiceURL = envOr("FILE_SERVICE_BASE_URL", "http://localhost:8082")
	if cfg.SettingsOpen, err = boolEnv("QA_SETTINGS_OPEN", false); err != nil {
		return Config{}, err
	}
	if cfg.AIGatewayStream, err = boolEnv("AI_GATEWAY_STREAM", false); err != nil {
		return Config{}, err
	}

	if raw := strings.TrimSpace(os.Getenv("MCP_SERVER_ARGS_JSON")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.MCPServerArgs); err != nil {
			return Config{}, fmt.Errorf("MCP_SERVER_ARGS_JSON must be a JSON string array: %w", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.AttachmentMaxBytes > maxSessionAttachmentBytes {
		return fmt.Errorf("QA_SESSION_ATTACHMENT_MAX_BYTES must not exceed %d", maxSessionAttachmentBytes)
	}
	if err := validateHTTPURL("KNOWLEDGE_SERVICE_URL", c.KnowledgeURL); err != nil {
		return err
	}
	if c.KnowledgeMCPURL != "" {
		if err := validateHTTPURL("KNOWLEDGE_MCP_URL", c.KnowledgeMCPURL); err != nil {
			return err
		}
	}
	if !validHeaderName(c.KnowledgeMCPTokenHeader) {
		return errors.New("KNOWLEDGE_MCP_TOKEN_HEADER is invalid")
	}
	if !validMCPAlias(c.KnowledgeMCPAlias) {
		return errors.New("KNOWLEDGE_MCP_ALIAS must match ^[a-z0-9_]{2,32}$")
	}
	if err := validateHTTPURL("FILE_SERVICE_BASE_URL", c.FileServiceURL); err != nil {
		return err
	}
	if err := validateHTTPURL("AI_GATEWAY_URL", c.AIGatewayURL); err != nil {
		return err
	}
	if c.ModelID == "" {
		return errors.New("MODEL_ID is required")
	}
	if !validHeaderName(c.AIGatewayTokenHeader) {
		return errors.New("AI_GATEWAY_TOKEN_HEADER is invalid")
	}
	if !validHeaderName(c.MCPServerTokenHeader) {
		return errors.New("MCP_SERVER_TOKEN_HEADER is invalid")
	}
	if c.MCPServerAlias == "" || !validMCPAlias(c.MCPServerAlias) {
		return errors.New("MCP_SERVER_ALIAS must match ^[a-z0-9_]{2,32}$")
	}
	root, err := filepath.Abs(c.WorkDir)
	if err != nil {
		return fmt.Errorf("AGENT_WORKDIR is invalid: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("AGENT_WORKDIR must be an existing directory")
	}
	switch c.MCPTransport {
	case TransportDisabled:
	case TransportStdio:
		return errors.New("MCP_TRANSPORT=stdio is test-only; use streamable_http for runtime MCP servers")
	case TransportStreamableHTTP:
		if err := validateHTTPURL("MCP_SERVER_URL", c.MCPServerURL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("MCP_TRANSPORT must be %q, %q, or %q", TransportDisabled, TransportStdio, TransportStreamableHTTP)
	}
	return nil
}

func validateHTTPURL(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if name == "AI_GATEWAY_URL" {
		if _, err := modelendpoint.NormalizeAIGatewayChatEndpoint(value); err != nil {
			return fmt.Errorf("%s is invalid: %w", name, err)
		}
		return nil
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
	return nil
}

func validHeaderName(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\r\n:")
}

func validMCPAlias(value string) bool {
	if len(value) < 2 || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func positiveInt64Env(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func hoursDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer hour count", name)
	}
	return time.Duration(parsed) * time.Hour, nil
}

func secondsDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer second count", name)
	}
	return time.Duration(parsed) * time.Second, nil
}

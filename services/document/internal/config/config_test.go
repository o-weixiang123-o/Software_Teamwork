package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing database URL error")
	}
	if !strings.Contains(err.Error(), "DOCUMENT_DATABASE_URL") {
		t.Fatalf("expected DOCUMENT_DATABASE_URL in error, got %v", err)
	}
}

func TestLoadValidatesDocumentDependencies(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("DOCUMENT_AI_GATEWAY_MODEL", "deepseek-chat")
	t.Setenv("DOCUMENT_MCP_SERVICE_TOKEN", "mcp-token")
	t.Setenv("DOCUMENT_PANDOC_PATH", "pandoc")
	t.Setenv("DOCUMENT_LIBREOFFICE_PATH", "soffice")
	t.Setenv("DOCUMENT_SHUTDOWN_TIMEOUT", "7s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8085" {
		t.Fatalf("HTTPAddr = %q, want :8085", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL == "" || cfg.RedisAddr == "" || cfg.FileServiceURL == "" || cfg.AIGatewayURL == "" {
		t.Fatalf("expected required dependency config to be populated: %+v", cfg)
	}
	if cfg.AIGatewayProfileID != "default-chat" {
		t.Fatalf("AIGatewayProfileID = %q", cfg.AIGatewayProfileID)
	}
	if cfg.AIGatewayModel != "deepseek-chat" {
		t.Fatalf("AIGatewayModel = %q, want deepseek-chat", cfg.AIGatewayModel)
	}
	if cfg.MCPPath != DefaultMCPPath || cfg.MCPTokenHeader != DefaultMCPTokenHeader {
		t.Fatalf("unexpected MCP defaults: %+v", cfg)
	}
	if cfg.PandocPath != "pandoc" || cfg.LibreOfficePath != "soffice" {
		t.Fatalf("unexpected DOCX tool paths: %+v", cfg)
	}
	if cfg.ShutdownTimeout != 7*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 7s", cfg.ShutdownTimeout)
	}
}

func TestLoadUsesDocumentFileServiceTokenFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FileServiceToken != "shared-token" {
		t.Fatalf("FileServiceToken = %q, want shared-token", cfg.FileServiceToken)
	}

	t.Setenv("DOCUMENT_FILE_SERVICE_TOKEN", "document-file-token")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with document file token error = %v", err)
	}
	if cfg.FileServiceToken != "document-file-token" {
		t.Fatalf("FileServiceToken = %q, want document-file-token", cfg.FileServiceToken)
	}
}

func TestLoadUsesDocumentAIGatewayServiceTokenFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AIGatewayServiceToken != "shared-token" {
		t.Fatalf("AIGatewayServiceToken = %q, want shared-token", cfg.AIGatewayServiceToken)
	}

	t.Setenv("DOCUMENT_AI_GATEWAY_SERVICE_TOKEN", "document-token")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with document token error = %v", err)
	}
	if cfg.AIGatewayServiceToken != "document-token" {
		t.Fatalf("AIGatewayServiceToken = %q, want document-token", cfg.AIGatewayServiceToken)
	}
}

func TestLoadRejectsEmptyDocumentMCPServiceToken(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing MCP service token error")
	}
	if !strings.Contains(err.Error(), "DOCUMENT_MCP_SERVICE_TOKEN") {
		t.Fatalf("expected DOCUMENT_MCP_SERVICE_TOKEN in error, got %v", err)
	}
}

func TestLoadUsesDocumentMCPServiceTokenFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MCPServiceToken != "shared-token" {
		t.Fatalf("MCPServiceToken = %q, want shared-token", cfg.MCPServiceToken)
	}

	t.Setenv("DOCUMENT_MCP_SERVICE_TOKEN", "document-mcp-token")
	t.Setenv("DOCUMENT_MCP_TOKEN_HEADER", "X-Service-Token")
	t.Setenv("DOCUMENT_MCP_PATH", "/document-mcp")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with document MCP token error = %v", err)
	}
	if cfg.MCPServiceToken != "document-mcp-token" || cfg.MCPTokenHeader != "X-Service-Token" || cfg.MCPPath != "/document-mcp" {
		t.Fatalf("MCP config = %+v", cfg)
	}
}

func TestLoadRejectsUntrustedDocumentAIGatewayURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "https://public.example.test")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")
	if _, err := Load(); err == nil {
		t.Fatal("expected untrusted AI Gateway host to fail")
	}
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://ai-gateway/internal/v1/model-profiles")
	if _, err := Load(); err == nil {
		t.Fatal("expected non-base AI Gateway path to fail")
	}
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:18086")
	if _, err := Load(); err == nil {
		t.Fatal("expected non-standard AI Gateway port to fail")
	}
}

func TestLoadUsesOptionalKnowledgeServiceConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("DOCUMENT_DATABASE_URL", "postgres://document:document@localhost:5432/document?sslmode=disable")
	t.Setenv("DOCUMENT_REDIS_ADDR", "localhost:6379")
	t.Setenv("DOCUMENT_FILE_SERVICE_URL", "http://localhost:8082")
	t.Setenv("DOCUMENT_AI_GATEWAY_URL", "http://localhost:8086")
	t.Setenv("DOCUMENT_AI_GATEWAY_PROFILE_ID", "default-chat")
	t.Setenv("DOCUMENT_MCP_SERVICE_TOKEN", "mcp-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() without knowledge URL error = %v", err)
	}
	if cfg.KnowledgeServiceURL != "" || cfg.KnowledgeServiceToken != "" {
		t.Fatalf("knowledge config should be optional by default: %+v", cfg)
	}

	t.Setenv("DOCUMENT_KNOWLEDGE_SERVICE_URL", "http://localhost:8083")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with knowledge URL error = %v", err)
	}
	if cfg.KnowledgeServiceURL != "http://localhost:8083" || cfg.KnowledgeServiceToken != "shared-token" {
		t.Fatalf("knowledge config = %+v", cfg)
	}

	t.Setenv("DOCUMENT_KNOWLEDGE_SERVICE_TOKEN", "document-knowledge-token")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with document knowledge token error = %v", err)
	}
	if cfg.KnowledgeServiceToken != "document-knowledge-token" {
		t.Fatalf("KnowledgeServiceToken = %q, want document-knowledge-token", cfg.KnowledgeServiceToken)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DOCUMENT_HTTP_ADDR",
		"DOCUMENT_DATABASE_URL",
		"DOCUMENT_REDIS_ADDR",
		"DOCUMENT_FILE_SERVICE_URL",
		"DOCUMENT_FILE_SERVICE_TOKEN",
		"DOCUMENT_AI_GATEWAY_URL",
		"DOCUMENT_AI_GATEWAY_PROFILE_ID",
		"DOCUMENT_AI_GATEWAY_MODEL",
		"DOCUMENT_AI_GATEWAY_SERVICE_TOKEN",
		"DOCUMENT_KNOWLEDGE_SERVICE_URL",
		"DOCUMENT_KNOWLEDGE_SERVICE_TOKEN",
		"DOCUMENT_MCP_PATH",
		"DOCUMENT_MCP_SERVICE_TOKEN",
		"DOCUMENT_MCP_TOKEN_HEADER",
		"INTERNAL_SERVICE_TOKEN",
		"DOCUMENT_PANDOC_PATH",
		"DOCUMENT_LIBREOFFICE_PATH",
		"DOCUMENT_SHUTDOWN_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

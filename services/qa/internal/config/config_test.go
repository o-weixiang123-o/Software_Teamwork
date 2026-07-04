package config

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("AI_GATEWAY_URL", "")
	t.Setenv("AI_GATEWAY_TOKEN", "")
	t.Setenv("AI_GATEWAY_TOKEN_HEADER", "")
	t.Setenv("AI_GATEWAY_PROFILE_ID", "")
	t.Setenv("AI_GATEWAY_STREAM", "")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-service-token")
	t.Setenv("MODEL_ID", "")
	t.Setenv("MCP_TRANSPORT", "")
	t.Setenv("MCP_SERVER_COMMAND", "")
	t.Setenv("MCP_SERVER_ARGS_JSON", "")
	t.Setenv("MCP_SERVER_ALIAS", "")
	t.Setenv("KNOWLEDGE_MCP_URL", "")
	t.Setenv("KNOWLEDGE_MCP_TOKEN", "")
	t.Setenv("KNOWLEDGE_MCP_TOKEN_HEADER", "")
	t.Setenv("KNOWLEDGE_MCP_ALIAS", "")
	t.Setenv("KNOWLEDGE_MCP_TIMEOUT", "")
}

func TestLoadDefaultConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPTransport != TransportDisabled || cfg.MCPServerAlias != "env_default" || len(cfg.MCPServerArgs) != 0 {
		t.Fatalf("unexpected MCP config: %+v", cfg)
	}
	if cfg.ModelTimeout != 60*time.Second || cfg.MaxIterations != 8 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.KnowledgeMCPURL != "" || cfg.KnowledgeMCPToken != "test-service-token" ||
		cfg.KnowledgeMCPTokenHeader != "X-Service-Token" || cfg.KnowledgeMCPAlias != "knowledge" ||
		cfg.KnowledgeMCPTimeout != 30*time.Second {
		t.Fatalf("unexpected Knowledge MCP defaults: %+v", cfg)
	}
	if cfg.HTTPAddr != ":8084" || cfg.ShutdownTimeout != 10*time.Second || cfg.MaxRequestBytes != 1<<20 {
		t.Fatalf("unexpected HTTP defaults: %+v", cfg)
	}
	if cfg.AttachmentMaxBytes != maxSessionAttachmentBytes {
		t.Fatalf("attachment max bytes = %d, want %d", cfg.AttachmentMaxBytes, maxSessionAttachmentBytes)
	}
	if cfg.AIGatewayURL != defaultAIGatewayURL ||
		cfg.AIGatewayToken != "test-service-token" ||
		cfg.AIGatewayTokenHeader != defaultAIGatewayTokenHeader ||
		cfg.ModelID != "" ||
		cfg.AIGatewayStream {
		t.Fatalf("unexpected AI Gateway defaults: %+v", cfg)
	}
}

func TestLoadKnowledgeMCPConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("KNOWLEDGE_MCP_URL", "http://knowledge:8083/mcp")
	t.Setenv("KNOWLEDGE_MCP_TOKEN", "knowledge-token")
	t.Setenv("KNOWLEDGE_MCP_TOKEN_HEADER", "X-Knowledge-Token")
	t.Setenv("KNOWLEDGE_MCP_ALIAS", "team_knowledge")
	t.Setenv("KNOWLEDGE_MCP_TIMEOUT", "12s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KnowledgeMCPURL != "http://knowledge:8083/mcp" ||
		cfg.KnowledgeMCPToken != "knowledge-token" ||
		cfg.KnowledgeMCPTokenHeader != "X-Knowledge-Token" ||
		cfg.KnowledgeMCPAlias != "team_knowledge" || cfg.KnowledgeMCPTimeout != 12*time.Second {
		t.Fatalf("unexpected Knowledge MCP config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidKnowledgeMCPConfiguration(t *testing.T) {
	for name, setup := range map[string]func(*testing.T){
		"url":    func(t *testing.T) { t.Setenv("KNOWLEDGE_MCP_URL", "file:///tmp/mcp") },
		"header": func(t *testing.T) { t.Setenv("KNOWLEDGE_MCP_TOKEN_HEADER", "bad:header") },
		"alias":  func(t *testing.T) { t.Setenv("KNOWLEDGE_MCP_ALIAS", "Knowledge-Tools") },
	} {
		t.Run(name, func(t *testing.T) {
			setRequiredEnvironment(t)
			setup(t)
			if _, err := Load(); err == nil {
				t.Fatal("expected invalid Knowledge MCP configuration to fail")
			}
		})
	}
}

func TestLoadRejectsAttachmentLimitAbovePublicContract(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("QA_SESSION_ATTACHMENT_MAX_BYTES", strconv.FormatInt(maxSessionAttachmentBytes+1, 10))

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "QA_SESSION_ATTACHMENT_MAX_BYTES must not exceed 20971520") {
		t.Fatalf("Load() error = %v, want public-contract limit error", err)
	}
}

func TestLoadAcceptsSmallerAttachmentLimit(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("QA_SESSION_ATTACHMENT_MAX_BYTES", "1048576")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AttachmentMaxBytes != 1<<20 {
		t.Fatalf("attachment max bytes = %d, want %d", cfg.AttachmentMaxBytes, 1<<20)
	}
}

func TestLoadBuiltInToolsWithoutMCPServer(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_TRANSPORT", TransportDisabled)
	t.Setenv("MCP_SERVER_COMMAND", "")
	t.Setenv("AGENT_WORKDIR", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPTransport != TransportDisabled || cfg.EnableCommandTool {
		t.Fatalf("unexpected built-in tool defaults: %+v", cfg)
	}
}

func TestLoadDefaultsToBuiltInToolsOnly(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_TRANSPORT", "")
	t.Setenv("MCP_SERVER_COMMAND", "")
	t.Setenv("AGENT_WORKDIR", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPTransport != TransportDisabled {
		t.Fatalf("MCP transport = %q, want disabled", cfg.MCPTransport)
	}
}

func TestLoadAcceptsExplicitAIGatewayEndpoint(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("AI_GATEWAY_URL", "http://ai-gateway:8086/internal/v1/chat/completions")
	t.Setenv("AI_GATEWAY_TOKEN", "explicit-token")
	t.Setenv("AI_GATEWAY_TOKEN_HEADER", "X-Service-Token")
	t.Setenv("AI_GATEWAY_PROFILE_ID", "profile-chat")
	t.Setenv("AI_GATEWAY_STREAM", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIGatewayURL != "http://ai-gateway:8086/internal/v1/chat/completions" ||
		cfg.AIGatewayToken != "explicit-token" ||
		cfg.AIGatewayTokenHeader != "X-Service-Token" ||
		cfg.AIGatewayProfileID != "profile-chat" ||
		!cfg.AIGatewayStream {
		t.Fatalf("unexpected AI Gateway override: %+v", cfg)
	}
}

func TestLoadRejectsUntrustedAIGatewayEndpoint(t *testing.T) {
	cases := []string{
		"https://public.example.test/internal/v1/chat/completions",
		"http://169.254.169.254/internal/v1/chat/completions",
		"http://10.0.0.5/internal/v1/chat/completions",
		"http://localhost:18086/internal/v1/chat/completions",
		"http://ai-gateway/internal/v1/model-profiles",
		"http://ai-gateway/internal/v1/chat/completions?redirect=http://example.test",
	}
	for _, endpoint := range cases {
		t.Run(endpoint, func(t *testing.T) {
			setRequiredEnvironment(t)
			t.Setenv("AI_GATEWAY_URL", endpoint)
			if _, err := Load(); err == nil {
				t.Fatalf("expected endpoint %q to fail", endpoint)
			}
		})
	}
}

func TestLoadStreamableHTTPConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_TRANSPORT", TransportStreamableHTTP)
	t.Setenv("MCP_SERVER_URL", "https://mcp.example.test/mcp")
	t.Setenv("MCP_SERVER_ALIAS", "document")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServerURL != "https://mcp.example.test/mcp" || cfg.MCPServerAlias != "document" {
		t.Fatalf("unexpected MCP config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidMCPAlias(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_SERVER_ALIAS", "Document-Tools")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid MCP alias to fail")
	}
}

func TestLoadRejectsShellStyleArguments(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_SERVER_ARGS_JSON", `server.py --unsafe`)
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid JSON arguments to fail")
	}
}

func TestLoadRejectsNonAllowlistedStdioSpec(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_TRANSPORT", TransportStdio)
	t.Setenv("MCP_SERVER_COMMAND", "python3")
	t.Setenv("MCP_SERVER_ARGS_JSON", `["server.py"]`)
	if _, err := Load(); err == nil {
		t.Fatal("expected non-allowlisted stdio command spec to fail")
	}
}

func TestLoadRejectsRuntimeStdioTransport(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("MCP_TRANSPORT", TransportStdio)
	t.Setenv("MCP_SERVER_COMMAND", "go")
	t.Setenv("MCP_SERVER_ARGS_JSON", `["run","./testserver/cmd/echo"]`)
	if _, err := Load(); err == nil {
		t.Fatal("expected runtime stdio transport to fail")
	}
}

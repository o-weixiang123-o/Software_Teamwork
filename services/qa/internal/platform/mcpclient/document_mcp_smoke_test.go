package mcpclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	qaconfig "github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/config"
)

func TestDocumentMCPSmoke(t *testing.T) {
	if strings.TrimSpace(os.Getenv("QA_DOCUMENT_MCP_SMOKE")) != "1" {
		t.Skip("set QA_DOCUMENT_MCP_SMOKE=1 to run the QA -> Document MCP smoke")
	}

	cfg, err := qaconfig.Load()
	if err != nil {
		t.Fatalf("load QA smoke configuration: %v", err)
	}
	if !cfg.DocumentMCPEnabled {
		t.Fatal("QA_DOCUMENT_MCP_ENABLED=true is required when QA_DOCUMENT_MCP_SMOKE=1")
	}
	if strings.TrimSpace(cfg.DocumentMCPServerURL) == "" {
		t.Fatal("DOCUMENT_MCP_SERVER_URL is required when QA_DOCUMENT_MCP_SMOKE=1")
	}

	requestID := fmt.Sprintf("qa-document-mcp-smoke-%d", time.Now().UTC().UnixNano())
	ctx := context.WithValue(context.Background(), "request_id", requestID)

	t.Run("tools_list", func(t *testing.T) {
		client := newDocumentMCPSmokeClient(t, cfg)
		defer client.Close()

		tools, err := client.ListTools(ctx)
		if err != nil {
			t.Fatalf("QA -> Document MCP tools/list failed (request_id=%s): %v; verify Document service readiness, MCP endpoint, and authorization", requestID, err)
		}
		if len(tools) == 0 {
			t.Fatal("Document MCP tools/list returned empty tool list")
		}

		expectedTools := []string{
			"document__generate_report_outline",
			"document__regenerate_report_outline",
			"document__generate_report_text",
			"document__regenerate_report_text",
			"document__regenerate_report_section",
			"document__get_generation_status",
			"document__get_template_schema",
			"document__export_report_docx",
			"document__get_report_result",
		}

		toolNames := make(map[string]bool)
		for _, tool := range tools {
			toolNames[tool.Function.Name] = true
			t.Logf("Found tool: %s - %s", tool.Function.Name, tool.Function.Description)
		}

		for _, expected := range expectedTools {
			if !toolNames[expected] {
				t.Errorf("Expected tool %q not found in Document MCP tools/list", expected)
			}
		}

		t.Logf("QA -> Document MCP tools/list smoke succeeded (request_id=%s tool_count=%d)", requestID, len(tools))
	})

	t.Run("invalid_token", func(t *testing.T) {
		invalidCfg := cfg
		invalidCfg.DocumentMCPServerToken = "invalid-token-" + requestID

		client, err := Connect(ctx, Config{
			Transport:   TransportStreamableHTTP,
			Endpoint:    invalidCfg.DocumentMCPServerURL,
			Token:       invalidCfg.DocumentMCPServerToken,
			TokenHeader: invalidCfg.DocumentMCPTokenHeader,
		})
		if err != nil {
			t.Logf("Expected connection error for invalid token: %v", err)
			return
		}
		defer client.Close()

		_, err = client.ListTools(ctx)
		if err == nil {
			t.Fatal("tools/list with invalid token unexpectedly succeeded")
		}
		t.Logf("tools/list with invalid token failed as expected: %v", err)
	})

	t.Run("mcp_unavailable", func(t *testing.T) {
		unavailableCfg := cfg
		unavailableCfg.DocumentMCPServerURL = "http://localhost:9999/mcp/v1"

		_, err := Connect(ctx, Config{
			Transport:   TransportStreamableHTTP,
			Endpoint:    unavailableCfg.DocumentMCPServerURL,
			Token:       unavailableCfg.DocumentMCPServerToken,
			TokenHeader: unavailableCfg.DocumentMCPTokenHeader,
		})
		if err == nil {
			t.Fatal("Connected to unavailable Document MCP server unexpectedly")
		}
		t.Logf("Connection to unavailable Document MCP server failed as expected: %v", err)
	})
}

func newDocumentMCPSmokeClient(t *testing.T, cfg qaconfig.Config) *Client {
	t.Helper()

	client, err := Connect(context.Background(), Config{
		Transport:   TransportStreamableHTTP,
		Endpoint:    cfg.DocumentMCPServerURL,
		Token:       cfg.DocumentMCPServerToken,
		TokenHeader: cfg.DocumentMCPTokenHeader,
	})
	if err != nil {
		t.Fatalf("create QA Document MCP smoke client: %v", err)
	}
	return client
}
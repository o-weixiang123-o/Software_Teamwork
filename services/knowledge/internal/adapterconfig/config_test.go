package adapterconfig

import (
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-service-token")
	t.Setenv("VENDOR_RUNTIME_SERVICE_TOKEN", "runtime-service-token")
}

func TestLoadDefaults(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_HTTP_ADDR", "")
	t.Setenv("VENDOR_RUNTIME_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr {
		t.Fatalf("HTTPAddr=%q", cfg.HTTPAddr)
	}
	if cfg.VendorRuntimeURL != DefaultVendorRuntimeURL {
		t.Fatalf("VendorRuntimeURL=%q", cfg.VendorRuntimeURL)
	}
	if cfg.ServiceToken != "test-service-token" {
		t.Fatalf("ServiceToken=%q", cfg.ServiceToken)
	}
	if cfg.VendorRuntimeToken != "runtime-service-token" {
		t.Fatalf("VendorRuntimeToken=%q", cfg.VendorRuntimeToken)
	}
	if cfg.ProjectRuntimeUserID != "knowledge_mcp_service" {
		t.Fatalf("ProjectRuntimeUserID=%q", cfg.ProjectRuntimeUserID)
	}
}

func TestLoadRequiresServiceToken(t *testing.T) {
	t.Setenv("VENDOR_RUNTIME_SERVICE_TOKEN", "runtime-service-token")
	t.Setenv("KNOWLEDGE_SERVICE_TOKEN", "")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "KNOWLEDGE_SERVICE_TOKEN") {
		t.Fatalf("Load() error = %v, want service token requirement", err)
	}
}

func TestLoadRequiresVendorRuntimeServiceToken(t *testing.T) {
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-service-token")
	t.Setenv("VENDOR_RUNTIME_SERVICE_TOKEN", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "VENDOR_RUNTIME_SERVICE_TOKEN") {
		t.Fatalf("Load() error = %v, want vendor runtime token requirement", err)
	}
}

func TestLoadKnowledgeServiceTokenOverridesSharedToken(t *testing.T) {
	t.Setenv("KNOWLEDGE_SERVICE_TOKEN", "knowledge-token")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "shared-token")
	t.Setenv("VENDOR_RUNTIME_SERVICE_TOKEN", "runtime-service-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ServiceToken != "knowledge-token" {
		t.Fatalf("ServiceToken=%q", cfg.ServiceToken)
	}
}

func TestLoadKnowledgeDatabaseURLFallback(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_URL", "")
	t.Setenv("KNOWLEDGE_DATABASE_URL", "postgres://knowledge_app:test@localhost:5432/knowledge_system?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL != "postgres://knowledge_app:test@localhost:5432/knowledge_system?sslmode=disable" {
		t.Fatalf("DatabaseURL=%q", cfg.DatabaseURL)
	}
}

func TestLoadAutoStartIngestionDefault(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_AUTO_START_INGESTION", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AutoStartIngestion {
		t.Fatal("AutoStartIngestion should default to true")
	}
}

func TestLoadAutoStartIngestionDisabled(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_AUTO_START_INGESTION", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AutoStartIngestion {
		t.Fatal("AutoStartIngestion should be false")
	}
}

func TestLoadCustomVendorURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("VENDOR_RUNTIME_URL", "http://knowledge-vendor:9380/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.VendorRuntimeURL != "http://knowledge-vendor:9380" {
		t.Fatalf("VendorRuntimeURL=%q", cfg.VendorRuntimeURL)
	}
}

func TestLoadProjectRuntimeUserIDOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_MCP_USER_ID", "mcp_service")
	t.Setenv("KNOWLEDGE_PROJECT_RUNTIME_USER_ID", "project_runtime")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ProjectRuntimeUserID != "project_runtime" {
		t.Fatalf("ProjectRuntimeUserID=%q", cfg.ProjectRuntimeUserID)
	}
	if cfg.MCPUserID != "mcp_service" {
		t.Fatalf("MCPUserID=%q", cfg.MCPUserID)
	}
}

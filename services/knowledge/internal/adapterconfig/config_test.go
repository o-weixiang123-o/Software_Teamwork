package adapterconfig

import (
	"strings"
	"testing"
	"time"
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
	if cfg.MCPCallerID != "knowledge_mcp" {
		t.Fatalf("MCPCallerID=%q", cfg.MCPCallerID)
	}
	if cfg.RuntimeReadinessMode != RuntimeReadinessModeIngestion {
		t.Fatalf("RuntimeReadinessMode=%q", cfg.RuntimeReadinessMode)
	}
	if cfg.RuntimeWorkerStartTimeout != DefaultWorkerStartTimeout {
		t.Fatalf("RuntimeWorkerStartTimeout=%s", cfg.RuntimeWorkerStartTimeout)
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

func TestLoadRuntimeReadinessModeQuery(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_RUNTIME_READINESS_MODE", "query")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RuntimeReadinessMode != RuntimeReadinessModeQuery {
		t.Fatalf("RuntimeReadinessMode=%q", cfg.RuntimeReadinessMode)
	}
}

func TestLoadRuntimeWorkerStartCommand(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_RUNTIME_WORKER_START_COMMAND", "systemctl start knowledge-runtime-worker")
	t.Setenv("KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RuntimeWorkerStartCommand != "systemctl start knowledge-runtime-worker" {
		t.Fatalf("RuntimeWorkerStartCommand=%q", cfg.RuntimeWorkerStartCommand)
	}
	if cfg.RuntimeWorkerStartTimeout != 45*time.Second {
		t.Fatalf("RuntimeWorkerStartTimeout=%s", cfg.RuntimeWorkerStartTimeout)
	}
}

func TestLoadRejectsInvalidRuntimeWorkerStartTimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT", "0s")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "KNOWLEDGE_RUNTIME_WORKER_START_TIMEOUT") {
		t.Fatalf("Load() error = %v, want worker start timeout requirement", err)
	}
}

func TestLoadRejectsUnknownRuntimeReadinessMode(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_RUNTIME_READINESS_MODE", "workerless")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "KNOWLEDGE_RUNTIME_READINESS_MODE") {
		t.Fatalf("Load() error = %v, want runtime readiness mode requirement", err)
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

func TestLoadMCPCallerIDOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("KNOWLEDGE_MCP_CALLER_ID", "knowledge_mcp_test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MCPCallerID != "knowledge_mcp_test" {
		t.Fatalf("MCPCallerID=%q", cfg.MCPCallerID)
	}
}

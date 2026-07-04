package config_test

import (
	"strings"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/config"
)

func TestLoadAcceptsLocalStorageBackend(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_STORAGE_BACKEND", "local")
	t.Setenv("FILE_LOCAL_STORAGE_DIR", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StorageBackend != "local" || cfg.LocalStorageDir == "" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.Env != "local" {
		t.Fatalf("Env = %q", cfg.Env)
	}
}

func TestLoadAcceptsMinIOStorageBackend(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_STORAGE_BACKEND", "minio")
	t.Setenv("FILE_MINIO_ENDPOINT", "minio:9000")
	t.Setenv("FILE_MINIO_ACCESS_KEY", "file-access")
	t.Setenv("FILE_MINIO_SECRET_KEY", "file-secret")
	t.Setenv("FILE_MINIO_BUCKET", "file-objects")
	t.Setenv("FILE_MINIO_USE_SSL", "true")
	t.Setenv("FILE_MINIO_REGION", "us-east-1")
	t.Setenv("FILE_MINIO_TIMEOUT", "3s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.StorageBackend != "minio" {
		t.Fatalf("StorageBackend = %q", cfg.StorageBackend)
	}
	if cfg.MinIOEndpoint != "minio:9000" || cfg.MinIOBucket != "file-objects" || !cfg.MinIOUseSSL || cfg.MinIORegion != "us-east-1" {
		t.Fatalf("minio config endpoint=%q bucket=%q useSSL=%t region=%q", cfg.MinIOEndpoint, cfg.MinIOBucket, cfg.MinIOUseSSL, cfg.MinIORegion)
	}
	if cfg.MinIOTimeout.String() != "3s" {
		t.Fatalf("MinIOTimeout = %s", cfg.MinIOTimeout)
	}
}

func TestLoadRejectsMinIOMissingRequiredConfig(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_STORAGE_BACKEND", "minio")
	t.Setenv("FILE_MINIO_ENDPOINT", "minio:9000")
	t.Setenv("FILE_MINIO_ACCESS_KEY", "file-access")
	t.Setenv("FILE_MINIO_SECRET_KEY", "file-secret")

	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if got := err.Error(); got == "" || strings.Contains(got, "file-secret") {
		t.Fatalf("Load() error = %q", got)
	}
}

func TestLoadRejectsUnsupportedStorageBackend(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_STORAGE_BACKEND", "unsupported")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadRequiresServiceTokenWhenDatabaseURLSet(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_DATABASE_URL", "postgres://file:file@localhost:5432/file?sslmode=disable")

	if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "FILE_INTERNAL_SERVICE_TOKEN") {
		t.Fatalf("Load() error = %v, want service token requirement", err)
	}
}

func TestLoadAcceptsFileServiceTokenWithDatabaseURL(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_DATABASE_URL", "postgres://file:file@localhost:5432/file?sslmode=disable")
	t.Setenv("FILE_INTERNAL_SERVICE_TOKEN", " file-token ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL == "" || cfg.InternalServiceToken != "file-token" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadAcceptsSharedServiceTokenFallback(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_DATABASE_URL", "postgres://file:file@localhost:5432/file?sslmode=disable")
	t.Setenv("INTERNAL_SERVICE_TOKEN", " shared-token ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InternalServiceToken != "shared-token" {
		t.Fatalf("InternalServiceToken = %q", cfg.InternalServiceToken)
	}
}

func TestLoadParsesFilePolicies(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_ALLOWED_CONTENT_TYPES", " application/pdf, text/plain ,,")
	t.Setenv("FILE_ALLOWED_CREATE_CALLERS", "document, qa")
	t.Setenv("FILE_ALLOWED_READ_CALLERS", " document ")
	t.Setenv("FILE_ALLOWED_DELETE_CALLERS", "qa")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertSliceEqual(t, cfg.AllowedContentTypes, []string{"application/pdf", "text/plain"})
	assertSliceEqual(t, cfg.AllowedCreateCallers, []string{"document", "qa"})
	assertSliceEqual(t, cfg.AllowedReadCallers, []string{"document"})
	assertSliceEqual(t, cfg.AllowedDeleteCallers, []string{"qa"})
}

func TestLoadUsesEnvFallbackForRuntimeEnv(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("ENV", "test")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "test" {
		t.Fatalf("Env = %q", cfg.Env)
	}
}

func TestLoadRejectsMemoryStorageInNonLocalEnv(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_ENV", "production")
	t.Setenv("FILE_STORAGE_BACKEND", "memory")
	t.Setenv("FILE_DATABASE_URL", "postgres://file:file@localhost:5432/file?sslmode=disable")
	t.Setenv("FILE_INTERNAL_SERVICE_TOKEN", "file-token")

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "FILE_STORAGE_BACKEND=memory") {
		t.Fatalf("Load() error = %v, want memory backend guard", err)
	}
}

func TestLoadRejectsMemoryMetadataInNonLocalEnv(t *testing.T) {
	clearFileEnv(t)
	t.Setenv("FILE_ENV", "production")
	t.Setenv("FILE_STORAGE_BACKEND", "local")
	t.Setenv("FILE_LOCAL_STORAGE_DIR", t.TempDir())

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "FILE_DATABASE_URL") {
		t.Fatalf("Load() error = %v, want database guard", err)
	}
}

func clearFileEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"FILE_HTTP_ADDR",
		"FILE_ENV",
		"ENV",
		"FILE_MAX_UPLOAD_BYTES",
		"FILE_STORAGE_BACKEND",
		"FILE_LOCAL_STORAGE_DIR",
		"FILE_MINIO_ENDPOINT",
		"FILE_MINIO_ACCESS_KEY",
		"FILE_MINIO_SECRET_KEY",
		"FILE_MINIO_BUCKET",
		"FILE_MINIO_USE_SSL",
		"FILE_MINIO_REGION",
		"FILE_MINIO_TIMEOUT",
		"FILE_DATABASE_URL",
		"FILE_INTERNAL_SERVICE_TOKEN",
		"INTERNAL_SERVICE_TOKEN",
		"FILE_ALLOWED_CONTENT_TYPES",
		"FILE_ALLOWED_CREATE_CALLERS",
		"FILE_ALLOWED_READ_CALLERS",
		"FILE_ALLOWED_DELETE_CALLERS",
		"FILE_SHUTDOWN_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func assertSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length = %d, want %d: got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] = %q, want %q: got=%v", i, got[i], want[i], got)
		}
	}
}

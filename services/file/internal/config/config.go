package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEnv             = "local"
	DefaultHTTPAddr        = ":8082"
	DefaultMaxUploadBytes  = int64(32 << 20)
	DefaultStorageBackend  = "memory"
	DefaultLocalStorageDir = ".file-storage"
	DefaultMinIOTimeout    = 10 * time.Second
	DefaultShutdownTimeout = 10 * time.Second
)

type Config struct {
	Env                  string
	HTTPAddr             string
	MaxUploadBytes       int64
	StorageBackend       string
	LocalStorageDir      string
	MinIOEndpoint        string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MinIOBucket          string
	MinIOUseSSL          bool
	MinIORegion          string
	MinIOTimeout         time.Duration
	DatabaseURL          string
	InternalServiceToken string
	AllowedContentTypes  []string
	AllowedCreateCallers []string
	AllowedReadCallers   []string
	AllowedDeleteCallers []string
	ShutdownTimeout      time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Env:             firstValue(os.Getenv("FILE_ENV"), os.Getenv("ENV"), DefaultEnv),
		HTTPAddr:        stringValue("FILE_HTTP_ADDR", DefaultHTTPAddr),
		StorageBackend:  strings.ToLower(strings.TrimSpace(stringValue("FILE_STORAGE_BACKEND", DefaultStorageBackend))),
		LocalStorageDir: stringValue("FILE_LOCAL_STORAGE_DIR", DefaultLocalStorageDir),
		MinIOEndpoint:   stringValue("FILE_MINIO_ENDPOINT", ""),
		MinIOAccessKey:  stringValue("FILE_MINIO_ACCESS_KEY", ""),
		MinIOSecretKey:  stringValue("FILE_MINIO_SECRET_KEY", ""),
		MinIOBucket:     stringValue("FILE_MINIO_BUCKET", ""),
		MinIORegion:     stringValue("FILE_MINIO_REGION", ""),
		DatabaseURL:     strings.TrimSpace(os.Getenv("FILE_DATABASE_URL")),
		InternalServiceToken: firstValue(
			os.Getenv("FILE_INTERNAL_SERVICE_TOKEN"),
			os.Getenv("INTERNAL_SERVICE_TOKEN"),
		),
		AllowedContentTypes:  splitCSV(os.Getenv("FILE_ALLOWED_CONTENT_TYPES")),
		AllowedCreateCallers: splitCSV(os.Getenv("FILE_ALLOWED_CREATE_CALLERS")),
		AllowedReadCallers:   splitCSV(os.Getenv("FILE_ALLOWED_READ_CALLERS")),
		AllowedDeleteCallers: splitCSV(os.Getenv("FILE_ALLOWED_DELETE_CALLERS")),
		MaxUploadBytes:       DefaultMaxUploadBytes,
		MinIOTimeout:         DefaultMinIOTimeout,
		ShutdownTimeout:      DefaultShutdownTimeout,
	}

	if raw := os.Getenv("FILE_MAX_UPLOAD_BYTES"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("FILE_MAX_UPLOAD_BYTES must be a positive integer")
		}
		cfg.MaxUploadBytes = value
	}

	if raw := os.Getenv("FILE_SHUTDOWN_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("FILE_SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = value
	}

	if raw := os.Getenv("FILE_MINIO_USE_SSL"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("FILE_MINIO_USE_SSL must be a boolean")
		}
		cfg.MinIOUseSSL = value
	}

	if raw := os.Getenv("FILE_MINIO_TIMEOUT"); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("FILE_MINIO_TIMEOUT must be a positive duration")
		}
		cfg.MinIOTimeout = value
	}

	switch cfg.StorageBackend {
	case "memory":
	case "local":
		if cfg.LocalStorageDir == "" {
			return Config{}, fmt.Errorf("FILE_LOCAL_STORAGE_DIR must not be empty when FILE_STORAGE_BACKEND=local")
		}
	case "minio":
		missing := []string{}
		if cfg.MinIOEndpoint == "" {
			missing = append(missing, "FILE_MINIO_ENDPOINT")
		}
		if cfg.MinIOAccessKey == "" {
			missing = append(missing, "FILE_MINIO_ACCESS_KEY")
		}
		if cfg.MinIOSecretKey == "" {
			missing = append(missing, "FILE_MINIO_SECRET_KEY")
		}
		if cfg.MinIOBucket == "" {
			missing = append(missing, "FILE_MINIO_BUCKET")
		}
		if len(missing) > 0 {
			return Config{}, fmt.Errorf("%s must be set when FILE_STORAGE_BACKEND=minio", joinNames(missing))
		}
	default:
		return Config{}, fmt.Errorf("FILE_STORAGE_BACKEND=%q is not implemented; supported values: memory, local, minio", cfg.StorageBackend)
	}

	if !isLocalLikeEnv(cfg.Env) {
		if cfg.StorageBackend == "memory" {
			return Config{}, fmt.Errorf("FILE_STORAGE_BACKEND=memory is not allowed when FILE_ENV=%s", cfg.Env)
		}
		if cfg.DatabaseURL == "" {
			return Config{}, fmt.Errorf("FILE_DATABASE_URL must be set when FILE_ENV=%s", cfg.Env)
		}
	}

	if cfg.DatabaseURL != "" && strings.TrimSpace(cfg.InternalServiceToken) == "" {
		return Config{}, fmt.Errorf("FILE_INTERNAL_SERVICE_TOKEN or INTERNAL_SERVICE_TOKEN is required when FILE_DATABASE_URL is set")
	}

	return cfg, nil
}

func stringValue(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func isLocalLikeEnv(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "", "local", "test", "development":
		return true
	default:
		return false
	}
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	result := names[0]
	for _, name := range names[1:] {
		result += ", " + name
	}
	return result
}

func firstValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

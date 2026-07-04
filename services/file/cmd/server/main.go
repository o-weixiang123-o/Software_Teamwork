package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/config"
	filehttp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/platform/storage"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/service"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const databasePingTimeout = 5 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration failed", "service", "file", "error", err)
		os.Exit(1)
	}

	repo, metadataBackend, readinessChecker, db, err := newRepository(context.Background(), cfg)
	if err != nil {
		logger.Error("metadata repository initialization failed", "service", "file", "metadata_backend", metadataBackend, "error", err)
		os.Exit(1)
	}
	if db != nil {
		defer db.Close()
	}

	objectStore, err := newObjectStore(cfg)
	if err != nil {
		logger.Error("storage initialization failed", "service", "file", "error", err)
		os.Exit(1)
	}
	documentService := service.New(repo, objectStore,
		service.WithStorageBackend(cfg.StorageBackend),
		service.WithAllowedContentTypes(cfg.AllowedContentTypes),
	)
	handler := filehttp.NewServer(documentService, filehttp.Config{
		MaxUploadBytes:       cfg.MaxUploadBytes,
		Logger:               logger,
		ServiceToken:         cfg.InternalServiceToken,
		MetadataBackend:      metadataBackend,
		StorageBackend:       cfg.StorageBackend,
		AllowedCreateCallers: cfg.AllowedCreateCallers,
		AllowedReadCallers:   cfg.AllowedReadCallers,
		AllowedDeleteCallers: cfg.AllowedDeleteCallers,
		ReadinessChecker:     readinessChecker,
	})

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("file service starting",
			"service", "file",
			"addr", cfg.HTTPAddr,
			"metadata_backend", metadataBackend,
			"storage_backend", cfg.StorageBackend,
			"service_token_configured", cfg.InternalServiceToken != "",
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("file service stopped unexpectedly", "service", "file", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	logger.Info("file service shutdown started", "service", "file")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("file service shutdown failed", "service", "file", "error", err)
		os.Exit(1)
	}
	logger.Info("file service shutdown complete", "service", "file")
}

func newRepository(ctx context.Context, cfg config.Config) (service.FileRepository, string, filehttp.ReadyChecker, *sql.DB, error) {
	if cfg.DatabaseURL == "" {
		return repository.NewMemoryRepository(), "memory", nil, nil, nil
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, "postgres", nil, nil, fmt.Errorf("open postgres metadata repository: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, "postgres", nil, nil, fmt.Errorf("ping postgres metadata repository: %w", err)
	}

	return repository.NewPostgresRepository(db), "postgres", databaseReadyChecker{db: db}, db, nil
}

func newObjectStore(cfg config.Config) (service.ObjectStore, error) {
	switch cfg.StorageBackend {
	case "memory":
		return storage.NewMemoryStore(), nil
	case "local":
		return storage.NewLocalStore(cfg.LocalStorageDir)
	case "minio":
		client, err := storage.NewMinIOClient(storage.MinIOClientConfig{
			Endpoint:  cfg.MinIOEndpoint,
			AccessKey: cfg.MinIOAccessKey,
			SecretKey: cfg.MinIOSecretKey,
			UseSSL:    cfg.MinIOUseSSL,
			Region:    cfg.MinIORegion,
			Timeout:   cfg.MinIOTimeout,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMinIOStore(client, cfg.MinIOBucket)
	default:
		return nil, fmt.Errorf("unsupported storage backend %q", cfg.StorageBackend)
	}
}

type databaseReadyChecker struct {
	db *sql.DB
}

func (c databaseReadyChecker) CheckReady(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

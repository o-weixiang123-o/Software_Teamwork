package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/config"
	gatewayhttp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/metrics"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/platform/authclient"
	redisstore "github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/platform/redis"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/gateway/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration failed", "service", "gateway", "error", err)
		os.Exit(1)
	}

	tokenHasher, err := service.NewTokenHasher(cfg.TokenHashSecret, cfg.TokenHashKeyVersion)
	if err != nil {
		logger.Error("token hash configuration failed", "service", "gateway", "error", err)
		os.Exit(1)
	}

	sessionStore, err := redisstore.New(redisstore.Config{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err != nil {
		logger.Error("redis configuration failed", "service", "gateway", "error", err)
		os.Exit(1)
	}
	defer sessionStore.Close()

	authClient, err := authclient.New(cfg.AuthBaseURL, cfg.InternalServiceToken, cfg.AuthAdminServiceToken, cfg.DownstreamTimeout)
	if err != nil {
		logger.Error("auth client configuration failed", "service", "gateway", "error", err)
		os.Exit(1)
	}

	metricsReg := metrics.New()

	ownerBaseURLs := map[string]string{
		"auth":       cfg.AuthBaseURL,
		"knowledge":  cfg.KnowledgeBaseURL,
		"qa":         cfg.QABaseURL,
		"document":   cfg.DocumentBaseURL,
		"ai-gateway": cfg.AIGatewayBaseURL,
	}
	if err := gatewayhttp.ValidateOwnerBaseURLs(ownerBaseURLs); err != nil {
		logger.Error("owner service base URL configuration failed", "service", "gateway", "error", err)
		os.Exit(1)
	}

	handler := gatewayhttp.NewServer(gatewayhttp.Config{
		Logger:                 logger,
		ServiceVersion:         cfg.ServiceVersion,
		Environment:            cfg.Environment,
		RequestTimeout:         cfg.RequestTimeout,
		MaxBodyBytes:           cfg.MaxBodyBytes,
		MaxInFlight:            cfg.MaxInFlight,
		AuthRefreshMaxInFlight: cfg.AuthRefreshMaxInFlight,
		CORSAllowedOrigins:     cfg.CORSAllowedOrigins,
		CORSAllowedMethods:     cfg.CORSAllowedMethods,
		CORSAllowedHeaders:     cfg.CORSAllowedHeaders,
		CORSAllowCredentials:   cfg.CORSAllowCredentials,
		DownstreamTimeout:      cfg.DownstreamTimeout,
		InternalServiceToken:   cfg.InternalServiceToken,
		AuthAdminServiceToken:  cfg.AuthAdminServiceToken,
		GitHubToken:            cfg.GitHubToken,
		AppVersionCurrentSHA:   cfg.AppVersionCurrentSHA,
		AppVersionAllowedSHAs:  cfg.AppVersionAllowedSHAs,
		AuthClient:             authClient,
		SessionStore:           sessionStore,
		TokenHasher:            tokenHasher,
		OwnerBaseURLs:          ownerBaseURLs,
		ReadyCheck:             gatewayReadyCheck(sessionStore, authClient, ownerBaseURLs),
		MetricsReg:             metricsReg,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsReg.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("gateway metrics server starting", "service", "gateway", "addr", cfg.MetricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("gateway metrics server stopped unexpectedly", "service", "gateway", "error", err)
			stop()
		}
	}()

	go func() {
		logger.Info("gateway service starting",
			"service", "gateway",
			"addr", cfg.HTTPAddr,
			"environment", cfg.Environment,
			"version", cfg.ServiceVersion,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("gateway service stopped unexpectedly", "service", "gateway", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	logger.Info("gateway service shutdown started", "service", "gateway")
	_ = metricsServer.Shutdown(shutdownCtx)
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("gateway service shutdown failed", "service", "gateway", "error", err)
		os.Exit(1)
	}
	logger.Info("gateway service shutdown complete", "service", "gateway")
}

func gatewayReadyCheck(sessionStore *redisstore.SessionStore, authClient *authclient.Client, ownerBaseURLs map[string]string) func(context.Context) error {
	return func(ctx context.Context) error {
		if sessionStore == nil {
			return service.ErrSessionStoreUnavailable
		}
		if err := sessionStore.CheckReady(ctx); err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		if authClient == nil {
			return fmt.Errorf("auth client is not configured")
		}
		if err := authClient.CheckReady(ctx); err != nil {
			return fmt.Errorf("auth service: %w", err)
		}
		if missing := missingOwnerBaseURLs(ownerBaseURLs); len(missing) > 0 {
			return fmt.Errorf("owner service base URLs are not configured: %s", strings.Join(missing, ","))
		}
		return nil
	}
}

func missingOwnerBaseURLs(ownerBaseURLs map[string]string) []string {
	required := []string{"knowledge", "qa", "document", "ai-gateway"}
	missing := make([]string, 0, len(required))
	for _, owner := range required {
		if strings.TrimSpace(ownerBaseURLs[owner]) == "" {
			missing = append(missing, owner)
		}
	}
	return missing
}

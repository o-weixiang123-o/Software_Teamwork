package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapter"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapterconfig"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/aigateway"
	kmcp "github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/mcp"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := adapterconfig.Load()
	if err != nil {
		logger.Error("configuration failed", "service", "knowledge-adapter", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var opts []adapter.Option
	if cfg.DatabaseURL != "" {
		pool, err := connectPostgres(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("postgres connection failed", "service", "knowledge-adapter", "dependency", "postgres", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		opts = append(opts, adapter.WithParserConfigService(service.New(repository.NewPostgresRepository(pool))))
		logger.Info("parser config storage enabled", "service", "knowledge-adapter")
	} else {
		logger.Warn("DATABASE_URL or KNOWLEDGE_DATABASE_URL not set; parser-config routes will return dependency_error", "service", "knowledge-adapter")
	}

	server := adapter.NewServer(cfg, logger, opts...)
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: server.Handler(),
	}

	chatClient, err := aigateway.NewChatClientFromEnv()
	if err != nil {
		logger.Error("ai gateway client configuration failed", "service", "knowledge-adapter", "error", err)
		os.Exit(1)
	}
	if chatClient != nil {
		logger.Info("ai gateway client enabled for answer_from_knowledge", "service", "knowledge-adapter")
	}

	go func() {
		logger.Info("knowledge adapter listening", "addr", cfg.HTTPAddr, "vendor_runtime_url", cfg.VendorRuntimeURL)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("adapter server failed", "error", err)
			os.Exit(1)
		}
	}()

	var mcpServer *http.Server
	if cfg.MCPAddr != "" {
		mcpCaller := kmcp.CallerContext{
			UserID:       cfg.ProjectRuntimeUserID,
			ServiceToken: cfg.ServiceToken,
			Roles:        cfg.MCPRoles,
			Permissions:  cfg.MCPPermissions,
		}
		mcpServer = &http.Server{
			Addr:    cfg.MCPAddr,
			Handler: kmcp.NewStreamableHTTPHandler(server, mcpCaller, chatClient),
		}
		go func() {
			logger.Info("knowledge MCP server listening", "addr", cfg.MCPAddr)
			if err := mcpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("MCP server failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("adapter shutdown failed", "error", err)
		os.Exit(1)
	}
	if mcpServer != nil {
		if err := mcpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("MCP shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}

func connectPostgres(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

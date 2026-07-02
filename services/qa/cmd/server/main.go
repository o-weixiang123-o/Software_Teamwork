package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/app"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/config"
	httpapi "github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/attachmentclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/connectiontest"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/knowledgeclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/secrets"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration failed", "service", "qa", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	repo, err := repository.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database initialization failed", "service", "qa", "dependency", "postgres", "error", err)
		os.Exit(1)
	}
	defer repo.Close()
	cipher, err := secrets.New(cfg.EncryptionKey)
	if err != nil {
		logger.Error("secret encryption initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	bootstrap := service.BootstrapSettings{
		LLM: service.RuntimeLLMConfig{
			Endpoint: cfg.AIGatewayURL, Token: cfg.AIGatewayToken,
			TokenHeader: cfg.AIGatewayTokenHeader, Model: cfg.ModelID,
			ProfileID: cfg.AIGatewayProfileID,
			Timeout:   cfg.ModelTimeout, MaxTokens: cfg.MaxTokens,
			Stream: cfg.AIGatewayStream,
		},
		SystemPrompt: cfg.SystemPrompt,
	}
	if cfg.MCPTransport != config.TransportDisabled {
		bootstrap.MCPServer = &service.RuntimeMCPConfig{
			Alias: cfg.MCPServerAlias, Transport: cfg.MCPTransport,
			Command: cfg.MCPServerCommand, Args: cfg.MCPServerArgs,
			EndpointURL: cfg.MCPServerURL, Token: cfg.MCPServerToken,
			TokenHeader: cfg.MCPServerTokenHeader, ToolTimeout: cfg.MCPToolTimeout,
		}
	}
	if cfg.DocumentMCPEnabled {
		bootstrap.DocumentMCPServer = &service.RuntimeMCPConfig{
			Alias:       "document",
			Transport:   config.TransportStreamableHTTP,
			EndpointURL: cfg.DocumentMCPServerURL,
			Token:       cfg.DocumentMCPServerToken,
			TokenHeader: cfg.DocumentMCPTokenHeader,
			ToolTimeout: cfg.MCPToolTimeout,
		}
	}
	tester := connectiontest.Tester{}
	settingsService, err := service.NewConfigService(repo, cipher, bootstrap, tester, tester)
	if err != nil {
		logger.Error("settings service initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	retriever, err := knowledgeclient.New(cfg.KnowledgeURL, cfg.ServiceToken, cfg.ModelTimeout)
	if err != nil {
		logger.Error("knowledge client initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	fileClient, err := attachmentclient.NewFileHTTPClient(attachmentclient.FileHTTPConfig{
		BaseURL:      cfg.FileServiceURL,
		ServiceToken: cfg.ServiceToken,
		Timeout:      cfg.AttachmentProcessTimeout,
		MaxReadBytes: cfg.AttachmentMaxBytes,
	})
	if err != nil {
		logger.Error("file client initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	parserClient, err := attachmentclient.NewParserHTTPClient(attachmentclient.ParserHTTPConfig{
		BaseURL:      cfg.ParserServiceBaseURL,
		ServiceToken: cfg.ParserServiceToken,
		Timeout:      cfg.ParserServiceTimeout,
	})
	if err != nil {
		logger.Error("parser client initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	attachmentService, err := service.NewAttachmentService(repo, fileClient, parserClient, service.AttachmentServiceConfig{TTL: cfg.AttachmentTTL, MaxBytes: cfg.AttachmentMaxBytes, MaxPerSession: cfg.AttachmentMaxPerSession, ProcessTimeout: cfg.AttachmentProcessTimeout})
	if err != nil {
		logger.Error("attachment service initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	manager, err := app.NewManager(ctx, settingsService, repo, retriever, attachmentService, app.ManagerConfig{
		WorkDir: cfg.WorkDir, MaxFileBytes: cfg.MaxFileBytes,
		MaxToolResultBytes: cfg.MaxToolResultBytes, EnableCommandTool: cfg.EnableCommandTool,
		CommandTimeout: cfg.CommandTimeout, MaxIterations: cfg.MaxIterations,
		DefaultToolTimeout: cfg.MCPToolTimeout,
	})
	if err != nil {
		logger.Error("agent runtime initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	settingsService.SetReloader(manager)
	defer func() {
		if err := manager.Close(); err != nil {
			logger.Warn("runtime close failed", "service", "qa", "dependency", "mcp", "error", err)
		}
	}()
	qaService, err := service.NewQAService(repo, manager)
	if err != nil {
		logger.Error("QA service initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	qaService.SetCitationSourceChecker(retriever)
	resourceService, err := service.NewResourceService(repo, retriever, tester, bootstrap.LLM, qaService)
	if err != nil {
		logger.Error("resource service initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	resourceService.SetReloader(manager)
	handler, err := httpapi.NewServer(qaService, settingsService, resourceService, attachmentService, httpapi.Config{
		MaxRequestBytes:    cfg.MaxRequestBytes,
		AttachmentMaxBytes: cfg.AttachmentMaxBytes,
		Logger:             logger, Ready: repo.Ping,
		AdminUserIDs: cfg.AdminUserIDs, SettingsOpen: cfg.SettingsOpen,
		ServiceToken: cfg.ServiceToken,
	})
	if err != nil {
		logger.Error("HTTP service initialization failed", "service", "qa", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		logger.Info("QA service starting", "service", "qa", "addr", cfg.HTTPAddr, "mcp_transport", cfg.MCPTransport)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("QA service stopped unexpectedly", "service", "qa", "error", err)
			stop()
		}
	}()
	go runAttachmentCleanup(ctx, logger, attachmentService)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	logger.Info("QA service shutdown started", "service", "qa")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("QA service shutdown failed", "service", "qa", "error", err)
		os.Exit(1)
	}
	logger.Info("QA service shutdown complete", "service", "qa")
}

func runAttachmentCleanup(ctx context.Context, logger *slog.Logger, attachments *service.AttachmentService) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			expired, err := attachments.CleanupExpired(cleanupCtx, 100)
			cancel()
			if err != nil {
				logger.Warn("attachment cleanup failed", "service", "qa", "error", err)
				continue
			}
			if len(expired) > 0 {
				logger.Info("attachment cleanup completed", "service", "qa", "count", len(expired))
			}
		}
	}
}

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

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/config"
	httpapi "github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/http"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/platform/aigateway"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/platform/fileclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/platform/knowledgeclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/platform/mcpserver"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration failed", "service", "document", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repo, err := repository.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database initialization failed", "service", "document", "dependency", "postgres", "error", err)
		os.Exit(1)
	}
	defer repo.Close()

	files, err := fileclient.NewWithServiceToken(cfg.FileServiceURL, cfg.FileServiceToken, nil)
	if err != nil {
		logger.Error("file client initialization failed", "service", "document", "dependency", "file", "error", err)
		os.Exit(1)
	}
	profiles, err := aigateway.NewProfileClient(cfg.AIGatewayURL, cfg.AIGatewayServiceToken, nil)
	if err != nil {
		logger.Error("ai gateway client initialization failed", "service", "document", "dependency", "ai-gateway", "error", err)
		os.Exit(1)
	}
	chatClient, err := aigateway.NewChatClient(cfg.AIGatewayURL, cfg.AIGatewayServiceToken, cfg.AIGatewayProfileID, cfg.AIGatewayModel, nil)
	if err != nil {
		logger.Error("ai gateway chat client initialization failed", "service", "document", "dependency", "ai-gateway", "error", err)
		os.Exit(1)
	}
	var knowledgeRetriever service.ReportGenerationKnowledgeRetriever
	if cfg.KnowledgeServiceURL != "" {
		knowledgeRetriever, err = knowledgeclient.New(cfg.KnowledgeServiceURL, cfg.KnowledgeServiceToken, nil)
		if err != nil {
			logger.Error("knowledge client initialization failed", "service", "document", "dependency", "knowledge", "error", err)
			os.Exit(1)
		}
	}
	taskClient := worker.NewClient(cfg.RedisAddr)
	documents := service.New(repo, files)
	reportService := service.NewReportService(repo)
	jobService := service.NewJobService(repo, taskClient)
	adminService := service.NewAdminService(repo, profiles)
	reportFileService := service.NewReportFileService(repo, files, taskClient, service.NewSimpleDOCXGenerator())
	reportGenerationService := service.NewReportGenerationService(repo, chatClient, knowledgeRetriever)
	documentMCPTools := service.NewMCPToolService(service.MCPToolServiceConfig{
		DocumentService:       documents,
		JobService:            jobService,
		ReportService:         reportService,
		ReportSettingsService: repo,
		ReportFileSvc:         reportFileService,
		Recorder:              repo,
	})
	w := worker.New(cfg.RedisAddr, logger, repo, reportFileService, reportGenerationService)
	go func() {
		if err := w.Start(); err != nil {
			logger.Error("worker failed to start", "service", "document", "error", err)
		}
	}()
	defer w.Stop()

	handler := httpapi.NewServer(httpapi.Config{
		Logger:          logger,
		ReadyChecker:    repo,
		DocumentService: documents,
		ReportService:   reportService,
		JobSvc:          jobService,
		AdminService:    adminService,
		ReportFileSvc:   reportFileService,
		MCPHandler: mcpserver.NewHandler(mcpserver.Config{
			ToolService:  documentMCPTools,
			ServiceToken: cfg.MCPServiceToken,
			TokenHeader:  cfg.MCPTokenHeader,
			Logger:       logger,
		}),
		MCPPath: cfg.MCPPath,
	})
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info("document service starting", "service", "document", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("document service stopped unexpectedly", "service", "document", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	logger.Info("document service shutdown started", "service", "document")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("document service shutdown failed", "service", "document", "error", err)
		os.Exit(1)
	}
	logger.Info("document service shutdown complete", "service", "document")
}

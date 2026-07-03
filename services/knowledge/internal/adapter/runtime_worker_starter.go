package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

type runtimeWorkerStarter interface {
	Start(context.Context) error
}

type commandRuntimeWorkerStarter struct {
	command string
	timeout time.Duration
	logger  *slog.Logger
}

func newCommandRuntimeWorkerStarter(command string, timeout time.Duration, logger *slog.Logger) *commandRuntimeWorkerStarter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &commandRuntimeWorkerStarter{command: strings.TrimSpace(command), timeout: timeout, logger: logger}
}

func (s *commandRuntimeWorkerStarter) Start(ctx context.Context) error {
	if s.command == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", s.command)
	output, err := cmd.CombinedOutput()
	if err == nil {
		if trimmed := truncateWorkerStartOutput(output); trimmed != "" {
			s.logger.InfoContext(ctx, "knowledge runtime worker start command completed", "output", trimmed)
		}
		return nil
	}
	trimmed := truncateWorkerStartOutput(output)
	if ctx.Err() == context.DeadlineExceeded {
		s.logger.ErrorContext(ctx, "knowledge runtime worker start command timed out", "timeout", s.timeout.String(), "output", trimmed)
		return fmt.Errorf("worker start command timed out after %s", s.timeout)
	}
	s.logger.ErrorContext(ctx, "knowledge runtime worker start command failed", "error", err, "output", trimmed)
	return fmt.Errorf("worker start command failed: %w", err)
}

func (s *Server) startRuntimeWorkerForIngestionAsync(requestID, docID string) {
	if s.runtimeWorker == nil {
		return
	}
	go func() {
		ctx := contextWithRequestID(context.Background(), requestID)
		startCtx, cancel := context.WithTimeout(ctx, s.runtimeWorkerStartTimeout())
		defer cancel()
		if err := s.startRuntimeWorkerForIngestion(startCtx); err != nil {
			s.logger.WarnContext(startCtx, "knowledge runtime worker start failed after parse enqueue",
				"service", "knowledge-adapter",
				"request_id", requestID,
				"document_id", docID,
				"error", err,
			)
		}
	}()
}

func (s *Server) startRuntimeWorkerForIngestion(ctx context.Context) error {
	if s.runtimeWorker == nil {
		return nil
	}
	if s.runtimeWorkerReady(ctx) {
		return nil
	}

	s.runtimeWorkerMu.Lock()
	defer s.runtimeWorkerMu.Unlock()
	if s.runtimeWorkerReady(ctx) {
		return nil
	}

	if err := s.runtimeWorker.Start(ctx); err != nil {
		return service.DependencyError("knowledge runtime worker could not be started", err)
	}
	return nil
}

func (s *Server) runtimeWorkerReady(ctx context.Context) bool {
	status, err := s.vendor.RuntimeStatus(ctx, s.runtimeScopeID())
	if err != nil {
		s.logger.WarnContext(ctx, "knowledge runtime status unavailable before worker start",
			"service", "knowledge-adapter",
			"request_id", requestIDFromContext(ctx),
			"error", err,
		)
		return false
	}
	_, ready := runtimeTaskExecutorReady(status["task_executor_heartbeats"])
	return ready
}

func (s *Server) runtimeWorkerStartTimeout() time.Duration {
	if s.cfg.RuntimeWorkerStartTimeout > 0 {
		return s.cfg.RuntimeWorkerStartTimeout
	}
	return 30 * time.Second
}

func truncateWorkerStartOutput(output []byte) string {
	trimmed := strings.TrimSpace(string(output))
	if len(trimmed) <= 2000 {
		return trimmed
	}
	return trimmed[:2000] + "...[truncated]"
}

package adapter

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapterconfig"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/vendorclient"
)

const runtimeTaskExecutorHeartbeatFreshness = 2 * time.Minute

type Server struct {
	cfg             adapterconfig.Config
	logger          *slog.Logger
	vendor          *vendorclient.Client
	parserConfigs   *service.Service
	knowledgeBases  service.RuntimeKnowledgeBaseCatalog
	runtimeDocs     service.RuntimeDocumentCatalog
	runtimeWorker   runtimeWorkerStarter
	runtimeWorkerMu sync.Mutex
	maxUploadBytes  int64
	mux             *http.ServeMux
}

type Option func(*Server)

func WithParserConfigService(svc *service.Service) Option {
	return func(s *Server) {
		s.parserConfigs = svc
	}
}

func WithRuntimeKnowledgeBaseCatalog(catalog service.RuntimeKnowledgeBaseCatalog) Option {
	return func(s *Server) {
		s.knowledgeBases = catalog
		if docs, ok := catalog.(service.RuntimeDocumentCatalog); ok {
			s.runtimeDocs = docs
		}
	}
}

func WithRuntimeDocumentCatalog(catalog service.RuntimeDocumentCatalog) Option {
	return func(s *Server) {
		s.runtimeDocs = catalog
	}
}

func WithRuntimeWorkerStarter(starter runtimeWorkerStarter) Option {
	return func(s *Server) {
		s.runtimeWorker = starter
	}
}

func NewServer(cfg adapterconfig.Config, logger *slog.Logger, opts ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.RuntimeReadinessMode == "" {
		cfg.RuntimeReadinessMode = adapterconfig.RuntimeReadinessModeIngestion
	}
	if cfg.RuntimeWorkerStartTimeout <= 0 {
		cfg.RuntimeWorkerStartTimeout = adapterconfig.DefaultWorkerStartTimeout
	}
	s := &Server{
		cfg:            cfg,
		logger:         logger,
		vendor:         vendorclient.New(cfg.VendorRuntimeURL, 60*time.Second, cfg.VendorRuntimeToken),
		maxUploadBytes: defaultMaxUploadBytes,
		mux:            http.NewServeMux(),
	}
	if strings.TrimSpace(cfg.RuntimeWorkerStartCommand) != "" {
		s.runtimeWorker = newCommandRuntimeWorkerStarter(cfg.RuntimeWorkerStartCommand, cfg.RuntimeWorkerStartTimeout, logger)
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = newRequestID()
	}

	ctx := contextWithRequestID(r.Context(), requestID)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-Id", requestID)

	if !s.requireServiceToken(w, r) {
		return
	}

	recorder := &statusRecorder{ResponseWriter: w}
	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.ErrorContext(ctx, "http panic recovered", "service", "knowledge-adapter", "request_id", requestID)
			writeAppError(recorder, r, service.NewError(service.CodeInternal, "internal server error", nil))
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= http.StatusInternalServerError {
			s.logRequestFailure(ctx, requestID, r.Method, r.URL.Path, status, time.Since(start).Milliseconds())
		}
	}()

	s.mux.ServeHTTP(recorder, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /internal/v1/runtime/status", s.handleRuntimeStatus)

	s.mux.HandleFunc("GET /internal/v1/knowledge-bases", s.handleListKnowledgeBases)
	s.mux.HandleFunc("POST /internal/v1/knowledge-bases", s.handleCreateKnowledgeBase)
	s.mux.HandleFunc("GET /internal/v1/knowledge-bases/{knowledgeBaseId}", s.handleGetKnowledgeBase)
	s.mux.HandleFunc("PATCH /internal/v1/knowledge-bases/{knowledgeBaseId}", s.handleUpdateKnowledgeBase)
	s.mux.HandleFunc("DELETE /internal/v1/knowledge-bases/{knowledgeBaseId}", s.handleDeleteKnowledgeBase)
	s.mux.HandleFunc("GET /internal/v1/knowledge-bases/{knowledgeBaseId}/documents", s.handleListDocuments)
	s.mux.HandleFunc("POST /internal/v1/knowledge-bases/{knowledgeBaseId}/documents", s.handleUploadDocument)
	s.mux.HandleFunc("GET /internal/v1/documents/{documentId}", s.handleGetDocument)
	s.mux.HandleFunc("PATCH /internal/v1/documents/{documentId}", s.handleUpdateDocument)
	s.mux.HandleFunc("DELETE /internal/v1/documents/{documentId}", s.handleDeleteDocument)
	s.mux.HandleFunc("GET /internal/v1/documents/{documentId}/chunks", s.handleListDocumentChunks)
	s.mux.HandleFunc("GET /internal/v1/documents/{documentId}/content", s.handleGetDocumentContent)
	s.mux.HandleFunc("GET /internal/v1/chunks/{chunkId}", s.handleGetChunk)
	s.mux.HandleFunc("POST /internal/v1/knowledge-queries", s.handleCreateKnowledgeQuery)
	s.mux.HandleFunc("GET /internal/v1/knowledge-statistics", s.handleKnowledgeStatistics)

	s.mux.HandleFunc("GET /internal/v1/parser-configs", s.handleListParserConfigs)
	s.mux.HandleFunc("POST /internal/v1/parser-configs", s.handleCreateParserConfig)
	s.mux.HandleFunc("GET /internal/v1/parser-configs/{parserConfigId}", s.handleGetParserConfig)
	s.mux.HandleFunc("PATCH /internal/v1/parser-configs/{parserConfigId}", s.handleUpdateParserConfig)
	s.mux.HandleFunc("DELETE /internal/v1/parser-configs/{parserConfigId}", s.handleDeleteParserConfig)

	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "knowledge-adapter",
		"version": s.cfg.ServiceVersion,
	}, requestIDFromContext(r.Context()))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vendorOK, vendorDetail := s.checkVendorRuntime(ctx, false)
	statusCode := http.StatusOK
	ready := "ok"
	if !vendorOK {
		statusCode = http.StatusServiceUnavailable
		ready = "degraded"
	}
	writeJSON(w, statusCode, map[string]any{
		"status":                 ready,
		"service":                "knowledge-adapter",
		"runtime_readiness_mode": string(s.cfg.RuntimeReadinessMode),
		"vendor_runtime":         vendorDetail,
		"vendor_runtime_ok":      vendorOK,
	}, requestIDFromContext(ctx))
}

func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vendorOK, vendorDetail := s.checkVendorRuntime(ctx, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":                   "adapter",
		"environment":            s.cfg.Environment,
		"runtime_readiness_mode": string(s.cfg.RuntimeReadinessMode),
		"vendor_runtime_url":     s.cfg.VendorRuntimeURL,
		"vendor_runtime":         vendorDetail,
		"vendor_runtime_ok":      vendorOK,
		"contract_routes":        "implemented",
	}, requestIDFromContext(ctx))
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeAppError(w, r, service.NotFoundError("route not found"))
}

func (s *Server) checkVendorRuntime(ctx context.Context, includeInternal bool) (bool, map[string]any) {
	pingURL := s.cfg.VendorRuntimeURL + "/api/v1/system/ping"
	if err := s.vendor.Ping(ctx); err != nil {
		s.logger.WarnContext(ctx, "knowledge runtime ping failed",
			"service", "knowledge-adapter",
			"request_id", requestIDFromContext(ctx),
			"error", err,
		)
		detail := map[string]any{
			"status_code":          http.StatusServiceUnavailable,
			"dependency":           "ping",
			"error":                "vendor runtime unavailable",
			"ingestion_diagnostic": "knowledge runtime API is unavailable",
		}
		if includeInternal {
			detail["url"] = pingURL
			detail["internal_error"] = err.Error()
		}
		return false, detail
	}
	statusURL := s.cfg.VendorRuntimeURL + "/api/v1/system/status"
	status, err := s.vendor.RuntimeStatus(ctx, s.runtimeScopeID())
	if err != nil {
		s.logger.WarnContext(ctx, "knowledge runtime status check failed",
			"service", "knowledge-adapter",
			"request_id", requestIDFromContext(ctx),
			"error", err,
		)
		detail := map[string]any{
			"status_code":          http.StatusServiceUnavailable,
			"dependency":           "status",
			"error":                "vendor runtime status unavailable",
			"ingestion_diagnostic": "knowledge runtime status check failed",
		}
		if includeInternal {
			detail["ping"] = map[string]any{"url": pingURL, "status_code": http.StatusOK, "body": "pong"}
			detail["status_url"] = statusURL
			detail["internal_error"] = err.Error()
		} else {
			detail["ping"] = map[string]any{"status_code": http.StatusOK, "body": "pong"}
		}
		return false, detail
	}
	statusOK, statusDetail := summarizeRuntimeStatus(status, s.cfg.RuntimeReadinessMode)
	if includeInternal {
		statusDetail["ping"] = map[string]any{"url": pingURL, "status_code": http.StatusOK, "body": "pong"}
		statusDetail["status_url"] = statusURL
	} else {
		statusDetail["ping"] = map[string]any{"status_code": http.StatusOK, "body": "pong"}
	}
	return statusOK, statusDetail
}

func (s *Server) runtimeScopeID() string {
	return ""
}

func summarizeRuntimeStatus(status map[string]interface{}, readinessMode adapterconfig.RuntimeReadinessMode) (bool, map[string]any) {
	dependencies := map[string]string{}
	ok := true
	for _, name := range []string{"doc_engine", "storage", "database", "redis"} {
		value, exists := status[name]
		if !exists {
			continue
		}
		state := runtimeComponentStatus(value)
		if state == "" {
			continue
		}
		dependencies[name] = state
		switch strings.ToLower(state) {
		case "red", "timeout", "nok", "down":
			ok = false
		}
	}

	workerCount, taskExecutorReady := runtimeTaskExecutorReady(status["task_executor_heartbeats"])
	if readinessMode != adapterconfig.RuntimeReadinessModeQuery && !taskExecutorReady {
		ok = false
	}
	return ok, map[string]any{
		"status_code":          http.StatusOK,
		"dependencies":         dependencies,
		"readiness_mode":       string(readinessMode),
		"task_executor_count":  workerCount,
		"task_executor_ready":  taskExecutorReady,
		"ingestion_diagnostic": ingestionDiagnostic(taskExecutorReady, dependencies),
	}
}

func runtimeComponentStatus(value any) string {
	component, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	return strings.TrimSpace(toString(component["status"]))
}

func runtimeTaskExecutorReady(value any) (int, bool) {
	heartbeats, ok := value.(map[string]interface{})
	if !ok || len(heartbeats) == 0 {
		return 0, false
	}
	count := 0
	now := time.Now()
	for _, raw := range heartbeats {
		entries, ok := raw.([]interface{})
		if !ok || len(entries) == 0 {
			continue
		}
		if runtimeTaskExecutorHeartbeatFresh(entries[len(entries)-1], now) {
			count++
		}
	}
	return count, count > 0
}

func runtimeTaskExecutorHeartbeatFresh(value any, now time.Time) bool {
	entry, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	if raw, ok := entry["now"].(string); ok && strings.TrimSpace(raw) != "" {
		ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
		if err == nil {
			return now.Sub(ts) <= runtimeTaskExecutorHeartbeatFreshness
		}
	}
	switch raw := entry["ts"].(type) {
	case float64:
		return now.Sub(time.Unix(int64(raw), 0)) <= runtimeTaskExecutorHeartbeatFreshness
	case int64:
		return now.Sub(time.Unix(raw, 0)) <= runtimeTaskExecutorHeartbeatFreshness
	case int:
		return now.Sub(time.Unix(int64(raw), 0)) <= runtimeTaskExecutorHeartbeatFreshness
	}
	return false
}

func ingestionDiagnostic(taskExecutorReady bool, dependencies map[string]string) string {
	for name, state := range dependencies {
		switch strings.ToLower(state) {
		case "red", "timeout", "nok", "down":
			return "knowledge runtime dependency " + name + " is " + state
		}
	}
	if !taskExecutorReady {
		return "knowledge runtime task executor heartbeat is missing; start services/knowledge-runtime/deploy/worker/run-local.sh"
	}
	return "ok"
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

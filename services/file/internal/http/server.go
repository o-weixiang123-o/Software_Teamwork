package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/file/internal/service"
)

const defaultMaxUploadBytes = int64(32 << 20)
const defaultReadinessTimeout = 2 * time.Second

type fileOperation string

const (
	fileOperationCreate fileOperation = "create"
	fileOperationRead   fileOperation = "read"
	fileOperationDelete fileOperation = "delete"
)

type ReadyChecker interface {
	CheckReady(ctx context.Context) error
}

type Config struct {
	MaxUploadBytes       int64
	Logger               *slog.Logger
	ServiceToken         string
	MetadataBackend      string
	StorageBackend       string
	AllowedCreateCallers []string
	AllowedReadCallers   []string
	AllowedDeleteCallers []string
	ReadinessChecker     ReadyChecker
	ReadinessTimeout     time.Duration
}

type Server struct {
	files                *service.Service
	maxUploadBytes       int64
	logger               *slog.Logger
	serviceToken         string
	metadataBackend      string
	storageBackend       string
	allowedCreateCallers map[string]struct{}
	allowedReadCallers   map[string]struct{}
	allowedDeleteCallers map[string]struct{}
	readinessChecker     ReadyChecker
	readinessTimeout     time.Duration
	mux                  *http.ServeMux
}

func NewServer(files *service.Service, cfg Config) *Server {
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = defaultMaxUploadBytes
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if strings.TrimSpace(cfg.MetadataBackend) == "" {
		cfg.MetadataBackend = "memory"
	}
	if strings.TrimSpace(cfg.StorageBackend) == "" {
		cfg.StorageBackend = "memory"
	}
	if cfg.ReadinessTimeout <= 0 {
		cfg.ReadinessTimeout = defaultReadinessTimeout
	}
	s := &Server{
		files:                files,
		maxUploadBytes:       cfg.MaxUploadBytes,
		logger:               cfg.Logger,
		serviceToken:         strings.TrimSpace(cfg.ServiceToken),
		metadataBackend:      strings.TrimSpace(cfg.MetadataBackend),
		storageBackend:       strings.TrimSpace(cfg.StorageBackend),
		allowedCreateCallers: callerSet(cfg.AllowedCreateCallers),
		allowedReadCallers:   callerSet(cfg.AllowedReadCallers),
		allowedDeleteCallers: callerSet(cfg.AllowedDeleteCallers),
		readinessChecker:     cfg.ReadinessChecker,
		readinessTimeout:     cfg.ReadinessTimeout,
		mux:                  http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("POST /internal/v1/files", s.handleCreateFile)
	s.mux.HandleFunc("GET /internal/v1/files/{fileId}", s.handleGetFile)
	s.mux.HandleFunc("DELETE /internal/v1/files/{fileId}", s.handleDeleteFile)
	s.mux.HandleFunc("GET /internal/v1/files/{fileId}/content", s.handleGetFileContent)
	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = newRequestID()
	}

	ctx := contextWithRequestID(r.Context(), requestID)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-Id", requestID)

	if s.requiresServiceToken(r) && !secureTokenEqual(r.Header.Get("X-Service-Token"), s.serviceToken) {
		writeAppError(w, r, service.NewError(service.CodeUnauthorized, "service authentication required", nil))
		return
	}

	recorder := &statusRecorder{ResponseWriter: w}
	start := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.ErrorContext(ctx, "http panic recovered", "service", "file", "request_id", requestID, "operation", "http_request")
			writeAppError(recorder, r, service.NewError(service.CodeInternal, "internal server error", nil))
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= http.StatusInternalServerError {
			s.logger.ErrorContext(ctx, "http request failed", "service", "file", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(start).Milliseconds())
		}
	}()

	s.mux.ServeHTTP(recorder, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"service": "file", "status": "ok"}, requestIDFromContext(r.Context()))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	ready := readinessResponse{
		Service:                "file",
		Status:                 "ready",
		MetadataBackend:        s.metadataBackend,
		StorageBackend:         s.storageBackend,
		ServiceTokenConfigured: s.serviceToken != "",
		Dependencies: []dependencyStatus{
			{Name: "postgres", Status: "not_configured"},
		},
	}

	if s.metadataBackend == "postgres" {
		ready.Dependencies[0].Status = "ready"
		if s.readinessChecker == nil {
			status = http.StatusServiceUnavailable
			ready.Status = "not_ready"
			ready.Dependencies[0].Status = "unavailable"
			ready.Dependencies[0].Message = "postgres readiness check is not configured"
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), s.readinessTimeout)
			defer cancel()
			if err := s.readinessChecker.CheckReady(ctx); err != nil {
				status = http.StatusServiceUnavailable
				ready.Status = "not_ready"
				ready.Dependencies[0].Status = "unavailable"
				ready.Dependencies[0].Message = "postgres is unavailable"
			}
		}
	}

	writeJSON(w, status, ready, requestIDFromContext(r.Context()))
}

func (s *Server) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r, fileOperationCreate)
	if !ok {
		return
	}

	file, header, checksum, ok := s.parseMultipartFile(w, r)
	if !ok {
		return
	}
	defer file.Close()

	created, err := s.files.CreateFile(r.Context(), reqCtx, service.CreateFileInput{
		FileName:       header.Filename,
		ContentType:    header.Header.Get("Content-Type"),
		SizeBytes:      header.Size,
		ChecksumSHA256: checksum,
		Content:        file,
	})
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, fileObjectFromDomain(created), requestIDFromContext(r.Context()))
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r, fileOperationRead)
	if !ok {
		return
	}
	file, err := s.files.GetFile(r.Context(), reqCtx, r.PathValue("fileId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, fileObjectFromDomain(file), requestIDFromContext(r.Context()))
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r, fileOperationDelete)
	if !ok {
		return
	}
	if err := s.files.DeleteFile(r.Context(), reqCtx, r.PathValue("fileId")); err != nil {
		writeAppError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetFileContent(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r, fileOperationRead)
	if !ok {
		return
	}
	content, err := s.files.GetFileContent(r.Context(), reqCtx, r.PathValue("fileId"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	defer content.Body.Close()
	writeContent(w, content.ContentType, content.File.Filename, content.SizeBytes, content.Body)
}

func (s *Server) parseMultipartFile(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	if err := r.ParseMultipartForm(s.maxUploadBytes); err != nil {
		fieldMessage := "multipart form is invalid"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			fieldMessage = "exceeds maximum upload size"
		}
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"file": fieldMessage}))
		return nil, nil, "", false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeAppError(w, r, service.ValidationError("request validation failed", map[string]string{"file": "is required"}))
		return nil, nil, "", false
	}
	checksum := ""
	if r.MultipartForm != nil {
		checksum = strings.TrimSpace(firstValue(r.MultipartForm.Value["checksumSha256"]))
	}
	return file, header, checksum, true
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeAppError(w, r, service.NotFoundError("route not found"))
}

func (s *Server) internalContext(w http.ResponseWriter, r *http.Request, operation fileOperation) (service.RequestContext, bool) {
	reqCtx := requestContextFromHeaders(r)
	if strings.TrimSpace(reqCtx.CallerService) == "" && strings.TrimSpace(reqCtx.UserID) == "" {
		writeAppError(w, r, service.UnauthorizedError())
		return service.RequestContext{}, false
	}
	if !s.callerAllowed(operation, reqCtx.CallerService) {
		if strings.TrimSpace(reqCtx.CallerService) == "" {
			writeAppError(w, r, service.UnauthorizedError())
			return service.RequestContext{}, false
		}
		writeAppError(w, r, service.ForbiddenError("caller service is not allowed for file operation"))
		return service.RequestContext{}, false
	}
	return reqCtx, true
}

func (s *Server) callerAllowed(operation fileOperation, caller string) bool {
	var allowed map[string]struct{}
	switch operation {
	case fileOperationCreate:
		allowed = s.allowedCreateCallers
	case fileOperationRead:
		allowed = s.allowedReadCallers
	case fileOperationDelete:
		allowed = s.allowedDeleteCallers
	default:
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[strings.TrimSpace(caller)]
	return ok
}

func (s *Server) requiresServiceToken(r *http.Request) bool {
	if s.serviceToken == "" {
		return false
	}
	path := strings.TrimSpace(r.URL.Path)
	return path == "/internal/v1/files" || strings.HasPrefix(path, "/internal/v1/files/")
}

func requestContextFromHeaders(r *http.Request) service.RequestContext {
	return service.RequestContext{
		RequestID:      requestIDFromContext(r.Context()),
		UserID:         strings.TrimSpace(r.Header.Get("X-User-Id")),
		CallerService:  strings.TrimSpace(r.Header.Get("X-Caller-Service")),
		ServiceToken:   strings.TrimSpace(r.Header.Get("X-Service-Token")),
		Roles:          splitCSV(r.Header.Get("X-User-Roles")),
		Permissions:    splitCSV(r.Header.Get("X-User-Permissions")),
		ForwardedFor:   strings.TrimSpace(r.Header.Get("X-Forwarded-For")),
		ForwardedProto: strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")),
	}
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

func callerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	result := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result[trimmed] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func writeContent(w http.ResponseWriter, contentType string, filename string, sizeBytes int64, body io.Reader) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(filename)}))
	if sizeBytes >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func safeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "file"
	}
	return name
}

func newRequestID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "req_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "req_" + hex.EncodeToString(bytes)
}

func secureTokenEqual(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

type readinessResponse struct {
	Service                string             `json:"service"`
	Status                 string             `json:"status"`
	MetadataBackend        string             `json:"metadataBackend"`
	StorageBackend         string             `json:"storageBackend"`
	ServiceTokenConfigured bool               `json:"serviceTokenConfigured"`
	Dependencies           []dependencyStatus `json:"dependencies"`
}

type dependencyStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

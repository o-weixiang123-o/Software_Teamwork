package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/middleware"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
)

type ModelProfileService interface {
	ListModelProfiles(context.Context, service.ListModelProfilesFilter) ([]service.ModelProfile, error)
	GetModelProfile(context.Context, string) (service.ModelProfile, error)
	CreateModelProfile(context.Context, service.RequestContext, service.CreateModelProfileInput) (service.ModelProfile, error)
	UpdateModelProfile(context.Context, service.RequestContext, service.UpdateModelProfileInput) (service.ModelProfile, error)
	DeleteModelProfile(context.Context, service.RequestContext, string) error
	CheckReady(context.Context) (service.Readiness, error)
	CreateChatCompletion(context.Context, service.ChatCompletionInput) (service.ChatCompletionResult, error)
	StreamChatCompletion(context.Context, service.ChatCompletionInput) (service.ChatCompletionStream, error)
	CreateEmbeddings(context.Context, service.RequestContext, service.EmbeddingInput) (service.EmbeddingResponse, error)
	CreateReranking(context.Context, service.RequestContext, service.RerankingInput) (service.RerankingResponse, error)
}

type Config struct {
	Logger          *slog.Logger
	Profiles        ModelProfileService
	Authenticator   *middleware.ServiceTokenAuthenticator
	MaxRequestBytes int64
}

type Server struct {
	logger          *slog.Logger
	profiles        ModelProfileService
	authenticator   *middleware.ServiceTokenAuthenticator
	maxRequestBytes int64
	mux             *http.ServeMux
}

const defaultMaxRequestBytes = int64(1 << 20)

func NewServer(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxRequestBytes <= 0 {
		cfg.MaxRequestBytes = defaultMaxRequestBytes
	}
	server := &Server{
		logger:          cfg.Logger,
		profiles:        cfg.Profiles,
		authenticator:   cfg.Authenticator,
		maxRequestBytes: cfg.MaxRequestBytes,
		mux:             http.NewServeMux(),
	}
	server.routes()
	return server
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /internal/v1/model-profiles", s.handleListModelProfiles)
	s.mux.HandleFunc("POST /internal/v1/model-profiles", s.handleCreateModelProfile)
	s.mux.HandleFunc("GET /internal/v1/model-profiles/{profileId}", s.handleGetModelProfile)
	s.mux.HandleFunc("PATCH /internal/v1/model-profiles/{profileId}", s.handleUpdateModelProfile)
	s.mux.HandleFunc("DELETE /internal/v1/model-profiles/{profileId}", s.handleDeleteModelProfile)
	s.mux.HandleFunc("POST /internal/v1/chat/completions", s.handleCreateChatCompletion)
	s.mux.HandleFunc("POST /internal/v1/embeddings", s.handleCreateEmbeddings)
	s.mux.HandleFunc("POST /internal/v1/rerankings", s.handleCreateReranking)
	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = newRequestID()
	}
	ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-Id", requestID)

	recorder := &statusRecorder{ResponseWriter: w}
	startedAt := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.ErrorContext(ctx, "http panic recovered", "service", "ai-gateway", "request_id", requestID, "operation", "http_request")
			if recorder.status == 0 {
				writeError(recorder, r, service.NewError(service.CodeInternal, "internal server error", nil))
			}
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		if status >= http.StatusInternalServerError {
			s.logger.ErrorContext(ctx, "http request failed", "service", "ai-gateway", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(startedAt).Milliseconds())
		}
	}()

	s.mux.ServeHTTP(recorder, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeData(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.profiles == nil {
		writeError(w, r, service.DependencyError("profile service is not configured", nil))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	ready, err := s.profiles.CheckReady(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if ready.Status != "ok" {
		status = http.StatusServiceUnavailable
	}
	writeData(w, r, status, ready)
}

func (s *Server) handleListModelProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeInternal(w, r) || !s.requireProfiles(w, r) {
		return
	}
	filter, ok := parseListFilter(w, r)
	if !ok {
		return
	}
	items, err := s.profiles.ListModelProfiles(r.Context(), filter)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, profilesFromDomain(items))
}

func (s *Server) handleCreateModelProfile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r)
	if !ok || !s.requireProfiles(w, r) {
		return
	}
	var payload createModelProfileRequest
	if !s.decodeJSON(w, r, &payload) {
		return
	}
	created, err := s.profiles.CreateModelProfile(r.Context(), reqCtx, createInputFromRequest(payload))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, profileFromDomain(created))
}

func (s *Server) handleGetModelProfile(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeInternal(w, r) || !s.requireProfiles(w, r) {
		return
	}
	profile, err := s.profiles.GetModelProfile(r.Context(), r.PathValue("profileId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, profileFromDomain(profile))
}

func (s *Server) handleUpdateModelProfile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r)
	if !ok || !s.requireProfiles(w, r) {
		return
	}
	var payload updateModelProfileRequest
	if !s.decodeJSON(w, r, &payload) {
		return
	}
	updated, err := s.profiles.UpdateModelProfile(r.Context(), reqCtx, updateInputFromRequest(r.PathValue("profileId"), payload))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, profileFromDomain(updated))
}

func (s *Server) handleDeleteModelProfile(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.internalContext(w, r)
	if !ok || !s.requireProfiles(w, r) {
		return
	}
	if err := s.profiles.DeleteModelProfile(r.Context(), reqCtx, r.PathValue("profileId")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleModelInvocationNotImplemented(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeModelInvocation(w, r) {
		return
	}
	writeOpenAIError(w, http.StatusNotImplemented, "model invocation is not implemented", "not_implemented_error", "not_implemented")
}

func (s *Server) handleCreateChatCompletion(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.modelInvocationContext(w, r)
	if !ok || !s.requireProfilesForModelInvocation(w, r) {
		return
	}
	payload, ok := s.decodeRawJSONObject(w, r)
	if !ok {
		return
	}
	input := service.ChatCompletionInput{RequestContext: reqCtx, Payload: payload}
	if rawBool(payload["stream"]) {
		s.handleCreateChatCompletionStream(w, r, input)
		return
	}
	result, err := s.profiles.CreateChatCompletion(r.Context(), input)
	if err != nil {
		writeOpenAIErrorFromError(w, err)
		return
	}
	sanitizedBody, valid := sanitizeChatCompletion(result.Body)
	if !valid {
		writeOpenAIErrorFromError(w, &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "invalid response from provider", Type: "upstream_error", Code: "invalid_response"})
		return
	}
	writeRawJSON(w, http.StatusOK, sanitizedBody)
}

func (s *Server) handleCreateChatCompletionStream(w http.ResponseWriter, r *http.Request, input service.ChatCompletionInput) {
	stream, err := s.profiles.StreamChatCompletion(r.Context(), input)
	if err != nil {
		writeOpenAIErrorFromError(w, err)
		return
	}
	defer stream.Body.Close()
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(stream.Body)
	status := service.InvocationSucceeded
	var streamErr *service.OpenAIError
	var usage *service.TokenUsage
	sawDone := false
	streamStarted := false
	startStream := func() {
		if streamStarted {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		streamStarted = true
	}
	for {
		chunk, readErr := reader.ReadBytes('\n')
		if len(chunk) > 0 {
			validation := validateStreamChunk(chunk)
			if validation.err != nil {
				status = service.InvocationFailed
				streamErr = validation.err
				if !streamStarted {
					writeOpenAIErrorFromError(w, streamErr)
				}
				break
			}
			if validation.skip {
				if streamStarted && len(bytes.TrimSpace(chunk)) == 0 {
					if _, err := w.Write(chunk); err != nil {
						status = service.InvocationCancelled
						streamErr = &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "request was cancelled", Type: "upstream_error", Code: "cancelled", Err: err}
						break
					}
					if flusher != nil {
						flusher.Flush()
					}
				}
				if readErr == nil {
					continue
				}
			} else {
				if validation.done {
					sawDone = true
				} else if validation.usage != nil {
					usage = validation.usage
				}
				startStream()
				if _, err := w.Write(validation.chunk); err != nil {
					status = service.InvocationCancelled
					streamErr = &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "request was cancelled", Type: "upstream_error", Code: "cancelled", Err: err}
					break
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		status = service.InvocationFailed
		if errors.Is(r.Context().Err(), context.Canceled) {
			status = service.InvocationCancelled
			streamErr = &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "request was cancelled", Type: "upstream_error", Code: "cancelled", Err: readErr}
		} else {
			streamErr = &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "provider stream failed", Type: "upstream_error", Code: "dependency_error", Err: readErr}
		}
		break
	}
	if status == service.InvocationSucceeded && !sawDone {
		status = service.InvocationFailed
		streamErr = &service.OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "provider stream ended without completion marker", Type: "upstream_error", Code: "dependency_error"}
		if !streamStarted {
			writeOpenAIErrorFromError(w, streamErr)
		}
	}
	if err := stream.Finalize(service.StreamFinalizeInput{Status: status, Error: streamErr, Usage: usage}); err != nil {
		s.logger.WarnContext(r.Context(), "record provider stream invocation failed", "service", "ai-gateway", "request_id", requestIDFromContext(r.Context()), "operation", "chat_completion")
	}
}

func (s *Server) handleCreateEmbeddings(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.modelInvocationContext(w, r)
	if !ok || !s.requireProfilesForModelInvocation(w, r) {
		return
	}
	var payload embeddingRequest
	if !s.decodeModelJSON(w, r, &payload) {
		return
	}
	response, err := s.profiles.CreateEmbeddings(r.Context(), reqCtx, embeddingInputFromRequest(payload))
	if err != nil {
		writeOpenAIAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateReranking(w http.ResponseWriter, r *http.Request) {
	reqCtx, ok := s.modelInvocationContext(w, r)
	if !ok || !s.requireProfilesForModelInvocation(w, r) {
		return
	}
	var payload rerankingRequest
	if !s.decodeModelJSON(w, r, &payload) {
		return
	}
	response, err := s.profiles.CreateReranking(r.Context(), reqCtx, rerankingInputFromRequest(payload))
	if err != nil {
		writeOpenAIAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, service.NotFoundError("route not found", nil))
}

func (s *Server) internalContext(w http.ResponseWriter, r *http.Request) (service.RequestContext, bool) {
	if !s.authorizeInternal(w, r) {
		return service.RequestContext{}, false
	}
	reqCtx := service.RequestContext{
		RequestID:     requestIDFromContext(r.Context()),
		CallerService: strings.TrimSpace(r.Header.Get("X-Caller-Service")),
		UserID:        strings.TrimSpace(r.Header.Get("X-User-Id")),
	}
	return reqCtx, true
}

func (s *Server) authorizeInternal(w http.ResponseWriter, r *http.Request) bool {
	if s.authenticator == nil || !s.authenticator.Authenticate(r.Header.Get("X-Service-Token")) {
		writeError(w, r, service.UnauthorizedError())
		return false
	}
	callerService := strings.TrimSpace(r.Header.Get("X-Caller-Service"))
	if callerService == "" {
		writeError(w, r, service.UnauthorizedError())
		return false
	}
	if !isAllowedCallerService(callerService) {
		writeError(w, r, service.NewError(service.CodeForbidden, "caller service is not allowed", nil))
		return false
	}
	return true
}

func (s *Server) authorizeModelInvocation(w http.ResponseWriter, r *http.Request) bool {
	if s.authenticator == nil || !s.authenticator.Authenticate(r.Header.Get("X-Service-Token")) {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication is required", "authentication_error", "unauthorized")
		return false
	}
	callerService := strings.TrimSpace(r.Header.Get("X-Caller-Service"))
	if callerService == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication is required", "authentication_error", "unauthorized")
		return false
	}
	if !isAllowedCallerService(callerService) {
		writeOpenAIError(w, http.StatusForbidden, "caller service is not allowed", "permission_error", "forbidden")
		return false
	}
	return true
}

func (s *Server) modelInvocationContext(w http.ResponseWriter, r *http.Request) (service.RequestContext, bool) {
	if !s.authorizeModelInvocation(w, r) {
		return service.RequestContext{}, false
	}
	return service.RequestContext{
		RequestID:     requestIDFromContext(r.Context()),
		CallerService: strings.TrimSpace(r.Header.Get("X-Caller-Service")),
		UserID:        strings.TrimSpace(r.Header.Get("X-User-Id")),
	}, true
}

func isAllowedCallerService(callerService string) bool {
	switch callerService {
	case "gateway", "qa", "knowledge", "document", "auth", "file":
		return true
	default:
		return false
	}
}

func (s *Server) requireProfiles(w http.ResponseWriter, r *http.Request) bool {
	if s.profiles != nil {
		return true
	}
	writeError(w, r, service.DependencyError("profile service is not configured", nil))
	return false
}

func (s *Server) requireProfilesForModelInvocation(w http.ResponseWriter, r *http.Request) bool {
	if s.profiles != nil {
		return true
	}
	writeOpenAIError(w, http.StatusBadGateway, "profile service is not configured", "upstream_error", "dependency_error")
	return false
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, r, service.ValidationError(map[string]string{"body": "must be a valid JSON object"}))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, r, service.ValidationError(map[string]string{"body": "must contain only one JSON object"}))
		return false
	}
	return true
}

func (s *Server) decodeRawJSONObject(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		writeOpenAIError(w, http.StatusBadRequest, "request body must be a JSON object", "invalid_request_error", "validation_error")
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeOpenAIError(w, http.StatusBadRequest, "request body must contain only one JSON object", "invalid_request_error", "validation_error")
		return nil, false
	}
	return payload, true
}

func (s *Server) decodeModelJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "request validation failed", "invalid_request_error", "validation_error")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeOpenAIError(w, http.StatusBadRequest, "request validation failed", "invalid_request_error", "validation_error")
		return false
	}
	return true
}

func parseListFilter(w http.ResponseWriter, r *http.Request) (service.ListModelProfilesFilter, bool) {
	var filter service.ListModelProfilesFilter
	if raw := strings.TrimSpace(r.URL.Query().Get("purpose")); raw != "" {
		purpose := service.Purpose(raw)
		filter.Purpose = &purpose
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("enabled")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, service.ValidationError(map[string]string{"enabled": "must be a boolean"}))
			return service.ListModelProfilesFilter{}, false
		}
		filter.Enabled = &value
	}
	return filter, true
}

type requestIDKey struct{}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

type successEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"requestId"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type openAIErrorEnvelope struct {
	Error openAIErrorBody `json:"error"`
}

type errorBody struct {
	Code      service.Code      `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"requestId"`
	Fields    map[string]string `json:"fields,omitempty"`
}

type openAIErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func writeData(w http.ResponseWriter, r *http.Request, status int, value any) {
	writeJSON(w, status, successEnvelope{Data: value, RequestID: requestIDFromContext(r.Context())})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := service.Classify(err)
	if !ok {
		appErr = service.NewError(service.CodeInternal, "internal server error", err)
	}
	writeJSON(w, statusForCode(appErr.Code), errorEnvelope{Error: errorBody{
		Code:      appErr.Code,
		Message:   appErr.Message,
		RequestID: requestIDFromContext(r.Context()),
		Fields:    appErr.Fields,
	}})
}

func writeOpenAIError(w http.ResponseWriter, status int, message, errorType, code string) {
	writeOpenAIErrorWithParam(w, status, message, errorType, "", code)
}

func writeOpenAIErrorWithParam(w http.ResponseWriter, status int, message, errorType, param, code string) {
	writeJSON(w, status, openAIErrorEnvelope{Error: openAIErrorBody{
		Message: message,
		Type:    errorType,
		Param:   strings.TrimSpace(param),
		Code:    code,
	}})
}

func writeOpenAIErrorFromError(w http.ResponseWriter, err error) {
	var openErr *service.OpenAIError
	if errors.As(err, &openErr) {
		writeOpenAIErrorWithParam(w, openErr.HTTPStatus, openErr.Message, openErr.Type, openErr.Param, openErr.Code)
		return
	}
	writeOpenAIError(w, http.StatusInternalServerError, "internal server error", "internal_error", "internal_error")
}

func writeOpenAIAppError(w http.ResponseWriter, err error) {
	appErr, ok := service.Classify(err)
	if !ok {
		appErr = service.NewError(service.CodeInternal, "internal server error", err)
	}
	writeOpenAIErrorWithParam(w, statusForCode(appErr.Code), appErr.Message, service.OpenAIErrorTypeForCode(appErr.Code), openAIErrorParam(appErr), string(appErr.Code))
}

func openAIErrorParam(appErr *service.AppError) string {
	if appErr == nil || appErr.Code != service.CodeValidation || len(appErr.Fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(appErr.Fields))
	for key := range appErr.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[0]
}

func writeRawJSON(w http.ResponseWriter, status int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}

func rawBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func parseStreamUsage(chunk []byte) *service.TokenUsage {
	line := bytes.TrimSpace(chunk)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return nil
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}
	var value struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &value); err != nil || value.Usage == nil {
		return nil
	}
	return &service.TokenUsage{
		PromptTokens:     value.Usage.PromptTokens,
		CompletionTokens: value.Usage.CompletionTokens,
		TotalTokens:      value.Usage.TotalTokens,
	}
}

type streamChunkValidation struct {
	chunk []byte
	skip  bool
	done  bool
	usage *service.TokenUsage
	err   *service.OpenAIError
}

func validateStreamChunk(chunk []byte) streamChunkValidation {
	line := bytes.TrimSpace(chunk)
	if len(line) == 0 || bytes.HasPrefix(line, []byte(":")) {
		return streamChunkValidation{skip: true}
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		return invalidProviderStreamChunk()
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if bytes.Equal(payload, []byte("[DONE]")) {
		return streamChunkValidation{chunk: []byte("data: [DONE]\n"), done: true}
	}
	sanitized, ok := sanitizeOpenAIChatCompletionChunk(payload)
	if !ok {
		return invalidProviderStreamChunk()
	}
	return streamChunkValidation{chunk: append(append([]byte("data: "), sanitized...), '\n'), usage: parseStreamUsage(chunk)}
}

func invalidProviderStreamChunk() streamChunkValidation {
	return streamChunkValidation{err: &service.OpenAIError{
		HTTPStatus: http.StatusBadGateway,
		Message:    "provider stream returned a non-contract response",
		Type:       "upstream_error",
		Code:       "dependency_error",
	}}
}

func sanitizeChatCompletion(payload []byte) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, false
	}
	sanitized := map[string]json.RawMessage{}
	copyRawFields(sanitized, raw, "id", "object", "created", "model", "system_fingerprint")
	choices, ok := raw["choices"]
	if !ok {
		return nil, false
	}
	sanitizedChoices, valid := sanitizeChatCompletionChoices(choices)
	if !valid {
		return nil, false
	}
	sanitized["choices"] = sanitizedChoices
	if usage, ok := raw["usage"]; ok {
		sanitizedUsage, valid := sanitizeStreamUsage(usage)
		if !valid {
			return nil, false
		}
		sanitized["usage"] = sanitizedUsage
	}
	encoded, err := json.Marshal(sanitized)
	return encoded, err == nil
}

func sanitizeChatCompletionChoices(rawChoices json.RawMessage) (json.RawMessage, bool) {
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return nil, false
	}
	sanitizedChoices := make([]map[string]json.RawMessage, 0, len(choices))
	for _, choice := range choices {
		sanitizedChoice := map[string]json.RawMessage{}
		copyRawFields(sanitizedChoice, choice, "index", "finish_reason")
		if message, ok := choice["message"]; ok {
			sanitizedMessage, valid := sanitizeChatCompletionMessage(message)
			if !valid {
				return nil, false
			}
			sanitizedChoice["message"] = sanitizedMessage
		}
		sanitizedChoices = append(sanitizedChoices, sanitizedChoice)
	}
	encoded, err := json.Marshal(sanitizedChoices)
	return encoded, err == nil
}

func sanitizeChatCompletionMessage(rawMessage json.RawMessage) (json.RawMessage, bool) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(rawMessage, &message); err != nil {
		return nil, false
	}
	sanitized := map[string]json.RawMessage{}
	copyRawFields(sanitized, message, "role", "content", "refusal")
	if reasoning, ok := message["reasoning"]; ok {
		if isJSONStringOrNull(reasoning) {
			sanitized["reasoning"] = reasoning
		}
	}
	if toolCalls, ok := message["tool_calls"]; ok {
		sanitizedToolCalls, valid := sanitizeStreamToolCalls(toolCalls)
		if !valid {
			return nil, false
		}
		sanitized["tool_calls"] = sanitizedToolCalls
	}
	encoded, err := json.Marshal(sanitized)
	return encoded, err == nil
}

func sanitizeOpenAIChatCompletionChunk(payload []byte) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, false
	}
	if streamRawString(raw["object"]) != "chat.completion.chunk" {
		return nil, false
	}
	sanitized := map[string]json.RawMessage{}
	copyRawFields(sanitized, raw, "id", "object", "created", "model", "system_fingerprint")
	choices, ok := raw["choices"]
	if !ok {
		return nil, false
	}
	sanitizedChoices, valid := sanitizeStreamChoices(choices)
	if !valid {
		return nil, false
	}
	sanitized["choices"] = sanitizedChoices
	if usage, ok := raw["usage"]; ok {
		sanitizedUsage, valid := sanitizeStreamUsage(usage)
		if !valid {
			return nil, false
		}
		sanitized["usage"] = sanitizedUsage
	}
	encoded, err := json.Marshal(sanitized)
	return encoded, err == nil
}

func sanitizeStreamChoices(rawChoices json.RawMessage) (json.RawMessage, bool) {
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(rawChoices, &choices); err != nil {
		return nil, false
	}
	sanitizedChoices := make([]map[string]json.RawMessage, 0, len(choices))
	for _, choice := range choices {
		sanitizedChoice := map[string]json.RawMessage{}
		copyRawFields(sanitizedChoice, choice, "index", "finish_reason")
		if delta, ok := choice["delta"]; ok {
			sanitizedDelta, valid := sanitizeStreamDelta(delta)
			if !valid {
				return nil, false
			}
			sanitizedChoice["delta"] = sanitizedDelta
		}
		sanitizedChoices = append(sanitizedChoices, sanitizedChoice)
	}
	encoded, err := json.Marshal(sanitizedChoices)
	return encoded, err == nil
}

func sanitizeStreamDelta(rawDelta json.RawMessage) (json.RawMessage, bool) {
	var delta map[string]json.RawMessage
	if err := json.Unmarshal(rawDelta, &delta); err != nil {
		return nil, false
	}
	sanitizedDelta := map[string]json.RawMessage{}
	copyRawFields(sanitizedDelta, delta, "role", "content", "refusal")
	if reasoning, ok := delta["reasoning"]; ok {
		if isJSONStringOrNull(reasoning) {
			sanitizedDelta["reasoning"] = reasoning
		}
	}
	if functionCall, ok := delta["function_call"]; ok {
		sanitizedFunctionCall, valid := sanitizeNamedArguments(functionCall)
		if !valid {
			return nil, false
		}
		sanitizedDelta["function_call"] = sanitizedFunctionCall
	}
	if toolCalls, ok := delta["tool_calls"]; ok {
		sanitizedToolCalls, valid := sanitizeStreamToolCalls(toolCalls)
		if !valid {
			return nil, false
		}
		sanitizedDelta["tool_calls"] = sanitizedToolCalls
	}
	encoded, err := json.Marshal(sanitizedDelta)
	return encoded, err == nil
}

func isJSONStringOrNull(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	s := strings.TrimSpace(string(raw))
	if s == "null" {
		return true
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return true
	}
	return false
}

func sanitizeStreamToolCalls(rawToolCalls json.RawMessage) (json.RawMessage, bool) {
	var toolCalls []map[string]json.RawMessage
	if err := json.Unmarshal(rawToolCalls, &toolCalls); err != nil {
		return nil, false
	}
	sanitizedToolCalls := make([]map[string]json.RawMessage, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		sanitizedToolCall := map[string]json.RawMessage{}
		copyRawFields(sanitizedToolCall, toolCall, "index", "id", "type")
		if function, ok := toolCall["function"]; ok {
			sanitizedFunction, valid := sanitizeNamedArguments(function)
			if !valid {
				return nil, false
			}
			sanitizedToolCall["function"] = sanitizedFunction
		}
		sanitizedToolCalls = append(sanitizedToolCalls, sanitizedToolCall)
	}
	encoded, err := json.Marshal(sanitizedToolCalls)
	return encoded, err == nil
}

func sanitizeNamedArguments(rawValue json.RawMessage) (json.RawMessage, bool) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return nil, false
	}
	sanitized := map[string]json.RawMessage{}
	copyRawFields(sanitized, value, "name", "arguments")
	encoded, err := json.Marshal(sanitized)
	return encoded, err == nil
}

func sanitizeStreamUsage(rawUsage json.RawMessage) (json.RawMessage, bool) {
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(rawUsage, &usage); err != nil {
		return nil, false
	}
	sanitizedUsage := map[string]json.RawMessage{}
	copyRawFields(sanitizedUsage, usage, "prompt_tokens", "completion_tokens", "total_tokens")
	if details, ok := usage["completion_tokens_details"]; ok {
		sanitizedDetails, valid := sanitizeCompletionTokensDetails(details)
		if valid {
			sanitizedUsage["completion_tokens_details"] = sanitizedDetails
		}
	}
	encoded, err := json.Marshal(sanitizedUsage)
	return encoded, err == nil
}

func sanitizeCompletionTokensDetails(rawDetails json.RawMessage) (json.RawMessage, bool) {
	var details map[string]json.RawMessage
	if err := json.Unmarshal(rawDetails, &details); err != nil {
		return nil, false
	}
	sanitized := map[string]json.RawMessage{}
	copyRawFields(sanitized, details, "reasoning_tokens")
	encoded, err := json.Marshal(sanitized)
	return encoded, err == nil
}

func copyRawFields(dst, src map[string]json.RawMessage, fields ...string) {
	for _, field := range fields {
		if value, ok := src[field]; ok {
			dst[field] = value
		}
	}
}

func streamRawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func statusForCode(code service.Code) int {
	switch code {
	case service.CodeValidation:
		return http.StatusBadRequest
	case service.CodeUnauthorized:
		return http.StatusUnauthorized
	case service.CodeForbidden:
		return http.StatusForbidden
	case service.CodeNotFound:
		return http.StatusNotFound
	case service.CodeConflict:
		return http.StatusConflict
	case service.CodeRateLimited:
		return http.StatusTooManyRequests
	case service.CodeDependency:
		return http.StatusBadGateway
	case service.CodeNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRequestID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return "req_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return "req_" + hex.EncodeToString(data)
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

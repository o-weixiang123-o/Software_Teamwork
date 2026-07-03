package httpapi

import (
	"net/http"
	"strconv"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

func (s *Server) registerResourceRoutes() {
	s.mux.HandleFunc("GET /internal/v1/qa-sessions/{sessionId}/events", s.handleListEvents)
	s.mux.HandleFunc("GET /internal/v1/response-runs/{responseRunId}", s.handleGetResponseRun)
	s.mux.HandleFunc("PATCH /internal/v1/response-runs/{responseRunId}", s.handleCancelResponseRun)
	s.mux.HandleFunc("GET /internal/v1/response-runs/{responseRunId}/tool-calls", s.handleListToolCalls)
	s.mux.HandleFunc("GET /internal/v1/messages/{messageId}/citations", s.handleListCitations)
	s.mux.HandleFunc("GET /internal/v1/citations/{citationId}", s.handleGetCitation)
	s.mux.HandleFunc("POST /internal/v1/citation-lookups", s.handleCitationLookup)
	s.mux.HandleFunc("GET /internal/v1/qa-config-versions/current", s.handleGetQAConfigVersion)
	s.mux.HandleFunc("POST /internal/v1/qa-config-versions", s.handleCreateQAConfigVersion)
	s.mux.HandleFunc("GET /internal/v1/llm-config-versions/current", s.handleGetLLMConfigVersion)
	s.mux.HandleFunc("POST /internal/v1/llm-config-versions", s.handleCreateLLMConfigVersion)
	s.mux.HandleFunc("POST /internal/v1/llm-connection-tests", s.handleProfileConnectionTest)
	s.mux.HandleFunc("POST /internal/v1/retrieval-test-runs", s.handleCreateRetrievalTest)
	s.mux.HandleFunc("GET /internal/v1/retrieval-test-runs/{testRunId}", s.handleGetRetrievalTest)
	s.mux.HandleFunc("GET /internal/v1/qa-metrics/overview", s.handleMetricsOverview)
	s.mux.HandleFunc("GET /internal/v1/qa-metrics/trend", s.handleMetricsTrend)
	s.mux.HandleFunc("GET /internal/v1/qa-metrics/top-queries", s.handleTopQueries)
	s.mux.HandleFunc("GET /internal/v1/qa-metrics/intent-distribution", s.handleIntentDistribution)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	after, err := positiveQuery(r, "afterEventSeq", 0, true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	runID := r.URL.Query().Get("responseRunId")
	if runID == "" {
		writeError(w, r, service.ValidationError(map[string]string{"responseRunId": "is required"}))
		return
	}
	value, err := s.resources.ListStreamEvents(r.Context(), user, r.PathValue("sessionId"), runID, after)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleGetResponseRun(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	value, err := s.resources.GetResponseRun(r.Context(), user, r.PathValue("responseRunId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleCancelResponseRun(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	if input.Status != "cancelled" {
		writeError(w, r, service.ValidationError(map[string]string{"status": "must be cancelled"}))
		return
	}
	value, err := s.resources.CancelResponseRun(r.Context(), user, r.PathValue("responseRunId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleListToolCalls(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	value, err := s.resources.ListToolCalls(r.Context(), user, r.PathValue("responseRunId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleListCitations(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	value, err := s.resources.ListMessageCitations(r.Context(), user, r.PathValue("messageId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleGetCitation(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	value, err := s.resources.GetCitation(r.Context(), user, r.PathValue("citationId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleCitationLookup(w http.ResponseWriter, r *http.Request) {
	user, ok := userIDFromRequest(w, r)
	if !ok {
		return
	}
	var input struct {
		CitationIDs []string `json:"citationIds"`
	}
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.LookupCitations(r.Context(), user, input.CitationIDs)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}

func (s *Server) handleGetQAConfigVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	value, err := s.resources.GetActiveQAConfigVersion(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleCreateQAConfigVersion(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var input service.CreateQAConfigVersionInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.CreateQAConfigVersion(r.Context(), user, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, value)
}
func (s *Server) handleGetLLMConfigVersion(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	value, err := s.resources.GetActiveLLMConfigVersion(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleCreateLLMConfigVersion(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var input service.CreateLLMConfigVersionInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.CreateLLMConfigVersion(r.Context(), user, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, value)
}
func (s *Server) handleProfileConnectionTest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var input service.LLMProfileTestInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.TestLLMConnection(r.Context(), user, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, value)
}
func (s *Server) handleCreateRetrievalTest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePermission(w, r, "qa:use", "QA retrieval test access is forbidden")
	if !ok {
		return
	}
	var input service.RetrievalTestInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.CreateRetrievalTestRun(r.Context(), user, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, value)
}
func (s *Server) handleGetRetrievalTest(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePermission(w, r, "qa:use", "QA retrieval test access is forbidden")
	if !ok {
		return
	}
	value, err := s.resources.GetRetrievalTestRun(r.Context(), user, r.PathValue("testRunId"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleMetricsOverview(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireSettingsPermission(w, r, "qa:settings:read")
	if !ok {
		return
	}
	days, err := metricsDaysQuery(r, "days", 1)
	if err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.GetMetricsOverview(r.Context(), userID, days)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleMetricsTrend(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	days, err := metricsDaysQuery(r, "days", 30)
	if err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.GetMetricsTrend(r.Context(), days)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleTopQueries(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	days, err := metricsDaysQuery(r, "days", 7)
	if err != nil {
		writeError(w, r, err)
		return
	}
	limit, err := positiveQuery(r, "limit", 10, false)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if limit > 100 {
		writeError(w, r, service.ValidationError(map[string]string{"limit": "must not exceed 100"}))
		return
	}
	value, err := s.resources.GetTopQueries(r.Context(), days, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}
func (s *Server) handleIntentDistribution(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	days, err := metricsDaysQuery(r, "days", 7)
	if err != nil {
		writeError(w, r, err)
		return
	}
	value, err := s.resources.GetIntentDistribution(r.Context(), days)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, value)
}

func metricsDaysQuery(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 366 {
		return 0, service.ValidationError(map[string]string{name: "must be an integer between 1 and 366"})
	}
	return value, nil
}

func positiveQuery(r *http.Request, name string, fallback int, allowZero bool) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (!allowZero && value == 0) {
		return 0, service.ValidationError(map[string]string{name: "must be a positive integer"})
	}
	return value, nil
}

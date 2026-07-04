package httpapi

import (
	"net/http"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	settings, err := s.settings.GetSettings(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var input service.UpdateQASettingsInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	settings, err := s.settings.UpdateSettings(r.Context(), userID, requestIDFromContext(r.Context()), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:read"); !ok {
		return
	}
	servers, err := s.settings.ListMCPServers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": servers})
}

func (s *Server) handleCreateMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var input service.MCPServerInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	server, err := s.settings.CreateMCPServer(r.Context(), userID, requestIDFromContext(r.Context()), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, server)
}

func (s *Server) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	var patch service.MCPServerPatch
	if err := s.decodeJSON(w, r, &patch); err != nil {
		writeError(w, r, err)
		return
	}
	server, err := s.settings.UpdateMCPServer(
		r.Context(), userID, requestIDFromContext(r.Context()), r.PathValue("serverId"), patch,
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, server)
}

func (s *Server) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireSettingsPermission(w, r, "qa:settings:write")
	if !ok {
		return
	}
	if err := s.settings.DeleteMCPServer(
		r.Context(), userID, requestIDFromContext(r.Context()), r.PathValue("serverId"),
	); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestLLMConnection(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:write"); !ok {
		return
	}
	var input service.LLMConnectionTestInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.settings.TestLLMConnection(r.Context(), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTestMCPConnection(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireSettingsPermission(w, r, "qa:settings:write"); !ok {
		return
	}
	var input service.MCPConnectionTestInput
	if err := s.decodeJSON(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.settings.TestMCPConnection(r.Context(), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) requireSettingsPermission(w http.ResponseWriter, r *http.Request, required string) (string, bool) {
	return s.requirePermission(w, r, required, "settings access is forbidden")
}

func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, required, message string) (string, bool) {
	userID, ok := userIDFromRequest(w, r)
	if !ok {
		return "", false
	}
	if s.settingsOpen {
		return userID, true
	}
	for _, permission := range strings.Split(r.Header.Get("X-User-Permissions"), ",") {
		if strings.TrimSpace(permission) == required {
			return userID, true
		}
	}
	if _, allowed := s.adminUserIDs[userID]; allowed {
		return userID, true
	}
	writeError(w, r, service.NewError(service.CodeForbidden, message, nil))
	return "", false
}

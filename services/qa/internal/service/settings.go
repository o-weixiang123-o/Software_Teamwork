package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/modelendpoint"
)

var mcpAliasPattern = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

const (
	llmProviderAIGateway    = "ai-gateway"
	llmProviderLegacyDirect = "direct"
)

type ConfigService struct {
	repository SettingsRepository
	secrets    SecretCipher
	bootstrap  BootstrapSettings
	llmTester  LLMConnectionTester
	mcpTester  MCPConnectionTester
	reloadMu   sync.RWMutex
	reloader   RuntimeReloader
}

func NewConfigService(repository SettingsRepository, secrets SecretCipher, bootstrap BootstrapSettings, llmTester LLMConnectionTester, mcpTester MCPConnectionTester) (*ConfigService, error) {
	if repository == nil || secrets == nil || llmTester == nil || mcpTester == nil {
		return nil, errors.New("settings repository, secret cipher, and connection testers are required")
	}
	return &ConfigService{repository: repository, secrets: secrets, bootstrap: bootstrap, llmTester: llmTester, mcpTester: mcpTester}, nil
}

func (s *ConfigService) SetReloader(reloader RuntimeReloader) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.reloader = reloader
}

func (s *ConfigService) GetSettings(ctx context.Context) (QASettings, error) {
	retrieval, knowledgeBaseIDs, err := s.repository.GetActiveQAConfig(ctx)
	if err != nil {
		return QASettings{}, err
	}
	llm, err := s.activeLLM(ctx)
	if err != nil {
		return QASettings{}, err
	}
	prompt, err := s.runtimePrompt(ctx)
	if err != nil {
		return QASettings{}, err
	}
	return QASettings{
		Retrieval: retrieval, DefaultKnowledgeBaseIDs: knowledgeBaseIDs,
		LLM: publicLLM(llm), SystemPrompt: prompt,
	}, nil
}

func (s *ConfigService) UpdateSettings(ctx context.Context, userID, requestID string, input UpdateQASettingsInput) (QASettings, error) {
	before, err := s.GetSettings(ctx)
	if err != nil {
		return QASettings{}, err
	}
	// Merge retrieval, KB and prompt changes into a single QA config version.
	retrieval := before.Retrieval
	knowledgeBaseIDs := append([]string(nil), before.DefaultKnowledgeBaseIDs...)
	prompt := before.SystemPrompt
	qaChanged := false
	if input.Retrieval != nil {
		retrieval = *input.Retrieval
		qaChanged = true
	}
	if input.DefaultKnowledgeBaseIDs != nil {
		knowledgeBaseIDs = normalizeIDs(*input.DefaultKnowledgeBaseIDs)
		qaChanged = true
	}
	if input.SystemPrompt != nil {
		prompt = strings.TrimSpace(*input.SystemPrompt)
		if prompt == "" || len(prompt) > 20000 {
			return QASettings{}, ValidationError(map[string]string{"systemPrompt": "must be between 1 and 20000 bytes"})
		}
		qaChanged = true
	}
	if qaChanged {
		if err := validateRetrieval(retrieval, knowledgeBaseIDs); err != nil {
			return QASettings{}, err
		}
		agentConfig, err := s.activeAgentConfig(ctx)
		if err != nil {
			return QASettings{}, err
		}
		if err := s.repository.CreateQAConfigVersion(ctx, userID, retrieval, knowledgeBaseIDs, agentConfig, prompt); err != nil {
			return QASettings{}, err
		}
	}
	if input.LLM != nil {
		stored, err := s.buildStoredLLM(ctx, *input.LLM)
		if err != nil {
			return QASettings{}, err
		}
		if err := s.repository.CreateLLMConfigVersion(ctx, userID, stored); err != nil {
			return QASettings{}, err
		}
	}
	after, err := s.GetSettings(ctx)
	if err != nil {
		return QASettings{}, err
	}
	if err := s.repository.WriteAuditLog(ctx, AuditLog{
		UserID: userID, Action: "update", TargetType: "qa_settings",
		BeforeData: settingsAuditData(before), AfterData: settingsAuditData(after), RequestID: requestID,
	}); err != nil {
		return QASettings{}, err
	}
	if err := s.reload(ctx); err != nil {
		return QASettings{}, NewError(CodeDependency, "runtime reload failed", err)
	}
	return after, nil
}

func (s *ConfigService) activeAgentConfig(ctx context.Context) (AgentConfig, error) {
	currentVersion, err := s.repository.GetActiveQAConfigVersion(ctx)
	if err == nil {
		return NormalizeAgentConfig(currentVersion.Agent), nil
	}
	if appErr, ok := Classify(err); ok && appErr.Code == CodeNotFound {
		return DefaultAgentConfig(), nil
	}
	return AgentConfig{}, err
}

func (s *ConfigService) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	records, err := s.repository.ListMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	servers := make([]MCPServer, 0, len(records))
	for _, record := range records {
		servers = append(servers, publicMCPServer(record))
	}
	return servers, nil
}

func (s *ConfigService) CreateMCPServer(ctx context.Context, userID, requestID string, input MCPServerInput) (MCPServer, error) {
	record, err := s.recordFromInput(input)
	if err != nil {
		return MCPServer{}, err
	}
	record.CreatedByUserID = userID
	record, err = s.repository.CreateMCPServer(ctx, record)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return MCPServer{}, NewError(CodeConflict, "MCP alias already exists", err)
		}
		return MCPServer{}, err
	}
	public := publicMCPServer(record)
	if err := s.repository.WriteAuditLog(ctx, AuditLog{
		UserID: userID, Action: "create", TargetType: "mcp_server", TargetID: record.ID,
		BeforeData: map[string]any{}, AfterData: mcpAuditData(public), RequestID: requestID,
	}); err != nil {
		return MCPServer{}, err
	}
	if err := s.reload(ctx); err != nil {
		return MCPServer{}, NewError(CodeDependency, "runtime reload failed", err)
	}
	return s.refreshedMCPServer(ctx, record.ID)
}

func (s *ConfigService) UpdateMCPServer(ctx context.Context, userID, requestID, id string, patch MCPServerPatch) (MCPServer, error) {
	current, err := s.repository.GetMCPServer(ctx, id)
	if err != nil {
		return MCPServer{}, err
	}
	before := publicMCPServer(current)
	if patch.DisplayName != nil {
		current.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.Transport != nil {
		current.Transport = strings.TrimSpace(*patch.Transport)
	}
	if patch.Command != nil {
		current.Command = strings.TrimSpace(*patch.Command)
	}
	if patch.Args != nil {
		current.Args = append([]string(nil), (*patch.Args)...)
	}
	if patch.EndpointURL != nil {
		current.EndpointURL = strings.TrimSpace(*patch.EndpointURL)
	}
	if patch.TokenHeader != nil {
		current.TokenHeader = strings.TrimSpace(*patch.TokenHeader)
	}
	if patch.ToolTimeoutSeconds != nil {
		current.ToolTimeoutSeconds = *patch.ToolTimeoutSeconds
	}
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.SortOrder != nil {
		current.SortOrder = *patch.SortOrder
	}
	if patch.Token != nil {
		if *patch.Token == "" {
			current.TokenEncrypted, current.TokenLast4 = nil, ""
		} else {
			current.TokenEncrypted, err = s.secrets.Encrypt(*patch.Token)
			if err != nil {
				return MCPServer{}, err
			}
			current.TokenLast4 = last4(*patch.Token)
		}
	}
	if current.Transport == "streamable_http" {
		current.Command = ""
		current.Args = []string{}
	}
	if err := validateMCPRecord(current); err != nil {
		return MCPServer{}, err
	}
	current, err = s.repository.UpdateMCPServer(ctx, current)
	if err != nil {
		return MCPServer{}, err
	}
	after := publicMCPServer(current)
	if err := s.repository.WriteAuditLog(ctx, AuditLog{
		UserID: userID, Action: "update", TargetType: "mcp_server", TargetID: id,
		BeforeData: mcpAuditData(before), AfterData: mcpAuditData(after), RequestID: requestID,
	}); err != nil {
		return MCPServer{}, err
	}
	if err := s.reload(ctx); err != nil {
		return MCPServer{}, NewError(CodeDependency, "runtime reload failed", err)
	}
	return s.refreshedMCPServer(ctx, id)
}

func (s *ConfigService) DeleteMCPServer(ctx context.Context, userID, requestID, id string) error {
	record, err := s.repository.GetMCPServer(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.DeleteMCPServer(ctx, id); err != nil {
		return err
	}
	if err := s.repository.WriteAuditLog(ctx, AuditLog{
		UserID: userID, Action: "delete", TargetType: "mcp_server", TargetID: id,
		BeforeData: mcpAuditData(publicMCPServer(record)), AfterData: map[string]any{}, RequestID: requestID,
	}); err != nil {
		return err
	}
	if err := s.reload(ctx); err != nil {
		return NewError(CodeDependency, "runtime reload failed", err)
	}
	return nil
}

func (s *ConfigService) TestLLMConnection(ctx context.Context, input LLMConnectionTestInput) (LLMConnectionTestResult, error) {
	active, err := s.runtimeLLM(ctx)
	if err != nil {
		return LLMConnectionTestResult{}, err
	}
	if strings.TrimSpace(input.APIEndpoint) != "" {
		active.Endpoint = strings.TrimSpace(input.APIEndpoint)
	}
	if strings.TrimSpace(input.Model) != "" {
		active.Model = strings.TrimSpace(input.Model)
	}
	if input.TimeoutSeconds > 0 {
		active.Timeout = time.Duration(input.TimeoutSeconds) * time.Second
	}
	if strings.TrimSpace(input.TokenHeader) != "" {
		active.TokenHeader = strings.TrimSpace(input.TokenHeader)
	}
	if input.APIKey != nil {
		active.Token = *input.APIKey
	}
	if err := validateRuntimeLLM(active); err != nil {
		return LLMConnectionTestResult{}, err
	}
	result, err := s.llmTester.TestLLM(ctx, active)
	if err != nil {
		return LLMConnectionTestResult{}, NewError(CodeDependency, "LLM connection test failed", err)
	}
	return result, nil
}

func (s *ConfigService) TestMCPConnection(ctx context.Context, input MCPConnectionTestInput) (MCPConnectionTestResult, error) {
	var runtime RuntimeMCPConfig
	if input.ID != "" {
		record, err := s.repository.GetMCPServer(ctx, input.ID)
		if err != nil {
			return MCPConnectionTestResult{}, err
		}
		runtime, err = s.runtimeMCP(record)
		if err != nil {
			return MCPConnectionTestResult{}, err
		}
	}
	if input.Transport != "" {
		runtime.Transport = input.Transport
	}
	if input.Command != "" {
		runtime.Command = input.Command
	}
	if input.Args != nil {
		runtime.Args = append([]string(nil), input.Args...)
	}
	if input.EndpointURL != "" {
		runtime.EndpointURL = input.EndpointURL
	}
	if input.TokenHeader != "" {
		runtime.TokenHeader = input.TokenHeader
	}
	if input.Token != nil {
		runtime.Token = *input.Token
	}
	if input.ToolTimeoutSeconds > 0 {
		runtime.ToolTimeout = time.Duration(input.ToolTimeoutSeconds) * time.Second
	}
	if runtime.Alias == "" {
		runtime.Alias = "connection_test"
	}
	if runtime.ToolTimeout <= 0 {
		runtime.ToolTimeout = 30 * time.Second
	}
	if runtime.TokenHeader == "" {
		runtime.TokenHeader = http.CanonicalHeaderKey("Authorization")
	}
	if err := validateRuntimeMCP(runtime); err != nil {
		return MCPConnectionTestResult{}, err
	}
	result, err := s.mcpTester.TestMCP(ctx, runtime)
	if err != nil {
		return MCPConnectionTestResult{}, NewError(CodeDependency, "MCP connection test failed", err)
	}
	return result, nil
}

func (s *ConfigService) LoadRuntimeConfiguration(ctx context.Context) (RuntimeConfiguration, error) {
	llm, err := s.runtimeLLM(ctx)
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	qaConfig, err := s.repository.GetActiveQAConfigVersion(ctx)
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return RuntimeConfiguration{}, err
		}
	}
	retrievalSettings, defaultKBIDs, err := s.repository.GetActiveQAConfig(ctx)
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return RuntimeConfiguration{}, err
		}
	}
	llmConfig, err := s.repository.GetActiveLLMConfigVersion(ctx)
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return RuntimeConfiguration{}, err
		}
	}
	prompt, err := s.runtimePrompt(ctx)
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	records, err := s.repository.ListMCPServers(ctx)
	if err != nil {
		return RuntimeConfiguration{}, err
	}
	servers := make([]RuntimeMCPConfig, 0, len(records)+1)
	bootstrapMatched := false
	for _, record := range records {
		if s.bootstrap.MCPServer != nil && record.Alias == s.bootstrap.MCPServer.Alias {
			bootstrapMatched = true
		}
		if !record.Enabled {
			continue
		}
		server, err := s.runtimeMCP(record)
		if err != nil {
			return RuntimeConfiguration{}, err
		}
		if s.bootstrap.MCPServer != nil && record.Alias == s.bootstrap.MCPServer.Alias && server.Token == "" {
			server.Token = s.bootstrap.MCPServer.Token
		}
		servers = append(servers, server)
	}
	if !bootstrapMatched && s.bootstrap.MCPServer != nil {
		servers = append(servers, *s.bootstrap.MCPServer)
	}
	agentConfig := NormalizeAgentConfig(qaConfig.Agent)
	if qaConfig.ID == "" {
		agentConfig = DefaultAgentConfig()
	}
	// Nil still means there is no active QA config. An active config with an
	// empty default list means no preselected KB scope; QA retrieval uses the
	// project-wide Knowledge pool at retrieval time.
	var effectiveDefaultKBIDs []string
	if qaConfig.ID != "" {
		effectiveDefaultKBIDs = append([]string{}, defaultKBIDs...)
	}
	return RuntimeConfiguration{
		LLM: llm, SystemPrompt: prompt, MCPServers: servers,
		QAConfigVersionID: qaConfig.ID, LLMConfigVersionID: llmConfig.ID, Agent: agentConfig,
		DefaultKnowledgeBaseIDs: effectiveDefaultKBIDs,
		RetrievalSettings:       retrievalSettings,
	}, nil
}

func (s *ConfigService) activeLLM(ctx context.Context) (StoredLLMConfig, error) {
	stored, err := s.repository.GetActiveLLMConfig(ctx)
	if err == nil && stored.Provider == llmProviderAIGateway {
		return stored, nil
	}
	if err == nil && stored.Provider == llmProviderLegacyDirect && stored.APIEndpoint != "" {
		return stored, nil
	}
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return StoredLLMConfig{}, err
		}
	}
	return StoredLLMConfig{
		Provider: llmProviderAIGateway, ProfileID: s.bootstrap.LLM.ProfileID,
		Model: s.bootstrap.LLM.Model, TimeoutSeconds: int(s.bootstrap.LLM.Timeout.Seconds()),
		Temperature: 0.7, MaxTokens: s.bootstrap.LLM.MaxTokens,
	}, nil
}

func (s *ConfigService) runtimeLLM(ctx context.Context) (RuntimeLLMConfig, error) {
	stored, err := s.repository.GetActiveLLMConfig(ctx)
	if err == nil && stored.Provider == llmProviderLegacyDirect && stored.APIEndpoint != "" {
		token := ""
		if len(stored.APIKeyEncrypted) > 0 {
			token, err = s.secrets.Decrypt(stored.APIKeyEncrypted)
			if err != nil {
				return RuntimeLLMConfig{}, err
			}
		}
		return RuntimeLLMConfig{
			Endpoint: stored.APIEndpoint, Token: token, TokenHeader: stored.TokenHeader,
			Model: stored.Model, Timeout: time.Duration(stored.TimeoutSeconds) * time.Second,
			MaxTokens: stored.MaxTokens,
		}, nil
	}
	if err == nil && stored.Provider == llmProviderAIGateway {
		runtime := s.bootstrap.LLM
		runtime.ProfileID = stored.ProfileID
		runtime.Model = stored.Model
		runtime.Timeout = time.Duration(stored.TimeoutSeconds) * time.Second
		runtime.MaxTokens = stored.MaxTokens
		return runtime, nil
	}
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return RuntimeLLMConfig{}, err
		}
	}
	return s.bootstrap.LLM, nil
}

func (s *ConfigService) runtimePrompt(ctx context.Context) (string, error) {
	config, err := s.repository.GetActiveQAConfigVersion(ctx)
	if err == nil && config.SystemPrompt != "" {
		return config.SystemPrompt, nil
	}
	if err != nil {
		if appErr, ok := Classify(err); !ok || appErr.Code != CodeNotFound {
			return "", err
		}
	}
	return s.bootstrap.SystemPrompt, nil
}

func (s *ConfigService) buildStoredLLM(ctx context.Context, update LLMUpdate) (StoredLLMConfig, error) {
	runtime, err := s.runtimeLLM(ctx)
	if err != nil {
		return StoredLLMConfig{}, err
	}
	if strings.TrimSpace(update.ProfileID) != "" {
		runtime.ProfileID = strings.TrimSpace(update.ProfileID)
	}
	if strings.TrimSpace(update.APIEndpoint) != "" {
		runtime.Endpoint = strings.TrimSpace(update.APIEndpoint)
	}
	if strings.TrimSpace(update.TokenHeader) != "" {
		runtime.TokenHeader = strings.TrimSpace(update.TokenHeader)
	}
	if strings.TrimSpace(update.Model) != "" {
		runtime.Model = strings.TrimSpace(update.Model)
	}
	if update.TimeoutSeconds > 0 {
		runtime.Timeout = time.Duration(update.TimeoutSeconds) * time.Second
	}
	if update.MaxTokens > 0 {
		runtime.MaxTokens = update.MaxTokens
	}
	if runtime.TokenHeader == "" {
		runtime.TokenHeader = http.CanonicalHeaderKey("Authorization")
	}
	if update.APIKey != nil {
		runtime.Token = *update.APIKey
	}
	if err := validateRuntimeLLM(runtime); err != nil {
		return StoredLLMConfig{}, err
	}
	if update.Temperature < 0 || update.Temperature > 2 {
		return StoredLLMConfig{}, ValidationError(map[string]string{"llm.temperature": "must be between 0 and 2"})
	}
	timeoutSeconds := update.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(runtime.Timeout.Seconds())
	}
	return StoredLLMConfig{
		Provider: llmProviderAIGateway, ProfileID: runtime.ProfileID,
		TokenHeader: runtime.TokenHeader, Model: runtime.Model,
		TimeoutSeconds: timeoutSeconds, Temperature: update.Temperature, MaxTokens: runtime.MaxTokens,
	}, nil
}

func (s *ConfigService) recordFromInput(input MCPServerInput) (MCPServerRecord, error) {
	record := MCPServerRecord{
		Alias: strings.TrimSpace(input.Alias), DisplayName: strings.TrimSpace(input.DisplayName),
		Transport: strings.TrimSpace(input.Transport), Command: strings.TrimSpace(input.Command),
		Args: append([]string(nil), input.Args...), EndpointURL: strings.TrimSpace(input.EndpointURL),
		TokenHeader: strings.TrimSpace(input.TokenHeader), ToolTimeoutSeconds: input.ToolTimeoutSeconds,
		Enabled: input.Enabled, SortOrder: input.SortOrder,
	}
	if record.TokenHeader == "" {
		record.TokenHeader = http.CanonicalHeaderKey("Authorization")
	}
	if record.ToolTimeoutSeconds == 0 {
		record.ToolTimeoutSeconds = 30
	}
	if input.Token != nil && *input.Token != "" {
		var err error
		record.TokenEncrypted, err = s.secrets.Encrypt(*input.Token)
		if err != nil {
			return MCPServerRecord{}, err
		}
		record.TokenLast4 = last4(*input.Token)
	}
	if record.Transport == "streamable_http" {
		record.Command = ""
		record.Args = []string{}
	}
	if err := validateMCPRecord(record); err != nil {
		return MCPServerRecord{}, err
	}
	return record, nil
}

func (s *ConfigService) runtimeMCP(record MCPServerRecord) (RuntimeMCPConfig, error) {
	token := ""
	var err error
	if len(record.TokenEncrypted) > 0 {
		token, err = s.secrets.Decrypt(record.TokenEncrypted)
		if err != nil {
			return RuntimeMCPConfig{}, err
		}
	}
	return RuntimeMCPConfig{
		ID: record.ID, Alias: record.Alias, Transport: record.Transport,
		Command: record.Command, Args: append([]string(nil), record.Args...),
		EndpointURL: record.EndpointURL, Token: token, TokenHeader: record.TokenHeader,
		ToolTimeout: time.Duration(record.ToolTimeoutSeconds) * time.Second,
	}, nil
}

func (s *ConfigService) refreshedMCPServer(ctx context.Context, id string) (MCPServer, error) {
	record, err := s.repository.GetMCPServer(ctx, id)
	if err != nil {
		return MCPServer{}, err
	}
	return publicMCPServer(record), nil
}

func (s *ConfigService) reload(ctx context.Context) error {
	s.reloadMu.RLock()
	reloader := s.reloader
	s.reloadMu.RUnlock()
	if reloader == nil {
		return nil
	}
	return reloader.Reload(ctx)
}

func validateRetrieval(settings RetrievalSettings, ids []string) error {
	fields := map[string]string{}
	if settings.TopK <= 0 || settings.TopK > 100 {
		fields["retrieval.topK"] = "must be between 1 and 100"
	}
	if settings.ScoreThreshold < 0 || settings.ScoreThreshold > 1 {
		fields["retrieval.scoreThreshold"] = "must be between 0 and 1"
	}
	if settings.RerankThreshold < 0 || settings.RerankThreshold > 1 {
		fields["retrieval.rerankThreshold"] = "must be between 0 and 1"
	}
	if settings.RerankTopN <= 0 || settings.RerankTopN > settings.TopK {
		fields["retrieval.rerankTopN"] = "must be positive and no greater than topK"
	}
	if len(ids) > 50 {
		fields["defaultKnowledgeBaseIds"] = "must not contain more than 50 items"
	}
	if len(fields) > 0 {
		return ValidationError(fields)
	}
	return nil
}

func validateMCPRecord(record MCPServerRecord) error {
	return validateRuntimeMCP(RuntimeMCPConfig{
		Alias: record.Alias, Transport: record.Transport, Command: record.Command,
		Args: record.Args, EndpointURL: record.EndpointURL, TokenHeader: record.TokenHeader,
		ToolTimeout: time.Duration(record.ToolTimeoutSeconds) * time.Second,
	})
}

func validateRuntimeMCP(config RuntimeMCPConfig) error {
	fields := map[string]string{}
	if !mcpAliasPattern.MatchString(config.Alias) {
		fields["alias"] = "must match ^[a-z0-9_]{2,32}$"
	}
	if config.ToolTimeout <= 0 || config.ToolTimeout > 10*time.Minute {
		fields["toolTimeoutSeconds"] = "must be between 1 and 600"
	}
	if !validHeader(config.TokenHeader) {
		fields["tokenHeader"] = "is invalid"
	}
	switch config.Transport {
	case "stdio":
		fields["transport"] = "stdio is test-only; use streamable_http for runtime MCP servers"
	case "streamable_http":
		if err := validateAbsoluteHTTPURL(config.EndpointURL); err != nil {
			fields["endpointUrl"] = "must be an absolute http(s) URL without credentials"
		}
	default:
		fields["transport"] = "must be streamable_http"
	}
	if len(fields) > 0 {
		return ValidationError(fields)
	}
	return nil
}

func validateRuntimeLLM(config RuntimeLLMConfig) error {
	fields := map[string]string{}
	if _, err := modelendpoint.NormalizeAIGatewayChatEndpoint(config.Endpoint); err != nil {
		fields["llm.apiEndpoint"] = "must target trusted AI Gateway chat completions endpoint"
	}
	if config.Token == "" {
		fields["llm.apiKey"] = "is required"
	}
	if !validHeader(config.TokenHeader) {
		fields["llm.tokenHeader"] = "is invalid"
	}
	if config.Timeout <= 0 || config.Timeout > 10*time.Minute {
		fields["llm.timeoutSeconds"] = "must be between 1 and 600"
	}
	if config.MaxTokens <= 0 || config.MaxTokens > 1_000_000 {
		fields["llm.maxTokens"] = "must be between 1 and 1000000"
	}
	if len(fields) > 0 {
		return ValidationError(fields)
	}
	return nil
}

func validateAbsoluteHTTPURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return errors.New("invalid URL")
	}
	return nil
}

func validHeader(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\r\n:")
}

func normalizeIDs(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func publicLLM(config StoredLLMConfig) LLMSettings {
	return LLMSettings{
		Provider: llmProviderAIGateway, ProfileID: config.ProfileID, Model: config.Model,
		TimeoutSeconds: config.TimeoutSeconds, Temperature: config.Temperature,
		MaxTokens: config.MaxTokens, TokenHeader: config.TokenHeader,
	}
}

func publicMCPServer(record MCPServerRecord) MCPServer {
	return MCPServer{
		ID: record.ID, Alias: record.Alias, DisplayName: record.DisplayName,
		Transport: record.Transport, Command: record.Command, Args: append([]string(nil), record.Args...),
		EndpointURL: record.EndpointURL, TokenHeader: record.TokenHeader,
		ToolTimeoutSeconds: record.ToolTimeoutSeconds, Enabled: record.Enabled,
		SortOrder: record.SortOrder, TokenConfigured: len(record.TokenEncrypted) > 0,
		TokenLast4: record.TokenLast4, ToolCount: record.ToolCount,
		LastConnectedAt: record.LastConnectedAt, LastError: record.LastError,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func settingsAuditData(settings QASettings) map[string]any {
	return map[string]any{
		"retrieval": settings.Retrieval, "defaultKnowledgeBaseIds": settings.DefaultKnowledgeBaseIDs,
		"llm": map[string]any{
			"provider": settings.LLM.Provider, "profileId": settings.LLM.ProfileID,
			"model": settings.LLM.Model, "timeoutSeconds": settings.LLM.TimeoutSeconds,
			"temperature": settings.LLM.Temperature, "maxTokens": settings.LLM.MaxTokens,
			"tokenHeader": settings.LLM.TokenHeader,
		},
		"systemPromptLength": len(settings.SystemPrompt),
	}
}

func mcpAuditData(server MCPServer) map[string]any {
	return map[string]any{
		"alias": server.Alias, "displayName": server.DisplayName, "transport": server.Transport,
		"command": server.Command, "args": server.Args, "endpointUrl": server.EndpointURL,
		"token": "***", "tokenHeader": server.TokenHeader,
		"toolTimeoutSeconds": server.ToolTimeoutSeconds, "enabled": server.Enabled,
		"sortOrder": server.SortOrder,
	}
}

func last4(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

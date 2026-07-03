package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type settingsRepositoryStub struct {
	activeLLM                     StoredLLMConfig
	activeQAConfigVersion         QAConfigVersion
	activeQAConfigErr             error
	activeDefaultKnowledgeBaseIDs *[]string
	createdAgent                  AgentConfig
	createdRetrieval              RetrievalSettings
	createdKBIDs                  []string
	createdPrompt                 string
	createCount                   int
	createCalled                  bool
	mcpServers                    []MCPServerRecord
}

func (r *settingsRepositoryStub) GetActiveQAConfig(context.Context) (RetrievalSettings, []string, error) {
	if r.activeDefaultKnowledgeBaseIDs != nil {
		return RetrievalSettings{TopK: 5, ScoreThreshold: .7, RerankThreshold: .5, RerankTopN: 3}.WithScoreThresholdConfigured(), append([]string(nil), (*r.activeDefaultKnowledgeBaseIDs)...), nil
	}
	return RetrievalSettings{TopK: 5, ScoreThreshold: .7, RerankThreshold: .5, RerankTopN: 3}.WithScoreThresholdConfigured(), []string{"kb-old"}, nil
}

func (r *settingsRepositoryStub) GetActiveQAConfigVersion(context.Context) (QAConfigVersion, error) {
	if r.activeQAConfigErr != nil {
		return QAConfigVersion{}, r.activeQAConfigErr
	}
	if r.activeQAConfigVersion.ID != "" {
		return r.activeQAConfigVersion, nil
	}
	return QAConfigVersion{ID: "qa-config"}, nil
}

func (r *settingsRepositoryStub) CreateQAConfigVersion(_ context.Context, _ string, retrieval RetrievalSettings, kbIDs []string, agent AgentConfig, prompt string) error {
	r.createCalled = true
	r.createCount++
	r.createdAgent = agent
	r.createdRetrieval = retrieval
	r.createdKBIDs = kbIDs
	r.createdPrompt = prompt
	return nil
}

func (r *settingsRepositoryStub) GetActiveLLMConfig(context.Context) (StoredLLMConfig, error) {
	if r.activeLLM.Provider != "" {
		return r.activeLLM, nil
	}
	return StoredLLMConfig{
		Provider: "direct", APIEndpoint: "https://llm.example.test/v1", APIKeyLast4: "1234",
		TokenHeader: "Authorization", Model: "model", TimeoutSeconds: 30, Temperature: .7, MaxTokens: 1024,
	}, nil
}

func (r *settingsRepositoryStub) GetActiveLLMConfigVersion(context.Context) (LLMConfigVersion, error) {
	return LLMConfigVersion{ID: "llm-config"}, nil
}

func (r *settingsRepositoryStub) CreateLLMConfigVersion(context.Context, string, StoredLLMConfig) error {
	return nil
}

func (r *settingsRepositoryStub) GetRuntimeSetting(context.Context, string) (string, error) {
	return "system prompt", nil
}

func (r *settingsRepositoryStub) UpsertRuntimeSetting(context.Context, string, string) error {
	return nil
}

func (r *settingsRepositoryStub) ListMCPServers(context.Context) ([]MCPServerRecord, error) {
	return append([]MCPServerRecord(nil), r.mcpServers...), nil
}

func (r *settingsRepositoryStub) GetMCPServer(context.Context, string) (MCPServerRecord, error) {
	return MCPServerRecord{}, NewError(CodeNotFound, "MCP server not found", nil)
}

func (r *settingsRepositoryStub) CreateMCPServer(context.Context, MCPServerRecord) (MCPServerRecord, error) {
	return MCPServerRecord{}, nil
}

func (r *settingsRepositoryStub) UpdateMCPServer(context.Context, MCPServerRecord) (MCPServerRecord, error) {
	return MCPServerRecord{}, nil
}

func (r *settingsRepositoryStub) DeleteMCPServer(context.Context, string) error {
	return nil
}

func (r *settingsRepositoryStub) UpdateMCPConnectionStatus(context.Context, string, int, *time.Time, string) error {
	return nil
}

func (r *settingsRepositoryStub) WriteAuditLog(context.Context, AuditLog) error {
	return nil
}

type settingsCipherStub struct{}

func (settingsCipherStub) Encrypt(value string) ([]byte, error) {
	return []byte(value), nil
}

func (settingsCipherStub) Decrypt(value []byte) (string, error) {
	return string(value), nil
}

type settingsLLMTesterStub struct {
	called bool
	seen   RuntimeLLMConfig
}

func (t *settingsLLMTesterStub) TestLLM(_ context.Context, config RuntimeLLMConfig) (LLMConnectionTestResult, error) {
	t.called = true
	t.seen = config
	return LLMConnectionTestResult{Success: true, Model: config.Model}, nil
}

type settingsMCPTesterStub struct{}

func (settingsMCPTesterStub) TestMCP(context.Context, RuntimeMCPConfig) (MCPConnectionTestResult, error) {
	return MCPConnectionTestResult{Success: true}, nil
}

func TestUpdateSettingsPreservesActiveAgentConfig(t *testing.T) {
	repository := &settingsRepositoryStub{activeQAConfigVersion: QAConfigVersion{
		ID: "qa-config-id",
		Agent: AgentConfig{
			MaxIterations:         8,
			ToolTimeoutSeconds:    11,
			ModelTimeoutSeconds:   70,
			OverallTimeoutSeconds: 150,
			EnabledToolNames:      []string{"search_knowledge", "get_citation_source"},
		},
	}}
	settings, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = settings.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{
		Retrieval: &RetrievalSettings{TopK: 6, ScoreThreshold: .6, RerankThreshold: .4, RerankTopN: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !repository.createCalled {
		t.Fatal("CreateQAConfigVersion was not called")
	}
	if !reflect.DeepEqual(repository.createdAgent, repository.activeQAConfigVersion.Agent) {
		t.Fatalf("agent=%+v, want %+v", repository.createdAgent, repository.activeQAConfigVersion.Agent)
	}
}

func TestUpdateSettingsBootstrapsAgentConfigWhenActiveConfigMissing(t *testing.T) {
	repository := &settingsRepositoryStub{activeQAConfigErr: NewError(CodeNotFound, "QA configuration not found", errors.New("no rows"))}
	settings, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	ids := []string{"kb-new"}
	_, err = settings.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{DefaultKnowledgeBaseIDs: &ids})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(repository.createdAgent, DefaultAgentConfig()) {
		t.Fatalf("agent=%+v, want default %+v", repository.createdAgent, DefaultAgentConfig())
	}
}

func TestValidateRuntimeMCPAllowsStreamableHTTP(t *testing.T) {
	err := validateRuntimeMCP(RuntimeMCPConfig{
		Alias:       "echo_test",
		Transport:   "streamable_http",
		EndpointURL: "https://mcp.example.test/mcp",
		TokenHeader: "Authorization",
		ToolTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("validateRuntimeMCP returned error: %v", err)
	}
}

func TestValidateRuntimeMCPRejectsStdioTransport(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "old exact test spec", command: "go", args: []string{"run", "./testserver/cmd/echo"}},
		{name: "shell", command: "sh", args: []string{"-c", "echo unsafe"}},
		{name: "path", command: "/usr/bin/go", args: []string{"run", "./testserver/cmd/echo"}},
		{name: "wrong args", command: "go", args: []string{"run", "./other"}},
		{name: "unsafe args", command: "go", args: []string{"run", "./testserver/cmd/echo\n--flag"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeMCP(RuntimeMCPConfig{
				Alias:       "echo_test",
				Transport:   "stdio",
				Command:     tt.command,
				Args:        tt.args,
				TokenHeader: "Authorization",
				ToolTimeout: time.Second,
			})
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Code != CodeValidation || appErr.Fields["transport"] == "" {
				t.Fatalf("expected transport validation error, got %v", err)
			}
		})
	}
}

func TestLoadRuntimeConfigurationMergesBootstrapTokenIntoMatchingMCPRecord(t *testing.T) {
	repository := &settingsRepositoryStub{mcpServers: []MCPServerRecord{{
		ID: "document-db", Alias: "document", Transport: "streamable_http",
		EndpointURL: "http://localhost:8085/mcp", TokenHeader: "Authorization",
		ToolTimeoutSeconds: 30, Enabled: true,
	}}}
	bootstrap := RuntimeMCPConfig{
		Alias: "document", Transport: "streamable_http", EndpointURL: "http://bootstrap.invalid/mcp",
		Token: "environment-token", TokenHeader: "Authorization", ToolTimeout: 30 * time.Second,
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{MCPServer: &bootstrap}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := svc.LoadRuntimeConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.MCPServers) != 1 {
		t.Fatalf("MCPServers=%+v, want one merged server", runtime.MCPServers)
	}
	server := runtime.MCPServers[0]
	if server.ID != "document-db" || server.EndpointURL != "http://localhost:8085/mcp" || server.Token != "environment-token" {
		t.Fatalf("merged server=%+v", server)
	}
}

func TestLoadRuntimeConfigurationKeepsStoredMCPToken(t *testing.T) {
	repository := &settingsRepositoryStub{mcpServers: []MCPServerRecord{{
		ID: "document-db", Alias: "document", Transport: "streamable_http",
		EndpointURL: "http://localhost:8085/mcp", TokenEncrypted: []byte("stored-token"),
		TokenHeader: "Authorization", ToolTimeoutSeconds: 30, Enabled: true,
	}}}
	bootstrap := RuntimeMCPConfig{Alias: "document", Token: "environment-token"}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{MCPServer: &bootstrap}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := svc.LoadRuntimeConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.MCPServers) != 1 || runtime.MCPServers[0].Token != "stored-token" {
		t.Fatalf("MCPServers=%+v, want stored token to win", runtime.MCPServers)
	}
}

func TestLoadRuntimeConfigurationDoesNotReenableDisabledBootstrapAlias(t *testing.T) {
	repository := &settingsRepositoryStub{mcpServers: []MCPServerRecord{{
		ID: "document-db", Alias: "document", Transport: "streamable_http",
		EndpointURL: "http://localhost:8085/mcp", TokenHeader: "Authorization",
		ToolTimeoutSeconds: 30, Enabled: false,
	}}}
	bootstrap := RuntimeMCPConfig{Alias: "document", Token: "environment-token"}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{MCPServer: &bootstrap}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := svc.LoadRuntimeConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.MCPServers) != 0 {
		t.Fatalf("MCPServers=%+v, want disabled database record to suppress bootstrap", runtime.MCPServers)
	}
}

func TestLoadRuntimeConfigurationAppendsBootstrapWhenOtherMCPRecordsExist(t *testing.T) {
	repository := &settingsRepositoryStub{mcpServers: []MCPServerRecord{{
		ID: "other-db", Alias: "other_mcp", Transport: "streamable_http",
		EndpointURL: "http://localhost:8090/mcp", TokenHeader: "Authorization",
		ToolTimeoutSeconds: 30, Enabled: true,
	}}}
	bootstrap := RuntimeMCPConfig{
		Alias: "document", Transport: "streamable_http", EndpointURL: "http://localhost:8085/mcp",
		Token: "environment-token", TokenHeader: "Authorization", ToolTimeout: 30 * time.Second,
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{MCPServer: &bootstrap}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := svc.LoadRuntimeConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.MCPServers) != 2 || runtime.MCPServers[1].Alias != "document" {
		t.Fatalf("MCPServers=%+v, want unrelated database server plus bootstrap", runtime.MCPServers)
	}
}

func TestLoadRuntimeConfigurationKeepsEmptyDefaultKnowledgeBaseScope(t *testing.T) {
	emptyKBIDs := []string{}
	repository := &settingsRepositoryStub{
		activeQAConfigVersion:         QAConfigVersion{ID: "qa-config"},
		activeDefaultKnowledgeBaseIDs: &emptyKBIDs,
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := svc.LoadRuntimeConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.DefaultKnowledgeBaseIDs == nil {
		t.Fatal("default knowledge base IDs should be an empty configured scope, not nil")
	}
	if len(runtime.DefaultKnowledgeBaseIDs) != 0 {
		t.Fatalf("default knowledge base IDs=%+v, want empty scope", runtime.DefaultKnowledgeBaseIDs)
	}
}

func TestValidateRuntimeLLMRejectsDirectProviderEscape(t *testing.T) {
	err := validateRuntimeLLM(RuntimeLLMConfig{
		Endpoint:    "http://169.254.169.254/latest/meta-data",
		Token:       "token",
		TokenHeader: "Authorization",
		Model:       "deepseek-chat",
		Timeout:     time.Second,
		MaxTokens:   100,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != CodeValidation || appErr.Fields["llm.apiEndpoint"] == "" {
		t.Fatalf("expected endpoint validation error, got %v", err)
	}
}

func TestTestLLMConnectionRejectsStoredDirectProviderEscape(t *testing.T) {
	tester := &settingsLLMTesterStub{}
	svc, err := NewConfigService(&settingsRepositoryStub{
		activeLLM: StoredLLMConfig{
			Provider:        "direct",
			APIEndpoint:     "http://169.254.169.254/latest/meta-data",
			APIKeyEncrypted: []byte("token"),
			TokenHeader:     "Authorization",
			Model:           "deepseek-chat",
			TimeoutSeconds:  30,
			MaxTokens:       100,
		},
	}, settingsCipherStub{}, BootstrapSettings{}, tester, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.TestLLMConnection(context.Background(), LLMConnectionTestInput{})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != CodeValidation || appErr.Fields["llm.apiEndpoint"] == "" {
		t.Fatalf("expected endpoint validation error, got %v", err)
	}
	if tester.called {
		t.Fatal("LLM tester was called for unsafe stored endpoint")
	}
}

func TestRuntimePromptFromActiveQAConfigVersion(t *testing.T) {
	repository := &settingsRepositoryStub{
		activeQAConfigVersion: QAConfigVersion{
			ID:           "qa-config-v2",
			SystemPrompt: "You are a test QA agent.",
		},
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{SystemPrompt: "bootstrap prompt"}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := svc.runtimePrompt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "You are a test QA agent." {
		t.Fatalf("prompt=%q, want %q", prompt, "You are a test QA agent.")
	}
}

func TestRuntimePromptFallsBackToBootstrapWhenConfigMissing(t *testing.T) {
	repository := &settingsRepositoryStub{
		activeQAConfigErr: NewError(CodeNotFound, "QA configuration not found", errors.New("no rows")),
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{SystemPrompt: "bootstrap prompt"}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := svc.runtimePrompt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "bootstrap prompt" {
		t.Fatalf("prompt=%q, want %q", prompt, "bootstrap prompt")
	}
}

func TestRuntimePromptFallsBackToBootstrapWhenConfigHasEmptyPrompt(t *testing.T) {
	repository := &settingsRepositoryStub{
		activeQAConfigVersion: QAConfigVersion{
			ID:           "qa-config-v1",
			SystemPrompt: "",
		},
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{SystemPrompt: "bootstrap fallback"}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := svc.runtimePrompt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "bootstrap fallback" {
		t.Fatalf("prompt=%q, want bootstrap fallback", prompt)
	}
}

func TestUpdateSettingsCreatesQAConfigVersionForPromptChange(t *testing.T) {
	repository := &settingsRepositoryStub{
		activeQAConfigVersion: QAConfigVersion{
			ID: "qa-config-v1",
			Agent: AgentConfig{
				MaxIterations: 5, ToolTimeoutSeconds: 10, ModelTimeoutSeconds: 60,
				OverallTimeoutSeconds: 120, EnabledToolNames: []string{"search_knowledge"},
			},
			SystemPrompt: "Old system prompt.",
		},
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	newPrompt := "New versioned system prompt."
	_, err = svc.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{
		SystemPrompt: &newPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !repository.createCalled {
		t.Fatal("CreateQAConfigVersion was not called for prompt change")
	}
}

func TestUpdateSettingsRejectsEmptyPrompt(t *testing.T) {
	repository := &settingsRepositoryStub{}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	prompt := "   "
	_, err = svc.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{
		SystemPrompt: &prompt,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != CodeValidation || appErr.Fields["systemPrompt"] == "" {
		t.Fatalf("expected validation error for empty prompt, got %v", err)
	}
}

func TestUpdateSettingsRejectsOversizedPrompt(t *testing.T) {
	repository := &settingsRepositoryStub{}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	large := make([]byte, 20001)
	for i := range large {
		large[i] = 'x'
	}
	prompt := string(large)
	_, err = svc.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{
		SystemPrompt: &prompt,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != CodeValidation || appErr.Fields["systemPrompt"] == "" {
		t.Fatalf("expected validation error for oversized prompt, got %v", err)
	}
}

func TestUpdateSettingsMergesRetrievalAndPromptIntoSingleVersion(t *testing.T) {
	repository := &settingsRepositoryStub{
		activeQAConfigVersion: QAConfigVersion{
			ID: "qa-config-v1",
			Agent: AgentConfig{
				MaxIterations: 5, ToolTimeoutSeconds: 10, ModelTimeoutSeconds: 60,
				OverallTimeoutSeconds: 120, EnabledToolNames: []string{"search_knowledge"},
			},
			SystemPrompt: "Old prompt.",
		},
	}
	svc, err := NewConfigService(repository, settingsCipherStub{}, BootstrapSettings{}, &settingsLLMTesterStub{}, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}

	newPrompt := "New merged prompt."
	_, err = svc.UpdateSettings(context.Background(), "user-1", "request-1", UpdateQASettingsInput{
		Retrieval:    &RetrievalSettings{TopK: 10, ScoreThreshold: .8, RerankThreshold: .3, RerankTopN: 5},
		SystemPrompt: &newPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}

	if repository.createCount != 1 {
		t.Fatalf("createCount=%d, want 1 (single merged version)", repository.createCount)
	}
	if repository.createdRetrieval.TopK != 10 {
		t.Fatalf("retrieval.TopK=%d, want %d", repository.createdRetrieval.TopK, 10)
	}
	if repository.createdPrompt != newPrompt {
		t.Fatalf("prompt=%q, want %q", repository.createdPrompt, newPrompt)
	}
}

func TestSettingsAuditDataDoesNotLeakFullPrompt(t *testing.T) {
	settings := QASettings{
		SystemPrompt: "This is a secret system prompt that must not appear in audit logs.",
	}
	data := settingsAuditData(settings)
	if _, hasPrompt := data["systemPrompt"]; hasPrompt {
		t.Fatal("audit data must not contain full systemPrompt text")
	}
	length, ok := data["systemPromptLength"].(int)
	if !ok || length != len(settings.SystemPrompt) {
		t.Fatalf("audit data systemPromptLength=%v, want %d", data["systemPromptLength"], len(settings.SystemPrompt))
	}
}

func TestTestLLMConnectionUsesTrustedStoredEndpoint(t *testing.T) {
	tester := &settingsLLMTesterStub{}
	svc, err := NewConfigService(&settingsRepositoryStub{
		activeLLM: StoredLLMConfig{
			Provider:        "direct",
			APIEndpoint:     "http://ai-gateway:8086/internal/v1/chat/completions",
			APIKeyEncrypted: []byte("token"),
			TokenHeader:     "X-Service-Token",
			Model:           "deepseek-chat",
			TimeoutSeconds:  30,
			MaxTokens:       100,
		},
	}, settingsCipherStub{}, BootstrapSettings{}, tester, settingsMCPTesterStub{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.TestLLMConnection(context.Background(), LLMConnectionTestInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !tester.called || result.Model != "deepseek-chat" || tester.seen.Endpoint != "http://ai-gateway:8086/internal/v1/chat/completions" {
		t.Fatalf("unexpected tester state result=%+v seen=%+v called=%v", result, tester.seen, tester.called)
	}
}

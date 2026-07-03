package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/localtools"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/mcpclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/modelclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/toolclient"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
	toolspkg "github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/tools"
)

type ManagerConfig struct {
	WorkDir                 string
	MaxFileBytes            int
	MaxToolResultBytes      int
	EnableCommandTool       bool
	CommandTimeout          time.Duration
	MaxIterations           int
	DefaultToolTimeout      time.Duration
	KnowledgeMCPURL         string
	KnowledgeMCPToken       string
	KnowledgeMCPTokenHeader string
	KnowledgeMCPAlias       string
	KnowledgeMCPTimeout     time.Duration
}

type runtimeState struct {
	runner                  *agent.Runner
	prompt                  string
	llmModel                string
	llmProfileID            string
	qaConfigVersionID       string
	llmConfigVersionID      string
	maxIterations           int
	overallTimeout          time.Duration
	clients                 []*mcpclient.Client
	defaultKnowledgeBaseIDs []string
	retrievalSettings       service.RetrievalSettings
}

type Manager struct {
	stateMu     sync.RWMutex
	reloadMu    sync.Mutex
	state       *runtimeState
	loader      service.RuntimeConfigLoader
	status      service.MCPStatusUpdater
	cfg         ManagerConfig
	retriever   service.KnowledgeRetriever
	attachments service.SessionAttachmentSearcher
}

func NewManager(ctx context.Context, loader service.RuntimeConfigLoader, status service.MCPStatusUpdater, retriever service.KnowledgeRetriever, attachments service.SessionAttachmentSearcher, cfg ManagerConfig) (*Manager, error) {
	if loader == nil || status == nil {
		return nil, errors.New("runtime config loader and MCP status updater are required")
	}
	manager := &Manager{loader: loader, status: status, retriever: retriever, attachments: attachments, cfg: cfg}
	if err := manager.Reload(ctx); err != nil {
		return nil, err
	}
	return manager, nil
}

// Acquire keeps a read lock until release is called. Reload waits for all
// acquired snapshots before closing their MCP sessions and swapping runtime.
func (m *Manager) Acquire() (service.RuntimeSnapshot, func(), error) {
	m.stateMu.RLock()
	if m.state == nil || m.state.runner == nil {
		m.stateMu.RUnlock()
		return service.RuntimeSnapshot{}, func() {}, errors.New("agent runtime is not initialized")
	}
	return service.RuntimeSnapshot{
		Runner: m.state.runner, SystemPrompt: m.state.prompt,
		LLMModel: m.state.llmModel, LLMProfileID: m.state.llmProfileID,
		QAConfigVersionID: m.state.qaConfigVersionID, LLMConfigVersionID: m.state.llmConfigVersionID,
		MaxIterations: m.state.maxIterations, OverallTimeout: m.state.overallTimeout,
		DefaultKnowledgeBaseIDs: m.state.defaultKnowledgeBaseIDs,
		RetrievalSettings:       m.state.retrievalSettings,
	}, m.stateMu.RUnlock, nil
}

func (m *Manager) Reload(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	runtimeConfig, err := m.loader.LoadRuntimeConfiguration(ctx)
	if err != nil {
		return fmt.Errorf("load runtime configuration: %w", err)
	}
	newState, err := m.buildState(ctx, runtimeConfig)
	if err != nil {
		return err
	}

	m.stateMu.Lock()
	oldState := m.state
	m.state = newState
	m.stateMu.Unlock()
	closeRuntimeState(oldState)
	return nil
}

func (m *Manager) Close() error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.stateMu.Lock()
	state := m.state
	m.state = nil
	m.stateMu.Unlock()
	return closeRuntimeState(state)
}

type knowledgeRetrieverAdapter struct {
	retriever service.KnowledgeRetriever
}

func (a *knowledgeRetrieverAdapter) Retrieve(ctx context.Context, userID string, input toolspkg.RetrievalTestInput) ([]toolspkg.RetrievalTestResult, error) {
	serviceInput := service.RetrievalTestInput{
		Question:         input.Question,
		KnowledgeBaseIDs: input.KnowledgeBaseIDs,
		Retrieval: service.RetrievalSettings{
			TopK:            input.Retrieval.TopK,
			ScoreThreshold:  input.Retrieval.ScoreThreshold,
			EnableRerank:    input.Retrieval.EnableRerank,
			RerankThreshold: input.Retrieval.RerankThreshold,
			RerankTopN:      input.Retrieval.RerankTopN,
		},
	}
	if input.Retrieval.ScoreThresholdConfigured {
		serviceInput.Retrieval = serviceInput.Retrieval.WithScoreThresholdConfigured()
	}
	serviceResults, err := a.retriever.Retrieve(ctx, userID, serviceInput)
	if err != nil {
		return nil, err
	}
	results := make([]toolspkg.RetrievalTestResult, 0, len(serviceResults))
	for _, r := range serviceResults {
		results = append(results, toolspkg.RetrievalTestResult{
			RankNo:          r.RankNo,
			KnowledgeBaseID: r.KnowledgeBaseID,
			DocumentID:      r.DocumentID,
			DocumentName:    r.DocumentName,
			ChunkID:         r.ChunkID,
			SectionPath:     r.SectionPath,
			ContentPreview:  r.ContentPreview,
			Score:           r.Score,
			RerankScore:     r.RerankScore,
			Metadata:        r.Metadata,
		})
	}
	return results, nil
}

type sessionAttachmentSearcherAdapter struct {
	searcher service.SessionAttachmentSearcher
}

func (a *sessionAttachmentSearcherAdapter) SearchSessionAttachments(ctx context.Context, userID, sessionID string, attachmentIDs []string, query string, limit int) ([]toolspkg.SessionAttachmentHit, error) {
	if a.searcher == nil {
		return nil, errors.New("session attachment searcher is unavailable")
	}
	chunks, err := a.searcher.SearchSessionAttachments(ctx, userID, sessionID, attachmentIDs, query, limit)
	if err != nil {
		return nil, err
	}
	return mapSessionAttachmentHits(chunks), nil
}

func (a *sessionAttachmentSearcherAdapter) ListSessionAttachmentReportSource(ctx context.Context, userID, sessionID string, attachmentIDs []string, limit int) ([]toolspkg.SessionAttachmentHit, error) {
	if a.searcher == nil {
		return nil, errors.New("session attachment searcher is unavailable")
	}
	chunks, err := a.searcher.ListSessionAttachmentReportSource(ctx, userID, sessionID, attachmentIDs, limit)
	if err != nil {
		return nil, err
	}
	return mapSessionAttachmentHits(chunks), nil
}

func mapSessionAttachmentHits(chunks []service.SessionAttachmentChunk) []toolspkg.SessionAttachmentHit {
	results := make([]toolspkg.SessionAttachmentHit, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, toolspkg.SessionAttachmentHit{
			AttachmentID:   chunk.AttachmentID,
			ChunkID:        chunk.ID,
			Filename:       chunk.Filename,
			SectionPath:    chunk.SectionPath,
			Content:        chunk.Content,
			ContentPreview: chunk.ContentPreview,
			PageNumber:     chunk.PageNumber,
			ChunkIndex:     chunk.ChunkIndex,
		})
	}
	return results
}

type policyToolClient struct {
	tools             agent.ToolClient
	policy            *toolspkg.Policy
	knowledgeMCPAlias string
	cachedDefs        []agent.ToolDefinition
	cachedOnce        sync.Once
	cachedErr         error
}

func (p *policyToolClient) ListTools(ctx context.Context) ([]agent.ToolDefinition, error) {
	definitions, err := p.getToolDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	return p.policy.FilterTools(definitions), nil
}

func (p *policyToolClient) getToolDefinitions(ctx context.Context) ([]agent.ToolDefinition, error) {
	p.cachedOnce.Do(func() {
		definitions, err := p.tools.ListTools(ctx)
		if err != nil {
			p.cachedErr = err
			return
		}
		p.cachedDefs = definitions
	})
	return p.cachedDefs, p.cachedErr
}

func (p *policyToolClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (agent.ToolResult, error) {
	definitions, err := p.getToolDefinitions(ctx)
	if err != nil {
		return agent.ToolResult{}, err
	}
	var toolDef agent.ToolDefinition
	for _, def := range definitions {
		if def.Function.Name == name {
			toolDef = def
			break
		}
	}
	if !p.policy.IsAllowed(name) {
		return agent.ToolResult{}, fmt.Errorf("tool %q is not in the enabled whitelist", name)
	}
	guardedArguments, guardResult, err := p.guardKnowledgeMCPCall(ctx, name, arguments)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if guardResult != nil {
		return *guardResult, nil
	}
	arguments = guardedArguments
	if err := p.policy.ValidateCall(name, arguments, toolDef); err != nil {
		return agent.ToolResult{}, err
	}
	return p.tools.CallTool(ctx, name, arguments)
}

func (p *policyToolClient) guardKnowledgeMCPCall(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, *agent.ToolResult, error) {
	switch {
	case p.isKnowledgeMCPTool(name, toolspkg.KnowledgeMCPToolSearch):
		return guardKnowledgeMCPSearch(ctx, arguments)
	case p.isKnowledgeMCPTool(name, toolspkg.KnowledgeMCPToolListDocuments):
		return guardKnowledgeMCPListDocuments(ctx, arguments)
	default:
		return arguments, nil, nil
	}
}

func (p *policyToolClient) isKnowledgeMCPTool(name string, toolName string) bool {
	alias := strings.TrimSpace(p.knowledgeMCPAlias)
	return alias != "" && name == alias+"__"+toolName
}

func guardKnowledgeMCPSearch(ctx context.Context, arguments json.RawMessage) (json.RawMessage, *agent.ToolResult, error) {
	payload, err := decodeJSONObject(arguments)
	if err != nil {
		return nil, nil, err
	}
	requestKBIDs := normalizeKnowledgeMCPIDs(contextutil.KnowledgeBaseIDsFromContext(ctx))
	defaultKBIDs := normalizeKnowledgeMCPIDs(contextutil.DefaultKnowledgeBaseIDsFromContext(ctx))
	hasDefaultKBIDs := contextutil.DefaultKnowledgeBaseIDsFromContext(ctx) != nil

	if len(requestKBIDs) > 0 {
		if hasDefaultKBIDs && len(defaultKBIDs) > 0 && !knowledgeMCPIDsSubset(requestKBIDs, defaultKBIDs) {
			result := knowledgeMCPToolFailure("unauthorized_knowledge_bases", "one or more requested knowledge bases are not accessible")
			return arguments, &result, nil
		}
		payload["knowledgeBaseIds"] = requestKBIDs
		return marshalKnowledgeMCPArguments(payload)
	}
	if hasDefaultKBIDs && len(defaultKBIDs) > 0 {
		if value, exists := payload["knowledgeBaseIds"]; exists {
			provided, ok := knowledgeMCPStringSlice(value)
			if !ok {
				return arguments, nil, nil
			}
			if len(provided) == 0 {
				payload["knowledgeBaseIds"] = defaultKBIDs
				return marshalKnowledgeMCPArguments(payload)
			}
			if !knowledgeMCPIDsSubset(provided, defaultKBIDs) {
				result := knowledgeMCPToolFailure("unauthorized_knowledge_bases", "one or more requested knowledge bases are not accessible")
				return arguments, &result, nil
			}
			payload["knowledgeBaseIds"] = normalizeKnowledgeMCPIDs(provided)
			return marshalKnowledgeMCPArguments(payload)
		}
		payload["knowledgeBaseIds"] = defaultKBIDs
		return marshalKnowledgeMCPArguments(payload)
	}
	return arguments, nil, nil
}

func guardKnowledgeMCPListDocuments(ctx context.Context, arguments json.RawMessage) (json.RawMessage, *agent.ToolResult, error) {
	payload, err := decodeJSONObject(arguments)
	if err != nil {
		return nil, nil, err
	}
	requestKBIDs := normalizeKnowledgeMCPIDs(contextutil.KnowledgeBaseIDsFromContext(ctx))
	defaultKBIDs := normalizeKnowledgeMCPIDs(contextutil.DefaultKnowledgeBaseIDsFromContext(ctx))
	hasDefaultKBIDs := contextutil.DefaultKnowledgeBaseIDsFromContext(ctx) != nil

	var allowed []string
	switch {
	case len(requestKBIDs) > 0:
		if hasDefaultKBIDs && len(defaultKBIDs) > 0 && !knowledgeMCPIDsSubset(requestKBIDs, defaultKBIDs) {
			result := knowledgeMCPToolFailure("unauthorized_knowledge_bases", "one or more requested knowledge bases are not accessible")
			return arguments, &result, nil
		}
		allowed = requestKBIDs
	case hasDefaultKBIDs && len(defaultKBIDs) > 0:
		allowed = defaultKBIDs
	default:
		return arguments, nil, nil
	}
	target, _ := payload["knowledgeBaseId"].(string)
	target = strings.TrimSpace(target)
	if target == "" {
		if len(allowed) != 1 {
			result := knowledgeMCPToolFailure("invalid_arguments", "knowledgeBaseId is required when multiple knowledge bases are available")
			return arguments, &result, nil
		}
		target = allowed[0]
	}
	if !knowledgeMCPIDsSubset([]string{target}, allowed) {
		result := knowledgeMCPToolFailure("unauthorized_knowledge_bases", "one or more requested knowledge bases are not accessible")
		return arguments, &result, nil
	}
	payload["knowledgeBaseId"] = target
	return marshalKnowledgeMCPArguments(payload)
}

func decodeJSONObject(arguments json.RawMessage) (map[string]any, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	var payload map[string]any
	if err := json.Unmarshal(arguments, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func marshalKnowledgeMCPArguments(payload map[string]any) (json.RawMessage, *agent.ToolResult, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	return json.RawMessage(encoded), nil, nil
}

func knowledgeMCPStringSlice(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		id, ok := item.(string)
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return normalizeKnowledgeMCPIDs(ids), true
}

func normalizeKnowledgeMCPIDs(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	seen := make(map[string]struct{}, len(ids))
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func knowledgeMCPIDsSubset(ids []string, allowed []string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = struct{}{}
	}
	for _, id := range normalizeKnowledgeMCPIDs(ids) {
		if _, ok := allowedSet[id]; !ok {
			return false
		}
	}
	return true
}

func knowledgeMCPToolFailure(code, message string) agent.ToolResult {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
	return agent.ToolResult{Content: string(payload), IsError: true}
}

func (m *Manager) buildState(ctx context.Context, runtimeConfig service.RuntimeConfiguration) (*runtimeState, error) {
	local, err := localtools.New(localtools.Config{
		WorkDir: m.cfg.WorkDir, MaxFileBytes: m.cfg.MaxFileBytes,
		MaxOutputBytes: m.cfg.MaxToolResultBytes, EnableCommandTool: m.cfg.EnableCommandTool,
		CommandTimeout: m.cfg.CommandTimeout,
	})
	if err != nil {
		return nil, err
	}
	providers := []agent.ToolClient{local}
	clients := make([]*mcpclient.Client, 0, len(runtimeConfig.MCPServers))
	for _, server := range runtimeConfig.MCPServers {
		client, connectErr := mcpclient.Connect(ctx, mcpclient.Config{
			Transport: server.Transport, Command: server.Command, Args: server.Args,
			Endpoint: server.EndpointURL, Token: server.Token, TokenHeader: server.TokenHeader,
		})
		if connectErr != nil {
			m.updateMCPStatus(ctx, server.ID, 0, nil, "connection failed")
			continue
		}
		mcpTools, listErr := client.ListTools(ctx)
		if listErr != nil {
			_ = client.Close()
			m.updateMCPStatus(ctx, server.ID, 0, nil, "tool discovery failed")
			continue
		}
		prefixed, prefixErr := mcpclient.NewPrefixed(server.Alias, client, server.ToolTimeout)
		if prefixErr != nil {
			_ = client.Close()
			m.updateMCPStatus(ctx, server.ID, 0, nil, "tool prefix is invalid")
			continue
		}
		clients = append(clients, client)
		providers = append(providers, prefixed)
		now := time.Now().UTC()
		m.updateMCPStatus(ctx, server.ID, len(mcpTools), &now, "")
	}
	knowledgeProvider, knowledgeClient, err := m.buildKnowledgeProvider(ctx)
	if err != nil {
		closeClients(clients)
		return nil, err
	}
	if knowledgeProvider != nil {
		providers = append(providers, knowledgeProvider)
	}
	if knowledgeClient != nil {
		clients = append(clients, knowledgeClient)
	}
	if m.attachments != nil {
		attachmentTool, err := toolspkg.NewAttachmentToolClient(toolspkg.AttachmentToolConfig{
			Searcher: &sessionAttachmentSearcherAdapter{searcher: m.attachments},
			Timeout:  m.cfg.DefaultToolTimeout,
		})
		if err != nil {
			closeClients(clients)
			return nil, fmt.Errorf("init attachment tool client: %w", err)
		}
		providers = append(providers, attachmentTool)
	}
	toolClient, err := toolclient.New(providers...)
	if err != nil {
		closeClients(clients)
		return nil, err
	}
	if _, err := toolClient.ListTools(ctx); err != nil {
		closeClients(clients)
		return nil, fmt.Errorf("validate merged tools: %w", err)
	}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{
		EnabledToolNames: runtimeConfig.Agent.EnabledToolNames,
	})
	if err != nil {
		closeClients(clients)
		return nil, fmt.Errorf("create tool policy: %w", err)
	}
	policyClient := &policyToolClient{tools: toolClient, policy: policy, knowledgeMCPAlias: m.cfg.KnowledgeMCPAlias}
	model, err := modelclient.New(modelclient.Config{
		Endpoint: runtimeConfig.LLM.Endpoint, Token: runtimeConfig.LLM.Token,
		TokenHeader: runtimeConfig.LLM.TokenHeader, Model: runtimeConfig.LLM.Model,
		ProfileID: runtimeConfig.LLM.ProfileID, MaxTokens: runtimeConfig.LLM.MaxTokens,
		Timeout: runtimeConfig.LLM.Timeout, Stream: runtimeConfig.LLM.Stream,
	})
	if err != nil {
		closeClients(clients)
		return nil, err
	}
	toolTimeout := m.cfg.DefaultToolTimeout
	if runtimeConfig.Agent.ToolTimeoutSeconds > 0 {
		toolTimeout = time.Duration(runtimeConfig.Agent.ToolTimeoutSeconds) * time.Second
	}
	if toolTimeout <= 0 {
		toolTimeout = 30 * time.Second
	}
	maxIterations := runtimeConfig.Agent.MaxIterations
	if maxIterations <= 0 {
		maxIterations = m.cfg.MaxIterations
	}
	if maxIterations > 10 {
		maxIterations = 10
	}
	overallTimeout := time.Duration(runtimeConfig.Agent.OverallTimeoutSeconds) * time.Second
	runner, err := agent.NewRunner(model, policyClient, agent.Config{
		MaxIterations:          maxIterations,
		ToolTimeout:            toolTimeout,
		MaxToolResultBytes:     m.cfg.MaxToolResultBytes,
		ReasoningFilterFactory: service.NewReasoningFilter,
	})
	if err != nil {
		closeClients(clients)
		return nil, err
	}
	return &runtimeState{
		runner: runner, prompt: runtimeConfig.SystemPrompt, clients: clients,
		llmModel: runtimeConfig.LLM.Model, llmProfileID: runtimeConfig.LLM.ProfileID,
		qaConfigVersionID: runtimeConfig.QAConfigVersionID, llmConfigVersionID: runtimeConfig.LLMConfigVersionID,
		maxIterations: maxIterations, overallTimeout: overallTimeout,
		defaultKnowledgeBaseIDs: runtimeConfig.DefaultKnowledgeBaseIDs,
		retrievalSettings:       runtimeConfig.RetrievalSettings,
	}, nil
}

func (m *Manager) buildKnowledgeProvider(ctx context.Context) (agent.ToolClient, *mcpclient.Client, error) {
	if m.cfg.KnowledgeMCPURL != "" {
		client, connectErr := mcpclient.Connect(ctx, mcpclient.Config{
			Transport: mcpclient.TransportStreamableHTTP,
			Endpoint:  m.cfg.KnowledgeMCPURL, Token: m.cfg.KnowledgeMCPToken,
			TokenHeader: m.cfg.KnowledgeMCPTokenHeader,
		})
		if connectErr == nil {
			prefixed, prefixErr := mcpclient.NewPrefixed(m.cfg.KnowledgeMCPAlias, client, m.cfg.KnowledgeMCPTimeout)
			if prefixErr == nil {
				definitions, listErr := prefixed.ListTools(ctx)
				if listErr == nil && hasRequiredKnowledgeMCPTools(definitions, m.cfg.KnowledgeMCPAlias) {
					return prefixed, client, nil
				}
			}
			_ = client.Close()
		}
	}
	if m.retriever == nil {
		return nil, nil, nil
	}
	knowledgeTool, err := toolspkg.NewKnowledgeToolClient(toolspkg.KnowledgeToolConfig{
		RetrievalClient: &knowledgeRetrieverAdapter{retriever: m.retriever},
		Timeout:         m.cfg.DefaultToolTimeout,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("init knowledge tool client: %w", err)
	}
	return knowledgeTool, nil, nil
}

func hasRequiredKnowledgeMCPTools(definitions []agent.ToolDefinition, alias string) bool {
	wanted := make(map[string]struct{}, len(toolspkg.DefaultKnowledgeMCPToolNames))
	for _, name := range toolspkg.DefaultKnowledgeMCPToolNames {
		wanted[alias+"__"+name] = struct{}{}
	}
	for _, definition := range definitions {
		delete(wanted, definition.Function.Name)
	}
	return len(wanted) == 0
}

func (m *Manager) updateMCPStatus(ctx context.Context, id string, toolCount int, connectedAt *time.Time, lastError string) {
	if id == "" {
		return
	}
	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = m.status.UpdateMCPConnectionStatus(statusCtx, id, toolCount, connectedAt, lastError)
}

func closeRuntimeState(state *runtimeState) error {
	if state == nil {
		return nil
	}
	return closeClients(state.clients)
}

func closeClients(clients []*mcpclient.Client) error {
	var combined error
	for _, client := range clients {
		combined = errors.Join(combined, client.Close())
	}
	return combined
}

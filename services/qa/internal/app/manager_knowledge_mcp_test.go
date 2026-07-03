package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
	toolspkg "github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHasRequiredKnowledgeMCPTools(t *testing.T) {
	definitions := []agent.ToolDefinition{
		{Function: agent.FunctionTool{Name: "knowledge__search"}},
		{Function: agent.FunctionTool{Name: "knowledge__list_documents"}},
		{Function: agent.FunctionTool{Name: "knowledge__get_document"}},
		{Function: agent.FunctionTool{Name: "knowledge__get_chunk"}},
	}
	if !hasRequiredKnowledgeMCPTools(definitions, "knowledge") {
		t.Fatal("expected the complete Knowledge MCP tool set to be accepted")
	}
	if hasRequiredKnowledgeMCPTools(definitions[:3], "knowledge") {
		t.Fatal("expected an incomplete Knowledge MCP tool set to be rejected for HTTP fallback")
	}
	if hasRequiredKnowledgeMCPTools(definitions, "other") {
		t.Fatal("expected alias mismatch to be rejected")
	}
}

func TestBuildKnowledgeProviderPrefersCompleteMCPDiscovery(t *testing.T) {
	server := newKnowledgeMCPTestServer(t, toolspkg.DefaultKnowledgeMCPToolNames)
	manager := &Manager{
		retriever: &fakeKnowledgeRetriever{},
		cfg: ManagerConfig{
			KnowledgeMCPURL: server.URL, KnowledgeMCPAlias: "knowledge",
			KnowledgeMCPTimeout: time.Second, DefaultToolTimeout: time.Second,
		},
	}
	provider, client, err := manager.buildKnowledgeProvider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("expected a connected Knowledge MCP client")
	}
	defer client.Close()
	definitions, err := provider.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasRequiredKnowledgeMCPTools(definitions, "knowledge") || hasTool(definitions, toolspkg.ToolSearchKnowledge) {
		t.Fatalf("MCP definitions = %#v", definitions)
	}
}

func TestBuildKnowledgeProviderFallsBackWhenDiscoveryIsIncomplete(t *testing.T) {
	server := newKnowledgeMCPTestServer(t, toolspkg.DefaultKnowledgeMCPToolNames[:3])
	manager := &Manager{
		retriever: &fakeKnowledgeRetriever{},
		cfg: ManagerConfig{
			KnowledgeMCPURL: server.URL, KnowledgeMCPAlias: "knowledge",
			KnowledgeMCPTimeout: time.Second, DefaultToolTimeout: time.Second,
		},
	}
	provider, client, err := manager.buildKnowledgeProvider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if client != nil {
		defer client.Close()
		t.Fatal("incomplete discovery must close the MCP client")
	}
	definitions, err := provider.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hasTool(definitions, toolspkg.ToolSearchKnowledge) || hasTool(definitions, "knowledge__search") {
		t.Fatalf("fallback definitions = %#v", definitions)
	}
}

func TestPolicyToolClientInjectsDefaultKnowledgeBaseIDsForKnowledgeMCPSearch(t *testing.T) {
	recorder := &recordingToolClient{definitions: []agent.ToolDefinition{knowledgeMCPSearchDefinition("knowledge")}}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{EnabledToolNames: []string{"knowledge__search"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &policyToolClient{tools: recorder, policy: policy, knowledgeMCPAlias: "knowledge"}
	ctx := contextutil.WithDefaultKnowledgeBaseIDs(context.Background(), []string{" kb-default ", "kb-default"})

	result, err := client.CallTool(ctx, "knowledge__search", json.RawMessage(`{"query":"relay"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || !recorder.called {
		t.Fatalf("result=%#v called=%v", result, recorder.called)
	}
	var args struct {
		KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	}
	if err := json.Unmarshal(recorder.calledArgs, &args); err != nil {
		t.Fatal(err)
	}
	if len(args.KnowledgeBaseIDs) != 1 || args.KnowledgeBaseIDs[0] != "kb-default" {
		t.Fatalf("knowledgeBaseIds = %#v", args.KnowledgeBaseIDs)
	}
}

func TestPolicyToolClientOverridesKnowledgeMCPSearchWithRequestKnowledgeBaseIDs(t *testing.T) {
	recorder := &recordingToolClient{definitions: []agent.ToolDefinition{knowledgeMCPSearchDefinition("knowledge")}}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{EnabledToolNames: []string{"knowledge__search"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &policyToolClient{tools: recorder, policy: policy, knowledgeMCPAlias: "knowledge"}
	ctx := contextutil.WithDefaultKnowledgeBaseIDs(context.Background(), []string{"kb-request", "kb-other"})
	ctx = contextutil.WithKnowledgeBaseIDs(ctx, []string{"kb-request"})

	result, err := client.CallTool(ctx, "knowledge__search", json.RawMessage(`{"query":"relay","knowledgeBaseIds":["kb-other"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || !recorder.called {
		t.Fatalf("result=%#v called=%v", result, recorder.called)
	}
	var args struct {
		KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	}
	if err := json.Unmarshal(recorder.calledArgs, &args); err != nil {
		t.Fatal(err)
	}
	if len(args.KnowledgeBaseIDs) != 1 || args.KnowledgeBaseIDs[0] != "kb-request" {
		t.Fatalf("knowledgeBaseIds = %#v", args.KnowledgeBaseIDs)
	}
}

func TestPolicyToolClientRejectsUnauthorizedKnowledgeMCPSearch(t *testing.T) {
	recorder := &recordingToolClient{definitions: []agent.ToolDefinition{knowledgeMCPSearchDefinition("knowledge")}}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{EnabledToolNames: []string{"knowledge__search"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &policyToolClient{tools: recorder, policy: policy, knowledgeMCPAlias: "knowledge"}
	ctx := contextutil.WithDefaultKnowledgeBaseIDs(context.Background(), []string{"kb-allowed"})

	result, err := client.CallTool(ctx, "knowledge__search", json.RawMessage(`{"query":"relay","knowledgeBaseIds":["kb-forbidden"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "unauthorized_knowledge_bases") {
		t.Fatalf("result = %#v", result)
	}
	if recorder.called {
		t.Fatal("unauthorized search must not call the Knowledge MCP server")
	}
}

func TestPolicyToolClientAllowsKnowledgeMCPSearchWhenDefaultKnowledgeBaseIDsEmpty(t *testing.T) {
	recorder := &recordingToolClient{definitions: []agent.ToolDefinition{knowledgeMCPSearchDefinition("knowledge")}}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{EnabledToolNames: []string{"knowledge__search"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &policyToolClient{tools: recorder, policy: policy, knowledgeMCPAlias: "knowledge"}
	ctx := contextutil.WithDefaultKnowledgeBaseIDs(context.Background(), []string{})

	result, err := client.CallTool(ctx, "knowledge__search", json.RawMessage(`{"query":"relay","knowledgeBaseIds":["kb-any"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || !recorder.called {
		t.Fatalf("result=%#v called=%v", result, recorder.called)
	}
	var args struct {
		KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	}
	if err := json.Unmarshal(recorder.calledArgs, &args); err != nil {
		t.Fatal(err)
	}
	if len(args.KnowledgeBaseIDs) != 1 || args.KnowledgeBaseIDs[0] != "kb-any" {
		t.Fatalf("knowledgeBaseIds = %#v", args.KnowledgeBaseIDs)
	}
}

func TestPolicyToolClientRejectsUnauthorizedKnowledgeMCPListDocuments(t *testing.T) {
	recorder := &recordingToolClient{definitions: []agent.ToolDefinition{knowledgeMCPListDocumentsDefinition("knowledge")}}
	policy, err := toolspkg.NewPolicy(toolspkg.PolicyConfig{EnabledToolNames: []string{"knowledge__list_documents"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &policyToolClient{tools: recorder, policy: policy, knowledgeMCPAlias: "knowledge"}
	ctx := contextutil.WithDefaultKnowledgeBaseIDs(context.Background(), []string{"kb-allowed"})

	result, err := client.CallTool(ctx, "knowledge__list_documents", json.RawMessage(`{"knowledgeBaseId":"kb-forbidden"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "unauthorized_knowledge_bases") {
		t.Fatalf("result = %#v", result)
	}
	if recorder.called {
		t.Fatal("unauthorized list_documents must not call the Knowledge MCP server")
	}
}

type fakeKnowledgeRetriever struct{}

func (*fakeKnowledgeRetriever) Retrieve(context.Context, string, service.RetrievalTestInput) ([]service.RetrievalTestResult, error) {
	return nil, nil
}

func newKnowledgeMCPTestServer(t *testing.T, toolNames []string) *httptest.Server {
	t.Helper()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		server := mcp.NewServer(&mcp.Implementation{Name: "knowledge-test", Version: "0.1.0"}, nil)
		for _, name := range toolNames {
			name := name
			server.AddTool(&mcp.Tool{
				Name: name,
				InputSchema: map[string]any{
					"type": "object", "properties": map[string]any{},
				},
			}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{}, nil
			})
		}
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func hasTool(definitions []agent.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Function.Name == name {
			return true
		}
	}
	return false
}

type recordingToolClient struct {
	definitions []agent.ToolDefinition
	called      bool
	calledName  string
	calledArgs  json.RawMessage
}

func (c *recordingToolClient) ListTools(context.Context) ([]agent.ToolDefinition, error) {
	return c.definitions, nil
}

func (c *recordingToolClient) CallTool(_ context.Context, name string, arguments json.RawMessage) (agent.ToolResult, error) {
	c.called = true
	c.calledName = name
	c.calledArgs = append(json.RawMessage(nil), arguments...)
	return agent.ToolResult{Content: `{"ok":true}`}, nil
}

func knowledgeMCPSearchDefinition(alias string) agent.ToolDefinition {
	return agent.ToolDefinition{
		Type: "function",
		Function: agent.FunctionTool{
			Name: alias + "__search",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"knowledgeBaseIds": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func knowledgeMCPListDocumentsDefinition(alias string) agent.ToolDefinition {
	return agent.ToolDefinition{
		Type: "function",
		Function: agent.FunctionTool{
			Name: alias + "__list_documents",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"knowledgeBaseId": map[string]any{"type": "string"},
				},
				"required": []string{"knowledgeBaseId"},
			},
		},
	}
}

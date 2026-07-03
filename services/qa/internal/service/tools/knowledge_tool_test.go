package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
)

type listToolsRetrieverStub struct{}

func (listToolsRetrieverStub) Retrieve(context.Context, string, RetrievalTestInput) ([]RetrievalTestResult, error) {
	return nil, nil
}

type captureKnowledgeRetriever struct {
	input RetrievalTestInput
}

func (r *captureKnowledgeRetriever) Retrieve(_ context.Context, _ string, input RetrievalTestInput) ([]RetrievalTestResult, error) {
	r.input = input
	return []RetrievalTestResult{{DocumentID: "doc-1", DocumentName: "Doc", ContentPreview: "content"}}, nil
}

func TestKnowledgeToolClientListsCitationSourceTool(t *testing.T) {
	client, err := NewKnowledgeToolClient(KnowledgeToolConfig{RetrievalClient: listToolsRetrieverStub{}})
	if err != nil {
		t.Fatal(err)
	}

	definitions, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Function.Name] = true
	}

	if !names[ToolSearchKnowledge] || !names[ToolGetCitationSource] {
		t.Fatalf("tool names=%#v, want search and citation source tools", names)
	}
}

func TestKnowledgeToolDoesNotRaiseConfiguredScoreThreshold(t *testing.T) {
	retriever := &captureKnowledgeRetriever{}
	client, err := NewKnowledgeToolClient(KnowledgeToolConfig{RetrievalClient: retriever})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithUserID(context.Background(), "user-1")
	ctx = contextutil.WithDefaultKnowledgeBaseIDs(ctx, []string{"kb-1"})
	ctx = contextutil.WithRetrievalSettings(ctx, contextutil.RetrievalSettings{
		TopK:                     5,
		ScoreThreshold:           0,
		ScoreThresholdConfigured: true,
	})
	args := json.RawMessage(`{"query":"支持向量机","score_threshold":0.2}`)

	result, err := client.CallTool(ctx, ToolSearchKnowledge, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool failure: %+v", result)
	}
	if retriever.input.Retrieval.ScoreThreshold != 0 {
		t.Fatalf("scoreThreshold=%v, want configured zero", retriever.input.Retrieval.ScoreThreshold)
	}
	if !retriever.input.Retrieval.ScoreThresholdConfigured {
		t.Fatal("scoreThreshold should be marked configured")
	}
}

func TestKnowledgeToolAllowsLowerScoreThreshold(t *testing.T) {
	retriever := &captureKnowledgeRetriever{}
	client, err := NewKnowledgeToolClient(KnowledgeToolConfig{RetrievalClient: retriever})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithUserID(context.Background(), "user-1")
	ctx = contextutil.WithDefaultKnowledgeBaseIDs(ctx, []string{"kb-1"})
	ctx = contextutil.WithRetrievalSettings(ctx, contextutil.RetrievalSettings{
		TopK:                     5,
		ScoreThreshold:           0.7,
		ScoreThresholdConfigured: true,
	})
	args := json.RawMessage(`{"query":"支持向量机","score_threshold":0.2}`)

	result, err := client.CallTool(ctx, ToolSearchKnowledge, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool failure: %+v", result)
	}
	if retriever.input.Retrieval.ScoreThreshold != 0.2 {
		t.Fatalf("scoreThreshold=%v, want model-lowered threshold", retriever.input.Retrieval.ScoreThreshold)
	}
	if !retriever.input.Retrieval.ScoreThresholdConfigured {
		t.Fatal("scoreThreshold should be marked configured")
	}
}

func TestKnowledgeToolUsesModelScoreThresholdWhenNoDefaultConfigured(t *testing.T) {
	retriever := &captureKnowledgeRetriever{}
	client, err := NewKnowledgeToolClient(KnowledgeToolConfig{RetrievalClient: retriever})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithUserID(context.Background(), "user-1")
	ctx = contextutil.WithDefaultKnowledgeBaseIDs(ctx, []string{"kb-1"})
	ctx = contextutil.WithRetrievalSettings(ctx, contextutil.RetrievalSettings{TopK: 5})
	args := json.RawMessage(`{"query":"支持向量机","score_threshold":0.8}`)

	result, err := client.CallTool(ctx, ToolSearchKnowledge, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool failure: %+v", result)
	}
	if retriever.input.Retrieval.ScoreThreshold != 0.8 {
		t.Fatalf("scoreThreshold=%v, want model threshold", retriever.input.Retrieval.ScoreThreshold)
	}
	if !retriever.input.Retrieval.ScoreThresholdConfigured {
		t.Fatal("scoreThreshold should be marked configured")
	}
}

func TestKnowledgeToolAllowsAllAccessibleBasesWhenDefaultListEmpty(t *testing.T) {
	retriever := &captureKnowledgeRetriever{}
	client, err := NewKnowledgeToolClient(KnowledgeToolConfig{RetrievalClient: retriever})
	if err != nil {
		t.Fatal(err)
	}
	ctx := contextutil.WithUserID(context.Background(), "user-1")
	ctx = contextutil.WithDefaultKnowledgeBaseIDs(ctx, []string{})
	args := json.RawMessage(`{"query":"继电保护","knowledge_base_ids":["kb-any"]}`)

	result, err := client.CallTool(ctx, ToolSearchKnowledge, args)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool failure: %+v", result)
	}
	if len(retriever.input.KnowledgeBaseIDs) != 1 || retriever.input.KnowledgeBaseIDs[0] != "kb-any" {
		t.Fatalf("knowledgeBaseIDs=%+v, want model-provided KB when default list is empty", retriever.input.KnowledgeBaseIDs)
	}
}

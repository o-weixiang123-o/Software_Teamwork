package repository

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

func TestStreamEventSeqInt32RejectsInvalidValues(t *testing.T) {
	if _, err := streamEventSeqInt32(-1); err == nil {
		t.Fatal("expected negative cursor to fail")
	}
	overflowValue := int64(math.MaxInt32) + 1
	if _, err := streamEventSeqInt32(int(overflowValue)); err == nil {
		t.Fatal("expected overflow cursor to fail")
	}
	if got, err := streamEventSeqInt32(math.MaxInt32); err != nil || got != math.MaxInt32 {
		t.Fatalf("streamEventSeqInt32(MaxInt32) = %d, %v", got, err)
	}
}

func TestMessageCitationLegacySelectDoesNotRequireSnapshotMigrationColumns(t *testing.T) {
	for _, column := range []string{
		"ci.response_run_id",
		"ci.content_preview",
		"ci.is_source_available",
		"ci.source_unavailable_reason",
	} {
		if strings.Contains(messageCitationLegacySelect, column) {
			t.Fatalf("legacy message citation query should not require migration 0006 column %q: %s", column, messageCitationLegacySelect)
		}
	}
	if strings.Contains(messageCitationLegacySelect, "FALSE AS is_source_available") {
		t.Fatalf("legacy message citation query should not hard-code source availability to false: %s", messageCitationLegacySelect)
	}
}

func TestAgentConfigFromCreateInputPreservesExplicitEmptyToolWhitelist(t *testing.T) {
	config := agentConfigFromCreateInput(service.CreateQAConfigVersionInput{
		Agent:            service.AgentConfig{EnabledToolNames: []string{}},
		EnabledToolNames: []string{"search_knowledge"},
	})

	if config.EnabledToolNames == nil || len(config.EnabledToolNames) != 0 {
		t.Fatalf("enabledToolNames=%#v, want explicit empty whitelist", config.EnabledToolNames)
	}
}

func TestAgentConfigFromCreateInputFallsBackToLegacyToolNamesWhenUnset(t *testing.T) {
	config := agentConfigFromCreateInput(service.CreateQAConfigVersionInput{
		EnabledToolNames: []string{"search_knowledge"},
	})

	if !reflect.DeepEqual(config.EnabledToolNames, []string{"search_knowledge"}) {
		t.Fatalf("enabledToolNames=%#v, want legacy tool names", config.EnabledToolNames)
	}
}

func TestAgentConfigFromCreateInputUsesDefaultToolNamesWhenUnset(t *testing.T) {
	config := agentConfigFromCreateInput(service.CreateQAConfigVersionInput{})

	if !reflect.DeepEqual(config.EnabledToolNames, service.DefaultAgentConfig().EnabledToolNames) {
		t.Fatalf("enabledToolNames=%#v, want defaults", config.EnabledToolNames)
	}
}

func TestRetrievalConfigFromCreateInputPreservesExplicitZeroScoreThreshold(t *testing.T) {
	var input service.CreateQAConfigVersionInput
	if err := json.Unmarshal([]byte(`{"retrieval":{"topK":5,"scoreThreshold":0}}`), &input); err != nil {
		t.Fatal(err)
	}

	config := retrievalConfigFromCreateInput(input)

	if config.ScoreThreshold != 0 {
		t.Fatalf("scoreThreshold=%v, want explicit zero", config.ScoreThreshold)
	}
	if config.TopK != 5 {
		t.Fatalf("topK=%d, want 5", config.TopK)
	}
}

func TestRetrievalConfigFromCreateInputFallsBackToDefaultThresholdWhenUnset(t *testing.T) {
	var input service.CreateQAConfigVersionInput
	if err := json.Unmarshal([]byte(`{"retrieval":{"topK":5}}`), &input); err != nil {
		t.Fatal(err)
	}

	config := retrievalConfigFromCreateInput(input)

	if config.ScoreThreshold != .7 {
		t.Fatalf("scoreThreshold=%v, want default .7", config.ScoreThreshold)
	}
}

func TestRetrievalConfigFromCreateInputUsesLegacySimilarityThreshold(t *testing.T) {
	config := retrievalConfigFromCreateInput(service.CreateQAConfigVersionInput{
		TopK:                6,
		SimilarityThreshold: .35,
	})

	if config.TopK != 6 || config.ScoreThreshold != .35 {
		t.Fatalf("retrieval=%+v, want legacy topK and threshold", config)
	}
}

func TestToolCallAuditSummariesDeriveSourceAndFailure(t *testing.T) {
	if got := toolSourceName("search_knowledge"); got != "qa_builtin" {
		t.Fatalf("builtin source=%q", got)
	}
	if got := toolSourceName("kbserver__search"); got != "kbserver" {
		t.Fatalf("prefixed source=%q", got)
	}

	code, message := toolCallErrorSummary("tool.failed", map[string]any{
		"raw": `{"error":{"code":"retrieval_failed","message":"knowledge retrieval service failed"}}`,
	})
	if code != "retrieval_failed" || message != "knowledge retrieval service failed" {
		t.Fatalf("error summary code=%q message=%q", code, message)
	}
}

func TestToolCallEventPayloadCarriesModelInvocationID(t *testing.T) {
	payload := map[string]any{
		"iterationNo":       1,
		"modelInvocationId": "invocation-id",
		"toolCallId":        "call-1",
		"tool":              "search_knowledge",
	}

	if got, _ := payload["modelInvocationId"].(string); got != "invocation-id" {
		t.Fatalf("modelInvocationId=%q", got)
	}
}

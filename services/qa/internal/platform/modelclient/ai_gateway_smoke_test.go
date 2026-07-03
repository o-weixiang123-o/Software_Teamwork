package modelclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	qaconfig "github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/config"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

func TestAIGatewaySmoke(t *testing.T) {
	if strings.TrimSpace(os.Getenv("QA_AI_GATEWAY_SMOKE")) != "1" {
		t.Skip("set QA_AI_GATEWAY_SMOKE=1 to run the QA -> AI Gateway smoke")
	}

	cfg, err := qaconfig.Load()
	if err != nil {
		t.Fatalf("load QA smoke configuration: %v", err)
	}
	if strings.TrimSpace(cfg.AIGatewayToken) == "" {
		t.Fatal("AI_GATEWAY_TOKEN or INTERNAL_SERVICE_TOKEN is required when QA_AI_GATEWAY_SMOKE=1")
	}
	if strings.TrimSpace(cfg.AIGatewayProfileID) == "" {
		t.Fatal("AI_GATEWAY_PROFILE_ID is required when QA_AI_GATEWAY_SMOKE=1; select a seeded controlled-provider or real-provider chat profile")
	}

	requestID := fmt.Sprintf("qa-ai-gateway-smoke-%d", time.Now().UTC().UnixNano())
	ctx := service.WithUserID(service.WithRequestID(context.Background(), requestID), "qa-ai-gateway-smoke-user")
	prompt := "Reply with a short acknowledgement for the QA to AI Gateway smoke test."

	t.Run("successful_completion", func(t *testing.T) {
		client := newSmokeClient(t, cfg, cfg.AIGatewayToken, cfg.AIGatewayProfileID)
		completion, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: prompt}}, nil)
		if err != nil {
			t.Fatalf("QA -> AI Gateway smoke failed for profile %q model %q (request_id=%s): %v; verify AI Gateway readiness, profile credentials, optional model exact-match when MODEL_ID is set, and provider availability", cfg.AIGatewayProfileID, cfg.ModelID, requestID, err)
		}
		if completion.Message.Role != agent.RoleAssistant {
			t.Fatalf("completion role = %q, want %q", completion.Message.Role, agent.RoleAssistant)
		}
		if strings.TrimSpace(completion.FinishReason) == "" {
			t.Fatal("completion finish reason is empty")
		}
		if strings.TrimSpace(completion.Message.Content) == "" && len(completion.Message.ToolCalls) == 0 {
			t.Fatal("completion has neither assistant content nor tool calls")
		}
		t.Logf("QA -> AI Gateway smoke succeeded (request_id=%s profile_id=%s model=%s finish_reason=%s)", requestID, cfg.AIGatewayProfileID, cfg.ModelID, completion.FinishReason)
	})

	t.Run("invalid_service_token", func(t *testing.T) {
		client := newSmokeClient(t, cfg, requestID+"-invalid-token", cfg.AIGatewayProfileID)
		_, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: prompt}}, nil)
		assertSmokeDependencyError(t, err, "invalid service token")
	})

	t.Run("missing_profile", func(t *testing.T) {
		client := newSmokeClient(t, cfg, cfg.AIGatewayToken, requestID+"-missing-profile")
		_, err := client.Complete(ctx, []agent.Message{{Role: agent.RoleUser, Content: prompt}}, nil)
		assertSmokeDependencyError(t, err, "missing profile")
	})
}

func newSmokeClient(t *testing.T, cfg qaconfig.Config, token string, profileID string) *Client {
	t.Helper()
	client, err := New(Config{
		Endpoint:    cfg.AIGatewayURL,
		Token:       token,
		TokenHeader: cfg.AIGatewayTokenHeader,
		Model:       cfg.ModelID,
		ProfileID:   profileID,
		MaxTokens:   64,
		Timeout:     cfg.ModelTimeout,
	})
	if err != nil {
		t.Fatalf("create QA AI Gateway smoke client: %v", err)
	}
	return client
}

func assertSmokeDependencyError(t *testing.T, err error, scenario string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s probe unexpectedly succeeded", scenario)
	}
	appErr, ok := service.Classify(err)
	if !ok || appErr.Code != service.CodeDependency || appErr.Message != "AI gateway request failed" {
		t.Fatalf("%s probe error = %v, want normalized dependency_error", scenario, err)
	}
}

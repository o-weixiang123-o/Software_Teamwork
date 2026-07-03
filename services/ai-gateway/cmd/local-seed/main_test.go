package main

import (
	"testing"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
)

func TestLoadSeedConfigSkipsWhenNoLocalProviderEnv(t *testing.T) {
	seed, err := loadSeedConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("loadSeedConfig() error = %v", err)
	}
	if seed.Requested {
		t.Fatal("seed should not be requested without local provider env")
	}
}

func TestLoadSeedConfigBuildsAllProfiles(t *testing.T) {
	env := map[string]string{
		envProvider:           "siliconflow",
		envProviderBaseURL:    "https://api.siliconflow.cn/v1",
		envProviderAPIKey:     "secret-key",
		envTimeoutMS:          "30000",
		envChatModel:          "deepseek-ai/DeepSeek-V3",
		envEmbeddingModel:     "BAAI/bge-m3",
		envEmbeddingDimension: "1024",
		envRerankModel:        "BAAI/bge-reranker-v2-m3",
		envRerankTopN:         "3",
		envQADatabaseURL:      "postgres://qa",
		envQATimeout:          "45s",
		envQAMaxTokens:        "2048",
		envQATemperature:      "0.2",
	}
	seed, err := loadSeedConfig(mapGetenv(env))
	if err != nil {
		t.Fatalf("loadSeedConfig() error = %v", err)
	}
	if !seed.Requested || seed.Provider != service.ProviderSiliconFlow || seed.BaseURL != env[envProviderBaseURL] || seed.TimeoutMS != 30000 {
		t.Fatalf("unexpected seed config: %+v", seed)
	}
	if len(seed.Profiles) != 3 {
		t.Fatalf("profiles = %d, want 3", len(seed.Profiles))
	}
	assertProfile(t, seed.Profiles[0], "default-chat", service.PurposeChat, "deepseek-ai/DeepSeek-V3")
	if seed.QALLM == nil {
		t.Fatal("QALLM seed is nil")
	}
	if seed.QALLM.DatabaseURL != "postgres://qa" || seed.QALLM.ProfileID != "default-chat" || seed.QALLM.Model != "deepseek-ai/DeepSeek-V3" {
		t.Fatalf("QALLM seed = %+v", *seed.QALLM)
	}
	if seed.QALLM.TimeoutSeconds != 45 || seed.QALLM.MaxTokens != 2048 || seed.QALLM.Temperature != "0.200" {
		t.Fatalf("QALLM runtime values = %+v", *seed.QALLM)
	}
	assertProfile(t, seed.Profiles[1], "default-embedding", service.PurposeEmbedding, "BAAI/bge-m3")
	if seed.Profiles[1].Dimensions == nil || *seed.Profiles[1].Dimensions != 1024 {
		t.Fatalf("embedding dimensions = %#v, want 1024", seed.Profiles[1].Dimensions)
	}
	assertProfile(t, seed.Profiles[2], "default-rerank", service.PurposeRerank, "BAAI/bge-reranker-v2-m3")
	if seed.Profiles[2].TopN == nil || *seed.Profiles[2].TopN != 3 {
		t.Fatalf("rerank topN = %#v, want 3", seed.Profiles[2].TopN)
	}
}

func TestLoadSeedConfigUsesNonPlaceholderModelIDAsChatFallback(t *testing.T) {
	env := map[string]string{
		envProviderBaseURL: "https://api.example.test/v1",
		envProviderAPIKey:  "secret-key",
		envQAmodelID:       "deepseek-chat",
	}
	seed, err := loadSeedConfig(mapGetenv(env))
	if err != nil {
		t.Fatalf("loadSeedConfig() error = %v", err)
	}
	if len(seed.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(seed.Profiles))
	}
	assertProfile(t, seed.Profiles[0], "default-chat", service.PurposeChat, "deepseek-chat")
	if seed.QALLM == nil || seed.QALLM.Model != "deepseek-chat" {
		t.Fatalf("QALLM seed = %+v", seed.QALLM)
	}
}

func TestLoadSeedConfigRequiresModel(t *testing.T) {
	env := map[string]string{
		envProviderBaseURL: "https://api.example.test/v1",
		envProviderAPIKey:  "secret-key",
		envQAmodelID:       placeholderLocalChat,
	}
	if _, err := loadSeedConfig(mapGetenv(env)); err == nil {
		t.Fatal("loadSeedConfig() error = nil, want missing model error")
	}
}

func TestLoadSeedConfigRequiresBaseURLAndAPIKeyTogether(t *testing.T) {
	env := map[string]string{
		envChatModel: "deepseek-chat",
	}
	if _, err := loadSeedConfig(mapGetenv(env)); err == nil {
		t.Fatal("loadSeedConfig() error = nil, want missing provider env error")
	}
}

func TestLoadSeedConfigCanBeExplicitlyDisabled(t *testing.T) {
	env := map[string]string{
		envSeedEnabled:     "false",
		envProviderBaseURL: "https://api.example.test/v1",
		envProviderAPIKey:  "secret-key",
		envChatModel:       "deepseek-chat",
	}
	seed, err := loadSeedConfig(mapGetenv(env))
	if err != nil {
		t.Fatalf("loadSeedConfig() error = %v", err)
	}
	if seed.Requested {
		t.Fatal("seed should not be requested when explicitly disabled")
	}
}

func TestLoadSeedConfigRejectsInvalidQALLMRuntimeValues(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"timeout": {
			envProviderBaseURL: "https://api.example.test/v1",
			envProviderAPIKey:  "secret-key",
			envChatModel:       "deepseek-chat",
			envQATimeout:       "0s",
		},
		"maxTokens": {
			envProviderBaseURL: "https://api.example.test/v1",
			envProviderAPIKey:  "secret-key",
			envChatModel:       "deepseek-chat",
			envQAMaxTokens:     "0",
		},
		"temperature": {
			envProviderBaseURL: "https://api.example.test/v1",
			envProviderAPIKey:  "secret-key",
			envChatModel:       "deepseek-chat",
			envQATemperature:   "3",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadSeedConfig(mapGetenv(env)); err == nil {
				t.Fatal("loadSeedConfig() error = nil, want validation error")
			}
		})
	}
}

func assertProfile(t *testing.T, profile profileSeed, id string, purpose service.Purpose, model string) {
	t.Helper()
	if profile.ID != id || profile.Purpose != purpose || profile.Model != model {
		t.Fatalf("profile = %+v, want id=%s purpose=%s model=%s", profile, id, purpose, model)
	}
}

func mapGetenv(values map[string]string) getenvFunc {
	return func(key string) string {
		return values[key]
	}
}

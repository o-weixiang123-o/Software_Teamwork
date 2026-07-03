package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/config"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/repository"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
	"github.com/jackc/pgx/v5"
)

const (
	envSeedEnabled        = "AI_GATEWAY_LOCAL_SEED_ENABLED"
	envProvider           = "AI_GATEWAY_LOCAL_PROVIDER"
	envProviderBaseURL    = "AI_GATEWAY_LOCAL_PROVIDER_BASE_URL"
	envProviderAPIKey     = "AI_GATEWAY_LOCAL_PROVIDER_API_KEY"
	envTimeoutMS          = "AI_GATEWAY_LOCAL_TIMEOUT_MS"
	envChatModel          = "AI_GATEWAY_LOCAL_CHAT_MODEL"
	envEmbeddingModel     = "AI_GATEWAY_LOCAL_EMBEDDING_MODEL"
	envEmbeddingDimension = "AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS"
	envRerankModel        = "AI_GATEWAY_LOCAL_RERANK_MODEL"
	envRerankTopN         = "AI_GATEWAY_LOCAL_RERANK_TOP_N"
	envQAmodelID          = "MODEL_ID"
	envQADatabaseURL      = "QA_DATABASE_URL"
	envQATimeout          = "AI_GATEWAY_TIMEOUT"
	envQAMaxTokens        = "AGENT_MAX_TOKENS"
	envQATemperature      = "AI_GATEWAY_LOCAL_QA_TEMPERATURE"

	defaultSeedUserID       = "usr_local_admin"
	defaultProvider         = service.ProviderOpenAICompatible
	defaultEmbeddingDim     = 1024
	defaultRerankTopN       = 5
	defaultQATimeoutSeconds = 60
	defaultQAMaxTokens      = 4096
	defaultQATemperature    = "0.700"
	placeholderLocalChat    = "local-placeholder-chat"
	localSeedCaller         = "local-seed"
	localSeedRequestID      = "local-env-seed"
	localSeedDefaultTimeout = config.DefaultTimeoutMS
)

type getenvFunc func(string) string

type seedConfig struct {
	Requested bool
	Provider  service.Provider
	BaseURL   string
	APIKey    string
	TimeoutMS int
	Profiles  []profileSeed
	QALLM     *qaLLMSeed
}

type profileSeed struct {
	ID                string
	Name              string
	Purpose           service.Purpose
	Model             string
	SupportsStreaming bool
	Dimensions        *int
	TopN              *int
	DefaultParameters json.RawMessage
}

type qaLLMSeed struct {
	DatabaseURL    string
	ProfileID      string
	Model          string
	TimeoutSeconds int
	MaxTokens      int
	Temperature    string
}

type modelProfileService interface {
	GetModelProfile(context.Context, string) (service.ModelProfile, error)
	CreateModelProfile(context.Context, service.RequestContext, service.CreateModelProfileInput) (service.ModelProfile, error)
	UpdateModelProfile(context.Context, service.RequestContext, service.UpdateModelProfileInput) (service.ModelProfile, error)
}

type credentialRepository interface {
	GetActiveCredential(context.Context, string) (service.ProviderCredential, error)
}

func main() {
	if err := run(context.Background(), os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ai-gateway local seed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv getenvFunc, out io.Writer) error {
	seed, err := loadSeedConfig(getenv)
	if err != nil {
		return err
	}
	if !seed.Requested {
		fmt.Fprintln(out, "ai-gateway local seed: skipped; set AI_GATEWAY_LOCAL_PROVIDER_BASE_URL, AI_GATEWAY_LOCAL_PROVIDER_API_KEY, and at least one AI_GATEWAY_LOCAL_*_MODEL")
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	repo, err := repository.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer repo.Close()
	encryptor, err := service.NewCredentialEncryptor(cfg.CredentialEncryptionKey, cfg.CredentialEncryptionKeyRef)
	if err != nil {
		return err
	}
	profiles := service.New(repo, encryptor, cfg.DefaultTimeoutMS)
	if err := applySeed(ctx, profiles, repo, encryptor, seed, out); err != nil {
		return err
	}
	return applyQALLMSeed(ctx, seed, out)
}

func loadSeedConfig(getenv getenvFunc) (seedConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	enabledRaw := strings.TrimSpace(getenv(envSeedEnabled))
	enabled, enabledSet, err := optionalBool(enabledRaw)
	if err != nil {
		return seedConfig{}, fmt.Errorf("%s must be one of 1, true, yes, on, 0, false, no, or off", envSeedEnabled)
	}
	if enabledSet && !enabled {
		return seedConfig{}, nil
	}

	relevantValues := []string{
		enabledRaw,
		getenv(envProvider),
		getenv(envProviderBaseURL),
		getenv(envProviderAPIKey),
		getenv(envTimeoutMS),
		getenv(envChatModel),
		getenv(envEmbeddingModel),
		getenv(envEmbeddingDimension),
		getenv(envRerankModel),
		getenv(envRerankTopN),
	}
	if !enabledSet && !anyNonBlank(relevantValues...) {
		return seedConfig{}, nil
	}

	baseURL := strings.TrimSpace(getenv(envProviderBaseURL))
	apiKey := strings.TrimSpace(getenv(envProviderAPIKey))
	missing := []string{}
	if baseURL == "" {
		missing = append(missing, envProviderBaseURL)
	}
	if apiKey == "" {
		missing = append(missing, envProviderAPIKey)
	}
	if len(missing) > 0 {
		return seedConfig{}, fmt.Errorf("%s must be set for the local provider seed", strings.Join(missing, ", "))
	}

	provider, err := parseProvider(firstNonBlank(getenv(envProvider), string(defaultProvider)))
	if err != nil {
		return seedConfig{}, err
	}
	timeoutMS, err := parsePositiveIntEnv(getenv(envTimeoutMS), localSeedDefaultTimeout, 1000, envTimeoutMS)
	if err != nil {
		return seedConfig{}, err
	}

	seed := seedConfig{
		Requested: true,
		Provider:  provider,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		TimeoutMS: timeoutMS,
	}

	chatModel := firstNonBlank(getenv(envChatModel), qaModelFallback(getenv(envQAmodelID)))
	if chatModel != "" {
		chatProfile := profileSeed{
			ID:                "default-chat",
			Name:              "Local env chat profile",
			Purpose:           service.PurposeChat,
			Model:             chatModel,
			SupportsStreaming: true,
			DefaultParameters: json.RawMessage(`{"temperature":0.2}`),
		}
		seed.Profiles = append(seed.Profiles, chatProfile)
		qaLLM, err := loadQALLMSeed(getenv, chatProfile)
		if err != nil {
			return seedConfig{}, err
		}
		seed.QALLM = &qaLLM
	}

	if embeddingModel := strings.TrimSpace(getenv(envEmbeddingModel)); embeddingModel != "" {
		dimensions, err := parsePositiveIntEnv(getenv(envEmbeddingDimension), defaultEmbeddingDim, 1, envEmbeddingDimension)
		if err != nil {
			return seedConfig{}, err
		}
		seed.Profiles = append(seed.Profiles, profileSeed{
			ID:                "default-embedding",
			Name:              "Local env embedding profile",
			Purpose:           service.PurposeEmbedding,
			Model:             embeddingModel,
			Dimensions:        intPtr(dimensions),
			DefaultParameters: json.RawMessage(`{}`),
		})
	}

	if rerankModel := strings.TrimSpace(getenv(envRerankModel)); rerankModel != "" {
		topN, err := parsePositiveIntEnv(getenv(envRerankTopN), defaultRerankTopN, 1, envRerankTopN)
		if err != nil {
			return seedConfig{}, err
		}
		seed.Profiles = append(seed.Profiles, profileSeed{
			ID:                "default-rerank",
			Name:              "Local env rerank profile",
			Purpose:           service.PurposeRerank,
			Model:             rerankModel,
			TopN:              intPtr(topN),
			DefaultParameters: json.RawMessage(`{}`),
		})
	}

	if len(seed.Profiles) == 0 {
		return seedConfig{}, fmt.Errorf("set at least one of %s, %s, or %s for the local provider seed", envChatModel, envEmbeddingModel, envRerankModel)
	}
	return seed, nil
}

func loadQALLMSeed(getenv getenvFunc, chatProfile profileSeed) (qaLLMSeed, error) {
	timeoutSeconds, err := parseDurationSecondsEnv(getenv(envQATimeout), defaultQATimeoutSeconds, envQATimeout)
	if err != nil {
		return qaLLMSeed{}, err
	}
	maxTokens, err := parsePositiveIntEnv(getenv(envQAMaxTokens), defaultQAMaxTokens, 1, envQAMaxTokens)
	if err != nil {
		return qaLLMSeed{}, err
	}
	temperature, err := parseTemperatureEnv(getenv(envQATemperature), defaultQATemperature, envQATemperature)
	if err != nil {
		return qaLLMSeed{}, err
	}
	return qaLLMSeed{
		DatabaseURL:    strings.TrimSpace(getenv(envQADatabaseURL)),
		ProfileID:      chatProfile.ID,
		Model:          chatProfile.Model,
		TimeoutSeconds: timeoutSeconds,
		MaxTokens:      maxTokens,
		Temperature:    temperature,
	}, nil
}

func applySeed(ctx context.Context, profiles modelProfileService, credentials credentialRepository, encryptor *service.CredentialEncryptor, seed seedConfig, out io.Writer) error {
	req := service.RequestContext{
		UserID:        defaultSeedUserID,
		CallerService: localSeedCaller,
		RequestID:     localSeedRequestID,
	}
	for _, profile := range seed.Profiles {
		current, err := profiles.GetModelProfile(ctx, profile.ID)
		if err != nil {
			if isNotFound(err) {
				if _, err := profiles.CreateModelProfile(ctx, req, createInput(seed, profile)); err != nil {
					return fmt.Errorf("create %s: %w", profile.ID, err)
				}
				fmt.Fprintf(out, "ai-gateway local seed: created %s (%s)\n", profile.ID, profile.Model)
				continue
			}
			return fmt.Errorf("load %s: %w", profile.ID, err)
		}
		if current.Purpose != profile.Purpose {
			return fmt.Errorf("%s already exists with purpose %q; expected %q", profile.ID, current.Purpose, profile.Purpose)
		}

		input, changed := updateInput(ctx, credentials, encryptor, seed, profile, current)
		if !changed {
			fmt.Fprintf(out, "ai-gateway local seed: unchanged %s (%s)\n", profile.ID, profile.Model)
			continue
		}
		if _, err := profiles.UpdateModelProfile(ctx, req, input); err != nil {
			return fmt.Errorf("update %s: %w", profile.ID, err)
		}
		fmt.Fprintf(out, "ai-gateway local seed: updated %s (%s)\n", profile.ID, profile.Model)
	}
	return nil
}

func applyQALLMSeed(ctx context.Context, seed seedConfig, out io.Writer) error {
	if seed.QALLM == nil {
		return nil
	}
	if strings.TrimSpace(seed.QALLM.DatabaseURL) == "" {
		fmt.Fprintln(out, "ai-gateway local seed: skipped QA LLM config sync; QA_DATABASE_URL is not set")
		return nil
	}
	conn, err := pgx.Connect(ctx, seed.QALLM.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect QA database for LLM config sync: %w", err)
	}
	defer conn.Close(ctx)
	changed, err := syncQALLMConfig(ctx, conn, *seed.QALLM)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(out, "ai-gateway local seed: activated QA LLM config %s/%s\n", seed.QALLM.ProfileID, seed.QALLM.Model)
		return nil
	}
	fmt.Fprintf(out, "ai-gateway local seed: unchanged QA LLM config %s/%s\n", seed.QALLM.ProfileID, seed.QALLM.Model)
	return nil
}

func syncQALLMConfig(ctx context.Context, conn *pgx.Conn, seed qaLLMSeed) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin QA LLM config sync: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE llm_config_versions IN EXCLUSIVE MODE`); err != nil {
		return false, fmt.Errorf("lock QA LLM config versions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE llm_config_versions
		SET is_active = false
		WHERE is_active = true
		  AND NOT (
			provider = 'ai-gateway'
			AND COALESCE(profile_id, '') = $1
			AND model_name = $2
			AND timeout_seconds = $3
			AND max_tokens = $4
			AND temperature = $5::numeric
		  )`,
		seed.ProfileID,
		seed.Model,
		seed.TimeoutSeconds,
		seed.MaxTokens,
		seed.Temperature,
	); err != nil {
		return false, fmt.Errorf("deactivate old QA LLM config: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO llm_config_versions (
			version_no, provider, profile_id, model_name, timeout_seconds,
			temperature, max_tokens, is_active, created_by_user_id
		)
		SELECT
			(SELECT COALESCE(MAX(version_no), 0) + 1 FROM llm_config_versions),
			'ai-gateway', $1, $2, $3, $5::numeric, $4, true, $6
		WHERE NOT EXISTS (
			SELECT 1 FROM llm_config_versions
			WHERE is_active = true
			  AND provider = 'ai-gateway'
			  AND COALESCE(profile_id, '') = $1
			  AND model_name = $2
			  AND timeout_seconds = $3
			  AND max_tokens = $4
			  AND temperature = $5::numeric
		)`,
		seed.ProfileID,
		seed.Model,
		seed.TimeoutSeconds,
		seed.MaxTokens,
		seed.Temperature,
		defaultSeedUserID,
	)
	if err != nil {
		return false, fmt.Errorf("insert QA LLM config: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit QA LLM config sync: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func createInput(seed seedConfig, profile profileSeed) service.CreateModelProfileInput {
	enabled := true
	isDefault := true
	supportsStreaming := profile.SupportsStreaming
	return service.CreateModelProfileInput{
		ID:                profile.ID,
		Name:              profile.Name,
		Purpose:           profile.Purpose,
		Provider:          seed.Provider,
		BaseURL:           seed.BaseURL,
		Model:             profile.Model,
		APIKey:            seed.APIKey,
		Enabled:           &enabled,
		IsDefault:         &isDefault,
		TimeoutMS:         &seed.TimeoutMS,
		SupportsStreaming: &supportsStreaming,
		Dimensions:        cloneIntPtr(profile.Dimensions),
		TopN:              cloneIntPtr(profile.TopN),
		DefaultParameters: append(json.RawMessage(nil), profile.DefaultParameters...),
	}
}

func updateInput(ctx context.Context, credentials credentialRepository, encryptor *service.CredentialEncryptor, seed seedConfig, profile profileSeed, current service.ModelProfile) (service.UpdateModelProfileInput, bool) {
	input := service.UpdateModelProfileInput{ID: profile.ID}
	changed := false
	if current.Name != profile.Name {
		input.Name = stringPtr(profile.Name)
		changed = true
	}
	if current.Provider != seed.Provider {
		input.Provider = providerPtr(seed.Provider)
		changed = true
	}
	if strings.TrimSpace(current.BaseURL) != seed.BaseURL {
		input.BaseURL = stringPtr(seed.BaseURL)
		changed = true
	}
	if current.Model != profile.Model {
		input.Model = stringPtr(profile.Model)
		changed = true
	}
	if !current.Enabled {
		input.Enabled = boolPtr(true)
		changed = true
	}
	if !current.IsDefault {
		input.IsDefault = boolPtr(true)
		changed = true
	}
	if current.TimeoutMS != seed.TimeoutMS {
		input.TimeoutMS = intPtr(seed.TimeoutMS)
		changed = true
	}
	if current.SupportsStreaming != profile.SupportsStreaming {
		input.SupportsStreaming = boolPtr(profile.SupportsStreaming)
		changed = true
	}
	if !intPtrEqual(current.Dimensions, profile.Dimensions) {
		input.Dimensions = cloneIntPtr(profile.Dimensions)
		changed = true
	}
	if !intPtrEqual(current.TopN, profile.TopN) {
		input.TopN = cloneIntPtr(profile.TopN)
		changed = true
	}
	if !jsonEqual(current.DefaultParameters, profile.DefaultParameters) {
		parameters := append(json.RawMessage(nil), profile.DefaultParameters...)
		input.DefaultParameters = &parameters
		changed = true
	}
	if !credentialMatches(ctx, credentials, encryptor, profile.ID, seed.APIKey) {
		input.APIKey = stringPtr(seed.APIKey)
		changed = true
	}
	return input, changed
}

func credentialMatches(ctx context.Context, credentials credentialRepository, encryptor *service.CredentialEncryptor, profileID, apiKey string) bool {
	if credentials == nil || encryptor == nil {
		return false
	}
	expected, err := encryptor.Encrypt(apiKey)
	if err != nil {
		return false
	}
	current, err := credentials.GetActiveCredential(ctx, profileID)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(current.FingerprintSHA256), strings.TrimSpace(expected.FingerprintSHA256))
}

func isNotFound(err error) bool {
	if errors.Is(err, service.ErrNotFound) {
		return true
	}
	appErr, ok := service.Classify(err)
	return ok && appErr.Code == service.CodeNotFound
}

func parseProvider(raw string) (service.Provider, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch service.Provider(value) {
	case service.ProviderOpenAICompatible:
		return service.ProviderOpenAICompatible, nil
	case service.ProviderSiliconFlow:
		return service.ProviderSiliconFlow, nil
	case service.ProviderLocalCompatible:
		return service.ProviderLocalCompatible, nil
	default:
		return "", fmt.Errorf("%s must be one of openai_compatible, siliconflow, or local_compatible", envProvider)
	}
}

func parsePositiveIntEnv(raw string, fallback, min int, name string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value < min {
		return 0, fmt.Errorf("%s must be an integer >= %d", name, min)
	}
	return value, nil
}

func parseDurationSecondsEnv(raw string, fallback int, name string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(trimmed)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	seconds := int(value.Seconds())
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be at least 1s", name)
	}
	return seconds, nil
}

func parseTemperatureEnv(raw, fallback, name string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 || value > 2 {
		return "", fmt.Errorf("%s must be a number between 0 and 2", name)
	}
	return strconv.FormatFloat(value, 'f', 3, 64), nil
}

func optionalBool(raw string) (bool, bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return false, false, nil
	case "1", "true", "yes", "y", "on":
		return true, true, nil
	case "0", "false", "no", "n", "off":
		return false, true, nil
	default:
		return false, true, fmt.Errorf("invalid boolean")
	}
}

func qaModelFallback(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || value == placeholderLocalChat {
		return ""
	}
	return value
}

func anyNonBlank(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func jsonEqual(left, right json.RawMessage) bool {
	return compactJSON(left) == compactJSON(right)
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(encoded)
}

func intPtrEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	return intPtr(*value)
}

func stringPtr(value string) *string { return &value }

func providerPtr(value service.Provider) *service.Provider { return &value }

func boolPtr(value bool) *bool { return &value }

func intPtr(value int) *int { return &value }

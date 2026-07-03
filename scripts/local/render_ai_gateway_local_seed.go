package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	storageModeEncryptedColumn = "encrypted_column"
	fingerprintContext         = "ai-gateway credential fingerprint v1"
	defaultTimeoutMS           = 60000
	defaultQATimeoutSeconds    = 60
	defaultQAMaxTokens         = 4096
	defaultQATemperature       = "0.700"
)

type localSeedConfig struct {
	Provider              string
	BaseURL               string
	APIKey                string
	ChatModel             string
	EmbeddingModel        string
	EmbeddingDimensions   int
	RerankModel           string
	RerankTopN            int
	EncryptionKey         string
	EncryptionKeyRef      string
	TimeoutMS             int
	QATimeoutSeconds      int
	QAMaxTokens           int
	QATemperature         string
	ChatDefaultParameters string
}

type encryptedCredential struct {
	ID           string
	ProfileID    string
	Ciphertext   string
	Nonce        string
	Fingerprint  string
	KeyLast4     string
	KeyVersion   string
	StorageMode  string
	CreatedBy    string
	CreatedAtSQL string
}

func main() {
	enabled, err := localSeedEnabled(os.Getenv("AI_GATEWAY_LOCAL_SEED_ENABLED"))
	if err != nil {
		fail(err)
	}
	if !enabled {
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fail(err)
	}
	credentials, err := encryptCredentials(cfg)
	if err != nil {
		fail(err)
	}

	fmt.Print(renderSQL(cfg, credentials))
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "render AI Gateway local seed: %v\n", err)
	os.Exit(1)
}

func localSeedEnabled(raw string) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	default:
		return false, fmt.Errorf("AI_GATEWAY_LOCAL_SEED_ENABLED must be true or false")
	}
}

func loadConfig() (localSeedConfig, error) {
	if missing := missingRequiredEnv(); len(missing) > 0 {
		return localSeedConfig{}, fmt.Errorf("missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	provider := requiredEnv("AI_GATEWAY_LOCAL_PROVIDER")
	if !isAllowedProvider(provider) {
		return localSeedConfig{}, fmt.Errorf("AI_GATEWAY_LOCAL_PROVIDER must be one of openai_compatible, siliconflow, or local_compatible")
	}
	baseURL := requiredEnv("AI_GATEWAY_LOCAL_PROVIDER_BASE_URL")
	if err := validateBaseURL(baseURL); err != nil {
		return localSeedConfig{}, err
	}
	apiKey := requiredEnv("AI_GATEWAY_LOCAL_PROVIDER_API_KEY")
	chatModel := requiredEnv("AI_GATEWAY_LOCAL_CHAT_MODEL")
	embeddingModel := requiredEnv("AI_GATEWAY_LOCAL_EMBEDDING_MODEL")
	rerankModel := requiredEnv("AI_GATEWAY_LOCAL_RERANK_MODEL")
	encryptionKey := requiredEnv("AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY")
	if len(encryptionKey) < 16 {
		return localSeedConfig{}, fmt.Errorf("AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY must be at least 16 characters")
	}
	encryptionKeyRef := requiredEnv("AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY_REF")

	embeddingDimensions, err := positiveIntEnv("AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS")
	if err != nil {
		return localSeedConfig{}, err
	}
	rerankTopN, err := positiveIntEnv("AI_GATEWAY_LOCAL_RERANK_TOP_N")
	if err != nil {
		return localSeedConfig{}, err
	}
	timeoutMS, err := optionalPositiveIntEnv("AI_GATEWAY_DEFAULT_TIMEOUT_MS", defaultTimeoutMS)
	if err != nil {
		return localSeedConfig{}, err
	}
	if timeoutMS < 1000 {
		return localSeedConfig{}, fmt.Errorf("AI_GATEWAY_DEFAULT_TIMEOUT_MS must be >= 1000")
	}
	qaTimeoutSeconds, err := qaTimeoutSeconds()
	if err != nil {
		return localSeedConfig{}, err
	}
	qaMaxTokens, err := optionalPositiveIntEnv("AGENT_MAX_TOKENS", defaultQAMaxTokens)
	if err != nil {
		return localSeedConfig{}, err
	}
	qaTemperature := strings.TrimSpace(os.Getenv("AI_GATEWAY_LOCAL_QA_TEMPERATURE"))
	if qaTemperature == "" {
		qaTemperature = defaultQATemperature
	}
	if _, err := strconv.ParseFloat(qaTemperature, 64); err != nil {
		return localSeedConfig{}, fmt.Errorf("AI_GATEWAY_LOCAL_QA_TEMPERATURE must be numeric")
	}

	return localSeedConfig{
		Provider:              provider,
		BaseURL:               baseURL,
		APIKey:                apiKey,
		ChatModel:             chatModel,
		EmbeddingModel:        embeddingModel,
		EmbeddingDimensions:   embeddingDimensions,
		RerankModel:           rerankModel,
		RerankTopN:            rerankTopN,
		EncryptionKey:         encryptionKey,
		EncryptionKeyRef:      encryptionKeyRef,
		TimeoutMS:             timeoutMS,
		QATimeoutSeconds:      qaTimeoutSeconds,
		QAMaxTokens:           qaMaxTokens,
		QATemperature:         qaTemperature,
		ChatDefaultParameters: `{"temperature":0.2}`,
	}, nil
}

func requiredEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func missingRequiredEnv() []string {
	keys := []string{
		"AI_GATEWAY_LOCAL_PROVIDER",
		"AI_GATEWAY_LOCAL_PROVIDER_BASE_URL",
		"AI_GATEWAY_LOCAL_PROVIDER_API_KEY",
		"AI_GATEWAY_LOCAL_CHAT_MODEL",
		"AI_GATEWAY_LOCAL_EMBEDDING_MODEL",
		"AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS",
		"AI_GATEWAY_LOCAL_RERANK_MODEL",
		"AI_GATEWAY_LOCAL_RERANK_TOP_N",
		"AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY",
		"AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY_REF",
	}
	var missing []string
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func isAllowedProvider(provider string) bool {
	switch provider {
	case "openai_compatible", "siliconflow", "local_compatible":
		return true
	default:
		return false
	}
}

func validateBaseURL(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("AI_GATEWAY_LOCAL_PROVIDER_BASE_URL must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("AI_GATEWAY_LOCAL_PROVIDER_BASE_URL must use http or https")
	}
	return nil
}

func positiveIntEnv(key string) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return 0, fmt.Errorf("%s is required", key)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func optionalPositiveIntEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func qaTimeoutSeconds() (int, error) {
	raw := strings.TrimSpace(os.Getenv("AI_GATEWAY_TIMEOUT"))
	if raw == "" {
		return defaultQATimeoutSeconds, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("AI_GATEWAY_TIMEOUT must be a positive duration")
	}
	seconds := int(duration.Seconds())
	if seconds <= 0 {
		return 0, fmt.Errorf("AI_GATEWAY_TIMEOUT must be at least 1s")
	}
	return seconds, nil
}

func encryptCredentials(cfg localSeedConfig) ([]encryptedCredential, error) {
	rawKey := sha256.Sum256([]byte(cfg.EncryptionKey))
	block, err := aes.NewCipher(rawKey[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	fingerprintKey := hmacSHA256(hmacSHA256(rawKey[:], []byte(fingerprintContext)), []byte(cfg.APIKey))
	fingerprint := hex.EncodeToString(fingerprintKey)
	profiles := []struct {
		credentialID string
		profileID    string
	}{
		{"cred-local-chat", "default-chat"},
		{"cred-local-embedding", "default-embedding"},
		{"cred-local-rerank", "default-rerank"},
	}
	result := make([]encryptedCredential, 0, len(profiles))
	for _, profile := range profiles {
		nonce := make([]byte, aead.NonceSize())
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return nil, err
		}
		ciphertext := aead.Seal(nil, nonce, []byte(cfg.APIKey), nil)
		result = append(result, encryptedCredential{
			ID:           profile.credentialID,
			ProfileID:    profile.profileID,
			Ciphertext:   hex.EncodeToString(ciphertext),
			Nonce:        hex.EncodeToString(nonce),
			Fingerprint:  fingerprint,
			KeyLast4:     last4(cfg.APIKey),
			KeyVersion:   cfg.EncryptionKeyRef,
			StorageMode:  storageModeEncryptedColumn,
			CreatedBy:    "usr_local_admin",
			CreatedAtSQL: "now()",
		})
	}
	return result, nil
}

func hmacSHA256(key, message []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil)
}

func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}

func renderSQL(cfg localSeedConfig, credentials []encryptedCredential) string {
	var b strings.Builder
	b.WriteString("-- Generated from the loaded config profile by scripts/local/render_ai_gateway_local_seed.go.\n")
	b.WriteString("-- Generated SQL contains encrypted provider credentials; do not commit generated output.\n")
	b.WriteString("\\connect ai_gateway_system\n\n")
	b.WriteString("BEGIN;\n\n")
	b.WriteString("UPDATE model_profiles\n")
	b.WriteString("SET is_default = false, updated_at = now()\n")
	b.WriteString("WHERE purpose IN ('chat', 'embedding', 'rerank')\n")
	b.WriteString("  AND id NOT IN ('default-chat', 'default-embedding', 'default-rerank')\n")
	b.WriteString("  AND enabled = true AND is_default = true AND deleted_at IS NULL;\n\n")
	b.WriteString("INSERT INTO model_profiles (\n")
	b.WriteString("    id, name, purpose, provider, base_url, model, enabled, is_default,\n")
	b.WriteString("    timeout_ms, api_key_configured, supports_streaming, dimensions, top_n,\n")
	b.WriteString("    default_parameters_json, credential_id, created_by_user_id,\n")
	b.WriteString("    updated_by_user_id, created_at, updated_at\n")
	b.WriteString(")\n")
	b.WriteString("VALUES\n")
	b.WriteString(fmt.Sprintf(
		"    ('default-chat', %s, 'chat', %s, %s, %s, true, true, %d, true, true, null, null, %s::jsonb, 'cred-local-chat', 'usr_local_admin', 'usr_local_admin', now(), now()),\n",
		sqlString(profileName(cfg.Provider, "chat")),
		sqlString(cfg.Provider),
		sqlString(cfg.BaseURL),
		sqlString(cfg.ChatModel),
		cfg.TimeoutMS,
		sqlString(cfg.ChatDefaultParameters),
	))
	b.WriteString(fmt.Sprintf(
		"    ('default-embedding', %s, 'embedding', %s, %s, %s, true, true, %d, true, false, %d, null, '{}'::jsonb, 'cred-local-embedding', 'usr_local_admin', 'usr_local_admin', now(), now()),\n",
		sqlString(profileName(cfg.Provider, "embedding")),
		sqlString(cfg.Provider),
		sqlString(cfg.BaseURL),
		sqlString(cfg.EmbeddingModel),
		cfg.TimeoutMS,
		cfg.EmbeddingDimensions,
	))
	b.WriteString(fmt.Sprintf(
		"    ('default-rerank', %s, 'rerank', %s, %s, %s, true, true, %d, true, false, null, %d, '{}'::jsonb, 'cred-local-rerank', 'usr_local_admin', 'usr_local_admin', now(), now())\n",
		sqlString(profileName(cfg.Provider, "rerank")),
		sqlString(cfg.Provider),
		sqlString(cfg.BaseURL),
		sqlString(cfg.RerankModel),
		cfg.TimeoutMS,
		cfg.RerankTopN,
	))
	b.WriteString("ON CONFLICT (id) DO UPDATE\n")
	b.WriteString("SET name = EXCLUDED.name,\n")
	b.WriteString("    provider = EXCLUDED.provider,\n")
	b.WriteString("    base_url = EXCLUDED.base_url,\n")
	b.WriteString("    model = EXCLUDED.model,\n")
	b.WriteString("    enabled = EXCLUDED.enabled,\n")
	b.WriteString("    is_default = EXCLUDED.is_default,\n")
	b.WriteString("    timeout_ms = EXCLUDED.timeout_ms,\n")
	b.WriteString("    api_key_configured = EXCLUDED.api_key_configured,\n")
	b.WriteString("    supports_streaming = EXCLUDED.supports_streaming,\n")
	b.WriteString("    dimensions = EXCLUDED.dimensions,\n")
	b.WriteString("    top_n = EXCLUDED.top_n,\n")
	b.WriteString("    default_parameters_json = EXCLUDED.default_parameters_json,\n")
	b.WriteString("    credential_id = EXCLUDED.credential_id,\n")
	b.WriteString("    updated_by_user_id = EXCLUDED.updated_by_user_id,\n")
	b.WriteString("    updated_at = now(),\n")
	b.WriteString("    deleted_at = null;\n\n")
	b.WriteString("UPDATE provider_credentials\n")
	b.WriteString("SET status = 'rotated', rotated_at = now(), disabled_at = null\n")
	b.WriteString("WHERE profile_id IN ('default-chat', 'default-embedding', 'default-rerank')\n")
	b.WriteString("  AND id NOT IN ('cred-local-chat', 'cred-local-embedding', 'cred-local-rerank')\n")
	b.WriteString("  AND status = 'active' AND deleted_at IS NULL;\n\n")
	b.WriteString("INSERT INTO provider_credentials (\n")
	b.WriteString("    id, profile_id, storage_mode, ciphertext, nonce, encryption_key_version,\n")
	b.WriteString("    fingerprint_sha256, key_last4, status, created_by_user_id, created_at\n")
	b.WriteString(")\n")
	b.WriteString("VALUES\n")
	for i, credential := range credentials {
		suffix := ",\n"
		if i == len(credentials)-1 {
			suffix = "\n"
		}
		b.WriteString(fmt.Sprintf(
			"    (%s, %s, %s, decode(%s, 'hex'), decode(%s, 'hex'), %s, %s, %s, 'active', %s, now())%s",
			sqlString(credential.ID),
			sqlString(credential.ProfileID),
			sqlString(credential.StorageMode),
			sqlString(credential.Ciphertext),
			sqlString(credential.Nonce),
			sqlString(credential.KeyVersion),
			sqlString(credential.Fingerprint),
			sqlString(credential.KeyLast4),
			sqlString(credential.CreatedBy),
			suffix,
		))
	}
	b.WriteString("ON CONFLICT (id) DO UPDATE\n")
	b.WriteString("SET profile_id = EXCLUDED.profile_id,\n")
	b.WriteString("    storage_mode = EXCLUDED.storage_mode,\n")
	b.WriteString("    ciphertext = EXCLUDED.ciphertext,\n")
	b.WriteString("    nonce = EXCLUDED.nonce,\n")
	b.WriteString("    encryption_key_version = EXCLUDED.encryption_key_version,\n")
	b.WriteString("    fingerprint_sha256 = EXCLUDED.fingerprint_sha256,\n")
	b.WriteString("    key_last4 = EXCLUDED.key_last4,\n")
	b.WriteString("    status = EXCLUDED.status,\n")
	b.WriteString("    disabled_at = null,\n")
	b.WriteString("    deleted_at = null;\n\n")
	b.WriteString("COMMIT;\n\n")
	b.WriteString("\\connect qa_system\n\n")
	b.WriteString("BEGIN;\n\n")
	b.WriteString("UPDATE llm_config_versions\n")
	b.WriteString("SET is_active = false\n")
	b.WriteString("WHERE is_active = true\n")
	b.WriteString("  AND NOT (\n")
	b.WriteString("    provider = 'ai-gateway'\n")
	b.WriteString("    AND COALESCE(profile_id, '') = 'default-chat'\n")
	b.WriteString(fmt.Sprintf("    AND model_name = %s\n", sqlString(cfg.ChatModel)))
	b.WriteString(fmt.Sprintf("    AND timeout_seconds = %d\n", cfg.QATimeoutSeconds))
	b.WriteString(fmt.Sprintf("    AND max_tokens = %d\n", cfg.QAMaxTokens))
	b.WriteString(fmt.Sprintf("    AND temperature = %s\n", cfg.QATemperature))
	b.WriteString("  );\n\n")
	b.WriteString("INSERT INTO llm_config_versions (\n")
	b.WriteString("    version_no, provider, profile_id, api_endpoint, api_key_encrypted,\n")
	b.WriteString("    api_key_last4, token_header, model_name, timeout_seconds, temperature,\n")
	b.WriteString("    max_tokens, is_active, created_by_user_id\n")
	b.WriteString(")\n")
	b.WriteString("SELECT\n")
	b.WriteString("    (SELECT COALESCE(MAX(version_no), 0) + 1 FROM llm_config_versions),\n")
	b.WriteString("    'ai-gateway', 'default-chat', null, null, null, 'X-Service-Token',\n")
	b.WriteString(fmt.Sprintf("    %s, %d, %s, %d, true, 'usr_local_admin'\n",
		sqlString(cfg.ChatModel),
		cfg.QATimeoutSeconds,
		cfg.QATemperature,
		cfg.QAMaxTokens,
	))
	b.WriteString("WHERE NOT EXISTS (\n")
	b.WriteString("    SELECT 1 FROM llm_config_versions\n")
	b.WriteString("    WHERE is_active = true\n")
	b.WriteString("      AND provider = 'ai-gateway'\n")
	b.WriteString("      AND COALESCE(profile_id, '') = 'default-chat'\n")
	b.WriteString(fmt.Sprintf("      AND model_name = %s\n", sqlString(cfg.ChatModel)))
	b.WriteString(fmt.Sprintf("      AND timeout_seconds = %d\n", cfg.QATimeoutSeconds))
	b.WriteString(fmt.Sprintf("      AND max_tokens = %d\n", cfg.QAMaxTokens))
	b.WriteString(fmt.Sprintf("      AND temperature = %s\n", cfg.QATemperature))
	b.WriteString(");\n\n")
	b.WriteString("COMMIT;\n")
	return b.String()
}

func profileName(provider, purpose string) string {
	display := strings.ReplaceAll(provider, "_", " ")
	switch provider {
	case "siliconflow":
		display = "SiliconFlow"
	case "openai_compatible":
		display = "OpenAI-compatible"
	case "local_compatible":
		display = "local compatible"
	}
	return fmt.Sprintf("Local %s %s profile", display, purpose)
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

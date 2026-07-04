package service

import (
	"context"
	"encoding/json"
	"time"
)

type RetrievalSettings struct {
	TopK                int     `json:"topK"`
	ScoreThreshold      float64 `json:"scoreThreshold"`
	SimilarityThreshold float64 `json:"similarityThreshold,omitempty"`
	EnableRerank        bool    `json:"enableRerank"`
	UseRerank           bool    `json:"useRerank,omitempty"`
	RerankThreshold     float64 `json:"rerankThreshold"`
	RerankTopN          int     `json:"rerankTopN"`
	scoreThresholdSet   bool
	similaritySet       bool
	enableRerankSet     bool
	useRerankSet        bool
	rerankThresholdSet  bool
}

type retrievalSettingsJSON struct {
	TopK                int      `json:"topK"`
	ScoreThreshold      *float64 `json:"scoreThreshold"`
	SimilarityThreshold *float64 `json:"similarityThreshold,omitempty"`
	EnableRerank        *bool    `json:"enableRerank"`
	UseRerank           *bool    `json:"useRerank,omitempty"`
	RerankThreshold     *float64 `json:"rerankThreshold"`
	RerankTopN          int      `json:"rerankTopN"`
	TagFilters          struct{} `json:"tagFilters,omitempty"`
}

func (s *RetrievalSettings) UnmarshalJSON(data []byte) error {
	var raw retrievalSettingsJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.TopK = raw.TopK
	if raw.ScoreThreshold != nil {
		s.ScoreThreshold = *raw.ScoreThreshold
	}
	if raw.SimilarityThreshold != nil {
		s.SimilarityThreshold = *raw.SimilarityThreshold
	}
	s.EnableRerank = raw.EnableRerank != nil && *raw.EnableRerank
	s.UseRerank = raw.UseRerank != nil && *raw.UseRerank
	if raw.RerankThreshold != nil {
		s.RerankThreshold = *raw.RerankThreshold
	}
	s.RerankTopN = raw.RerankTopN
	s.scoreThresholdSet = raw.ScoreThreshold != nil
	s.similaritySet = raw.SimilarityThreshold != nil
	s.enableRerankSet = raw.EnableRerank != nil
	s.useRerankSet = raw.UseRerank != nil
	s.rerankThresholdSet = raw.RerankThreshold != nil
	return nil
}

func (s RetrievalSettings) HasScoreThreshold() bool {
	return s.scoreThresholdSet || s.similaritySet || s.ScoreThreshold > 0
}

func (s RetrievalSettings) WithScoreThresholdConfigured() RetrievalSettings {
	s.scoreThresholdSet = true
	return s
}

type StoredLLMConfig struct {
	ID              string
	Provider        string
	ProfileID       string
	APIEndpoint     string
	APIKeyEncrypted []byte
	APIKeyLast4     string
	TokenHeader     string
	Model           string
	TimeoutSeconds  int
	Temperature     float64
	MaxTokens       int
}

type LLMSettings struct {
	Provider         string  `json:"provider"`
	ProfileID        string  `json:"profileId,omitempty"`
	APIEndpoint      string  `json:"apiEndpoint,omitempty"`
	Model            string  `json:"model"`
	TimeoutSeconds   int     `json:"timeoutSeconds"`
	Temperature      float64 `json:"temperature"`
	MaxTokens        int     `json:"maxTokens"`
	TokenHeader      string  `json:"tokenHeader,omitempty"`
	APIKeyConfigured bool    `json:"apiKeyConfigured,omitempty"`
	APIKeyLast4      string  `json:"apiKeyLast4,omitempty"`
}

type QASettings struct {
	Retrieval               RetrievalSettings `json:"retrieval"`
	DefaultKnowledgeBaseIDs []string          `json:"defaultKnowledgeBaseIds"`
	LLM                     LLMSettings       `json:"llm"`
	SystemPrompt            string            `json:"systemPrompt"`
}

type LLMUpdate struct {
	ProfileID      string  `json:"profileId,omitempty"`
	APIEndpoint    string  `json:"apiEndpoint,omitempty"`
	APIKey         *string `json:"apiKey,omitempty"`
	Model          string  `json:"model"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
	Temperature    float64 `json:"temperature"`
	MaxTokens      int     `json:"maxTokens"`
	TokenHeader    string  `json:"tokenHeader"`
}

type UpdateQASettingsInput struct {
	Retrieval               *RetrievalSettings `json:"retrieval,omitempty"`
	DefaultKnowledgeBaseIDs *[]string          `json:"defaultKnowledgeBaseIds,omitempty"`
	LLM                     *LLMUpdate         `json:"llm,omitempty"`
	SystemPrompt            *string            `json:"systemPrompt,omitempty"`
}

type MCPServerRecord struct {
	ID                 string
	Alias              string
	DisplayName        string
	Transport          string
	Command            string
	Args               []string
	EndpointURL        string
	TokenEncrypted     []byte
	TokenLast4         string
	TokenHeader        string
	ToolTimeoutSeconds int
	Enabled            bool
	SortOrder          int
	ToolCount          int
	LastConnectedAt    *time.Time
	LastError          string
	CreatedByUserID    string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type MCPServer struct {
	ID                 string     `json:"id"`
	Alias              string     `json:"alias"`
	DisplayName        string     `json:"displayName"`
	Transport          string     `json:"transport"`
	Command            string     `json:"command,omitempty"`
	Args               []string   `json:"args"`
	EndpointURL        string     `json:"endpointUrl,omitempty"`
	TokenHeader        string     `json:"tokenHeader"`
	ToolTimeoutSeconds int        `json:"toolTimeoutSeconds"`
	Enabled            bool       `json:"enabled"`
	SortOrder          int        `json:"sortOrder"`
	TokenConfigured    bool       `json:"tokenConfigured"`
	TokenLast4         string     `json:"tokenLast4,omitempty"`
	ToolCount          int        `json:"toolCount"`
	LastConnectedAt    *time.Time `json:"lastConnectedAt,omitempty"`
	LastError          string     `json:"lastError,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type MCPServerInput struct {
	Alias              string   `json:"alias"`
	DisplayName        string   `json:"displayName"`
	Transport          string   `json:"transport"`
	Command            string   `json:"command,omitempty"`
	Args               []string `json:"args,omitempty"`
	EndpointURL        string   `json:"endpointUrl,omitempty"`
	Token              *string  `json:"token,omitempty"`
	TokenHeader        string   `json:"tokenHeader,omitempty"`
	ToolTimeoutSeconds int      `json:"toolTimeoutSeconds"`
	Enabled            bool     `json:"enabled"`
	SortOrder          int      `json:"sortOrder"`
}

type MCPServerPatch struct {
	DisplayName        *string   `json:"displayName,omitempty"`
	Transport          *string   `json:"transport,omitempty"`
	Command            *string   `json:"command,omitempty"`
	Args               *[]string `json:"args,omitempty"`
	EndpointURL        *string   `json:"endpointUrl,omitempty"`
	Token              *string   `json:"token,omitempty"`
	TokenHeader        *string   `json:"tokenHeader,omitempty"`
	ToolTimeoutSeconds *int      `json:"toolTimeoutSeconds,omitempty"`
	Enabled            *bool     `json:"enabled,omitempty"`
	SortOrder          *int      `json:"sortOrder,omitempty"`
}

type MCPConnectionTestInput struct {
	ID                 string   `json:"id,omitempty"`
	Transport          string   `json:"transport,omitempty"`
	Command            string   `json:"command,omitempty"`
	Args               []string `json:"args,omitempty"`
	EndpointURL        string   `json:"endpointUrl,omitempty"`
	Token              *string  `json:"token,omitempty"`
	TokenHeader        string   `json:"tokenHeader,omitempty"`
	ToolTimeoutSeconds int      `json:"toolTimeoutSeconds,omitempty"`
}

type LLMConnectionTestInput struct {
	APIEndpoint    string  `json:"apiEndpoint"`
	APIKey         *string `json:"apiKey,omitempty"`
	Model          string  `json:"model"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
	TokenHeader    string  `json:"tokenHeader,omitempty"`
}

type ToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type MCPConnectionTestResult struct {
	Success   bool          `json:"success"`
	ToolCount int           `json:"toolCount"`
	LatencyMS int64         `json:"latencyMs"`
	Tools     []ToolSummary `json:"tools"`
}

type LLMConnectionTestResult struct {
	Success   bool   `json:"success"`
	Model     string `json:"model"`
	LatencyMS int64  `json:"latencyMs"`
}

type RuntimeLLMConfig struct {
	Endpoint    string
	Token       string
	TokenHeader string
	ProfileID   string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Stream      bool
}

type RuntimeMCPConfig struct {
	ID          string
	Alias       string
	Transport   string
	Command     string
	Args        []string
	EndpointURL string
	Token       string
	TokenHeader string
	ToolTimeout time.Duration
}

type RuntimeConfiguration struct {
	LLM                     RuntimeLLMConfig
	SystemPrompt            string
	MCPServers              []RuntimeMCPConfig
	QAConfigVersionID       string
	LLMConfigVersionID      string
	Agent                   AgentConfig
	DefaultKnowledgeBaseIDs []string
	RetrievalSettings       RetrievalSettings
}

type BootstrapSettings struct {
	LLM          RuntimeLLMConfig
	SystemPrompt string
	MCPServer    *RuntimeMCPConfig
}

type AuditLog struct {
	UserID     string
	Action     string
	TargetType string
	TargetID   string
	BeforeData map[string]any
	AfterData  map[string]any
	RequestID  string
}

type SettingsRepository interface {
	GetActiveQAConfig(context.Context) (RetrievalSettings, []string, error)
	GetActiveQAConfigVersion(context.Context) (QAConfigVersion, error)
	CreateQAConfigVersion(context.Context, string, RetrievalSettings, []string, AgentConfig, string) error
	GetActiveLLMConfig(context.Context) (StoredLLMConfig, error)
	GetActiveLLMConfigVersion(context.Context) (LLMConfigVersion, error)
	CreateLLMConfigVersion(context.Context, string, StoredLLMConfig) error
	GetRuntimeSetting(context.Context, string) (string, error)
	UpsertRuntimeSetting(context.Context, string, string) error
	ListMCPServers(context.Context) ([]MCPServerRecord, error)
	GetMCPServer(context.Context, string) (MCPServerRecord, error)
	CreateMCPServer(context.Context, MCPServerRecord) (MCPServerRecord, error)
	UpdateMCPServer(context.Context, MCPServerRecord) (MCPServerRecord, error)
	DeleteMCPServer(context.Context, string) error
	UpdateMCPConnectionStatus(context.Context, string, int, *time.Time, string) error
	WriteAuditLog(context.Context, AuditLog) error
}

type SecretCipher interface {
	Encrypt(string) ([]byte, error)
	Decrypt([]byte) (string, error)
}

type RuntimeReloader interface {
	Reload(context.Context) error
}

type LLMConnectionTester interface {
	TestLLM(context.Context, RuntimeLLMConfig) (LLMConnectionTestResult, error)
}

type MCPConnectionTester interface {
	TestMCP(context.Context, RuntimeMCPConfig) (MCPConnectionTestResult, error)
}

type RuntimeConfigLoader interface {
	LoadRuntimeConfiguration(context.Context) (RuntimeConfiguration, error)
}

type MCPStatusUpdater interface {
	UpdateMCPConnectionStatus(context.Context, string, int, *time.Time, string) error
}

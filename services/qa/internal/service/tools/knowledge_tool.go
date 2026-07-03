package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

const (
	ToolSearchKnowledge    = "search_knowledge"
	ToolGetCitationSource  = "get_citation_source"
	maxKnowledgeResultSize = 8192 // bytes
)

type RetrievalTestInput struct {
	Question         string
	KnowledgeBaseIDs []string
	Retrieval        RetrievalSettings
}

type RetrievalSettings struct {
	TopK                     int
	ScoreThreshold           float64
	ScoreThresholdConfigured bool
	EnableRerank             bool
	RerankThreshold          float64
	RerankTopN               int
}

type RetrievalTestResult struct {
	RankNo          int
	KnowledgeBaseID string
	DocumentID      string
	DocumentName    string
	ChunkID         string
	SectionPath     string
	ContentPreview  string
	Score           float64
	RerankScore     *float64
	Metadata        map[string]any
}

type KnowledgeRetriever interface {
	Retrieve(context.Context, string, RetrievalTestInput) ([]RetrievalTestResult, error)
}

// KnowledgeToolClient adapts the knowledge service HTTP client into MCP tools
// that can be used by the agent loop.
type KnowledgeToolClient struct {
	retrievalClient KnowledgeRetriever
	timeout         time.Duration
}

type KnowledgeToolConfig struct {
	RetrievalClient KnowledgeRetriever
	Timeout         time.Duration
}

func NewKnowledgeToolClient(cfg KnowledgeToolConfig) (*KnowledgeToolClient, error) {
	if cfg.RetrievalClient == nil {
		return nil, errors.New("knowledge retriever is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &KnowledgeToolClient{
		retrievalClient: cfg.RetrievalClient,
		timeout:         cfg.Timeout,
	}, nil
}

func (c *KnowledgeToolClient) ListTools(ctx context.Context) ([]agent.ToolDefinition, error) {
	return []agent.ToolDefinition{
		{
			Type: "function",
			Function: agent.FunctionTool{
				Name:        ToolSearchKnowledge,
				Description: "Search the project knowledge base pool available to QA. Returns summarized results with citations.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The search query text.",
						},
						"knowledge_base_ids": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Optional list of knowledge base IDs to search. If empty, uses default configured bases or the project pool.",
						},
						"top_k": map[string]any{
							"type":        "integer",
							"minimum":     1,
							"maximum":     100,
							"description": "Maximum number of results to return.",
						},
						"score_threshold": map[string]any{
							"type":        "number",
							"minimum":     0.0,
							"maximum":     1.0,
							"description": "Minimum relevance score threshold.",
						},
						"enable_rerank": map[string]any{
							"type":        "boolean",
							"description": "Whether to enable reranking for better relevance.",
						},
					},
					"required":             []string{"query"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: agent.FunctionTool{
				Name:        ToolGetCitationSource,
				Description: "Look up source details for a citation or knowledge chunk when source lookup is available.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"citation_id": map[string]any{
							"type":        "string",
							"description": "Citation identifier returned by the answer citation list.",
						},
						"chunk_id": map[string]any{
							"type":        "string",
							"description": "Knowledge chunk identifier returned by search_knowledge.",
						},
					},
					"additionalProperties": false,
				},
			},
		},
	}, nil
}

func (c *KnowledgeToolClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (agent.ToolResult, error) {
	switch name {
	case ToolSearchKnowledge:
		return c.searchKnowledge(ctx, arguments)
	case ToolGetCitationSource:
		return c.getCitationSource(ctx, arguments)
	default:
		return agent.ToolResult{}, fmt.Errorf("knowledge tool %q is not registered", name)
	}
}

func (c *KnowledgeToolClient) searchKnowledge(ctx context.Context, arguments json.RawMessage) (agent.ToolResult, error) {
	var input struct {
		Query            string   `json:"query"`
		KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
		TopK             *int     `json:"top_k"`
		ScoreThreshold   *float64 `json:"score_threshold"`
		EnableRerank     *bool    `json:"enable_rerank"`
	}

	if err := decodeToolArguments(arguments, &input); err != nil {
		return toolFailure("invalid_arguments", err.Error()), nil
	}

	if strings.TrimSpace(input.Query) == "" {
		return toolFailure("invalid_arguments", "query must not be empty"), nil
	}

	// Get user ID from context
	userID := contextutil.UserIDFromContext(ctx)
	if strings.TrimSpace(userID) == "" {
		return toolFailure("invalid_arguments", "user ID is required"), nil
	}

	// Apply timeout
	toolCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Enforce knowledge base restrictions from context. Non-empty defaultKBIDs
	// acts as an allowlist. Empty defaultKBIDs means QA has no preselected KB
	// scope; Knowledge expands retrieval to the project pool when no IDs are
	// provided.
	requestKBIDs := contextutil.KnowledgeBaseIDsFromContext(ctx)
	defaultKBIDs := contextutil.DefaultKnowledgeBaseIDsFromContext(ctx)

	var targetKBIDs []string
	if len(requestKBIDs) > 0 {
		targetKBIDs = requestKBIDs
	} else {
		targetKBIDs = input.KnowledgeBaseIDs
	}

	if len(defaultKBIDs) > 0 {
		// QA config exists - enforce KB allowlist restriction
		allowed := make(map[string]struct{}, len(defaultKBIDs))
		for _, id := range defaultKBIDs {
			allowed[id] = struct{}{}
		}
		if len(targetKBIDs) == 0 {
			input.KnowledgeBaseIDs = defaultKBIDs
		} else {
			for _, id := range targetKBIDs {
				if _, ok := allowed[id]; !ok {
					return toolFailure("unauthorized_knowledge_bases", "one or more requested knowledge bases are not accessible"), nil
				}
			}
			input.KnowledgeBaseIDs = targetKBIDs
		}
	} else {
		// No QA config - no restriction
		input.KnowledgeBaseIDs = targetKBIDs
	}

	// Build retrieval input with QA config defaults as fallback
	defaultSettings := contextutil.RetrievalSettingsFromContext(ctx)

	retrievalInput := RetrievalTestInput{
		Question:         input.Query,
		KnowledgeBaseIDs: input.KnowledgeBaseIDs,
		Retrieval: RetrievalSettings{
			TopK:            defaultSettings.TopK,
			ScoreThreshold:  defaultSettings.ScoreThreshold,
			EnableRerank:    defaultSettings.EnableRerank,
			RerankThreshold: defaultSettings.RerankThreshold,
			RerankTopN:      defaultSettings.RerankTopN,
		},
	}
	scoreThresholdConfigured := defaultSettings.ScoreThresholdConfigured

	if input.TopK != nil && *input.TopK > 0 {
		topK := *input.TopK
		if topK > 100 {
			topK = 100
		}
		retrievalInput.Retrieval.TopK = topK
	}
	if input.ScoreThreshold != nil {
		threshold := clampScoreThreshold(*input.ScoreThreshold)
		if scoreThresholdConfigured && threshold > retrievalInput.Retrieval.ScoreThreshold {
			threshold = retrievalInput.Retrieval.ScoreThreshold
		}
		retrievalInput.Retrieval.ScoreThreshold = threshold
		scoreThresholdConfigured = true
	}
	if input.EnableRerank != nil {
		retrievalInput.Retrieval.EnableRerank = *input.EnableRerank
	}

	// Call knowledge service
	if scoreThresholdConfigured {
		retrievalInput.Retrieval.ScoreThresholdConfigured = true
	}
	results, err := c.retrievalClient.Retrieve(toolCtx, userID, retrievalInput)
	if err != nil {
		return toolFailure("retrieval_failed", "knowledge retrieval service failed"), nil
	}

	startCitationNo := contextutil.CitationNoFromContext(ctx)
	if startCitationNo <= 0 {
		startCitationNo = 1
	}

	// Generate sanitized summary with size limits at structure level
	summary := generateSearchSummary(results, startCitationNo)

	// No byte-level truncation - structure-level limits ensure valid JSON
	return agent.ToolResult{Content: summary}, nil
}

func clampScoreThreshold(threshold float64) float64 {
	if threshold < 0 {
		return 0
	}
	if threshold > 1 {
		return 1
	}
	return threshold
}

func (c *KnowledgeToolClient) getCitationSource(ctx context.Context, arguments json.RawMessage) (agent.ToolResult, error) {
	var input struct {
		CitationID string `json:"citation_id"`
		ChunkID    string `json:"chunk_id"`
	}

	if err := decodeToolArguments(arguments, &input); err != nil {
		return toolFailure("invalid_arguments", err.Error()), nil
	}

	if strings.TrimSpace(input.CitationID) == "" && strings.TrimSpace(input.ChunkID) == "" {
		return toolFailure("invalid_arguments", "either citation_id or chunk_id must be provided"), nil
	}

	// TODO: Implement citation source lookup via knowledge service
	// This requires knowledge service to provide a citation/chunk lookup endpoint
	// For now, return a placeholder indicating the feature is pending implementation

	summary := map[string]any{
		"status":  "pending",
		"message": "Citation source lookup is pending knowledge service endpoint implementation",
		"hint":    "Use the citation information already embedded in the answer for now",
	}

	payload, _ := json.Marshal(summary)
	return agent.ToolResult{Content: string(payload)}, nil
}

func generateSearchSummary(results []RetrievalTestResult, startCitationNo int) string {
	if len(results) == 0 {
		return `{"hit_count": 0, "message": "No relevant results found"}`
	}

	totalHits := len(results)
	maxResults := totalHits
	if maxResults > 10 {
		maxResults = 10
	}

	previewLen := 200
	contextLen := 500

	for maxResults > 0 {
		summary := map[string]any{
			"hit_count": totalHits,
			"returned":  maxResults,
			"results":   make([]map[string]any, 0, maxResults),
		}

		for i := 0; i < maxResults; i++ {
			result := results[i]
			citationNo := startCitationNo + i

			item := map[string]any{
				"citation_no":       citationNo,
				"rank":              result.RankNo,
				"score":             result.Score,
				"knowledge_base_id": truncateString(result.KnowledgeBaseID, 64),
				"document_id":       truncateString(result.DocumentID, 64),
				"document_name":     truncateString(result.DocumentName, 100),
				"chunk_id":          truncateString(result.ChunkID, 64),
				"section_path":      truncateString(result.SectionPath, 100),
				"preview":           truncateString(result.ContentPreview, previewLen),
				"context":           truncateString(result.ContentPreview, contextLen),
				"rerank_score":      result.RerankScore,
			}
			if result.Metadata != nil {
				if pageNumber, ok := result.Metadata["page_number"]; ok {
					item["page_number"] = pageNumber
				}
				if chunkType, ok := result.Metadata["chunk_type"]; ok {
					item["chunk_type"] = chunkType
				}
			}

			summary["results"] = append(summary["results"].([]map[string]any), item)
		}

		payload, _ := json.Marshal(summary)
		if len(payload) <= maxKnowledgeResultSize {
			return string(payload)
		}

		if maxResults > 1 {
			maxResults--
		} else if contextLen > 100 {
			contextLen -= 100
		} else if previewLen > 50 {
			previewLen -= 50
		} else {
			break
		}
	}

	truncatedSummary := map[string]any{
		"hit_count": totalHits,
		"returned":  0,
		"truncated": true,
		"message":   "Results truncated due to size limit",
	}
	payload, _ := json.Marshal(truncatedSummary)
	return string(payload)
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}

	suffix := "\n...[result truncated]"
	limit := maxBytes - len(suffix)
	if limit <= 0 {
		return suffix[:maxBytes]
	}

	// Ensure we don't break UTF-8 characters
	for limit > 0 && (value[limit]&0xC0) == 0x80 {
		limit--
	}

	return value[:limit] + suffix
}

func decodeToolArguments(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("arguments do not match the tool schema")
	}
	return nil
}

func toolFailure(code, message string) agent.ToolResult {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
	return agent.ToolResult{Content: string(payload), IsError: true}
}

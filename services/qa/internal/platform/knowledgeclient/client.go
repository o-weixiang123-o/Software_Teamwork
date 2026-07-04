package knowledgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

const (
	knowledgeRetrievalScopeHeader  = "X-Knowledge-Retrieval-Scope"
	knowledgeRetrievalScopeProject = "project"
)

type Client struct {
	baseURL      string
	endpoint     string
	serviceToken string
	http         *http.Client
}

func New(baseURL, serviceToken string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("knowledge service URL must be absolute http(s)")
	}
	if strings.TrimSpace(serviceToken) == "" {
		return nil, errors.New("service token is required")
	}
	if timeout <= 0 {
		return nil, errors.New("knowledge request timeout must be positive")
	}
	normalizedBaseURL := strings.TrimRight(parsed.String(), "/")
	return &Client{baseURL: normalizedBaseURL, endpoint: normalizedBaseURL + "/internal/v1/knowledge-queries", serviceToken: serviceToken, http: &http.Client{Timeout: timeout}}, nil
}

func (c *Client) Retrieve(ctx context.Context, userID string, input service.RetrievalTestInput) ([]service.RetrievalTestResult, error) {
	payload := map[string]any{"query": input.Question, "knowledgeBaseIds": input.KnowledgeBaseIDs}
	retrieval := input.Retrieval
	if retrieval.TopK == 0 {
		retrieval = input.Overrides
	}
	if retrieval.TopK > 0 {
		payload["topK"] = retrieval.TopK
	}
	if retrieval.HasScoreThreshold() {
		payload["scoreThreshold"] = retrieval.ScoreThreshold
	}
	payload["rerank"] = retrieval.EnableRerank
	if retrieval.RerankTopN > 0 {
		payload["rerankTopN"] = retrieval.RerankTopN
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode knowledge query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create knowledge query: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setTrustedHeaders(ctx, req, userID)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call knowledge service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, message := decodeErrorCode(resp.Body)
		if code == "forbidden" || (code == "not_found" && len(input.KnowledgeBaseIDs) > 0 && strings.Contains(message, "resource not found")) {
			return nil, service.NewError(service.CodeForbidden, "knowledge base access is forbidden", nil)
		}
		return nil, service.NewError(service.CodeDependency, "knowledge retrieval failed", fmt.Errorf("knowledge service returned HTTP %d", resp.StatusCode))
	}
	var decoded struct {
		Data struct {
			Results []struct {
				Score           float64        `json:"score"`
				VectorScore     *float64       `json:"vectorScore"`
				RerankScore     *float64       `json:"rerankScore"`
				KnowledgeBaseID string         `json:"knowledgeBaseId"`
				DocumentID      string         `json:"documentId"`
				ChunkID         string         `json:"chunkId"`
				DocumentName    string         `json:"documentName"`
				SectionPath     string         `json:"sectionPath"`
				ContentPreview  string         `json:"contentPreview"`
				ChunkIndex      *int           `json:"chunkIndex"`
				ChunkType       *string        `json:"chunkType"`
				Tags            []string       `json:"tags"`
				Metadata        map[string]any `json:"metadata"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode knowledge response: %w", err)
	}
	results := make([]service.RetrievalTestResult, 0, len(decoded.Data.Results))
	for i, item := range decoded.Data.Results {
		var vectorScore *float64
		if item.VectorScore != nil {
			score := *item.VectorScore
			vectorScore = &score
		} else if !retrieval.EnableRerank {
			score := item.Score
			vectorScore = &score
		}
		rerankScore := item.RerankScore
		if rerankScore == nil && retrieval.EnableRerank {
			score := item.Score
			rerankScore = &score
		}
		metadata := sanitizedMetadata(item.Metadata)
		if item.ChunkIndex != nil {
			metadata["chunkIndex"] = *item.ChunkIndex
		}
		if item.ChunkType != nil && strings.TrimSpace(*item.ChunkType) != "" {
			metadata["chunkType"] = strings.TrimSpace(*item.ChunkType)
		}
		if len(item.Tags) > 0 {
			metadata["tags"] = append([]string(nil), item.Tags...)
		}
		results = append(results, service.RetrievalTestResult{RankNo: i + 1, KnowledgeBaseID: item.KnowledgeBaseID, DocumentID: item.DocumentID, DocID: item.DocumentID, ChunkID: item.ChunkID, DocumentName: item.DocumentName, DocName: item.DocumentName, SectionPath: item.SectionPath, Score: item.Score, VectorScore: vectorScore, RerankScore: rerankScore, ContentPreview: item.ContentPreview, Text: item.ContentPreview, Metadata: metadata})
	}
	return results, nil
}

func (c *Client) CheckCitationSources(ctx context.Context, userID string, refs []service.CitationSourceRef) (map[string]bool, error) {
	availability := make(map[string]bool, len(refs))
	for _, ref := range refs {
		knowledgeBaseID := strings.TrimSpace(ref.KnowledgeBaseID)
		documentID := strings.TrimSpace(ref.DocumentID)
		if knowledgeBaseID == "" || documentID == "" {
			continue
		}
		key := service.CitationSourceRefKey(knowledgeBaseID, documentID)
		if _, exists := availability[key]; exists {
			continue
		}
		availability[key] = false
		endpoint := c.baseURL + "/internal/v1/documents/" + url.PathEscape(documentID) + "?knowledgeBaseId=" + url.QueryEscape(knowledgeBaseID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return availability, fmt.Errorf("create document visibility check: %w", err)
		}
		c.setUserScopedHeaders(ctx, req, userID)
		resp, err := c.http.Do(req)
		if err != nil {
			return availability, fmt.Errorf("call knowledge document visibility: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			availability[key] = true
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			availability[key] = false
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return availability, fmt.Errorf("knowledge document visibility returned HTTP %d", resp.StatusCode)
		default:
			return availability, fmt.Errorf("knowledge document visibility returned HTTP %d", resp.StatusCode)
		}
	}
	return availability, nil
}

// GetStats fetches knowledge base and document counts from the knowledge
// service's internal statistics endpoint. The endpoint uses service-level
// authentication and accepts a user context for product authorization while
// Knowledge reads totals from its fixed runtime namespace.
func (c *Client) GetStats(ctx context.Context, userID string) (int, int, error) {
	endpoint := c.baseURL + "/internal/v1/knowledge-statistics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("create knowledge stats request: %w", err)
	}
	req.Header.Set("X-Service-Token", c.serviceToken)
	req.Header.Set("X-Caller-Service", "qa")
	if userID = strings.TrimSpace(userID); userID != "" {
		req.Header.Set("X-User-Id", userID)
		req.Header.Set("X-User-Permissions", "knowledge:read")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("knowledge stats request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("knowledge stats returned HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data struct {
			KnowledgeBaseCount int64 `json:"knowledgeBaseCount"`
			DocumentCount      int64 `json:"documentCount"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, fmt.Errorf("decode knowledge stats: %w", err)
	}
	return int(body.Data.KnowledgeBaseCount), int(body.Data.DocumentCount), nil
}

func (c *Client) setTrustedHeaders(ctx context.Context, req *http.Request, userID string) {
	req.Header.Set("X-Service-Token", c.serviceToken)
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set(knowledgeRetrievalScopeHeader, knowledgeRetrievalScopeProject)
	req.Header.Set("X-User-Id", userID)
	c.setRequestUserContextHeaders(ctx, req)
}

func (c *Client) setUserScopedHeaders(ctx context.Context, req *http.Request, userID string) {
	req.Header.Set("X-Service-Token", c.serviceToken)
	req.Header.Set("X-Caller-Service", "qa")
	req.Header.Set("X-User-Id", userID)
	c.setRequestUserContextHeaders(ctx, req)
}

func (c *Client) setRequestUserContextHeaders(ctx context.Context, req *http.Request) {
	if requestID := service.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	if roles := service.UserRolesFromContext(ctx); roles != "" {
		req.Header.Set("X-User-Roles", roles)
	}
	if permissions := service.UserPermissionsFromContext(ctx); permissions != "" {
		req.Header.Set("X-User-Permissions", permissions)
	}
}

func decodeErrorCode(body io.Reader) (string, string) {
	var decoded struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 4096)).Decode(&decoded); err != nil {
		return "", ""
	}
	return strings.TrimSpace(decoded.Error.Code), strings.TrimSpace(decoded.Error.Message)
}

func sanitizedMetadata(input map[string]any) map[string]any {
	metadata := map[string]any{}
	for key, value := range input {
		switch key {
		case "vector", "embedding", "payload", "prompt", "internalUrl", "objectKey":
			continue
		default:
			metadata[key] = value
		}
	}
	return metadata
}

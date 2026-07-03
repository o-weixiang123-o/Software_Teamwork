package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/ai-gateway/internal/service"
)

const (
	embeddingsPath = "/embeddings"
	rerankPath     = "/rerank"
)

type HTTPClient struct {
	client *http.Client
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{client: client}
}

func (c *HTTPClient) CreateEmbeddings(ctx context.Context, req service.ProviderEmbeddingRequest) (service.EmbeddingResponse, service.ProviderCallMetadata, error) {
	body, err := embeddingRequestBody(req)
	if err != nil {
		return service.EmbeddingResponse{}, service.ProviderCallMetadata{}, err
	}
	var response service.EmbeddingResponse
	metadata, err := c.doJSON(ctx, req.RequestID, req.BaseURL, embeddingsPath, req.APIKey, req.TimeoutMS, body, &response)
	if err != nil {
		return service.EmbeddingResponse{}, metadata, err
	}
	if response.Object == "" {
		response.Object = "list"
	}
	if response.Model == "" {
		response.Model = req.Model
	}
	return response, metadata, nil
}

func (c *HTTPClient) CreateReranking(ctx context.Context, req service.ProviderRerankingRequest) (service.RerankingResponse, service.ProviderCallMetadata, error) {
	body, err := rerankingRequestBody(req)
	if err != nil {
		return service.RerankingResponse{}, service.ProviderCallMetadata{}, err
	}
	var providerResponse rerankingProviderResponse
	metadata, err := c.doJSON(ctx, req.RequestID, req.BaseURL, rerankPath, req.APIKey, req.TimeoutMS, body, &providerResponse)
	if err != nil {
		return service.RerankingResponse{}, metadata, err
	}
	response, err := normalizeRerankingResponse(req, providerResponse)
	if err != nil {
		return service.RerankingResponse{}, metadata, err
	}
	return response, metadata, nil
}

func (c *HTTPClient) doJSON(ctx context.Context, requestID, baseURL, path, apiKey string, timeoutMS int, body map[string]any, target any) (service.ProviderCallMetadata, error) {
	endpoint, err := joinURL(baseURL, path)
	if err != nil {
		return service.ProviderCallMetadata{}, service.NewProviderError(service.CodeDependency, "provider configuration is invalid", nil, err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return service.ProviderCallMetadata{}, service.NewProviderError(service.CodeDependency, "provider request could not be encoded", nil, err)
	}
	if timeoutMS < 1000 {
		timeoutMS = service.DefaultTimeoutMS
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return service.ProviderCallMetadata{}, service.NewProviderError(service.CodeDependency, "provider request could not be created", nil, err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(requestID) != "" {
		httpReq.Header.Set("X-Request-Id", strings.TrimSpace(requestID))
	}
	if strings.TrimSpace(apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return service.ProviderCallMetadata{}, providerTransportError(err)
	}
	defer resp.Body.Close()
	metadata := service.ProviderCallMetadata{StatusCode: resp.StatusCode}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return metadata, providerStatusError(resp.StatusCode)
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(target); err != nil {
		return metadata, service.NewProviderError(service.CodeDependency, "provider returned an invalid response", &metadata.StatusCode, err)
	}
	return metadata, nil
}

func embeddingRequestBody(req service.ProviderEmbeddingRequest) (map[string]any, error) {
	body, err := defaultParameters(req.DefaultParameters)
	if err != nil {
		return nil, err
	}
	body["model"] = req.Model
	body["input"] = append([]string(nil), req.Input...)
	if req.Dimensions != nil {
		body["dimensions"] = *req.Dimensions
	}
	if req.Provider == service.ProviderSiliconFlow {
		// SiliconFlow bge embeddings reject a dimensions field; keep the
		// configured dimension in gateway invocation records, but omit it upstream.
		delete(body, "dimensions")
	}
	if strings.TrimSpace(req.EncodingFormat) != "" {
		body["encoding_format"] = strings.TrimSpace(req.EncodingFormat)
	}
	if strings.TrimSpace(req.User) != "" {
		body["user"] = strings.TrimSpace(req.User)
	}
	return body, nil
}

func rerankingRequestBody(req service.ProviderRerankingRequest) (map[string]any, error) {
	body, err := defaultParameters(req.DefaultParameters)
	if err != nil {
		return nil, err
	}
	documents := make([]string, len(req.Documents))
	for i, document := range req.Documents {
		documents[i] = document.Text
	}
	body["model"] = req.Model
	body["query"] = req.Query
	body["documents"] = documents
	body["return_documents"] = false
	if req.TopN != nil {
		body["top_n"] = *req.TopN
	}
	if len(req.Metadata) > 0 {
		body["metadata"] = req.Metadata
	}
	return body, nil
}

func defaultParameters(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, service.NewProviderError(service.CodeDependency, "provider default parameters are invalid", nil, err)
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

type rerankingProviderResponse struct {
	Object  string                    `json:"object"`
	Data    []rerankingProviderData   `json:"data"`
	Results []rerankingProviderResult `json:"results"`
	Model   string                    `json:"model"`
	Usage   *service.TokenUsage       `json:"usage"`
	Meta    *rerankingProviderMeta    `json:"meta"`
}

type rerankingProviderData struct {
	Index      int      `json:"index"`
	DocumentID string   `json:"document_id"`
	Score      *float64 `json:"score"`
}

type rerankingProviderResult struct {
	Index          int      `json:"index"`
	RelevanceScore *float64 `json:"relevance_score"`
	Score          *float64 `json:"score"`
}

type rerankingProviderMeta struct {
	Tokens *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"tokens"`
}

func normalizeRerankingResponse(req service.ProviderRerankingRequest, response rerankingProviderResponse) (service.RerankingResponse, error) {
	out := service.RerankingResponse{
		Object: "list",
		Model:  response.Model,
		Usage:  response.Usage,
	}
	if out.Model == "" {
		out.Model = req.Model
	}
	if len(response.Data) > 0 {
		out.Data = make([]service.RerankingResult, len(response.Data))
		seen := make(map[int]struct{}, len(response.Data))
		for i, item := range response.Data {
			if item.Index < 0 || item.Index >= len(req.Documents) {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			if _, ok := seen[item.Index]; ok {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			if item.Score == nil {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			if item.DocumentID != "" && item.DocumentID != req.Documents[item.Index].ID {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			out.Data[i] = service.RerankingResult{Index: item.Index, DocumentID: req.Documents[item.Index].ID, Score: *item.Score}
			seen[item.Index] = struct{}{}
		}
		return out, nil
	}
	if len(response.Results) > 0 {
		out.Data = make([]service.RerankingResult, len(response.Results))
		seen := make(map[int]struct{}, len(response.Results))
		for i, item := range response.Results {
			if item.Index < 0 || item.Index >= len(req.Documents) {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			if _, ok := seen[item.Index]; ok {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			score := item.RelevanceScore
			if score == nil {
				score = item.Score
			}
			if score == nil {
				return service.RerankingResponse{}, invalidProviderResponse()
			}
			out.Data[i] = service.RerankingResult{Index: item.Index, DocumentID: req.Documents[item.Index].ID, Score: *score}
			seen[item.Index] = struct{}{}
		}
		if out.Usage == nil && response.Meta != nil && response.Meta.Tokens != nil {
			inputTokens := response.Meta.Tokens.InputTokens
			outputTokens := response.Meta.Tokens.OutputTokens
			out.Usage = &service.TokenUsage{
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      inputTokens + outputTokens,
			}
		}
		return out, nil
	}
	return service.RerankingResponse{}, invalidProviderResponse()
}

func invalidProviderResponse() error {
	return service.NewProviderError(service.CodeDependency, "provider returned an invalid response", nil, nil)
}

func joinURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid provider base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func providerTransportError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return service.NewProviderError(service.CodeDependency, "provider request failed", nil, err)
	}
	return service.NewProviderError(service.CodeDependency, "provider request failed", nil, err)
}

func providerStatusError(status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return service.NewProviderError(service.CodeValidation, "provider rejected model request", &status, nil)
	case http.StatusTooManyRequests:
		return service.NewProviderError(service.CodeRateLimited, "provider rate limit exceeded", &status, nil)
	default:
		return service.NewProviderError(service.CodeDependency, "provider request failed", &status, nil)
	}
}

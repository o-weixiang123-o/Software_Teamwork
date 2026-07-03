package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/document/internal/service"
)

const maxChatResponseBytes = 2 << 20

type ChatClient struct {
	baseURL          trustedBaseURL
	serviceToken     string
	defaultProfileID string
	defaultModel     string
	httpClient       *http.Client
}

func NewChatClient(baseURL, serviceToken, defaultProfileID, defaultModel string, httpClient *http.Client) (*ChatClient, error) {
	normalized, err := validateAIGatewayBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(defaultProfileID) == "" {
		return nil, errors.New("DOCUMENT_AI_GATEWAY_PROFILE_ID is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultChatTimeout}
	}
	return &ChatClient{
		baseURL:          normalized,
		serviceToken:     strings.TrimSpace(serviceToken),
		defaultProfileID: strings.TrimSpace(defaultProfileID),
		defaultModel:     strings.TrimSpace(defaultModel),
		httpClient:       httpClient,
	}, nil
}

func (c *ChatClient) CreateChatCompletion(ctx context.Context, reqCtx service.RequestContext, input service.ChatCompletionRequest) (service.ChatCompletionResponse, error) {
	if len(input.Messages) == 0 {
		return service.ChatCompletionResponse{}, service.ValidationError(map[string]string{"messages": "must not be empty"})
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = c.defaultModel
	}
	profileID := strings.TrimSpace(input.ProfileID)
	if profileID == "" {
		profileID = c.defaultProfileID
	}
	body := chatCompletionRequest{
		Model:       model,
		ProfileID:   profileID,
		Messages:    input.Messages,
		Temperature: input.Temperature,
		TopP:        input.TopP,
		MaxTokens:   input.MaxTokens,
		Stream:      false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeInternal, "encode ai gateway request", err)
	}
	endpoint, err := c.baseURL.Join("internal/v1/chat/completions")
	if err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "build ai gateway chat request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "build ai gateway chat request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}
	req.Header.Set("X-Caller-Service", callerService)
	if strings.TrimSpace(reqCtx.RequestID) != "" {
		req.Header.Set("X-Request-Id", strings.TrimSpace(reqCtx.RequestID))
	}
	if strings.TrimSpace(reqCtx.UserID) != "" {
		req.Header.Set("X-User-Id", strings.TrimSpace(reqCtx.UserID))
	}
	if len(reqCtx.Roles) > 0 {
		req.Header.Set("X-User-Roles", strings.Join(reqCtx.Roles, ","))
	}
	if len(reqCtx.Permissions) > 0 {
		req.Header.Set("X-User-Permissions", strings.Join(reqCtx.Permissions, ","))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "ai gateway chat request failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusBadRequest {
			return service.ChatCompletionResponse{}, service.NewError(service.CodeValidation, "ai gateway rejected chat request", fmt.Errorf("status %d", resp.StatusCode))
		}
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "ai gateway chat request failed", fmt.Errorf("status %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxChatResponseBytes+1))
	if err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "read ai gateway chat response", err)
	}
	if len(data) > maxChatResponseBytes {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "ai gateway chat response too large", nil)
	}
	var decoded chatCompletionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "decode ai gateway chat response", err)
	}
	if len(decoded.Choices) == 0 {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "ai gateway chat response has no choices", nil)
	}
	choice := decoded.Choices[0]
	if strings.TrimSpace(choice.Message.Content) == "" {
		return service.ChatCompletionResponse{}, service.NewError(service.CodeDependency, "ai gateway chat response content is empty", nil)
	}
	return service.ChatCompletionResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		Usage: service.ChatTokenUsage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
	}, nil
}

type chatCompletionRequest struct {
	Model       string                `json:"model,omitempty"`
	ProfileID   string                `json:"profile_id"`
	Messages    []service.ChatMessage `json:"messages"`
	Temperature *float64              `json:"temperature,omitempty"`
	TopP        *float64              `json:"top_p,omitempty"`
	MaxTokens   int                   `json:"max_tokens,omitempty"`
	Stream      bool                  `json:"stream"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message      service.ChatMessage `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

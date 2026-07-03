package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Service) CreateEmbeddings(ctx context.Context, req RequestContext, input EmbeddingInput) (EmbeddingResponse, error) {
	fields := validateEmbeddingInput(input)
	if len(fields) > 0 {
		return EmbeddingResponse{}, ValidationError(fields)
	}
	if s.invoker == nil {
		return EmbeddingResponse{}, NotImplementedError("model invocation is not implemented")
	}
	profile, credential, err := s.resolveInvocationProfile(ctx, input.ProfileID, PurposeEmbedding)
	if err != nil {
		return EmbeddingResponse{}, err
	}
	providerModel, err := modelForInvocation(input.Model, profile)
	if err != nil {
		return EmbeddingResponse{}, err
	}
	apiKey, err := s.decryptCredential(credential)
	if err != nil {
		return EmbeddingResponse{}, err
	}
	defer func() { apiKey = "" }()

	dimensions := cloneIntPtr(input.Dimensions)
	if dimensions == nil {
		dimensions = cloneIntPtr(profile.Dimensions)
	}
	if dimensions == nil || *dimensions <= 0 {
		return EmbeddingResponse{}, DependencyError("embedding profile dimensions are not configured", nil)
	}
	startedAt := time.Now().UTC()
	invocation := s.newInvocation(req, OperationEmbedding, profile, providerModel, startedAt)
	invocation.InputCount = intPtr(len(input.Input))
	invocation.EmbeddingDimensions = cloneIntPtr(dimensions)
	encodingFormat := normalizedEncodingFormat(input.EncodingFormat)

	response, metadata, callErr := s.invoker.CreateEmbeddings(ctx, ProviderEmbeddingRequest{
		RequestID:         strings.TrimSpace(req.RequestID),
		Provider:          profile.Provider,
		BaseURL:           profile.BaseURL,
		APIKey:            apiKey,
		TimeoutMS:         profile.TimeoutMS,
		Model:             providerModel,
		Input:             append([]string(nil), input.Input...),
		Dimensions:        cloneIntPtr(dimensions),
		EncodingFormat:    encodingFormat,
		User:              strings.TrimSpace(input.User),
		DefaultParameters: cloneRaw(profile.DefaultParameters),
	})
	if callErr != nil {
		return EmbeddingResponse{}, s.finishInvocation(ctx, invocation, metadata, nil, callErr)
	}
	if err := validateEmbeddingResponse(response, len(input.Input), encodingFormat); err != nil {
		return EmbeddingResponse{}, s.finishInvocation(ctx, invocation, metadata, nil, err)
	}
	if err := s.finishInvocation(ctx, invocation, metadata, response.Usage, nil); err != nil {
		return EmbeddingResponse{}, err
	}
	return response, nil
}

func (s *Service) CreateReranking(ctx context.Context, req RequestContext, input RerankingInput) (RerankingResponse, error) {
	fields := validateRerankingInput(input)
	if len(fields) > 0 {
		return RerankingResponse{}, ValidationError(fields)
	}
	if s.invoker == nil {
		return RerankingResponse{}, NotImplementedError("model invocation is not implemented")
	}
	profile, credential, err := s.resolveInvocationProfile(ctx, input.ProfileID, PurposeRerank)
	if err != nil {
		return RerankingResponse{}, err
	}
	providerModel, err := modelForInvocation(input.Model, profile)
	if err != nil {
		return RerankingResponse{}, err
	}
	apiKey, err := s.decryptCredential(credential)
	if err != nil {
		return RerankingResponse{}, err
	}
	defer func() { apiKey = "" }()

	topN := cloneIntPtr(input.TopN)
	if topN == nil {
		topN = cloneIntPtr(profile.TopN)
	}
	if topN == nil || *topN <= 0 {
		return RerankingResponse{}, DependencyError("rerank profile topN is not configured", nil)
	}
	startedAt := time.Now().UTC()
	invocation := s.newInvocation(req, OperationReranking, profile, providerModel, startedAt)
	invocation.InputCount = intPtr(len(input.Documents))
	invocation.RerankTopN = cloneIntPtr(topN)

	response, metadata, callErr := s.invoker.CreateReranking(ctx, ProviderRerankingRequest{
		RequestID:         strings.TrimSpace(req.RequestID),
		Provider:          profile.Provider,
		BaseURL:           profile.BaseURL,
		APIKey:            apiKey,
		TimeoutMS:         profile.TimeoutMS,
		Model:             providerModel,
		Query:             strings.TrimSpace(input.Query),
		Documents:         cloneRerankingDocuments(input.Documents),
		TopN:              cloneIntPtr(topN),
		Metadata:          cloneStringMap(input.Metadata),
		DefaultParameters: cloneRaw(profile.DefaultParameters),
	})
	if callErr != nil {
		return RerankingResponse{}, s.finishInvocation(ctx, invocation, metadata, nil, callErr)
	}
	if err := validateRerankingResponse(response, input.Documents); err != nil {
		return RerankingResponse{}, s.finishInvocation(ctx, invocation, metadata, nil, err)
	}
	response = limitRerankingResponse(response, *topN)
	if err := s.finishInvocation(ctx, invocation, metadata, response.Usage, nil); err != nil {
		return RerankingResponse{}, err
	}
	return response, nil
}

func (s *Service) resolveInvocationProfile(ctx context.Context, profileID string, purpose Purpose) (ModelProfile, ProviderCredential, error) {
	if s.repo == nil {
		return ModelProfile{}, ProviderCredential{}, DependencyError("model profile store is unavailable", nil)
	}
	var profile ModelProfile
	var err error
	if strings.TrimSpace(profileID) != "" {
		profile, err = s.GetModelProfile(ctx, strings.TrimSpace(profileID))
		if err != nil {
			return ModelProfile{}, ProviderCredential{}, err
		}
	} else {
		profile, err = s.repo.GetDefaultModelProfile(ctx, purpose)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return ModelProfile{}, ProviderCredential{}, NotFoundError("default model profile not found", err)
			}
			return ModelProfile{}, ProviderCredential{}, DependencyError("model profile store is unavailable", err)
		}
	}
	if !profile.Enabled || profile.DeletedAt != nil {
		return ModelProfile{}, ProviderCredential{}, NotFoundError("model profile not found", ErrNotFound)
	}
	if profile.Purpose != purpose {
		return ModelProfile{}, ProviderCredential{}, ValidationError(map[string]string{"profile_id": "must reference a " + string(purpose) + " profile"})
	}
	if !profile.APIKeyConfigured || strings.TrimSpace(profile.CredentialID) == "" {
		return ModelProfile{}, ProviderCredential{}, DependencyError("model profile credential is not configured", nil)
	}
	credential, err := s.repo.GetActiveCredential(ctx, profile.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ModelProfile{}, ProviderCredential{}, DependencyError("model profile credential is not configured", err)
		}
		return ModelProfile{}, ProviderCredential{}, DependencyError("model profile credential store is unavailable", err)
	}
	return profile, credential, nil
}

func modelForInvocation(requestModel string, profile ModelProfile) (string, error) {
	profileModel := strings.TrimSpace(profile.Model)
	if profileModel == "" {
		return "", DependencyError("model profile model is not configured", nil)
	}
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" {
		return profileModel, nil
	}
	if requestModel != profileModel {
		return "", ValidationError(map[string]string{"model": "must match selected model profile"})
	}
	return profileModel, nil
}

func (s *Service) decryptCredential(credential ProviderCredential) (string, error) {
	if s.encryptor == nil {
		return "", DependencyError("credential encryption is not configured", nil)
	}
	apiKey, err := s.encryptor.Decrypt(credential)
	if err != nil {
		return "", DependencyError("model profile credential is unavailable", err)
	}
	if strings.TrimSpace(apiKey) == "" {
		return "", DependencyError("model profile credential is not configured", nil)
	}
	return apiKey, nil
}

func (s *Service) newInvocation(req RequestContext, operation string, profile ModelProfile, model string, startedAt time.Time) ProviderInvocation {
	return ProviderInvocation{
		ID:             newID("pinv"),
		RequestID:      strings.TrimSpace(req.RequestID),
		CallerService:  strings.TrimSpace(req.CallerService),
		ExternalUserID: strings.TrimSpace(req.UserID),
		Operation:      operation,
		ProfileID:      profile.ID,
		Provider:       profile.Provider,
		Model:          model,
		Stream:         false,
		Status:         InvocationSucceeded,
		AttemptCount:   1,
		CreatedAt:      startedAt,
	}
}

func (s *Service) finishInvocation(ctx context.Context, invocation ProviderInvocation, metadata ProviderCallMetadata, usage *TokenUsage, callErr error) error {
	finishedAt := time.Now().UTC()
	invocation.FinishedAt = finishedAt
	invocation.DurationMS = finishedAt.Sub(invocation.CreatedAt).Milliseconds()
	if invocation.DurationMS < 0 {
		invocation.DurationMS = 0
	}
	if metadata.StatusCode > 0 {
		invocation.ProviderStatusCode = intPtr(metadata.StatusCode)
	}
	if usage != nil {
		invocation.PromptTokens = intPtr(usage.PromptTokens)
		invocation.CompletionTokens = intPtr(usage.CompletionTokens)
		invocation.TotalTokens = intPtr(usage.TotalTokens)
	}
	var resultErr error
	if callErr != nil {
		resultErr = normalizeInvocationError(callErr)
		appErr, _ := Classify(resultErr)
		invocation.Status = statusForInvocationError(callErr)
		invocation.NormalizedErrorCode = string(appErr.Code)
		invocation.NormalizedErrorType = OpenAIErrorTypeForCode(appErr.Code)
		invocation.ErrorMessage = appErr.Message
		var providerErr *ProviderError
		if errors.As(callErr, &providerErr) && providerErr.ProviderStatusCode != nil {
			invocation.ProviderStatusCode = cloneIntPtr(providerErr.ProviderStatusCode)
		}
	}
	if err := s.repo.RecordProviderInvocation(ctx, invocation, nil); err != nil {
		return DependencyError("model invocation summary store is unavailable", err)
	}
	return resultErr
}

func normalizeInvocationError(err error) error {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.Code {
		case CodeValidation:
			return ValidationError(map[string]string{"provider": "rejected model request"})
		case CodeRateLimited:
			return RateLimitedError("provider rate limit exceeded", err)
		case CodeDependency:
			return DependencyError(providerErr.Message, err)
		default:
			return DependencyError("provider request failed", err)
		}
	}
	return DependencyError("provider request failed", err)
}

func statusForInvocationError(err error) InvocationStatus {
	if errors.Is(err, context.Canceled) {
		return InvocationCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return InvocationTimeout
	}
	return InvocationFailed
}

func validateEmbeddingInput(input EmbeddingInput) map[string]string {
	fields := map[string]string{}
	if len(input.Input) == 0 {
		fields["input"] = "must include at least one item"
	}
	for _, value := range input.Input {
		if strings.TrimSpace(value) == "" {
			fields["input"] = "items must not be empty"
			break
		}
	}
	if input.Dimensions != nil && *input.Dimensions <= 0 {
		fields["dimensions"] = "must be >= 1"
	}
	if format := normalizedEncodingFormat(input.EncodingFormat); format != "float" && format != "base64" {
		fields["encoding_format"] = "must be float or base64"
	}
	return fields
}

func validateRerankingInput(input RerankingInput) map[string]string {
	fields := map[string]string{}
	if strings.TrimSpace(input.Query) == "" {
		fields["query"] = "is required"
	}
	if len(input.Documents) == 0 {
		fields["documents"] = "must include at least one document"
	}
	for _, document := range input.Documents {
		if strings.TrimSpace(document.ID) == "" || strings.TrimSpace(document.Text) == "" {
			fields["documents"] = "document id and text are required"
			break
		}
	}
	if input.TopN != nil && *input.TopN <= 0 {
		fields["top_n"] = "must be >= 1"
	}
	if len(input.Metadata) > 0 {
		value := make(map[string]any, len(input.Metadata))
		for key, item := range input.Metadata {
			value[key] = item
		}
		if err := rejectSensitiveKeys(value); err != nil {
			fields["metadata"] = err.Error()
		}
	}
	return fields
}

func validateEmbeddingResponse(response EmbeddingResponse, expectedCount int, encodingFormat string) error {
	if response.Object != "list" || strings.TrimSpace(response.Model) == "" || expectedCount <= 0 || len(response.Data) != expectedCount {
		return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
	}
	seen := make(map[int]struct{}, expectedCount)
	for position, item := range response.Data {
		if item.Object != "embedding" || !json.Valid(item.Embedding) || !validEmbeddingPayload(item.Embedding, encodingFormat) {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		if item.Index < 0 || item.Index >= expectedCount {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		if _, ok := seen[item.Index]; ok {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		if item.Index != position {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		seen[item.Index] = struct{}{}
	}
	return nil
}

func validEmbeddingPayload(payload json.RawMessage, encodingFormat string) bool {
	switch normalizedEncodingFormat(encodingFormat) {
	case "float":
		var values []float64
		if err := json.Unmarshal(payload, &values); err != nil {
			return false
		}
		return len(values) > 0
	case "base64":
		var value string
		if err := json.Unmarshal(payload, &value); err != nil {
			return false
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return false
		}
		if _, err := base64.StdEncoding.DecodeString(value); err == nil {
			return true
		}
		if _, err := base64.RawStdEncoding.DecodeString(value); err == nil {
			return true
		}
		return false
	default:
		return false
	}
}

func validateRerankingResponse(response RerankingResponse, documents []RerankingDocument) error {
	if response.Object != "list" || strings.TrimSpace(response.Model) == "" || len(response.Data) == 0 || len(response.Data) > len(documents) {
		return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
	}
	seen := make(map[int]struct{}, len(response.Data))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(documents) {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		if _, ok := seen[item.Index]; ok {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		if strings.TrimSpace(item.DocumentID) == "" || item.DocumentID != documents[item.Index].ID {
			return NewProviderError(CodeDependency, "provider returned an invalid response", nil, nil)
		}
		seen[item.Index] = struct{}{}
	}
	return nil
}

func limitRerankingResponse(response RerankingResponse, topN int) RerankingResponse {
	if topN <= 0 || len(response.Data) <= topN {
		return response
	}
	response.Data = append([]RerankingResult(nil), response.Data[:topN]...)
	return response
}

func normalizedEncodingFormat(value string) string {
	if strings.TrimSpace(value) == "" {
		return "float"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneRerankingDocuments(items []RerankingDocument) []RerankingDocument {
	out := make([]RerankingDocument, len(items))
	copy(out, items)
	return out
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func intPtr(value int) *int {
	return &value
}

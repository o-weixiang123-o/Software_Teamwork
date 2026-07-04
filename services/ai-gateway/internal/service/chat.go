package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const chatCompletionOperation = "chat_completion"

func NewWithChatProvider(repo Repository, encryptor *CredentialEncryptor, defaultTimeoutMS int, chatProvider ChatProvider, invokers ...ModelInvoker) *Service {
	svc := New(repo, encryptor, defaultTimeoutMS, invokers...)
	svc.chatProvider = chatProvider
	return svc
}

func (s *Service) CreateChatCompletion(ctx context.Context, input ChatCompletionInput) (ChatCompletionResult, error) {
	prepared, err := s.prepareChat(ctx, input)
	if err != nil {
		return ChatCompletionResult{}, err
	}
	startedAt := time.Now().UTC()
	providerCtx, cancel := context.WithTimeout(ctx, time.Duration(prepared.profile.TimeoutMS)*time.Millisecond)
	defer cancel()

	result, err := s.chatProvider.CompleteChat(providerCtx, serviceProviderRequest(prepared, false))
	finishedAt := time.Now().UTC()
	status, normalized := classifyInvocationError(providerCtx, err)
	if err == nil {
		status = InvocationSucceeded
	}
	recordCtx := ctx
	var recordCancel context.CancelFunc
	if err != nil {
		recordCtx, recordCancel = recordContext(ctx)
		defer recordCancel()
	}
	if recordErr := s.recordChatInvocation(recordCtx, prepared, invocationRecordInput{
		status:             status,
		startedAt:          startedAt,
		finishedAt:         finishedAt,
		providerStatusCode: providerStatusFromResultOrError(result.ProviderStatusCode, normalized),
		usage:              result.Usage,
		normalized:         normalized,
	}); recordErr != nil && err == nil {
		return ChatCompletionResult{}, dependencyOpenAIError("invocation record failed", recordErr)
	}
	if err != nil {
		return ChatCompletionResult{}, normalized
	}
	return ChatCompletionResult{Body: result.Body}, nil
}

func (s *Service) StreamChatCompletion(ctx context.Context, input ChatCompletionInput) (ChatCompletionStream, error) {
	prepared, err := s.prepareChat(ctx, input)
	if err != nil {
		return ChatCompletionStream{}, err
	}
	if !prepared.profile.SupportsStreaming {
		return ChatCompletionStream{}, validationOpenAIError("profile does not support streaming", "stream")
	}
	startedAt := time.Now().UTC()
	providerCtx, cancel := context.WithTimeout(ctx, time.Duration(prepared.profile.TimeoutMS)*time.Millisecond)
	stream, err := s.chatProvider.StreamChat(providerCtx, serviceProviderRequest(prepared, true))
	if err != nil {
		cancel()
		finishedAt := time.Now().UTC()
		status, normalized := classifyInvocationError(providerCtx, err)
		recordCtx, recordCancel := recordContext(ctx)
		defer recordCancel()
		if recordErr := s.recordChatInvocation(recordCtx, prepared, invocationRecordInput{
			status:             status,
			startedAt:          startedAt,
			finishedAt:         finishedAt,
			providerStatusCode: providerStatusFromError(normalized),
			normalized:         normalized,
		}); recordErr != nil {
			return ChatCompletionStream{}, dependencyOpenAIError("invocation record failed", recordErr)
		}
		return ChatCompletionStream{}, normalized
	}
	finalizeOnce := false
	return ChatCompletionStream{
		Body: stream.Body,
		Finalize: func(final StreamFinalizeInput) error {
			if finalizeOnce {
				return nil
			}
			finalizeOnce = true
			cancel()
			finishedAt := time.Now().UTC()
			status := final.Status
			if status == "" {
				status = InvocationSucceeded
			}
			normalized := final.Error
			if normalized == nil && status != InvocationSucceeded {
				normalized = &OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "provider stream failed", Type: "upstream_error", Code: "dependency_error"}
			}
			providerStatus := final.ProviderStatusCode
			if providerStatus == nil {
				providerStatus = intPtrFromValue(stream.ProviderStatusCode)
			}
			recordCtx, recordCancel := recordContext(ctx)
			defer recordCancel()
			return s.recordChatInvocation(recordCtx, prepared, invocationRecordInput{
				status:             status,
				startedAt:          startedAt,
				finishedAt:         finishedAt,
				providerStatusCode: providerStatus,
				usage:              final.Usage,
				normalized:         normalized,
			})
		},
	}, nil
}

type preparedChat struct {
	reqCtx  RequestContext
	profile ModelProfile
	apiKey  string
	payload map[string]json.RawMessage
}

type invocationRecordInput struct {
	status             InvocationStatus
	startedAt          time.Time
	finishedAt         time.Time
	providerStatusCode *int
	usage              *TokenUsage
	normalized         *OpenAIError
}

func (s *Service) prepareChat(ctx context.Context, input ChatCompletionInput) (preparedChat, error) {
	if s.chatProvider == nil {
		return preparedChat{}, dependencyOpenAIError("chat provider is not configured", nil)
	}
	if s.encryptor == nil {
		return preparedChat{}, dependencyOpenAIError("credential encryption is not configured", nil)
	}
	payload := clonePayload(input.Payload)
	fields := validateChatPayload(payload)
	if len(fields) > 0 {
		return preparedChat{}, validationOpenAIFields(fields)
	}
	profile, err := s.selectChatProfile(ctx, payload)
	if err != nil {
		return preparedChat{}, err
	}
	if err := validateChatModelForProfile(payload, profile); err != nil {
		return preparedChat{}, err
	}
	credential, err := s.repo.GetActiveCredential(ctx, profile.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return preparedChat{}, validationOpenAIError("profile has no active credential", "profile_id")
		}
		return preparedChat{}, dependencyOpenAIError("credential store is unavailable", err)
	}
	apiKey, err := s.encryptor.Decrypt(credential)
	if err != nil {
		return preparedChat{}, dependencyOpenAIError("credential could not be decrypted", err)
	}
	merged, err := mergeChatPayload(profile, payload)
	if err != nil {
		return preparedChat{}, err
	}
	return preparedChat{
		reqCtx:  input.RequestContext,
		profile: profile,
		apiKey:  apiKey,
		payload: merged,
	}, nil
}

func (s *Service) selectChatProfile(ctx context.Context, payload map[string]json.RawMessage) (ModelProfile, error) {
	profileID := rawString(payload["profile_id"])
	var profile ModelProfile
	var err error
	if profileID != "" {
		profile, err = s.repo.GetModelProfile(ctx, profileID)
	} else {
		profile, err = s.repo.GetDefaultModelProfile(ctx, PurposeChat)
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ModelProfile{}, notFoundOpenAIError("chat profile not found", "profile_id")
		}
		return ModelProfile{}, dependencyOpenAIError("model profile store is unavailable", err)
	}
	if profile.Purpose != PurposeChat {
		return ModelProfile{}, validationOpenAIError("profile is not a chat profile", "profile_id")
	}
	if !profile.Enabled || profile.DeletedAt != nil {
		return ModelProfile{}, validationOpenAIError("profile is not enabled", "profile_id")
	}
	if !profile.APIKeyConfigured {
		return ModelProfile{}, validationOpenAIError("profile has no active credential", "profile_id")
	}
	return profile, nil
}

func validateChatPayload(payload map[string]json.RawMessage) map[string]string {
	fields := map[string]string{}
	messagesRaw, ok := payload["messages"]
	if !ok {
		fields["messages"] = "is required"
		return fields
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(messagesRaw, &messages); err != nil || len(messages) == 0 {
		fields["messages"] = "must be a non-empty array"
		return fields
	}
	for i, message := range messages {
		role := rawString(message["role"])
		switch role {
		case "system", "user", "assistant":
		case "tool":
			if strings.TrimSpace(rawString(message["tool_call_id"])) == "" {
				fields[fmt.Sprintf("messages.%d.tool_call_id", i)] = "is required for tool messages"
			}
		default:
			fields[fmt.Sprintf("messages.%d.role", i)] = "must be system, user, assistant, or tool"
		}
	}
	if raw, ok := payload["temperature"]; ok && !rawNumberInRange(raw, 0, 2, true) {
		fields["temperature"] = "must be between 0 and 2"
	}
	if raw, ok := payload["top_p"]; ok && !rawNumberInRange(raw, 0, 1, true) {
		fields["top_p"] = "must be between 0 and 1"
	}
	if raw, ok := payload["max_tokens"]; ok && !rawPositiveInteger(raw) {
		fields["max_tokens"] = "must be an integer >= 1"
	}
	if raw, ok := payload["profile_id"]; ok && strings.TrimSpace(rawString(raw)) == "" {
		fields["profile_id"] = "must not be empty"
	}
	return fields
}

func validateChatModelForProfile(payload map[string]json.RawMessage, profile ModelProfile) error {
	requestModel := strings.TrimSpace(rawString(payload["model"]))
	if requestModel == "" {
		return nil
	}
	profileModel := strings.TrimSpace(profile.Model)
	if profileModel == "" {
		return dependencyOpenAIError("model profile model is not configured", nil)
	}
	if requestModel != profileModel {
		return validationOpenAIError("model must match selected model profile", "model")
	}
	return nil
}

func mergeChatPayload(profile ModelProfile, payload map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(profile.DefaultParameters) > 0 {
		var defaults map[string]json.RawMessage
		if err := json.Unmarshal(profile.DefaultParameters, &defaults); err != nil {
			return nil, dependencyOpenAIError("profile default parameters are invalid", err)
		}
		for key, value := range defaults {
			if containsSensitiveToken(key) {
				return nil, validationOpenAIError("profile default parameters contain sensitive keys", "defaultParameters")
			}
			merged[key] = append(json.RawMessage(nil), value...)
		}
	}
	for key, value := range payload {
		if key == "profile_id" {
			continue
		}
		if containsSensitiveToken(key) {
			return nil, validationOpenAIError("request contains sensitive parameter keys", key)
		}
		merged[key] = append(json.RawMessage(nil), value...)
	}
	modelValue, _ := json.Marshal(profile.Model)
	merged["model"] = modelValue
	return merged, nil
}

func serviceProviderRequest(prepared preparedChat, stream bool) ProviderChatRequest {
	payload := clonePayload(prepared.payload)
	streamValue, _ := json.Marshal(stream)
	payload["stream"] = streamValue
	return ProviderChatRequest{
		Profile:   prepared.profile,
		APIKey:    prepared.apiKey,
		Payload:   payload,
		Stream:    stream,
		RequestID: prepared.reqCtx.RequestID,
	}
}

func (s *Service) recordChatInvocation(ctx context.Context, prepared preparedChat, input invocationRecordInput) error {
	invocationID := newID("inv")
	usage := input.usage
	invocation := ProviderInvocation{
		ID:                 invocationID,
		RequestID:          strings.TrimSpace(prepared.reqCtx.RequestID),
		CallerService:      strings.TrimSpace(prepared.reqCtx.CallerService),
		ExternalUserID:     strings.TrimSpace(prepared.reqCtx.UserID),
		Operation:          chatCompletionOperation,
		ProfileID:          prepared.profile.ID,
		Provider:           prepared.profile.Provider,
		Model:              prepared.profile.Model,
		Stream:             rawBool(prepared.payload["stream"]),
		Status:             input.status,
		ProviderStatusCode: input.providerStatusCode,
		PromptTokens:       usageIntPtr(usage, "prompt"),
		CompletionTokens:   usageIntPtr(usage, "completion"),
		TotalTokens:        usageIntPtr(usage, "total"),
		DurationMS:         input.finishedAt.Sub(input.startedAt).Milliseconds(),
		AttemptCount:       1,
		CreatedAt:          input.startedAt,
		FinishedAt:         input.finishedAt,
	}
	if input.normalized != nil {
		invocation.NormalizedErrorCode = input.normalized.Code
		invocation.NormalizedErrorType = input.normalized.Type
		invocation.ErrorMessage = input.normalized.Message
	}
	attempt := ProviderInvocationAttempt{
		ID:                 newID("att"),
		InvocationID:       invocationID,
		AttemptNo:          1,
		Provider:           prepared.profile.Provider,
		BaseURLHost:        baseURLHost(prepared.profile.BaseURL),
		Model:              prepared.profile.Model,
		Status:             input.status,
		ProviderStatusCode: input.providerStatusCode,
		DurationMS:         invocation.DurationMS,
		StartedAt:          input.startedAt,
		FinishedAt:         input.finishedAt,
	}
	if input.normalized != nil {
		attempt.ErrorCode = input.normalized.Code
		attempt.ErrorMessage = input.normalized.Message
	}
	return s.repo.RecordProviderInvocation(ctx, invocation, []ProviderInvocationAttempt{attempt})
}

func classifyInvocationError(ctx context.Context, err error) (InvocationStatus, *OpenAIError) {
	if err == nil {
		return InvocationSucceeded, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return InvocationTimeout, timeoutOpenAIError(err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return InvocationCancelled, cancelledOpenAIError(err)
	}
	var openErr *OpenAIError
	if errors.As(err, &openErr) {
		if openErr.Code == "timeout" {
			return InvocationTimeout, openErr
		}
		return InvocationFailed, openErr
	}
	return InvocationFailed, dependencyOpenAIError("provider request failed", err)
}

func recordContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
}

func validationOpenAIFields(fields map[string]string) *OpenAIError {
	param := ""
	for key := range fields {
		param = key
		break
	}
	return validationOpenAIError("request validation failed", param)
}

func validationOpenAIError(message, param string) *OpenAIError {
	if strings.TrimSpace(message) == "" {
		message = "request validation failed"
	}
	return &OpenAIError{HTTPStatus: http.StatusBadRequest, Message: message, Type: "invalid_request_error", Param: strings.TrimSpace(param), Code: "validation_error"}
}

func notFoundOpenAIError(message, param string) *OpenAIError {
	return &OpenAIError{HTTPStatus: http.StatusNotFound, Message: message, Type: "not_found_error", Param: strings.TrimSpace(param), Code: "not_found"}
}

func dependencyOpenAIError(message string, err error) *OpenAIError {
	return &OpenAIError{HTTPStatus: http.StatusBadGateway, Message: message, Type: "upstream_error", Code: "dependency_error", Err: err}
}

func timeoutOpenAIError(err error) *OpenAIError {
	return &OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "provider request timed out", Type: "upstream_error", Code: "timeout", Err: err}
}

func cancelledOpenAIError(err error) *OpenAIError {
	return &OpenAIError{HTTPStatus: http.StatusBadGateway, Message: "request was cancelled", Type: "upstream_error", Code: "cancelled", Err: err}
}

func providerStatusFromError(err *OpenAIError) *int {
	if err == nil {
		return nil
	}
	return err.ProviderStatusCode
}

func providerStatusFromResultOrError(resultStatus int, err *OpenAIError) *int {
	if status := intPtrFromValue(resultStatus); status != nil {
		return status
	}
	return providerStatusFromError(err)
}

func clonePayload(payload map[string]json.RawMessage) map[string]json.RawMessage {
	cloned := make(map[string]json.RawMessage, len(payload))
	for key, value := range payload {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func rawBool(raw json.RawMessage) bool {
	var value bool
	return json.Unmarshal(raw, &value) == nil && value
}

func rawNumberInRange(raw json.RawMessage, min, max float64, hasMax bool) bool {
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	if value < min {
		return false
	}
	return !hasMax || value <= max
}

func rawPositiveInteger(raw json.RawMessage) bool {
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return value >= 1
}

func usageIntPtr(usage *TokenUsage, field string) *int {
	if usage == nil {
		return nil
	}
	switch field {
	case "prompt":
		return &usage.PromptTokens
	case "completion":
		return &usage.CompletionTokens
	default:
		return &usage.TotalTokens
	}
}

func intPtrFromValue(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func baseURLHost(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.Host
}

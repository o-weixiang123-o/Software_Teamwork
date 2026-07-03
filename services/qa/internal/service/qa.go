package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/tools"
)

type Code string

const (
	CodeValidation        Code = "validation_error"
	CodeUnauthorized      Code = "unauthorized"
	CodeForbidden         Code = "forbidden"
	CodeNotFound          Code = "not_found"
	CodeConflict          Code = "conflict"
	CodeDependency        Code = "dependency_error"
	CodeInternal          Code = "internal_error"
	CodeUnsupportedMedia  Code = "unsupported_media_type"
	CodeUnsupportedIntent Code = "unsupported_intent"
	CodeTooLarge          Code = "too_large"
)

var (
	reasoningInternalHTTPURLPattern = regexp.MustCompile(`(?i)\bhttps?://(?:localhost|127\.0\.0\.1|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|[a-z0-9-]+(?:\.[a-z0-9-]+)*(?:\.internal|\.svc(?:\.cluster\.local)?|\.cluster\.local|\.local|\.consul)|(?:ai-gateway|gateway|auth|file|knowledge|document|qa|redis|postgres|postgresql|qdrant|minio|knowledge-vendor|document-mcp|knowledge-runtime|knowledge-api))(?:[/:?#][^\s]*)?`)
	reasoningInternalHostURLPattern = regexp.MustCompile(`(?i)\bhttps?://[a-z0-9.-]*internal[a-z0-9.-]*(?::\d{1,5})?(?:[/?#][^\s]*)?`)
	reasoningNonHTTPURLPattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s]+`)
	reasoningHostPortPattern        = regexp.MustCompile(`(?i)\b(?:localhost|127\.0\.0\.1|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|(?:ai-gateway|gateway|auth|file|knowledge|document|qa|redis|postgres|postgresql|qdrant|minio|knowledge-vendor|document-mcp|knowledge-runtime|knowledge-api)|[a-z0-9-]+(?:\.[a-z0-9-]+)*(?:\.internal|\.svc(?:\.cluster\.local)?|\.cluster\.local|\.local|\.consul)|[a-z][a-z0-9]*(?:-[a-z0-9]+)+):\d{2,5}\b`)
	reasoningEnvConnectionPattern   = regexp.MustCompile(`(?i)\b(?:database_url|postgres(?:ql)?_url|redis_url|qdrant_url|minio_endpoint|connection_string|conn_string|dsn)\s*[:=]\s*\S+`)
	reasoningKVHostPattern          = regexp.MustCompile(`(?i)\b(?:host|server|addr|address|endpoint)\s*=\s*(?:localhost|127\.0\.0\.1|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[0-1])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|(?:ai-gateway|gateway|auth|file|knowledge|document|qa|redis|postgres|postgresql|qdrant|minio|knowledge-vendor|document-mcp|knowledge-runtime|knowledge-api)|[a-z][a-z0-9]*(?:-[a-z0-9]+)+)\b`)
)

type AppError struct {
	Code    Code
	Message string
	Fields  map[string]string
	Err     error
}

func (e *AppError) Error() string { return e.Message }
func (e *AppError) Unwrap() error { return e.Err }

func NewError(code Code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func ValidationError(fields map[string]string) *AppError {
	return &AppError{Code: CodeValidation, Message: "request validation failed", Fields: fields}
}

func Classify(err error) (*AppError, bool) {
	var appErr *AppError
	return appErr, errors.As(err, &appErr)
}

type Conversation struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	OwnerUserID        string     `json:"-"`
	Status             string     `json:"status"`
	MessageCount       int        `json:"messageCount"`
	LastMessagePreview string     `json:"lastMessagePreview,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	LastMessageAt      *time.Time `json:"-"`
}

type ConversationListOptions struct {
	Page     int
	PageSize int
	Status   string
	Sort     string
}

type Message struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"sessionId"`
	SequenceNo     int             `json:"sequenceNo"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Intent         string          `json:"intent,omitempty"`
	Status         string          `json:"status"`
	Thinking       []ReasoningStep `json:"thinking,omitempty"`
	Citations      []Citation      `json:"citations,omitempty"`
	CitationCount  int             `json:"-"`
	AttachmentIDs  []string        `json:"attachmentIds,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
}

type MessageListOptions struct {
	Page             int
	PageSize         int
	IncludeThinking  bool
	IncludeCitations bool
}

type ReasoningStep struct {
	ID        string    `json:"id,omitempty"`
	MessageID string    `json:"messageId,omitempty"`
	Type      string    `json:"type"`
	Title     string    `json:"title,omitempty"`
	Summary   string    `json:"summary"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

func (s ReasoningStep) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type   string `json:"type"`
		Label  string `json:"label,omitempty"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}{Type: publicStepType(s.Type), Label: s.Title, Status: publicStepStatus(s.Status), Detail: s.Summary})
}

type Page[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

type RetrievalOptions struct {
	TopK            int                 `json:"topK,omitempty"`
	ScoreThreshold  float64             `json:"scoreThreshold,omitempty"`
	RerankThreshold float64             `json:"rerankThreshold,omitempty"`
	RerankTopN      int                 `json:"rerankTopN,omitempty"`
	EnableRerank    bool                `json:"enableRerank,omitempty"`
	TagFilters      map[string][]string `json:"tagFilters,omitempty"`
	topKSet         bool
	scoreSet        bool
	rerankSet       bool
	rerankTopNSet   bool
	enableSet       bool
}

func (o *RetrievalOptions) UnmarshalJSON(data []byte) error {
	var raw struct {
		TopK            *int                `json:"topK,omitempty"`
		ScoreThreshold  *float64            `json:"scoreThreshold,omitempty"`
		RerankThreshold *float64            `json:"rerankThreshold,omitempty"`
		RerankTopN      *int                `json:"rerankTopN,omitempty"`
		EnableRerank    *bool               `json:"enableRerank,omitempty"`
		TagFilters      map[string][]string `json:"tagFilters,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*o = RetrievalOptions{TagFilters: raw.TagFilters}
	if raw.TopK != nil {
		o.TopK = *raw.TopK
		o.topKSet = true
	}
	if raw.ScoreThreshold != nil {
		o.ScoreThreshold = *raw.ScoreThreshold
		o.scoreSet = true
	}
	if raw.RerankThreshold != nil {
		o.RerankThreshold = *raw.RerankThreshold
		o.rerankSet = true
	}
	if raw.RerankTopN != nil {
		o.RerankTopN = *raw.RerankTopN
		o.rerankTopNSet = true
	}
	if raw.EnableRerank != nil {
		o.EnableRerank = *raw.EnableRerank
		o.enableSet = true
	}
	return nil
}

type AskInput struct {
	Message          string           `json:"message"`
	Mode             string           `json:"mode,omitempty"`
	KnowledgeBaseIDs []string         `json:"knowledgeBaseIds,omitempty"`
	Retrieval        RetrievalOptions `json:"retrieval,omitempty"`
	AttachmentIDs    []string         `json:"attachmentIds,omitempty"`
}

type AskResult struct {
	UserMessage      Message         `json:"userMessage"`
	AssistantMessage Message         `json:"assistantMessage"`
	ResponseRun      ResponseRun     `json:"responseRun"`
	Citations        []Citation      `json:"citations"`
	ReasoningSteps   []ReasoningStep `json:"reasoningSteps"`
}

type ProgressEvent struct {
	Type             string
	Sequence         int
	Payload          map[string]any
	UserMessageID    string
	AssistantMessage string
	Intent           string
	Step             ReasoningStep
}

type ProgressObserver func(ProgressEvent)

type ModelInvocation struct {
	ResponseRunID    string
	IterationNo      int
	Provider         string
	ProfileID        string
	ModelName        string
	FinishReason     string
	Status           string
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	TotalTokens      int
	LatencyMS        int64
	ErrorCode        string
	ErrorMessage     string
	StartedAt        time.Time
	FinishedAt       *time.Time
}

type ResponseRunStart struct {
	RequestID          string
	QAConfigVersionID  string
	LLMConfigVersionID string
	MaxIterations      int
}

type ResponseRunFinalization struct {
	RunID             string
	AssistantMessage  Message
	ReasoningSteps    []ReasoningStep
	StreamEvents      []StreamEvent
	Citations         []Citation
	Status            string
	TerminationReason string
	CurrentIteration  int
	PromptTokens      int
	CompletionTokens  int
	ReasoningTokens   int
	TotalTokens       int
	CompletedAt       time.Time
}

type Repository interface {
	CreateConversation(context.Context, Conversation) (Conversation, error)
	ListConversations(context.Context, string, ConversationListOptions) (Page[Conversation], error)
	GetConversation(context.Context, string, string) (Conversation, error)
	UpdateConversation(context.Context, string, Conversation) (Conversation, error)
	DeleteConversation(context.Context, string, string) error
	ListMessages(context.Context, string, string, MessageListOptions) (Page[Message], error)
	AppendMessages(context.Context, string, string, ResponseRunStart, []string, ...Message) (ResponseRun, error)
	UpdateMessage(context.Context, string, Message) error
	FinalizeResponseRun(context.Context, string, ResponseRunFinalization) (ResponseRun, error)
	SaveReasoningSteps(context.Context, string, string, []ReasoningStep) error
	SaveStreamEvents(context.Context, string, string, []StreamEvent) error
	SaveCitations(context.Context, string, string, []Citation) error
	SaveModelInvocation(context.Context, string, ModelInvocation) (string, error)
	ValidateReadyAttachments(context.Context, string, string, []string) ([]SessionAttachment, error)
	GetResponseRun(context.Context, string, string) (ResponseRun, error)
}

type AgentRunner interface {
	RunWithObserver(context.Context, []agent.Message, agent.Observer) (agent.Result, error)
	RunWithToolResultCallback(context.Context, []agent.Message, agent.Observer, agent.ToolObserver) (agent.Result, error)
}

type RuntimeSnapshot struct {
	Runner                  AgentRunner
	SystemPrompt            string
	LLMModel                string
	LLMProfileID            string
	QAConfigVersionID       string
	LLMConfigVersionID      string
	MaxIterations           int
	OverallTimeout          time.Duration
	DefaultKnowledgeBaseIDs []string
	RetrievalSettings       RetrievalSettings
}

type RuntimeProvider interface {
	Acquire() (RuntimeSnapshot, func(), error)
}

type QAService struct {
	repository    Repository
	runtime       RuntimeProvider
	sourceChecker CitationSourceChecker
	now           func() time.Time
	activeMu      sync.Mutex
	activeRuns    map[string]context.CancelFunc
}

func NewQAService(repository Repository, runtime RuntimeProvider) (*QAService, error) {
	if repository == nil || runtime == nil {
		return nil, errors.New("repository and runtime provider are required")
	}
	return &QAService{repository: repository, runtime: runtime, now: time.Now, activeRuns: map[string]context.CancelFunc{}}, nil
}

func (s *QAService) SetCitationSourceChecker(checker CitationSourceChecker) {
	s.sourceChecker = checker
}

func (s *QAService) CreateConversation(ctx context.Context, userID, title string) (Conversation, error) {
	if strings.TrimSpace(userID) == "" {
		return Conversation{}, NewError(CodeUnauthorized, "authentication required", nil)
	}
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > 200 {
		return Conversation{}, ValidationError(map[string]string{"title": "must not exceed 200 characters"})
	}
	now := s.now().UTC()
	return s.repository.CreateConversation(ctx, Conversation{
		ID: newID("conv"), Title: title, OwnerUserID: userID, Status: "active", CreatedAt: now, UpdatedAt: now,
	})
}

func (s *QAService) ListConversations(ctx context.Context, userID string, options ConversationListOptions) (Page[Conversation], error) {
	if strings.TrimSpace(userID) == "" {
		return Page[Conversation]{}, NewError(CodeUnauthorized, "authentication required", nil)
	}
	normalized, err := normalizeConversationListOptions(options)
	if err != nil {
		return Page[Conversation]{}, err
	}
	return s.repository.ListConversations(ctx, userID, normalized)
}

func (s *QAService) GetConversation(ctx context.Context, userID, id string) (Conversation, error) {
	return s.repository.GetConversation(ctx, userID, id)
}

func (s *QAService) UpdateConversation(ctx context.Context, userID, id, title, status string) (Conversation, error) {
	title, status = strings.TrimSpace(title), strings.TrimSpace(status)
	if title == "" && status == "" {
		return Conversation{}, ValidationError(map[string]string{"body": "title or status is required"})
	}
	if utf8.RuneCountInString(title) > 200 {
		return Conversation{}, ValidationError(map[string]string{"title": "must not exceed 200 characters"})
	}
	if status != "" && status != "active" && status != "archived" {
		return Conversation{}, ValidationError(map[string]string{"status": "must be active or archived"})
	}
	conversation, err := s.repository.GetConversation(ctx, userID, id)
	if err != nil {
		return Conversation{}, err
	}
	if title != "" {
		conversation.Title = title
	}
	if status != "" {
		conversation.Status = status
	}
	conversation.UpdatedAt = s.now().UTC()
	return s.repository.UpdateConversation(ctx, userID, conversation)
}

func (s *QAService) DeleteConversation(ctx context.Context, userID, id string) error {
	return s.repository.DeleteConversation(ctx, userID, id)
}

func (s *QAService) ListMessages(ctx context.Context, userID, conversationID string, options MessageListOptions) (Page[Message], error) {
	if strings.TrimSpace(userID) == "" {
		return Page[Message]{}, NewError(CodeUnauthorized, "authentication required", nil)
	}
	if strings.TrimSpace(conversationID) == "" {
		return Page[Message]{}, ValidationError(map[string]string{"sessionId": "is required"})
	}
	normalized, err := normalizeMessageListOptions(options)
	if err != nil {
		return Page[Message]{}, err
	}
	page, err := s.repository.ListMessages(ctx, userID, conversationID, normalized)
	if err != nil {
		return Page[Message]{}, err
	}
	if normalized.IncludeCitations {
		revalidateMessageCitations(ctx, userID, s.sourceChecker, page.Items)
	}
	return page, nil
}

func (s *QAService) Ask(ctx context.Context, userID, conversationID string, input AskInput, observe ProgressObserver) (AskResult, error) {
	if err := validateAskInput(input); err != nil {
		return AskResult{}, err
	}
	conversation, err := s.repository.GetConversation(ctx, userID, conversationID)
	if err != nil {
		return AskResult{}, err
	}
	history, err := s.repository.ListMessages(ctx, userID, conversationID, MessageListOptions{Page: 1, PageSize: 100})
	if err != nil {
		return AskResult{}, err
	}

	runtime, release, err := s.runtime.Acquire()
	if err != nil {
		return AskResult{}, NewError(CodeDependency, "agent runtime is unavailable", err)
	}
	defer release()

	now := s.now().UTC()
	intent := input.Mode
	if intent == "" {
		intent = "unknown"
	}
	userMessage := Message{ID: newID("msg"), ConversationID: conversationID, Role: agent.RoleUser, Content: strings.TrimSpace(input.Message), Intent: intent, Status: "completed", CreatedAt: now}
	assistantMessage := Message{ID: newID("msg"), ConversationID: conversationID, Role: agent.RoleAssistant, Intent: intent, Status: "streaming", CreatedAt: now}

	attachmentIDs := normalizeIDList(input.AttachmentIDs)
	if len(attachmentIDs) > 0 {
		if _, err := s.repository.ValidateReadyAttachments(ctx, userID, conversationID, attachmentIDs); err != nil {
			return AskResult{}, err
		}
	}
	input.AttachmentIDs = attachmentIDs
	input.KnowledgeBaseIDs = normalizeIDList(input.KnowledgeBaseIDs)

	if len(runtime.DefaultKnowledgeBaseIDs) > 0 {
		if len(input.KnowledgeBaseIDs) > 0 {
			allowed := make(map[string]struct{}, len(runtime.DefaultKnowledgeBaseIDs))
			for _, id := range runtime.DefaultKnowledgeBaseIDs {
				allowed[id] = struct{}{}
			}
			for _, id := range input.KnowledgeBaseIDs {
				if _, ok := allowed[id]; !ok {
					return AskResult{}, NewError(CodeValidation, "one or more requested knowledge bases are not accessible", nil)
				}
			}
		}
	}

	run, err := s.repository.AppendMessages(ctx, userID, conversationID, ResponseRunStart{
		RequestID:          RequestIDFromContext(ctx),
		QAConfigVersionID:  runtime.QAConfigVersionID,
		LLMConfigVersionID: runtime.LLMConfigVersionID,
		MaxIterations:      runtime.MaxIterations,
	}, attachmentIDs, userMessage, assistantMessage)
	if err != nil {
		return AskResult{}, err
	}
	baseCtx := WithUserID(ctx, userID)
	baseCtx = contextutil.WithKnowledgeBaseIDs(baseCtx, input.KnowledgeBaseIDs)
	baseCtx = contextutil.WithDefaultKnowledgeBaseIDs(baseCtx, runtime.DefaultKnowledgeBaseIDs)
	baseCtx = contextutil.WithRetrievalSettings(baseCtx, retrievalSettingsForAsk(runtime.RetrievalSettings, input.Retrieval))
	baseCtx = contextutil.WithCitationNo(baseCtx, 1)
	baseCtx = contextutil.WithSessionID(baseCtx, conversationID)
	baseCtx = contextutil.WithMessageAttachmentIDs(baseCtx, attachmentIDs)
	cancelBase := func() {}
	if runtime.OverallTimeout > 0 {
		var cancel context.CancelFunc
		baseCtx, cancel = context.WithTimeout(baseCtx, runtime.OverallTimeout)
		cancelBase = cancel
	}
	runCtx, cancelRun := context.WithCancel(baseCtx)
	s.activeMu.Lock()
	s.activeRuns[run.ID] = cancelRun
	s.activeMu.Unlock()
	defer func() {
		cancelRun()
		cancelBase()
		s.activeMu.Lock()
		delete(s.activeRuns, run.ID)
		s.activeMu.Unlock()
	}()
	if conversation.Title == "" || conversation.Title == "新对话" {
		conversation.Title = truncateRunes(userMessage.Content, 40)
		conversation.UpdatedAt = now
		_, _ = s.repository.UpdateConversation(ctx, userID, conversation)
	}
	events := make([]StreamEvent, 0, 12)
	eventSeq := 0
	emit := func(eventType string, payload map[string]any) {
		eventSeq++
		event := StreamEvent{EventSeq: eventSeq, EventType: eventType, Payload: payload, CreatedAt: s.now().UTC()}
		events = append(events, event)
		emitProgress(observe, ProgressEvent{Type: eventType, Sequence: event.EventSeq, Payload: payload, UserMessageID: userMessage.ID, AssistantMessage: assistantMessage.ID, Intent: intent})
	}
	emit("message.created", map[string]any{"responseRunId": run.ID, "userMessageId": userMessage.ID, "assistantMessageId": assistantMessage.ID, "status": "running"})
	startedAt := s.now()
	messages := make([]agent.Message, 0, len(history.Items)+3)
	messages = append(messages, agent.Message{Role: agent.RoleSystem, Content: runtime.SystemPrompt})
	if directive := requestDirective(input); directive != "" {
		messages = append(messages, agent.Message{Role: agent.RoleSystem, Content: directive})
	}
	for _, item := range history.Items {
		if item.Status == "completed" && (item.Role == agent.RoleUser || item.Role == agent.RoleAssistant) {
			messages = append(messages, agent.Message{Role: item.Role, Content: item.Content})
		}
	}
	messages = append(messages, agent.Message{Role: agent.RoleUser, Content: userMessage.Content})

	steps := make([]ReasoningStep, 0, 4)
	citations := make([]Citation, 0, 8)
	reasoningDeltaIndex := 0
	answerDeltaIndex := 0
	var streamedAnswer strings.Builder
	iterationStartedAt := map[int]time.Time{}
	completedIterations := map[int]struct{}{}
	modelInvocationIDs := map[int]string{}
	usage := agent.TokenUsage{}
	var invocationErr error
	profileID := runtime.LLMProfileID
	if profileID == "" {
		profileID = "default"
	}
	toolObservations := map[string]agent.ToolObservation{}
	onToolObservation := func(observation agent.ToolObservation) {
		toolObservations[observation.ToolCallID] = observation
	}
	seenCitationKeys := map[string]struct{}{}
	reasoningBuffers := map[int]*reasoningDeltaBuffer{}
	emitReasoningDelta := func(text string) {
		emit("reasoning.delta", map[string]any{"messageId": assistantMessage.ID, "text": text, "index": reasoningDeltaIndex})
		reasoningDeltaIndex++
	}
	emitAnswerDelta := func(text string) {
		if text == "" {
			return
		}
		emit("answer.delta", map[string]any{"messageId": assistantMessage.ID, "text": text, "index": answerDeltaIndex})
		answerDeltaIndex++
		streamedAnswer.WriteString(text)
	}
	bufferReasoningDelta := func(iteration int, raw string) {
		if raw == "" {
			return
		}
		buffer := reasoningBuffers[iteration]
		if buffer == nil {
			buffer = newReasoningDeltaBuffer()
			reasoningBuffers[iteration] = buffer
		}
		for _, text := range buffer.add(raw) {
			emitReasoningDelta(text)
		}
	}
	flushReasoningDeltas := func(iteration int) {
		buffer := reasoningBuffers[iteration]
		if buffer == nil {
			return
		}
		for _, text := range buffer.flush() {
			emitReasoningDelta(text)
		}
		delete(reasoningBuffers, iteration)
	}
	emitSearchCitations := func(observation agent.ToolObservation) {
		if observation.Type != agent.EventToolCompleted {
			return
		}
		if observation.Result == "" {
			return
		}
		if isCitationToolName(observation.ToolName) {
			startNo := contextutil.CitationNoFromContext(runCtx)
			if startNo <= 0 {
				startNo = 1
			}
			extracted := extractCitationsFromToolResult(observation.Result, startNo)
			newCitations := make([]Citation, 0, len(extracted))
			for _, citation := range extracted {
				citation.MessageID = assistantMessage.ID
				citation.ResponseRunID = run.ID
				citation = NormalizeCitation(citation)
				key := citationSnapshotKey(citation)
				if _, ok := seenCitationKeys[key]; ok {
					continue
				}
				seenCitationKeys[key] = struct{}{}
				citation.CitationNo = len(citations) + len(newCitations) + 1
				newCitations = append(newCitations, citation)
			}
			if len(newCitations) == 0 {
				return
			}
			newCitations = revalidateCitationSources(ctx, userID, s.sourceChecker, newCitations)
			citations = append(citations, newCitations...)
			for _, citation := range newCitations {
				emit("citation.delta", map[string]any{"citation": citation})
			}
			contextutil.AddCitationNo(runCtx, len(newCitations))
		}
	}
	result, runErr := runtime.Runner.RunWithToolResultCallback(runCtx, messages, func(event agent.Event) {
		switch event.Type {
		case agent.EventModelStarted:
			iterationStartedAt[event.Iteration] = s.now().UTC()
			emit("agent.iteration.started", map[string]any{"responseRunId": run.ID, "iterationNo": event.Iteration})
		case agent.EventModelCompleted:
			startedAt := iterationStartedAt[event.Iteration]
			if startedAt.IsZero() {
				startedAt = s.now().UTC()
			}
			finishedAt := s.now().UTC()
			accumulateUsage(&usage, event.Usage)
			completedIterations[event.Iteration] = struct{}{}
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			invocationID, err := s.repository.SaveModelInvocation(saveCtx, userID, ModelInvocation{
				ResponseRunID:    run.ID,
				IterationNo:      event.Iteration,
				Provider:         "ai-gateway",
				ProfileID:        profileID,
				ModelName:        runtime.LLMModel,
				FinishReason:     event.FinishReason,
				Status:           "completed",
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				ReasoningTokens:  event.Usage.ReasoningTokens,
				TotalTokens:      event.Usage.TotalTokens,
				StartedAt:        startedAt,
				FinishedAt:       &finishedAt,
				LatencyMS:        finishedAt.Sub(startedAt).Milliseconds(),
			})
			if err != nil && invocationErr == nil {
				invocationErr = err
			}
			if invocationID != "" {
				modelInvocationIDs[event.Iteration] = invocationID
			}
			flushReasoningDeltas(event.Iteration)
		case agent.EventToolStarted:
			observation := toolObservations[event.ToolCallID]
			emit("tool.started", toolProgressPayload("正在执行工具 "+event.ToolName, event, observation, modelInvocationIDs[event.Iteration], false))
		case agent.EventToolCompleted:
			observation := toolObservations[event.ToolCallID]
			emit("tool.completed", toolProgressPayload("工具 "+event.ToolName+" 执行完成", event, observation, modelInvocationIDs[event.Iteration], true))
		case agent.EventToolFailed:
			observation := toolObservations[event.ToolCallID]
			emit("tool.failed", toolProgressPayload("工具 "+event.ToolName+" 执行失败", event, observation, modelInvocationIDs[event.Iteration], true))
		case agent.EventReasoningDelta:
			bufferReasoningDelta(event.Iteration, event.ReasoningContent)
			return
		case agent.EventAnswerDelta:
			emitAnswerDelta(event.AnswerContent)
			return
		}
		step, ok := stepFromAgentEvent(assistantMessage.ID, event, s.now().UTC())
		if !ok {
			return
		}
		steps = append(steps, step)
		emit("reasoning.step", map[string]any{"type": publicStepType(step.Type), "label": step.Title, "status": publicStepStatus(step.Status), "detail": step.Summary})
		if event.Type == agent.EventToolCompleted {
			emitSearchCitations(toolObservations[event.ToolCallID])
		}
	}, onToolObservation)
	if runErr == nil && invocationErr != nil {
		runErr = fmt.Errorf("save model invocation: %w", invocationErr)
	}
	if runErr != nil {
		status, reason, errorCode, publicMessage := classifyRunError(runErr)
		assistantMessage.Status = "failed"
		if status == "cancelled" {
			assistantMessage.Status = "cancelled"
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if shouldRecordFailedModelInvocation(reason, iterationStartedAt, completedIterations) {
			s.saveFailedModelInvocation(cleanupCtx, userID, run.ID, runtime, profileID, reason, errorCode, iterationStartedAt)
		}
		emit("error", map[string]any{"responseRunId": run.ID, "code": string(errorCode), "message": publicMessage})
		finalized, finalizeErr := s.repository.FinalizeResponseRun(cleanupCtx, userID, ResponseRunFinalization{
			RunID: run.ID, AssistantMessage: assistantMessage, ReasoningSteps: steps, StreamEvents: events,
			Citations: citations,
			Status:    status, TerminationReason: reason, CurrentIteration: maxStartedIteration(iterationStartedAt),
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
			CompletedAt: s.now().UTC(),
		})
		if finalizeErr != nil {
			if appErr, ok := Classify(finalizeErr); ok && appErr.Code == CodeConflict {
				if finalized.ID == "" {
					loaded, loadErr := s.repository.GetResponseRun(cleanupCtx, userID, run.ID)
					if loadErr != nil {
						return AskResult{}, NewError(CodeDependency, "answer state persistence failed", fmt.Errorf("load response run after finalization conflict: %w", loadErr))
					}
					finalized = loaded
				}
				if saveErr := s.saveReplayRecords(cleanupCtx, userID, run.ID, assistantMessage.ID, steps, events); saveErr != nil {
					return AskResult{}, NewError(CodeDependency, "answer state persistence failed", fmt.Errorf("save replay records after finalization conflict: %w", saveErr))
				}
				run = finalized
				return AskResult{UserMessage: userMessage, AssistantMessage: assistantMessage, ResponseRun: run, Citations: []Citation{}, ReasoningSteps: steps}, NewError(errorCode, publicMessage, runErr)
			}
			return AskResult{}, NewError(CodeDependency, "answer state persistence failed", fmt.Errorf("finalize failed response run after agent error: %w", finalizeErr))
		}
		run = finalized
		return AskResult{UserMessage: userMessage, AssistantMessage: assistantMessage, ResponseRun: run, Citations: []Citation{}, ReasoningSteps: steps}, NewError(errorCode, publicMessage, runErr)
	}
	assistantMessage.Content = result.Final.Content
	assistantMessage.Status = "completed"
	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finalCitations := citations
	if len(finalCitations) == 0 {
		finalCitations = citationsFromAgentMessages(assistantMessage.ID, run.ID, result.Messages)
		finalCitations = revalidateCitationSources(finalizeCtx, userID, s.sourceChecker, finalCitations)
		for _, citation := range finalCitations {
			emit("citation.delta", map[string]any{"citation": citation})
		}
	} else {
		finalCitations = revalidateCitationSources(finalizeCtx, userID, s.sourceChecker, finalCitations)
	}
	assistantMessage.Citations = finalCitations
	streamedAnswerText := streamedAnswer.String()
	if streamedAnswerText == "" {
		emitAnswerDelta(assistantMessage.Content)
	} else if strings.HasPrefix(assistantMessage.Content, streamedAnswerText) && len(streamedAnswerText) < len(assistantMessage.Content) {
		emitAnswerDelta(assistantMessage.Content[len(streamedAnswerText):])
	} else if streamedAnswerText != assistantMessage.Content {
		publicMessage := "answer stream did not match final answer"
		assistantMessage.Status = "failed"
		emit("error", map[string]any{"responseRunId": run.ID, "code": string(CodeDependency), "message": publicMessage})
		finalized, finalizeErr := s.repository.FinalizeResponseRun(finalizeCtx, userID, ResponseRunFinalization{
			RunID: run.ID, AssistantMessage: assistantMessage, ReasoningSteps: steps, StreamEvents: events,
			Citations: finalCitations,
			Status:    "failed", TerminationReason: "answer_stream_mismatch", CurrentIteration: result.Iterations,
			PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
			CompletedAt: s.now().UTC(),
		})
		if finalizeErr != nil {
			return AskResult{}, NewError(CodeDependency, "answer state persistence failed", fmt.Errorf("finalize mismatched answer stream: %w", finalizeErr))
		}
		run = finalized
		return AskResult{UserMessage: userMessage, AssistantMessage: assistantMessage, ResponseRun: run, Citations: finalCitations, ReasoningSteps: steps}, NewError(CodeDependency, publicMessage, errors.New("streamed answer deltas do not match final answer"))
	}
	emit("answer.completed", map[string]any{
		"responseRunId": run.ID,
		"messageId":     assistantMessage.ID,
		"totalTokens":   usage.TotalTokens,
		"latencyMs":     int(s.now().Sub(startedAt).Milliseconds()),
	})
	run, err = s.repository.FinalizeResponseRun(finalizeCtx, userID, ResponseRunFinalization{
		RunID: run.ID, AssistantMessage: assistantMessage, ReasoningSteps: steps, StreamEvents: events,
		Citations: finalCitations,
		Status:    "completed", TerminationReason: "completed", CurrentIteration: result.Iterations,
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		CompletedAt: s.now().UTC(),
	})
	if err != nil {
		return AskResult{}, fmt.Errorf("finalize response run: %w", err)
	}
	return AskResult{UserMessage: userMessage, AssistantMessage: assistantMessage, ResponseRun: run, Citations: finalCitations, ReasoningSteps: steps}, nil
}

func (s *QAService) saveReplayRecords(ctx context.Context, userID, runID, assistantMessageID string, steps []ReasoningStep, events []StreamEvent) error {
	if err := s.repository.SaveReasoningSteps(ctx, userID, assistantMessageID, steps); err != nil {
		return fmt.Errorf("save reasoning steps: %w", err)
	}
	if err := s.repository.SaveStreamEvents(ctx, userID, runID, events); err != nil {
		return fmt.Errorf("save stream events: %w", err)
	}
	return nil
}

func shouldRecordFailedModelInvocation(reason string, started map[int]time.Time, completed map[int]struct{}) bool {
	if reason == "max_iterations" {
		return false
	}
	iteration := maxStartedIteration(started)
	if iteration == 0 {
		return false
	}
	_, done := completed[iteration]
	return !done
}

func (s *QAService) saveFailedModelInvocation(ctx context.Context, userID, runID string, runtime RuntimeSnapshot, profileID string, reason string, errorCode Code, started map[int]time.Time) {
	iteration := maxStartedIteration(started)
	if iteration == 0 {
		iteration = 1
	}
	startedAt := started[iteration]
	if startedAt.IsZero() {
		startedAt = s.now().UTC()
	}
	finishedAt := s.now().UTC()
	status := "failed"
	if reason == "cancelled" {
		status = "cancelled"
	}
	_, _ = s.repository.SaveModelInvocation(ctx, userID, ModelInvocation{
		ResponseRunID: runID, IterationNo: iteration, Provider: "ai-gateway",
		ProfileID: profileID, ModelName: runtime.LLMModel, Status: status,
		ErrorCode: string(errorCode), ErrorMessage: publicRunErrorMessage(reason),
		StartedAt: startedAt, FinishedAt: &finishedAt, LatencyMS: finishedAt.Sub(startedAt).Milliseconds(),
	})
}

func classifyRunError(err error) (status, reason string, code Code, publicMessage string) {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled", "cancelled", CodeDependency, publicRunErrorMessage("cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return "failed", "timeout", CodeDependency, publicRunErrorMessage("timeout")
	case errors.Is(err, agent.ErrMaxIterations):
		return "failed", "max_iterations", CodeDependency, publicRunErrorMessage("max_iterations")
	default:
		if appErr, ok := Classify(err); ok {
			reason := string(appErr.Code)
			message := appErr.Message
			if message == "" {
				message = publicRunErrorMessage(reason)
			}
			return "failed", reason, appErr.Code, message
		}
		return "failed", "model_error", CodeDependency, publicRunErrorMessage("model_error")
	}
}

func publicRunErrorMessage(reason string) string {
	switch reason {
	case "cancelled":
		return "answer generation cancelled"
	case "timeout":
		return "answer generation timed out"
	case "max_iterations":
		return "answer generation reached the maximum iterations"
	case "validation_error":
		return "AI gateway rejected model request"
	default:
		return "answer generation failed"
	}
}

func accumulateUsage(total *agent.TokenUsage, next agent.TokenUsage) {
	if next.TotalTokens == 0 {
		next.TotalTokens = next.PromptTokens + next.CompletionTokens + next.ReasoningTokens
	}
	total.PromptTokens += next.PromptTokens
	total.CompletionTokens += next.CompletionTokens
	total.ReasoningTokens += next.ReasoningTokens
	total.TotalTokens += next.TotalTokens
}

func maxStartedIteration(started map[int]time.Time) int {
	maxIteration := 0
	for iteration := range started {
		if iteration > maxIteration {
			maxIteration = iteration
		}
	}
	return maxIteration
}

func (s *QAService) CancelActiveRun(runID string) {
	s.activeMu.Lock()
	cancel := s.activeRuns[runID]
	s.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func publicStepType(value string) string {
	if value == "tool" {
		return "tool_call"
	}
	return value
}

func publicStepStatus(value string) string {
	if value == "completed" {
		return "done"
	}
	return value
}

func sanitizeReasoningContent(value string) string {
	if value == "" || containsUnsafeReasoningContent(value) {
		return ""
	}
	return truncateUTF8String(value, 4000)
}

type reasoningDeltaBuffer struct {
	seen    strings.Builder
	pending string
	blocked bool
}

func newReasoningDeltaBuffer() *reasoningDeltaBuffer {
	return &reasoningDeltaBuffer{}
}

// NewReasoningFilter returns a stateful filter for one model iteration. It
// buffers the full provider reasoning block so later unsafe markers can prevent
// earlier text from being exposed through public events or replay storage.
func NewReasoningFilter() agent.ReasoningFilter {
	buffer := newReasoningDeltaBuffer()
	return func(delta string, final bool) []string {
		var out []string
		if delta != "" {
			out = append(out, buffer.add(delta)...)
		}
		if final {
			out = append(out, buffer.flush()...)
		}
		return out
	}
}

func (b *reasoningDeltaBuffer) add(value string) []string {
	if b == nil || b.blocked || value == "" {
		return nil
	}
	candidate := b.seen.String() + value
	if containsUnsafeReasoningContent(candidate) {
		b.blocked = true
		b.pending = ""
		return nil
	}
	b.seen.WriteString(value)
	b.pending += value
	return nil
}

func (b *reasoningDeltaBuffer) flush() []string {
	if b == nil || b.blocked {
		return nil
	}
	pendingRunes := []rune(b.pending)
	if len(pendingRunes) == 0 {
		return nil
	}
	b.pending = ""
	return splitSafeReasoningDelta(string(pendingRunes))
}

func splitSafeReasoningDelta(value string) []string {
	if value == "" || containsUnsafeReasoningContent(value) {
		return nil
	}
	values := make([]string, 0, 1)
	for value != "" {
		chunk := truncateUTF8String(value, 4000)
		if chunk == "" {
			break
		}
		values = append(values, chunk)
		value = value[len(chunk):]
	}
	return values
}

func containsUnsafeReasoningContent(value string) bool {
	normalized := strings.ToLower(value)
	for _, marker := range []string{
		"private_chain_of_thought",
		"chain-of-thought",
		"chain of thought",
		"system prompt",
		"developer prompt",
		"tool arguments",
		"tool result",
		"mcp result",
		"raw provider",
		"provider raw",
		"raw error",
		"api_key",
		"apikey",
		"api key",
		"authorization:",
		"bearer ",
		"token=",
		"sk-",
		"object key",
		"objectkey",
		"file_ref",
		"database_url",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return containsInternalConnectionReference(value)
}

func containsInternalConnectionReference(value string) bool {
	if reasoningInternalHTTPURLPattern.MatchString(value) ||
		reasoningInternalHostURLPattern.MatchString(value) ||
		reasoningHostPortPattern.MatchString(value) ||
		reasoningEnvConnectionPattern.MatchString(value) ||
		reasoningKVHostPattern.MatchString(value) {
		return true
	}
	matches := reasoningNonHTTPURLPattern.FindAllString(value, -1)
	for _, match := range matches {
		if !strings.HasPrefix(strings.ToLower(match), "http://") && !strings.HasPrefix(strings.ToLower(match), "https://") {
			return true
		}
	}
	return false
}

func truncateUTF8String(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	if maxBytes <= 0 {
		return ""
	}
	limit := maxBytes
	for limit > 0 && (value[limit]&0xC0) == 0x80 {
		limit--
	}
	return value[:limit]
}

func validateAskInput(input AskInput) error {
	message := strings.TrimSpace(input.Message)
	if message == "" || utf8.RuneCountInString(message) > 32000 {
		return ValidationError(map[string]string{"message": "must be between 1 and 32000 characters"})
	}
	allowedModes := map[string]bool{"": true, "knowledge_qa": true, "general_chat": true, "report_generation": true, "data_analysis": true, "unknown": true}
	if !allowedModes[input.Mode] {
		return ValidationError(map[string]string{"mode": "is not supported"})
	}
	if input.Mode == "data_analysis" {
		return NewError(CodeUnsupportedIntent, "data analysis is not supported", nil)
	}
	if len(input.KnowledgeBaseIDs) > 50 {
		return ValidationError(map[string]string{"knowledgeBaseIds": "must not contain more than 50 items"})
	}
	if err := validateAskRetrieval(input.Retrieval); err != nil {
		return err
	}
	return nil
}

func validateAskRetrieval(retrieval RetrievalOptions) error {
	fields := map[string]string{}
	if (retrieval.topKSet || retrieval.TopK != 0) && (retrieval.TopK <= 0 || retrieval.TopK > 100) {
		fields["retrieval.topK"] = "must be between 1 and 100"
	}
	if (retrieval.scoreSet || retrieval.ScoreThreshold != 0) && (retrieval.ScoreThreshold < 0 || retrieval.ScoreThreshold > 1) {
		fields["retrieval.scoreThreshold"] = "must be between 0 and 1"
	}
	if (retrieval.rerankSet || retrieval.RerankThreshold != 0) && (retrieval.RerankThreshold < 0 || retrieval.RerankThreshold > 1) {
		fields["retrieval.rerankThreshold"] = "must be between 0 and 1"
	}
	if (retrieval.rerankTopNSet || retrieval.RerankTopN != 0) && retrieval.RerankTopN < 0 {
		fields["retrieval.rerankTopN"] = "must be positive"
	}
	if len(fields) > 0 {
		return ValidationError(fields)
	}
	return nil
}

func retrievalSettingsForAsk(defaults RetrievalSettings, override RetrievalOptions) contextutil.RetrievalSettings {
	settings := contextutil.RetrievalSettings{
		TopK:                     defaults.TopK,
		ScoreThreshold:           defaults.ScoreThreshold,
		ScoreThresholdConfigured: defaults.HasScoreThreshold(),
		EnableRerank:             defaults.EnableRerank,
		RerankThreshold:          defaults.RerankThreshold,
		RerankTopN:               defaults.RerankTopN,
	}
	if override.topKSet || override.TopK != 0 {
		settings.TopK = override.TopK
	}
	if override.scoreSet || override.ScoreThreshold != 0 {
		settings.ScoreThreshold = override.ScoreThreshold
		settings.ScoreThresholdConfigured = true
	}
	if override.rerankSet || override.RerankThreshold != 0 {
		settings.RerankThreshold = override.RerankThreshold
	}
	if override.rerankTopNSet || override.RerankTopN != 0 {
		settings.RerankTopN = override.RerankTopN
	}
	if override.enableSet || override.EnableRerank {
		settings.EnableRerank = override.EnableRerank
	}
	return settings
}

func requestDirective(input AskInput) string {
	var parts []string
	if input.Mode != "" && input.Mode != "unknown" {
		parts = append(parts, "The requested QA mode is "+input.Mode+".")
	}
	attachmentIDs := normalizeIDList(input.AttachmentIDs)
	knowledgeBaseIDs := normalizeIDList(input.KnowledgeBaseIDs)
	if input.Mode == "knowledge_qa" {
		parts = append(parts, "For knowledge QA, use long-term knowledge-base retrieval with search_knowledge or knowledge__search when the answer depends on approved domain facts.")
	}
	if len(attachmentIDs) > 0 {
		parts = append(parts, "The current message has ready session attachments. Use search_session_attachments to retrieve relevant attachment chunks when they may help answer the user. Session attachments are temporary context and do not replace long-term knowledge-base RAG; when the question may need approved knowledge-base facts, also use search_knowledge or knowledge__search if those tools are available.")
	}
	if input.Mode == "report_generation" {
		parts = append(parts, "For report generation, use search_session_attachments with include_report_source=true when the user uploaded files, then pass report_source_excerpt as content to document__generate_report_from_content. Use long-term knowledge-base retrieval as an additional source when the report needs approved domain facts. You may also call available Document report tools such as document__generate_report_outline, document__generate_report_text, document__get_generation_status, document__export_report_docx, and document__get_report_result. Treat accepted, pending, or running jobs as asynchronous work and avoid long blocking waits.")
	}
	if len(knowledgeBaseIDs) > 0 {
		parts = append(parts, "When a knowledge tool supports knowledge-base filtering, restrict it to: "+strings.Join(knowledgeBaseIDs, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func toolProgressPayload(summary string, event agent.Event, observation agent.ToolObservation, modelInvocationID string, includeResult bool) map[string]any {
	payload := map[string]any{
		"toolCallId":    event.ToolCallID,
		"tool":          event.ToolName,
		"mcpServerName": toolSourceName(event.ToolName),
		"iterationNo":   event.Iteration,
		"summary":       summary,
		"arguments":     tools.GenerateArgumentsSummary(event.ToolName, observation.Arguments),
	}
	if modelInvocationID != "" {
		payload["modelInvocationId"] = modelInvocationID
	}
	if includeResult {
		payload["result"] = tools.GenerateResultSummary(event.ToolName, observation.Result)
	}
	return payload
}

func toolSourceName(toolName string) string {
	switch toolName {
	case tools.ToolSearchKnowledge, tools.ToolGetCitationSource, tools.ToolSearchSessionAttachments:
		return "qa_builtin"
	}
	if before, _, ok := strings.Cut(toolName, "__"); ok {
		return before
	}
	if before, _, ok := strings.Cut(toolName, "."); ok {
		return before
	}
	return ""
}

func stepFromAgentEvent(messageID string, event agent.Event, now time.Time) (ReasoningStep, bool) {
	step := ReasoningStep{ID: newID("step"), MessageID: messageID, Status: "completed", CreatedAt: now}
	switch event.Type {
	case agent.EventModelStarted:
		step.Type, step.Title, step.Summary, step.Status = "generation", "生成回答", "模型开始处理当前步骤", "running"
	case agent.EventModelCompleted:
		step.Type, step.Title, step.Summary = "generation", "生成回答", "模型完成当前步骤"
	case agent.EventToolStarted:
		step.Type, step.Title, step.Summary, step.Status = "tool", "调用工具", "开始调用工具 "+event.ToolName, "running"
	case agent.EventToolCompleted:
		step.Type, step.Title, step.Summary = "tool", "调用工具", "工具 "+event.ToolName+" 调用完成"
	case agent.EventToolFailed:
		step.Type, step.Title, step.Summary, step.Status = "tool", "调用工具", "工具 "+event.ToolName+" 调用失败", "failed"
	default:
		return ReasoningStep{}, false
	}
	return step, true
}

func emitProgress(observer ProgressObserver, event ProgressEvent) {
	if observer != nil {
		observer(event)
	}
}

func newID(prefix string) string {
	return newUUID()
}

func newUUID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(data)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func normalizeConversationListOptions(options ConversationListOptions) (ConversationListOptions, error) {
	if options.Page <= 0 {
		options.Page = 1
	}
	if options.PageSize <= 0 {
		options.PageSize = 20
	}
	options.Status = strings.TrimSpace(options.Status)
	if options.Status == "" {
		options.Status = "active"
	}
	if options.Status != "active" && options.Status != "archived" {
		return ConversationListOptions{}, ValidationError(map[string]string{"status": "must be active or archived"})
	}
	options.Sort = strings.TrimSpace(options.Sort)
	if options.Sort == "" {
		options.Sort = "-updatedAt"
	}
	switch options.Sort {
	case "-updatedAt", "updatedAt", "-createdAt", "createdAt":
	default:
		return ConversationListOptions{}, ValidationError(map[string]string{"sort": "must be updatedAt, -updatedAt, createdAt, or -createdAt"})
	}
	return options, nil
}

func normalizeMessageListOptions(options MessageListOptions) (MessageListOptions, error) {
	if options.Page <= 0 {
		options.Page = 1
	}
	if options.PageSize <= 0 {
		options.PageSize = 50
	}
	if options.PageSize > 100 {
		return MessageListOptions{}, ValidationError(map[string]string{"pageSize": "must be between 1 and 100"})
	}
	return options, nil
}

func extractCitationsFromToolResult(result string, startCitationNo int) []Citation {
	var payload any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil
	}
	records := collectCitationRecords(payload)
	citations := make([]Citation, 0, len(records))
	citationNo := startCitationNo
	for _, record := range records {
		citation, ok := citationFromRecord(record)
		if !ok {
			continue
		}
		citation.ID = newUUID()
		citation.CitationNo = citationNo
		citations = append(citations, citation)
		citationNo++
	}
	return citations
}

func normalizeIDList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

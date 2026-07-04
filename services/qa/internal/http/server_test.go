package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service"
)

type fakeQAService struct {
	create       func(context.Context, string, string) (service.Conversation, error)
	list         func(context.Context, string, service.ConversationListOptions) (service.Page[service.Conversation], error)
	get          func(context.Context, string, string) (service.Conversation, error)
	update       func(context.Context, string, string, string, string) (service.Conversation, error)
	delete       func(context.Context, string, string) error
	listMessages func(context.Context, string, string, service.MessageListOptions) (service.Page[service.Message], error)
	ask          func(context.Context, string, string, service.AskInput, service.ProgressObserver) (service.AskResult, error)
}

type fakeSettingsService struct{}

type fakeAttachmentService struct {
	upload func(context.Context, string, string, service.CreateAttachmentInput) (service.AttachmentUploadResult, error)
	list   func(context.Context, string, string, service.AttachmentListOptions) (service.Page[service.SessionAttachment], error)
	get    func(context.Context, string, string, string) (service.SessionAttachment, error)
}

func (f fakeAttachmentService) Upload(ctx context.Context, userID, sessionID string, input service.CreateAttachmentInput) (service.AttachmentUploadResult, error) {
	if f.upload != nil {
		return f.upload(ctx, userID, sessionID, input)
	}
	return service.AttachmentUploadResult{}, nil
}
func (f fakeAttachmentService) List(ctx context.Context, userID, sessionID string, options service.AttachmentListOptions) (service.Page[service.SessionAttachment], error) {
	if f.list != nil {
		return f.list(ctx, userID, sessionID, options)
	}
	return service.Page[service.SessionAttachment]{}, nil
}
func (f fakeAttachmentService) Get(ctx context.Context, userID, sessionID, attachmentID string) (service.SessionAttachment, error) {
	if f.get != nil {
		return f.get(ctx, userID, sessionID, attachmentID)
	}
	return service.SessionAttachment{}, nil
}
func (fakeAttachmentService) Delete(context.Context, string, string, string) error  { return nil }
func (fakeAttachmentService) Process(context.Context, string, string, string) error { return nil }

type fakeResourceService struct{}
type fakeResourceServiceWithListEvents struct {
	fakeResourceService
	listStreamEvents func(context.Context, string, string, string, int) ([]service.StreamEvent, error)
}
type fakeResourceServiceWithCreate struct {
	fakeResourceService
	createRetrievalTestRun func(context.Context, string, service.RetrievalTestInput) (service.RetrievalTestRun, error)
}
type fakeResourceServiceWithGetRetrieval struct {
	fakeResourceService
	getRetrievalTestRun func(context.Context, string, string) (service.RetrievalTestRun, error)
}

func (fakeSettingsService) GetSettings(context.Context) (service.QASettings, error) {
	return service.QASettings{}, nil
}
func (fakeSettingsService) UpdateSettings(context.Context, string, string, service.UpdateQASettingsInput) (service.QASettings, error) {
	return service.QASettings{}, nil
}
func (fakeSettingsService) ListMCPServers(context.Context) ([]service.MCPServer, error) {
	return []service.MCPServer{}, nil
}
func (fakeSettingsService) CreateMCPServer(context.Context, string, string, service.MCPServerInput) (service.MCPServer, error) {
	return service.MCPServer{}, nil
}
func (fakeSettingsService) UpdateMCPServer(context.Context, string, string, string, service.MCPServerPatch) (service.MCPServer, error) {
	return service.MCPServer{}, nil
}
func (fakeSettingsService) DeleteMCPServer(context.Context, string, string, string) error {
	return nil
}
func (fakeSettingsService) TestLLMConnection(context.Context, service.LLMConnectionTestInput) (service.LLMConnectionTestResult, error) {
	return service.LLMConnectionTestResult{Success: true}, nil
}
func (fakeSettingsService) TestMCPConnection(context.Context, service.MCPConnectionTestInput) (service.MCPConnectionTestResult, error) {
	return service.MCPConnectionTestResult{Success: true}, nil
}

func (f fakeQAService) CreateConversation(ctx context.Context, userID, title string) (service.Conversation, error) {
	return f.create(ctx, userID, title)
}
func (f fakeQAService) ListConversations(ctx context.Context, userID string, options service.ConversationListOptions) (service.Page[service.Conversation], error) {
	if f.list != nil {
		return f.list(ctx, userID, options)
	}
	return service.Page[service.Conversation]{Items: []service.Conversation{}, Page: 1, PageSize: 20}, nil
}
func (f fakeQAService) GetConversation(ctx context.Context, userID, sessionID string) (service.Conversation, error) {
	if f.get != nil {
		return f.get(ctx, userID, sessionID)
	}
	return service.Conversation{}, nil
}
func (f fakeQAService) UpdateConversation(ctx context.Context, userID, sessionID, title, status string) (service.Conversation, error) {
	if f.update != nil {
		return f.update(ctx, userID, sessionID, title, status)
	}
	return service.Conversation{}, nil
}
func (fakeResourceService) GetResponseRun(context.Context, string, string) (service.ResponseRun, error) {
	return service.ResponseRun{}, nil
}
func (fakeResourceService) CancelResponseRun(context.Context, string, string) (service.ResponseRun, error) {
	return service.ResponseRun{}, nil
}
func (fakeResourceService) ListStreamEvents(context.Context, string, string, string, int) ([]service.StreamEvent, error) {
	return []service.StreamEvent{}, nil
}
func (f fakeResourceServiceWithListEvents) ListStreamEvents(ctx context.Context, userID, sessionID, runID string, after int) ([]service.StreamEvent, error) {
	return f.listStreamEvents(ctx, userID, sessionID, runID, after)
}
func (fakeResourceService) ListMessageCitations(context.Context, string, string) ([]service.Citation, error) {
	return []service.Citation{}, nil
}
func (fakeResourceService) GetCitation(context.Context, string, string) (service.Citation, error) {
	return service.Citation{}, nil
}
func (fakeResourceService) LookupCitations(context.Context, string, []string) ([]service.Citation, error) {
	return []service.Citation{}, nil
}
func (fakeResourceService) ListToolCalls(context.Context, string, string) ([]service.AgentToolCall, error) {
	return []service.AgentToolCall{}, nil
}
func (fakeResourceService) GetActiveQAConfigVersion(context.Context) (service.QAConfigVersion, error) {
	return service.QAConfigVersion{}, nil
}
func (fakeResourceService) CreateQAConfigVersion(context.Context, string, service.CreateQAConfigVersionInput) (service.QAConfigVersion, error) {
	return service.QAConfigVersion{}, nil
}
func (fakeResourceService) GetActiveLLMConfigVersion(context.Context) (service.LLMConfigVersion, error) {
	return service.LLMConfigVersion{}, nil
}
func (fakeResourceService) CreateLLMConfigVersion(context.Context, string, service.CreateLLMConfigVersionInput) (service.LLMConfigVersion, error) {
	return service.LLMConfigVersion{}, nil
}
func (fakeResourceService) TestLLMConnection(context.Context, string, service.LLMProfileTestInput) (service.LLMProfileTestResult, error) {
	return service.LLMProfileTestResult{}, nil
}
func (fakeResourceService) CreateRetrievalTestRun(context.Context, string, service.RetrievalTestInput) (service.RetrievalTestRun, error) {
	return service.RetrievalTestRun{}, nil
}
func (f fakeResourceServiceWithCreate) CreateRetrievalTestRun(ctx context.Context, userID string, input service.RetrievalTestInput) (service.RetrievalTestRun, error) {
	return f.createRetrievalTestRun(ctx, userID, input)
}
func (fakeResourceService) GetRetrievalTestRun(context.Context, string, string) (service.RetrievalTestRun, error) {
	return service.RetrievalTestRun{}, nil
}
func (f fakeResourceServiceWithGetRetrieval) GetRetrievalTestRun(ctx context.Context, userID, id string) (service.RetrievalTestRun, error) {
	return f.getRetrievalTestRun(ctx, userID, id)
}
func (fakeResourceService) GetMetricsOverview(context.Context, string, int) (service.MetricsOverview, error) {
	return service.MetricsOverview{}, nil
}
func (fakeResourceService) GetMetricsTrend(context.Context, int) (service.MetricsTrend, error) {
	return service.MetricsTrend{}, nil
}
func (fakeResourceService) GetTopQueries(context.Context, int, int) ([]service.TopQuery, error) {
	return []service.TopQuery{}, nil
}
func (fakeResourceService) GetIntentDistribution(context.Context, int) ([]service.IntentDistribution, error) {
	return []service.IntentDistribution{}, nil
}
func (f fakeQAService) DeleteConversation(ctx context.Context, userID, sessionID string) error {
	if f.delete != nil {
		return f.delete(ctx, userID, sessionID)
	}
	return nil
}
func (f fakeQAService) ListMessages(ctx context.Context, userID, sessionID string, options service.MessageListOptions) (service.Page[service.Message], error) {
	if f.listMessages != nil {
		return f.listMessages(ctx, userID, sessionID, options)
	}
	return service.Page[service.Message]{Items: []service.Message{}, Page: 1, PageSize: 50}, nil
}
func (f fakeQAService) Ask(ctx context.Context, userID, conversationID string, input service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
	return f.ask(ctx, userID, conversationID, input, observer)
}

func TestConversationEndpointRequiresUserContext(t *testing.T) {
	server := newTestServer(t, fakeQAService{create: func(context.Context, string, string) (service.Conversation, error) {
		t.Fatal("service should not be called")
		return service.Conversation{}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions", strings.NewReader(`{"title":"test"}`))
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}

func TestAPIRequiresServiceToken(t *testing.T) {
	server := newTestServer(t, fakeQAService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions", nil)
	request.Header.Set("X-User-Id", "forged-user")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("unexpected error body: %s", recorder.Body.String())
	}
}

func TestCreateConversationMatchesContract(t *testing.T) {
	now := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	server := newTestServer(t, fakeQAService{create: func(_ context.Context, userID, title string) (service.Conversation, error) {
		if userID != "user-1" || title != "锅炉咨询" {
			t.Fatalf("unexpected input: user=%q title=%q", userID, title)
		}
		return service.Conversation{ID: "conversation-id", Title: title, Status: "active", CreatedAt: now, UpdatedAt: now}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions", strings.NewReader(`{"title":"锅炉咨询"}`))
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data      service.Conversation `json:"data"`
		RequestID string               `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	conversation := body.Data
	if conversation.ID != "conversation-id" || conversation.Title != "锅炉咨询" {
		t.Fatalf("unexpected response: %+v", conversation)
	}
}

func TestCreateConversationAllowsEmptyBody(t *testing.T) {
	server := newTestServer(t, fakeQAService{create: func(_ context.Context, _, title string) (service.Conversation, error) {
		if title != "" {
			t.Fatalf("title=%q", title)
		}
		return service.Conversation{ID: "conversation-id", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestListConversationUsesDocumentedQueryParameters(t *testing.T) {
	server := newTestServer(t, fakeQAService{list: func(_ context.Context, userID string, options service.ConversationListOptions) (service.Page[service.Conversation], error) {
		if userID != "user-1" {
			t.Fatalf("userID=%q", userID)
		}
		want := service.ConversationListOptions{Page: 2, PageSize: 5, Status: "archived", Sort: "createdAt"}
		if options != want {
			t.Fatalf("options=%+v want %+v", options, want)
		}
		return service.Page[service.Conversation]{Items: []service.Conversation{{ID: "conversation-id", Status: "archived"}}, Page: options.Page, PageSize: options.PageSize, Total: 1}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions?page=2&pageSize=5&status=archived&sort=createdAt", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"pageSize":5`) {
		t.Fatalf("unexpected page response: %s", recorder.Body.String())
	}
}

func TestListAttachmentsForwardsStatusAndReturnsPublicShape(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	attachments := fakeAttachmentService{list: func(_ context.Context, userID, sessionID string, options service.AttachmentListOptions) (service.Page[service.SessionAttachment], error) {
		if userID != "user-1" || sessionID != "session-id" {
			t.Fatalf("unexpected owner: user=%q session=%q", userID, sessionID)
		}
		if options != (service.AttachmentListOptions{Page: 2, PageSize: 5, Status: service.AttachmentStatusFailed}) {
			t.Fatalf("options=%+v", options)
		}
		return service.Page[service.SessionAttachment]{
			Items: []service.SessionAttachment{{
				ID: "attachment-id", SessionID: sessionID, OwnerUserID: userID,
				FileRef: "private-file-ref", Filename: "failed.pdf", ContentType: "application/pdf",
				SizeBytes: 42, Status: service.AttachmentStatusFailed, ErrorSummary: "safe parse failure",
				PageCount: 3, ChunkCount: 4, ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
			}},
			Page: 2, PageSize: 5, Total: 1,
		}, nil
	}}
	server := newTestServerWithAttachments(t, attachments)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-id/attachments?page=2&pageSize=5&status=failed", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{`"errorCode":"attachment_parse_failed"`, `"errorMessage":"safe parse failure"`, `"status":"failed"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %s: %s", expected, body)
		}
	}
	for _, privateField := range []string{"private-file-ref", `"errorSummary"`, `"pageCount"`, `"chunkCount"`, `"updatedAt"`, `"deletedAt"`} {
		if strings.Contains(body, privateField) {
			t.Fatalf("response exposed %s: %s", privateField, body)
		}
	}
}

func TestListAttachmentsReturnsValidationErrorForUnsupportedStatus(t *testing.T) {
	attachments := fakeAttachmentService{list: func(_ context.Context, _, _ string, options service.AttachmentListOptions) (service.Page[service.SessionAttachment], error) {
		if options.Status != "unknown" {
			t.Fatalf("status=%q", options.Status)
		}
		return service.Page[service.SessionAttachment]{}, service.ValidationError(map[string]string{"status": "unsupported"})
	}}
	server := newTestServerWithAttachments(t, attachments)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-id/attachments?status=unknown", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"validation_error"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStreamUsesContractEventNames(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, input service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		if input.Message != "检查要求" {
			t.Fatalf("unexpected input: %+v", input)
		}
		observer(service.ProgressEvent{Type: "message.created", Sequence: 1, Payload: map[string]any{"userMessageId": "user-message"}})
		observer(service.ProgressEvent{Type: "reasoning.step", Sequence: 2, Payload: map[string]any{"detail": "检索完成"}})
		observer(service.ProgressEvent{Type: "answer.delta", Sequence: 3, Payload: map[string]any{"text": "回答内容"}})
		observer(service.ProgressEvent{Type: "answer.completed", Sequence: 4, Payload: map[string]any{"messageId": "assistant-message"}})
		return service.AskResult{AssistantMessage: service.Message{ID: "assistant-message", Content: "回答内容", Status: "completed"}}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/conversation-id/messages", strings.NewReader(`{"message":"检查要求","mode":"knowledge_qa"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("unexpected stream response: status=%d content-type=%q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
	body := recorder.Body.String()
	for _, event := range []string{"message.created", "reasoning.step", "answer.delta", "answer.completed"} {
		if !strings.Contains(body, "event: "+event) {
			t.Fatalf("missing event %q in %s", event, body)
		}
	}
}

func TestCreateRetrievalTestReturnsSavedFailedRun(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	resources := fakeResourceServiceWithCreate{
		createRetrievalTestRun: func(_ context.Context, userID string, input service.RetrievalTestInput) (service.RetrievalTestRun, error) {
			if userID != "user-1" || input.Question != "query" {
				t.Fatalf("unexpected input: user=%q input=%+v", userID, input)
			}
			return service.RetrievalTestRun{
				ID:           "rt-1",
				Question:     input.Question,
				Status:       "failed",
				ResultCount:  0,
				LatencyMS:    25,
				ErrorMessage: "knowledge retrieval failed",
				Results:      []service.RetrievalTestResult{},
				CreatedAt:    now,
				FinishedAt:   &now,
			}, nil
		},
	}
	server := newTestServerWithResources(t, fakeQAService{}, resources)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/retrieval-test-runs", strings.NewReader(`{"question":"query"}`))
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-User-Permissions", "qa:use")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data service.RetrievalTestRun `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.ID != "rt-1" || body.Data.Status != "failed" || body.Data.ErrorMessage == "" {
		t.Fatalf("unexpected run: %+v", body.Data)
	}
}

func TestCreateRetrievalTestRequiresQAUse(t *testing.T) {
	resources := fakeResourceServiceWithCreate{
		createRetrievalTestRun: func(context.Context, string, service.RetrievalTestInput) (service.RetrievalTestRun, error) {
			t.Fatal("CreateRetrievalTestRun should not be called without qa:use")
			return service.RetrievalTestRun{}, nil
		},
	}
	server := newTestServerWithResources(t, fakeQAService{}, resources)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/retrieval-test-runs", strings.NewReader(`{"question":"query"}`))
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-User-Permissions", "knowledge:read")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestGetRetrievalTestAllowsQAUse(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	resources := fakeResourceServiceWithGetRetrieval{
		getRetrievalTestRun: func(_ context.Context, userID, id string) (service.RetrievalTestRun, error) {
			if userID != "user-1" || id != "rt-1" {
				t.Fatalf("unexpected input: user=%q id=%q", userID, id)
			}
			return service.RetrievalTestRun{
				ID:          "rt-1",
				Question:    "query",
				Status:      "completed",
				ResultCount: 0,
				Results:     []service.RetrievalTestResult{},
				CreatedAt:   now,
				FinishedAt:  &now,
			}, nil
		},
	}
	server := newTestServerWithResources(t, fakeQAService{}, resources)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/retrieval-test-runs/rt-1", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-User-Permissions", "qa:use")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data service.RetrievalTestRun `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.ID != "rt-1" || body.Data.Status != "completed" {
		t.Fatalf("unexpected run: %+v", body.Data)
	}
}

func TestListEventsReturnsValidationErrorForCursorOverflow(t *testing.T) {
	resources := fakeResourceServiceWithListEvents{
		listStreamEvents: func(context.Context, string, string, string, int) ([]service.StreamEvent, error) {
			return nil, service.ValidationError(map[string]string{"afterEventSeq": "must be between 0 and 2147483647"})
		},
	}
	server := newTestServerWithResources(t, fakeQAService{}, resources)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-id/events?responseRunId=run-id&afterEventSeq=2147483648", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"validation_error"`) || !strings.Contains(recorder.Body.String(), "afterEventSeq") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
}

func TestStreamDoesNotDuplicateServiceErrorEvent(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		observer(service.ProgressEvent{Type: "error", Sequence: 2, Payload: map[string]any{"code": "dependency_error", "message": "answer generation failed"}})
		return service.AskResult{}, service.NewError(service.CodeDependency, "answer generation failed", nil)
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/session-id/messages", strings.NewReader(`{"message":"question"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if count := strings.Count(recorder.Body.String(), "event: error"); count != 1 {
		t.Fatalf("error event count=%d body=%s", count, recorder.Body.String())
	}
}

func TestStreamEmitsHeartbeatWhileAskIsIdle(t *testing.T) {
	originalInterval := streamHeartbeatInterval
	streamHeartbeatInterval = time.Millisecond
	t.Cleanup(func() { streamHeartbeatInterval = originalInterval })

	server := newTestServer(t, fakeQAService{ask: func(ctx context.Context, _, _ string, _ service.AskInput, _ service.ProgressObserver) (service.AskResult, error) {
		select {
		case <-time.After(10 * time.Millisecond):
			return service.AskResult{}, nil
		case <-ctx.Done():
			return service.AskResult{}, ctx.Err()
		}
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/session-id/messages", strings.NewReader(`{"message":"question"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if !strings.Contains(body, "event: heartbeat") {
		t.Fatalf("missing heartbeat in stream body: %s", body)
	}
	if strings.Contains(body, "event: heartbeat\nid:") {
		t.Fatalf("heartbeat must not use a replay event id: %s", body)
	}
}

func TestListMessagesUsesDocumentedQueryParameters(t *testing.T) {
	server := newTestServer(t, fakeQAService{listMessages: func(_ context.Context, userID, sessionID string, options service.MessageListOptions) (service.Page[service.Message], error) {
		if userID != "user-1" || sessionID != "session-1" {
			t.Fatalf("userID=%q sessionID=%q", userID, sessionID)
		}
		want := service.MessageListOptions{Page: 3, PageSize: 10, IncludeThinking: false, IncludeCitations: false}
		if options != want {
			t.Fatalf("options=%+v want %+v", options, want)
		}
		return service.Page[service.Message]{Items: []service.Message{{ID: "message-id", ConversationID: sessionID, Role: "user", Status: "completed", Content: "question"}}, Page: options.Page, PageSize: options.PageSize, Total: 1}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-1/messages?page=3&pageSize=10&includeThinking=false&includeCitations=false", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"pageSize":10`) {
		t.Fatalf("unexpected page response: %s", recorder.Body.String())
	}
}

func TestListMessagesDefaultsIncludeParametersToTrue(t *testing.T) {
	server := newTestServer(t, fakeQAService{listMessages: func(_ context.Context, _, _ string, options service.MessageListOptions) (service.Page[service.Message], error) {
		if !options.IncludeThinking || !options.IncludeCitations {
			t.Fatalf("include defaults = %+v", options)
		}
		return service.Page[service.Message]{Items: []service.Message{}, Page: options.Page, PageSize: options.PageSize}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-1/messages", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestListMessagesRejectsInvalidIncludeParameter(t *testing.T) {
	server := newTestServer(t, fakeQAService{listMessages: func(context.Context, string, string, service.MessageListOptions) (service.Page[service.Message], error) {
		t.Fatal("service should not be called")
		return service.Page[service.Message]{}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-1/messages?includeThinking=maybe", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"includeThinking":"must be a boolean"`) {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

func TestListMessagesPropagatesCrossUserForbidden(t *testing.T) {
	server := newTestServer(t, fakeQAService{listMessages: func(context.Context, string, string, service.MessageListOptions) (service.Page[service.Message], error) {
		return service.Page[service.Message]{}, service.NewError(service.CodeForbidden, "conversation access denied", nil)
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/other-session/messages", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

func TestSessionOperationsReturnForbiddenForNonOwnerEvenWithAdminRole(t *testing.T) {
	forbidden := service.NewError(service.CodeForbidden, "conversation access denied", nil)
	qa := fakeQAService{
		get: func(context.Context, string, string) (service.Conversation, error) {
			return service.Conversation{}, forbidden
		},
		update: func(context.Context, string, string, string, string) (service.Conversation, error) {
			return service.Conversation{}, forbidden
		},
		delete: func(context.Context, string, string) error {
			return forbidden
		},
	}
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "detail", method: http.MethodGet},
		{name: "update", method: http.MethodPatch, body: `{"title":"private"}`},
		{name: "delete", method: http.MethodDelete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, qa)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, "/internal/v1/qa-sessions/other-session", strings.NewReader(test.body))
			request.Header.Set("X-User-Id", "user-1")
			request.Header.Set("X-User-Roles", "admin")
			request.Header.Set("X-Service-Token", "test-service-token")
			server.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
				t.Fatalf("unexpected error response: %s", recorder.Body.String())
			}
		})
	}
}

func TestStreamIncludesToolEventsWithSummary(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		observer(service.ProgressEvent{Type: "message.created", Sequence: 1, Payload: map[string]any{"userMessageId": "user-message"}})
		observer(service.ProgressEvent{Type: "tool.started", Sequence: 2, Payload: map[string]any{"tool": "search_knowledge", "summary": "正在执行工具 search_knowledge"}})
		observer(service.ProgressEvent{Type: "tool.completed", Sequence: 3, Payload: map[string]any{"tool": "search_knowledge", "summary": "工具 search_knowledge 执行完成"}})
		observer(service.ProgressEvent{Type: "answer.delta", Sequence: 4, Payload: map[string]any{"text": "回答内容"}})
		observer(service.ProgressEvent{Type: "answer.completed", Sequence: 5, Payload: map[string]any{"messageId": "assistant-message"}})
		return service.AskResult{AssistantMessage: service.Message{ID: "assistant-message", Content: "回答内容", Status: "completed"}}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/conversation-id/messages", strings.NewReader(`{"message":"测试工具事件"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, event := range []string{"tool.started", "tool.completed"} {
		if !strings.Contains(body, "event: "+event) {
			t.Fatalf("missing event %q in %s", event, body)
		}
	}
	if !strings.Contains(body, "正在执行工具") || !strings.Contains(body, "执行完成") {
		t.Fatalf("missing summary in tool events: %s", body)
	}
}

func TestStreamIncludesHeartbeatEvent(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		observer(service.ProgressEvent{Type: "message.created", Sequence: 1, Payload: map[string]any{"userMessageId": "user-message"}})
		observer(service.ProgressEvent{Type: "answer.delta", Sequence: 2, Payload: map[string]any{"text": "回答"}})
		observer(service.ProgressEvent{Type: "heartbeat", Sequence: 3, Payload: map[string]any{}})
		observer(service.ProgressEvent{Type: "answer.completed", Sequence: 4, Payload: map[string]any{"messageId": "assistant-message"}})
		return service.AskResult{AssistantMessage: service.Message{ID: "assistant-message", Content: "回答", Status: "completed"}}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/conversation-id/messages", strings.NewReader(`{"message":"测试心跳"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: heartbeat") {
		t.Fatalf("missing heartbeat event: %s", body)
	}
}

func TestStreamEmitsErrorOnServiceFailure(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		observer(service.ProgressEvent{Type: "error", Sequence: 1, Payload: map[string]any{"code": "validation_error", "message": "invalid request"}})
		return service.AskResult{}, service.NewError(service.CodeValidation, "invalid request", nil)
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/session-id/messages", strings.NewReader(`{"message":"测试错误"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, `"code":"validation_error"`) {
		t.Fatalf("missing error code in response: %s", body)
	}
}

func TestStreamEventSequenceIsMonotonic(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		for i := 1; i <= 5; i++ {
			observer(service.ProgressEvent{Type: "reasoning.step", Sequence: i, Payload: map[string]any{"detail": "step"}})
		}
		return service.AskResult{AssistantMessage: service.Message{ID: "assistant-message", Content: "done", Status: "completed"}}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/conversation-id/messages", strings.NewReader(`{"message":"测试序号"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestListEventsRequiresResponseRunId(t *testing.T) {
	server := newTestServer(t, fakeQAService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-sessions/session-id/events", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"responseRunId":"is required"`) {
		t.Fatalf("unexpected error response: %s", recorder.Body.String())
	}
}

func TestStreamIncludesCitationDeltaEvents(t *testing.T) {
	server := newTestServer(t, fakeQAService{ask: func(_ context.Context, _, _ string, _ service.AskInput, observer service.ProgressObserver) (service.AskResult, error) {
		observer(service.ProgressEvent{Type: "tool.completed", Sequence: 1, Payload: map[string]any{"tool": "search_knowledge"}})
		observer(service.ProgressEvent{Type: "citation.delta", Sequence: 2, Payload: map[string]any{"citation": map[string]any{"id": "cit_1", "citationNo": 1, "docName": "测试文档.pdf", "score": 0.95}}})
		observer(service.ProgressEvent{Type: "answer.delta", Sequence: 3, Payload: map[string]any{"text": "基于文档内容的回答"}})
		observer(service.ProgressEvent{Type: "answer.completed", Sequence: 4, Payload: map[string]any{"messageId": "assistant-message"}})
		return service.AskResult{AssistantMessage: service.Message{ID: "assistant-message", Content: "基于文档内容的回答", Status: "completed"}}, nil
	}})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/qa-sessions/conversation-id/messages", strings.NewReader(`{"message":"测试引用"}`))
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "event: citation.delta") {
		t.Fatalf("missing citation.delta event: %s", body)
	}
	if !strings.Contains(body, `"docName":"测试文档.pdf"`) {
		t.Fatalf("missing docName in citation.delta: %s", body)
	}
	if !strings.Contains(body, `"score":0.95`) {
		t.Fatalf("missing score in citation.delta: %s", body)
	}
}

func newTestServer(t *testing.T, qa fakeQAService) *Server {
	t.Helper()
	return newTestServerWithResources(t, qa, fakeResourceService{})
}

func newTestServerWithResources(t *testing.T, qa fakeQAService, resources ResourceService) *Server {
	t.Helper()
	if qa.create == nil {
		qa.create = func(context.Context, string, string) (service.Conversation, error) {
			return service.Conversation{}, nil
		}
	}
	if qa.ask == nil {
		qa.ask = func(context.Context, string, string, service.AskInput, service.ProgressObserver) (service.AskResult, error) {
			return service.AskResult{}, nil
		}
	}
	server, err := NewServer(qa, fakeSettingsService{}, resources, fakeAttachmentService{}, Config{MaxRequestBytes: 4096, ServiceToken: "test-service-token"})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func newTestServerWithAttachments(t *testing.T, attachments AttachmentService) *Server {
	t.Helper()
	qa := fakeQAService{
		create: func(context.Context, string, string) (service.Conversation, error) {
			return service.Conversation{}, nil
		},
		ask: func(context.Context, string, string, service.AskInput, service.ProgressObserver) (service.AskResult, error) {
			return service.AskResult{}, nil
		},
	}
	server, err := NewServer(qa, fakeSettingsService{}, fakeResourceService{}, attachments, Config{MaxRequestBytes: 4096, ServiceToken: "test-service-token"})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func newTestServerSettingsOpen(t *testing.T, qa fakeQAService, resources ResourceService) *Server {
	t.Helper()
	if qa.create == nil {
		qa.create = func(context.Context, string, string) (service.Conversation, error) {
			return service.Conversation{}, nil
		}
	}
	if qa.ask == nil {
		qa.ask = func(context.Context, string, string, service.AskInput, service.ProgressObserver) (service.AskResult, error) {
			return service.AskResult{}, nil
		}
	}
	server, err := NewServer(qa, fakeSettingsService{}, resources, fakeAttachmentService{}, Config{MaxRequestBytes: 4096, ServiceToken: "test-service-token", SettingsOpen: true})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestMetricsEndpointRequiresSettingsPermission(t *testing.T) {
	server := newTestServer(t, fakeQAService{})
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/internal/v1/qa-metrics/overview"},
		{http.MethodGet, "/internal/v1/qa-metrics/trend"},
		{http.MethodGet, "/internal/v1/qa-metrics/top-queries"},
		{http.MethodGet, "/internal/v1/qa-metrics/intent-distribution"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("X-User-Id", "user-1")
			request.Header.Set("X-Service-Token", "test-service-token")
			server.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("%s %s: expected 403, got %d", tc.method, tc.path, recorder.Code)
			}
		})
	}
}

func TestMetricsOverviewReturnsSuccessWithPermission(t *testing.T) {
	resources := fakeResourceService{}
	server := newTestServerSettingsOpen(t, fakeQAService{}, resources)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-metrics/overview", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data      service.MetricsOverview `json:"data"`
		RequestID string                  `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RequestID == "" {
		t.Fatal("expected requestId in response")
	}
}

func TestMetricsDaysValidation(t *testing.T) {
	server := newTestServerSettingsOpen(t, fakeQAService{}, fakeResourceService{})
	for _, tc := range []string{
		"/internal/v1/qa-metrics/overview?days=0",
		"/internal/v1/qa-metrics/overview?days=400",
		"/internal/v1/qa-metrics/trend?days=-1",
	} {
		t.Run(tc, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, tc, nil)
			request.Header.Set("X-User-Id", "user-1")
			request.Header.Set("X-Service-Token", "test-service-token")
			server.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d", tc, recorder.Code)
			}
		})
	}
}

func TestTopQueriesLimitValidation(t *testing.T) {
	server := newTestServerSettingsOpen(t, fakeQAService{}, fakeResourceService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-metrics/top-queries?limit=200", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestMetricsTrendDoesNotLeakQueryContent(t *testing.T) {
	server := newTestServerSettingsOpen(t, fakeQAService{}, fakeResourceService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/internal/v1/qa-metrics/trend?days=7", nil)
	request.Header.Set("X-User-Id", "user-1")
	request.Header.Set("X-Service-Token", "test-service-token")
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"secret", "token", "prompt", "apiKey", "objectKey"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("metrics response leaked %q", forbidden)
		}
	}
}

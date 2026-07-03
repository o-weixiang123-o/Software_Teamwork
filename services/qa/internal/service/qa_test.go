package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/platform/contextutil"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

type fakeRepository struct {
	conversation             Conversation
	getConversationErr       error
	deleteErr                error
	messages                 []Message
	listMessagesErr          error
	messageOptions           MessageListOptions
	savedSteps               []ReasoningStep
	savedEvents              []StreamEvent
	invocations              []ModelInvocation
	finalization             ResponseRunFinalization
	run                      ResponseRun
	finalizeErr              error
	finalizeErrRun           ResponseRun
	failOnCanceledFinalizing bool
	failOnCanceledInvocation bool
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (r *fakeRepository) CreateConversation(_ context.Context, value Conversation) (Conversation, error) {
	r.conversation = value
	return value, nil
}
func (r *fakeRepository) ListConversations(_ context.Context, _ string, options ConversationListOptions) (Page[Conversation], error) {
	return Page[Conversation]{Items: []Conversation{r.conversation}, Page: options.Page, PageSize: options.PageSize, Total: 1}, nil
}
func (r *fakeRepository) GetConversation(context.Context, string, string) (Conversation, error) {
	if r.getConversationErr != nil {
		return Conversation{}, r.getConversationErr
	}
	return r.conversation, nil
}
func (r *fakeRepository) UpdateConversation(_ context.Context, _ string, value Conversation) (Conversation, error) {
	r.conversation = value
	return value, nil
}
func (r *fakeRepository) DeleteConversation(context.Context, string, string) error {
	return r.deleteErr
}
func (r *fakeRepository) ListMessages(_ context.Context, _ string, _ string, options MessageListOptions) (Page[Message], error) {
	if r.listMessagesErr != nil {
		return Page[Message]{}, r.listMessagesErr
	}
	r.messageOptions = options
	return Page[Message]{Items: append([]Message(nil), r.messages...), Page: options.Page, PageSize: options.PageSize, Total: len(r.messages)}, nil
}
func (r *fakeRepository) AppendMessages(_ context.Context, _, sessionID string, start ResponseRunStart, _ []string, values ...Message) (ResponseRun, error) {
	r.messages = append(r.messages, values...)
	maxIterations := start.MaxIterations
	if maxIterations == 0 {
		maxIterations = 5
	}
	r.run = ResponseRun{ID: "run-id", SessionID: sessionID, UserMessageID: values[0].ID, AssistantMessageID: values[1].ID, Status: "running", MaxIterations: maxIterations, CreatedAt: values[0].CreatedAt}
	return r.run, nil
}
func (r *fakeRepository) SaveStreamEvents(_ context.Context, _, _ string, events []StreamEvent) error {
	r.savedEvents = append([]StreamEvent(nil), events...)
	return nil
}
func (r *fakeRepository) GetResponseRun(context.Context, string, string) (ResponseRun, error) {
	r.run.Status = "completed"
	return r.run, nil
}
func (r *fakeRepository) UpdateMessage(_ context.Context, _ string, value Message) error {
	for index := range r.messages {
		if r.messages[index].ID == value.ID {
			r.messages[index] = value
			return nil
		}
	}
	return errors.New("message not found")
}
func (r *fakeRepository) FinalizeResponseRun(ctx context.Context, _ string, final ResponseRunFinalization) (ResponseRun, error) {
	if r.failOnCanceledFinalizing {
		if err := ctx.Err(); err != nil {
			return ResponseRun{}, err
		}
	}
	if r.finalizeErr != nil {
		if r.finalizeErrRun.ID != "" {
			return r.finalizeErrRun, r.finalizeErr
		}
		return r.run, r.finalizeErr
	}
	r.finalization = final
	if err := r.UpdateMessage(context.Background(), "", final.AssistantMessage); err != nil {
		return ResponseRun{}, err
	}
	r.savedSteps = append([]ReasoningStep(nil), final.ReasoningSteps...)
	r.savedEvents = append([]StreamEvent(nil), final.StreamEvents...)
	r.run.Status = final.Status
	r.run.CurrentIteration = final.CurrentIteration
	r.run.TotalTokens = final.TotalTokens
	r.run.CompletedAt = &final.CompletedAt
	if final.TerminationReason != "" {
		reason := final.TerminationReason
		r.run.TerminationReason = &reason
	}
	return r.run, nil
}
func (r *fakeRepository) SaveReasoningSteps(_ context.Context, _, _ string, steps []ReasoningStep) error {
	r.savedSteps = append([]ReasoningStep(nil), steps...)
	return nil
}
func (r *fakeRepository) SaveModelInvocation(ctx context.Context, _ string, invocation ModelInvocation) (string, error) {
	if r.failOnCanceledInvocation {
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	r.invocations = append(r.invocations, invocation)
	return fmt.Sprintf("invocation-%d", invocation.IterationNo), nil
}
func (r *fakeRepository) SaveCitations(_ context.Context, _, _ string, citations []Citation) error {
	return nil
}
func (r *fakeRepository) ValidateReadyAttachments(context.Context, string, string, []string) ([]SessionAttachment, error) {
	return nil, nil
}

type fakeAgentRunner struct {
	input             []agent.Message
	userID            string
	retrievalSettings contextutil.RetrievalSettings
}
type blockingAgentRunner struct{ started chan struct{} }

func (r blockingAgentRunner) RunWithObserver(ctx context.Context, _ []agent.Message, _ agent.Observer) (agent.Result, error) {
	close(r.started)
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}
func (r blockingAgentRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type completedThenCancelledRunner struct{ completed chan struct{} }

func (r completedThenCancelledRunner) RunWithObserver(ctx context.Context, _ []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}})
	close(r.completed)
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}
func (r completedThenCancelledRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type cancelAfterCompletedRunner struct{ cancel context.CancelFunc }

func (r cancelAfterCompletedRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}})
	r.cancel()
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer after disconnect"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}
func (r cancelAfterCompletedRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type cancelAfterCompletedCitationRunner struct{ cancel context.CancelFunc }

func (r cancelAfterCompletedCitationRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}})
	r.cancel()
	tool := agent.Message{
		Role:    agent.RoleTool,
		Name:    "search_knowledge",
		Content: `{"results":[{"knowledgeBaseId":"kb-1","documentId":"doc-1","documentName":"Doc","chunkId":"chunk-1","text":"quoted"}]}`,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with citation"}
	messages := append(append([]agent.Message(nil), input...), tool, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}

func (r cancelAfterCompletedCitationRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type cancelBeforeCompletedObserverRunner struct{ cancel context.CancelFunc }

func (r cancelBeforeCompletedObserverRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	r.cancel()
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}})
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer after early disconnect"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}
func (r cancelBeforeCompletedObserverRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type cancelRequestDuringModelRunner struct {
	cancel      context.CancelFunc
	sawCanceled bool
}

func (r *cancelRequestDuringModelRunner) RunWithObserver(ctx context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	r.cancel()
	if err := ctx.Err(); err != nil {
		r.sawCanceled = true
		return agent.Result{}, err
	}
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10}})
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer survived disconnect"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}
func (r *cancelRequestDuringModelRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type toolProgressRunner struct{}

func (toolProgressRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12}})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	final := agent.Message{Role: agent.RoleAssistant, Content: "tool answer"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}
func (toolProgressRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return toolProgressRunner{}.RunWithObserver(ctx, input, observer)
}

type citationToolRunner struct{}

const citationToolResultContent = `{"data":{"results":[{"documentId":"doc-1","documentName":"Boiler Manual","knowledgeBaseId":"kb-1","chunkId":"chunk-7","sectionPath":"3.1","quoteText":"inspect the valve before startup","contentPreview":"inspect the valve before startup","context":"Operators inspect the valve before startup.","content":"FULL RAW DOCUMENT BODY MUST NOT LEAK","fullText":"FULL RAW DOCUMENT BODY MUST NOT LEAK EITHER","pageNumber":12,"score":0.91,"rerankScore":0.88,"chunkType":"paragraph","metadata":{"pageLabel":"12","objectKey":"secret","internalUrl":"http://internal/doc","vector":[0.1,0.2]}}]}}`

func TestCitationsRecognizePrefixedKnowledgeMCPSearch(t *testing.T) {
	citations := citationsFromAgentMessages("message-1", "run-1", []agent.Message{{
		Role: agent.RoleTool, Name: "knowledge__search", Content: citationToolResultContent,
	}})
	if len(citations) != 1 || citations[0].DocumentID != "doc-1" || citations[0].ChunkID != "chunk-7" {
		t.Fatalf("citations = %#v", citations)
	}
}

type documentReportToolRunner struct{}

const documentReportToolResultContent = `{"status":"accepted","reportFile":{"id":"rf-1","reportId":"rpt-1","jobId":"job-1","filename":"inspection.docx","format":"docx","fileSize":2048,"status":"succeeded","contentPath":"http://internal/file/ref"}}`

func (documentReportToolRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	return documentReportToolRunner{}.RunWithToolResultCallback(context.Background(), input, observer, nil)
}

func (documentReportToolRunner) RunWithToolResultCallback(_ context.Context, input []agent.Message, observer agent.Observer, toolObserver agent.ToolObserver) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-doc-1", ToolName: "document__export_report_docx"})
	if toolObserver != nil {
		toolObserver(agent.ToolObservation{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-doc-1", ToolName: "document__export_report_docx", Result: documentReportToolResultContent})
	}
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-doc-1", ToolName: "document__export_report_docx"})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	toolResult := agent.Message{
		Role:       agent.RoleTool,
		Name:       "document__export_report_docx",
		ToolCallID: "call-doc-1",
		Content:    documentReportToolResultContent,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "report exported"}
	messages := append([]agent.Message{}, input...)
	messages = append(messages, toolResult, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}

func (citationToolRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	toolResult := agent.Message{
		Role:       agent.RoleTool,
		Name:       "search_knowledge",
		ToolCallID: "call-1",
		Content:    citationToolResultContent,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with citation [1]"}
	messages := append([]agent.Message{}, input...)
	messages = append(messages, toolResult, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}
func (citationToolRunner) RunWithToolResultCallback(_ context.Context, input []agent.Message, observer agent.Observer, toolObserver agent.ToolObserver) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	if toolObserver != nil {
		toolObserver(agent.ToolObservation{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge", Result: citationToolResultContent})
	}
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	toolResult := agent.Message{
		Role:       agent.RoleTool,
		Name:       "search_knowledge",
		ToolCallID: "call-1",
		Content:    citationToolResultContent,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with citation [1]"}
	messages := append([]agent.Message{}, input...)
	messages = append(messages, toolResult, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}

type knowledgeMCPSearchCitationToolRunner struct{}

func (knowledgeMCPSearchCitationToolRunner) RunWithObserver(ctx context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	return knowledgeMCPSearchCitationToolRunner{}.RunWithToolResultCallback(ctx, input, observer, nil)
}
func (knowledgeMCPSearchCitationToolRunner) RunWithToolResultCallback(_ context.Context, input []agent.Message, observer agent.Observer, toolObserver agent.ToolObserver) (agent.Result, error) {
	const toolName = "knowledge__search"
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: toolName})
	if toolObserver != nil {
		toolObserver(agent.ToolObservation{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: toolName, Result: citationToolResultContent})
	}
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: toolName})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	toolResult := agent.Message{
		Role:       agent.RoleTool,
		Name:       toolName,
		ToolCallID: "call-1",
		Content:    citationToolResultContent,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with citation [1]"}
	messages := append([]agent.Message{}, input...)
	messages = append(messages, toolResult, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}

type duplicateCitationToolRunner struct{}

func (duplicateCitationToolRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	for _, toolCallID := range []string{"call-1", "call-2"} {
		observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: toolCallID, ToolName: "search_knowledge"})
		observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: toolCallID, ToolName: "search_knowledge"})
	}
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	messages := append([]agent.Message{}, input...)
	for _, toolCallID := range []string{"call-1", "call-2"} {
		messages = append(messages, agent.Message{
			Role:       agent.RoleTool,
			Name:       "search_knowledge",
			ToolCallID: toolCallID,
			Content:    citationToolResultContent,
		})
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with duplicated citation [1]"}
	messages = append(messages, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}
func (duplicateCitationToolRunner) RunWithToolResultCallback(_ context.Context, input []agent.Message, observer agent.Observer, toolObserver agent.ToolObserver) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	for _, toolCallID := range []string{"call-1", "call-2"} {
		observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: toolCallID, ToolName: "search_knowledge"})
		if toolObserver != nil {
			toolObserver(agent.ToolObservation{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: toolCallID, ToolName: "search_knowledge", Result: citationToolResultContent})
		}
		observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: toolCallID, ToolName: "search_knowledge"})
	}
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	messages := append([]agent.Message{}, input...)
	for _, toolCallID := range []string{"call-1", "call-2"} {
		messages = append(messages, agent.Message{
			Role:       agent.RoleTool,
			Name:       "search_knowledge",
			ToolCallID: toolCallID,
			Content:    citationToolResultContent,
		})
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with duplicated citation [1]"}
	messages = append(messages, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}

type fallbackCitationToolRunner struct{}

func (fallbackCitationToolRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventToolCompleted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14}})
	toolResult := agent.Message{
		Role:       agent.RoleTool,
		Name:       "search_knowledge",
		ToolCallID: "call-1",
		Content:    citationToolResultContent,
	}
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer with fallback citation [1]"}
	messages := append([]agent.Message{}, input...)
	messages = append(messages, toolResult, final)
	return agent.Result{Final: final, Messages: messages, Iterations: 1}, nil
}
func (fallbackCitationToolRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return fallbackCitationToolRunner{}.RunWithObserver(ctx, input, observer)
}

func (r *fakeAgentRunner) RunWithObserver(ctx context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	r.userID = UserIDFromContext(ctx)
	r.input = append([]agent.Message(nil), input...)
	r.retrievalSettings = contextutil.RetrievalSettingsFromContext(ctx)
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 10, CompletionTokens: 4, TotalTokens: 14}})
	final := agent.Message{Role: agent.RoleAssistant, Content: "测试回答"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}
func (r *fakeAgentRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type reasoningDeltaRunner struct {
	deltas []string
}

type answerDeltaRunner struct {
	deltas []string
	final  string
}

type streamingToolThenAnswerModel struct {
	calls int
}

func (m *streamingToolThenAnswerModel) Complete(ctx context.Context, _ []agent.Message, _ []agent.ToolDefinition) (agent.Completion, error) {
	m.calls++
	if observer := agent.AnswerDeltaObserverFromContext(ctx); observer != nil {
		if m.calls == 1 {
			observer("checking ")
		} else {
			observer("final ")
			observer("answer")
		}
	}
	if m.calls == 1 {
		return agent.Completion{Message: agent.Message{Role: agent.RoleAssistant, Content: "checking ", ToolCalls: []agent.ToolCall{{
			ID: "call-1", Type: "function", Function: agent.FunctionCall{Name: "search_knowledge", Arguments: `{"query":"x"}`},
		}}}, FinishReason: "tool_calls"}, nil
	}
	return agent.Completion{Message: agent.Message{Role: agent.RoleAssistant, Content: "final answer"}, FinishReason: "stop"}, nil
}

type streamingToolClient struct{}

func (streamingToolClient) ListTools(context.Context) ([]agent.ToolDefinition, error) {
	return []agent.ToolDefinition{{Type: "function", Function: agent.FunctionTool{
		Name:        "search_knowledge",
		Description: "search knowledge",
		Parameters:  map[string]any{"type": "object"},
	}}}, nil
}

func (streamingToolClient) CallTool(context.Context, string, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{Content: `{"results":[]}`}, nil
}

func (r answerDeltaRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	for _, delta := range r.deltas {
		observer(agent.Event{Type: agent.EventAnswerDelta, Iteration: 1, AnswerContent: delta})
	}
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}})
	final := agent.Message{Role: agent.RoleAssistant, Content: r.final}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}

func (r answerDeltaRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

func (r reasoningDeltaRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	for _, delta := range r.deltas {
		observer(agent.Event{Type: agent.EventReasoningDelta, Iteration: 1, ReasoningContent: delta})
	}
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}})
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer after reasoning"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}

func (r reasoningDeltaRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type interleavedReasoningDeltaRunner struct{}

func (interleavedReasoningDeltaRunner) RunWithObserver(_ context.Context, input []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	observer(agent.Event{Type: agent.EventReasoningDelta, Iteration: 1, ReasoningContent: strings.Repeat("a", 160)})
	observer(agent.Event{Type: agent.EventToolStarted, Iteration: 1, ToolCallID: "call-1", ToolName: "search_knowledge"})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 1, Usage: agent.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}})
	final := agent.Message{Role: agent.RoleAssistant, Content: "answer after reasoning"}
	return agent.Result{Final: final, Messages: append(input, final), Iterations: 1}, nil
}

func (r interleavedReasoningDeltaRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type errorAgentRunner struct{ err error }

func (r errorAgentRunner) RunWithObserver(_ context.Context, _ []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 1})
	return agent.Result{}, r.err
}
func (r errorAgentRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return r.RunWithObserver(ctx, input, observer)
}

type maxIterationsAgentRunner struct{}

func (maxIterationsAgentRunner) RunWithObserver(_ context.Context, _ []agent.Message, observer agent.Observer) (agent.Result, error) {
	observer(agent.Event{Type: agent.EventModelStarted, Iteration: 2})
	observer(agent.Event{Type: agent.EventModelCompleted, Iteration: 2, Usage: agent.TokenUsage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}})
	return agent.Result{Iterations: 2}, agent.ErrMaxIterations
}
func (maxIterationsAgentRunner) RunWithToolResultCallback(ctx context.Context, input []agent.Message, observer agent.Observer, _ agent.ToolObserver) (agent.Result, error) {
	return maxIterationsAgentRunner{}.RunWithObserver(ctx, input, observer)
}

type fakeRuntimeProvider struct {
	runner                  AgentRunner
	prompt                  string
	maxIterations           int
	overallTimeout          time.Duration
	retrieval               RetrievalSettings
	defaultKnowledgeBaseIDs []string
}

func (p fakeRuntimeProvider) Acquire() (RuntimeSnapshot, func(), error) {
	maxIterations := p.maxIterations
	if maxIterations == 0 {
		maxIterations = 5
	}
	return RuntimeSnapshot{
		Runner: p.runner, SystemPrompt: p.prompt, LLMModel: "deepseek-v4-pro", LLMProfileID: "default",
		QAConfigVersionID: "qa-config-id", LLMConfigVersionID: "llm-config-id",
		MaxIterations: maxIterations, OverallTimeout: p.overallTimeout,
		RetrievalSettings:       p.retrieval,
		DefaultKnowledgeBaseIDs: p.defaultKnowledgeBaseIDs,
	}, func() {}, nil
}

type fakeCitationSourceChecker struct {
	availability               map[string]bool
	failWhenContextIsCancelled bool
	userID                     string
	refs                       []CitationSourceRef
}

func (c *fakeCitationSourceChecker) CheckCitationSources(ctx context.Context, userID string, refs []CitationSourceRef) (map[string]bool, error) {
	c.userID = userID
	c.refs = append([]CitationSourceRef(nil), refs...)
	if c.failWhenContextIsCancelled {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return c.availability, nil
}

func TestAskPersistsConversationMessagesAndDisplayableSteps(t *testing.T) {
	now := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Title: "新对话", Status: "active", CreatedAt: now, UpdatedAt: now}}
	runner := &fakeAgentRunner{}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system prompt"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	var events []ProgressEvent
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "锅炉检查要求", Mode: "knowledge_qa"}, func(event ProgressEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantMessage.Content != "测试回答" || result.AssistantMessage.Status != "completed" {
		t.Fatalf("unexpected answer: %+v", result.AssistantMessage)
	}
	if repository.conversation.Title != "锅炉检查要求" {
		t.Fatalf("automatic title = %q", repository.conversation.Title)
	}
	if len(repository.messages) != 2 || repository.messages[1].Content != "测试回答" {
		t.Fatalf("unexpected persisted messages: %+v", repository.messages)
	}
	if len(repository.savedSteps) != 2 || len(events) != 6 || len(repository.savedEvents) != 6 {
		t.Fatalf("steps=%d events=%d", len(repository.savedSteps), len(events))
	}
	if result.ResponseRun.Status != "completed" || result.ResponseRun.TerminationReason == nil || *result.ResponseRun.TerminationReason != "completed" || result.ResponseRun.TotalTokens != 14 {
		t.Fatalf("unexpected response run: %+v", result.ResponseRun)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].TotalTokens != 14 {
		t.Fatalf("unexpected model invocations: %+v", repository.invocations)
	}
	if len(runner.input) < 2 || runner.input[0].Role != agent.RoleSystem || runner.input[len(runner.input)-1].Content != "锅炉检查要求" {
		t.Fatalf("unexpected agent input: %+v", runner.input)
	}
	if runner.userID != "user-id" {
		t.Fatalf("agent context userID = %q", runner.userID)
	}
}

func TestAskRejectsUnsupportedDataAnalysis(t *testing.T) {
	err := validateAskInput(AskInput{Message: "分析表格", Mode: "data_analysis"})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeUnsupportedIntent {
		t.Fatalf("error = %v, want unsupported_intent", err)
	}
}

func TestAskAddsAttachmentAndKnowledgeRAGDirective(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Title: "新对话", Status: "active"}}
	runner := &fakeAgentRunner{}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system prompt"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{
		Message:          "结合附件和知识库回答",
		Mode:             "knowledge_qa",
		AttachmentIDs:    []string{"att-1"},
		KnowledgeBaseIDs: []string{"kb-1"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.input) < 2 || runner.input[1].Role != agent.RoleSystem {
		t.Fatalf("runner messages=%+v, want request directive system message", runner.input)
	}
	directive := runner.input[1].Content
	for _, want := range []string{
		"search_session_attachments",
		"do not replace long-term knowledge-base RAG",
		"search_knowledge or knowledge__search",
		"restrict it to: kb-1",
	} {
		if !strings.Contains(directive, want) {
			t.Fatalf("directive=%q, want %q", directive, want)
		}
	}
}

func TestAskAllowsKnowledgeBaseIDsWhenDefaultKnowledgeBaseListEmpty(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Title: "新对话", Status: "active"}}
	runner := &fakeAgentRunner{}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner:                  runner,
		prompt:                  "system prompt",
		defaultKnowledgeBaseIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{
		Message:          "查询知识库",
		Mode:             "knowledge_qa",
		KnowledgeBaseIDs: []string{"kb-any"},
	}, nil); err != nil {
		t.Fatalf("Ask returned error with empty default KB list: %v", err)
	}
	if len(runner.input) < 2 || !strings.Contains(runner.input[1].Content, "restrict it to: kb-any") {
		t.Fatalf("runner directive=%+v, want requested KB restriction guidance", runner.input)
	}
}

func TestAskMergesRequestRetrievalOverridesIntoToolContext(t *testing.T) {
	var input AskInput
	if err := json.Unmarshal([]byte(`{"message":"检索","retrieval":{"topK":3,"scoreThreshold":0.42,"enableRerank":false,"rerankThreshold":0.25,"rerankTopN":2}}`), &input); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	runner := &fakeAgentRunner{}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: runner,
		prompt: "system",
		retrieval: RetrievalSettings{
			TopK:            8,
			ScoreThreshold:  0.7,
			EnableRerank:    true,
			RerankThreshold: 0.6,
			RerankTopN:      5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = qa.Ask(context.Background(), "user-id", "conversation-id", input, nil); err != nil {
		t.Fatal(err)
	}

	want := contextutil.RetrievalSettings{TopK: 3, ScoreThreshold: 0.42, ScoreThresholdConfigured: true, EnableRerank: false, RerankThreshold: 0.25, RerankTopN: 2}
	if runner.retrievalSettings != want {
		t.Fatalf("retrieval settings=%+v, want %+v", runner.retrievalSettings, want)
	}
}

func TestListConversationsNormalizesDocumentedOptions(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := qa.ListConversations(context.Background(), "user-id", ConversationListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("page=%d pageSize=%d", result.Page, result.PageSize)
	}
	if _, err = qa.ListConversations(context.Background(), "user-id", ConversationListOptions{Status: "deleted"}); err == nil {
		t.Fatal("expected invalid status to fail")
	}
	if _, err = qa.ListConversations(context.Background(), "user-id", ConversationListOptions{Sort: "title"}); err == nil {
		t.Fatal("expected invalid sort to fail")
	}
}

func TestListMessagesNormalizesDocumentedOptions(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.ListMessages(context.Background(), "user-id", "conversation-id", MessageListOptions{IncludeThinking: true, IncludeCitations: true})
	if err != nil {
		t.Fatal(err)
	}
	want := MessageListOptions{Page: 1, PageSize: 50, IncludeThinking: true, IncludeCitations: true}
	if repository.messageOptions != want {
		t.Fatalf("options=%+v want %+v", repository.messageOptions, want)
	}
	if _, err = qa.ListMessages(context.Background(), "user-id", "conversation-id", MessageListOptions{Page: 1, PageSize: 101}); err == nil {
		t.Fatal("expected invalid page size to fail")
	}
	if _, err = qa.ListMessages(context.Background(), "", "conversation-id", MessageListOptions{}); err == nil {
		t.Fatal("expected missing user to fail")
	}
}

func TestListMessagesRevalidatesEmbeddedCitationSources(t *testing.T) {
	repository := &fakeRepository{
		conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"},
		messages: []Message{{
			ID:             "assistant-message-id",
			ConversationID: "conversation-id",
			Role:           agent.RoleAssistant,
			Citations: []Citation{{
				ID:              "citation-id",
				MessageID:       "assistant-message-id",
				CitationNo:      1,
				DocumentID:      "doc-1",
				KnowledgeBaseID: "kb-1",
				Text:            "saved quote",
			}},
		}},
	}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}}
	qa.SetCitationSourceChecker(checker)

	page, err := qa.ListMessages(context.Background(), "user-id", "conversation-id", MessageListOptions{IncludeCitations: true})
	if err != nil {
		t.Fatal(err)
	}
	if checker.userID != "user-id" || !reflect.DeepEqual(checker.refs, []CitationSourceRef{{KnowledgeBaseID: "kb-1", DocumentID: "doc-1"}}) {
		t.Fatalf("source checker called with user=%q refs=%v", checker.userID, checker.refs)
	}
	citation := page.Items[0].Citations[0]
	if !citation.IsSourceAvailable || citation.Source == nil || !citation.Source.Available || citation.Source.DownloadEndpoint != "/api/v1/documents/doc-1/content?knowledgeBaseId=kb-1" {
		t.Fatalf("embedded citation source was not revalidated: %+v", citation)
	}
}

func TestSessionOperationsPropagateForbidden(t *testing.T) {
	forbidden := NewError(CodeForbidden, "conversation access denied", nil)
	repository := &fakeRepository{getConversationErr: forbidden, deleteErr: forbidden, listMessagesErr: forbidden}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}

	operations := []struct {
		name string
		call func() error
	}{
		{name: "detail", call: func() error {
			_, err := qa.GetConversation(context.Background(), "user-id", "other-session")
			return err
		}},
		{name: "update", call: func() error {
			_, err := qa.UpdateConversation(context.Background(), "user-id", "other-session", "private", "active")
			return err
		}},
		{name: "delete", call: func() error {
			return qa.DeleteConversation(context.Background(), "user-id", "other-session")
		}},
		{name: "list messages", call: func() error {
			_, err := qa.ListMessages(context.Background(), "user-id", "other-session", MessageListOptions{})
			return err
		}},
		{name: "create message", call: func() error {
			_, err := qa.Ask(context.Background(), "user-id", "other-session", AskInput{Message: "private question"}, nil)
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			appErr, ok := Classify(operation.call())
			if !ok || appErr.Code != CodeForbidden {
				t.Fatalf("error=%v, want forbidden", appErr)
			}
		})
	}
}

func TestCancelActiveRunCancelsAgentAndPersistsCancelledMessage(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	runner := blockingAgentRunner{started: make(chan struct{})}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "cancel me"}, nil)
		done <- err
	}()
	<-runner.started
	qa.CancelActiveRun("run-id")
	if err := <-done; err == nil {
		t.Fatal("expected cancelled ask to fail")
	}
	if got := repository.messages[1].Status; got != "cancelled" {
		t.Fatalf("assistant status=%q", got)
	}
	if repository.finalization.TerminationReason != "cancelled" || repository.finalization.Status != "cancelled" {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
	if len(repository.invocations) != 0 {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
	if len(repository.savedEvents) == 0 || repository.savedEvents[len(repository.savedEvents)-1].EventType != "error" {
		t.Fatalf("saved events=%+v, want replayable cancellation error event", repository.savedEvents)
	}
	if got := repository.savedEvents[len(repository.savedEvents)-1].Payload["code"]; got != string(CodeDependency) {
		t.Fatalf("cancel error code=%v, want %s", got, CodeDependency)
	}
}

func TestCancelAfterCompletedModelCallDoesNotCreateFailedInvocation(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	runner := completedThenCancelledRunner{completed: make(chan struct{})}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "cancel after completion"}, nil)
		done <- err
	}()
	<-runner.completed
	qa.CancelActiveRun("run-id")
	if err := <-done; err == nil {
		t.Fatal("expected cancelled ask to fail")
	}
	if len(repository.invocations) != 1 || repository.invocations[0].Status != "completed" {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
}

func TestAskFinalizesSuccessfulRunAfterRequestContextCancelled(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		conversation:             Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now},
		failOnCanceledFinalizing: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: cancelAfterCompletedRunner{cancel: cancel}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	result, err := qa.Ask(ctx, "user-id", "conversation-id", AskInput{Message: "disconnect after model"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseRun.Status != "completed" || result.AssistantMessage.Status != "completed" {
		t.Fatalf("result=%+v assistant=%+v", result.ResponseRun, result.AssistantMessage)
	}
	if repository.finalization.Status != "completed" || repository.finalization.TerminationReason != "completed" {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
}

func TestAskRevalidatesFinalCitationsAfterRequestContextCancelled(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		conversation:             Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now},
		failOnCanceledFinalizing: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: cancelAfterCompletedCitationRunner{cancel: cancel}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	checker := &fakeCitationSourceChecker{
		availability:               map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true},
		failWhenContextIsCancelled: true,
	}
	qa.SetCitationSourceChecker(checker)
	result, err := qa.Ask(ctx, "user-id", "conversation-id", AskInput{Message: "disconnect after model with citation"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseRun.Status != "completed" || repository.finalization.Status != "completed" {
		t.Fatalf("result=%+v finalization=%+v", result.ResponseRun, repository.finalization)
	}
	if len(repository.finalization.Citations) != 1 {
		t.Fatalf("citations=%+v", repository.finalization.Citations)
	}
	if !repository.finalization.Citations[0].IsSourceAvailable {
		t.Fatalf("citation source availability was lost after request cancellation: %+v", repository.finalization.Citations[0])
	}
	if checker.userID != "user-id" || !reflect.DeepEqual(checker.refs, []CitationSourceRef{{KnowledgeBaseID: "kb-1", DocumentID: "doc-1"}}) {
		t.Fatalf("checker user=%q refs=%v", checker.userID, checker.refs)
	}
}

func TestAskPersistsCompletedInvocationAfterRequestContextCancelled(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		conversation:             Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now},
		failOnCanceledInvocation: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: cancelBeforeCompletedObserverRunner{cancel: cancel}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	result, err := qa.Ask(ctx, "user-id", "conversation-id", AskInput{Message: "disconnect before invocation save"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResponseRun.Status != "completed" || result.AssistantMessage.Status != "completed" {
		t.Fatalf("result=%+v assistant=%+v", result.ResponseRun, result.AssistantMessage)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].Status != "completed" {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
}

func TestAskCancelsModelRunWhenRequestContextIsCancelled(t *testing.T) {
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		conversation:             Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now},
		failOnCanceledFinalizing: true,
		failOnCanceledInvocation: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancelRequestDuringModelRunner{cancel: cancel}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }

	result, err := qa.Ask(ctx, "user-id", "conversation-id", AskInput{Message: "disconnect during model"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency || appErr.Message != "answer generation cancelled" {
		t.Fatalf("error=%v, want cancellation dependency_error", err)
	}
	if !runner.sawCanceled {
		t.Fatal("model runner did not observe request cancellation")
	}
	if result.ResponseRun.Status != "cancelled" || result.AssistantMessage.Status != "cancelled" {
		t.Fatalf("result=%+v assistant=%+v", result.ResponseRun, result.AssistantMessage)
	}
	if repository.finalization.Status != "cancelled" || repository.finalization.TerminationReason != "cancelled" {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].Status != "cancelled" {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
}

func TestAskPersistsModelFailureReason(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: errorAgentRunner{err: errors.New("provider secret detail")}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "hello"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency {
		t.Fatalf("error=%v, want dependency_error", err)
	}
	if repository.finalization.Status != "failed" || repository.finalization.TerminationReason != "model_error" {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].Status != "failed" || repository.invocations[0].ErrorMessage != "answer generation failed" {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
}

func TestAskPreservesGatewayValidationErrorClassification(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	modelErr := NewError(CodeValidation, "AI gateway rejected model request", errors.New("AI gateway returned HTTP 400"))
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: errorAgentRunner{err: modelErr}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "hello"}, func(event ProgressEvent) {
		events = append(events, event)
	})
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeValidation || appErr.Message != "AI gateway rejected model request" {
		t.Fatalf("error=%v, want validation_error", err)
	}
	if repository.finalization.Status != "failed" || repository.finalization.TerminationReason != string(CodeValidation) {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].ErrorCode != string(CodeValidation) {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
	if len(repository.savedEvents) == 0 {
		t.Fatal("expected saved stream events")
	}
	last := repository.savedEvents[len(repository.savedEvents)-1]
	if last.EventType != "error" || last.Payload["code"] != string(CodeValidation) {
		t.Fatalf("last saved event=%+v, want validation error event", last)
	}
	if len(events) == 0 || events[len(events)-1].Payload["code"] != string(CodeValidation) {
		t.Fatalf("observed events=%+v, want validation error event", events)
	}
}

func TestAskReturnsPersistenceErrorWhenFailureFinalizationFails(t *testing.T) {
	repository := &fakeRepository{
		conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"},
		finalizeErr:  errors.New("database timeout"),
	}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: errorAgentRunner{err: errors.New("provider failed")}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "hello"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency || appErr.Message != "answer state persistence failed" {
		t.Fatalf("error=%v, want persistence dependency_error", err)
	}
	if result.ResponseRun.ID != "" {
		t.Fatalf("returned stale response run: %+v", result.ResponseRun)
	}
}

func TestAskKeepsCurrentRunWhenFailureFinalizationConflicts(t *testing.T) {
	cancelledAt := time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"},
		finalizeErr:  NewError(CodeConflict, "response run already finalized", nil),
		finalizeErrRun: ResponseRun{
			ID: "run-id", Status: "cancelled", CurrentIteration: 1, CompletedAt: &cancelledAt,
		},
	}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: errorAgentRunner{err: errors.New("provider failed")}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "hello"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency {
		t.Fatalf("error=%v, want dependency_error", err)
	}
	if result.ResponseRun.Status != "cancelled" {
		t.Fatalf("response run=%+v, want cancelled state", result.ResponseRun)
	}
	if len(repository.savedSteps) == 0 {
		t.Fatal("expected reasoning steps to be saved after finalization conflict")
	}
	if len(repository.savedEvents) < 3 {
		t.Fatalf("saved events=%+v, want replayable cancellation events", repository.savedEvents)
	}
	if repository.savedEvents[len(repository.savedEvents)-1].EventType != "error" {
		t.Fatalf("last saved event=%+v, want error event", repository.savedEvents[len(repository.savedEvents)-1])
	}
}

func TestAskPersistsTimeoutReason(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	runner := blockingAgentRunner{started: make(chan struct{})}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: runner, prompt: "system", overallTimeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "timeout"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency {
		t.Fatalf("error=%v, want dependency_error", err)
	}
	if repository.finalization.Status != "failed" || repository.finalization.TerminationReason != "timeout" {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
}

func TestAskPersistsMaxIterationsReason(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: maxIterationsAgentRunner{}, prompt: "system", maxIterations: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "loop"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency {
		t.Fatalf("error=%v, want dependency_error", err)
	}
	if repository.finalization.Status != "failed" || repository.finalization.TerminationReason != "max_iterations" || repository.finalization.CurrentIteration != 2 {
		t.Fatalf("finalization=%+v", repository.finalization)
	}
	if len(repository.invocations) != 1 || repository.invocations[0].Status != "completed" || repository.invocations[0].TotalTokens != 5 {
		t.Fatalf("invocations=%+v", repository.invocations)
	}
}

func TestAskToolProgressEventsExposeOnlySafeSummaries(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: toolProgressRunner{}, prompt: "system prompt with private instruction"})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "use tool"}, func(event ProgressEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	seenToolEvent := false
	for _, event := range events {
		if event.Type != "tool.started" && event.Type != "tool.completed" && event.Type != "tool.failed" {
			continue
		}
		seenToolEvent = true
		for _, forbidden := range []string{"args", "rawResult", "internalUrl", "prompt"} {
			if _, ok := event.Payload[forbidden]; ok {
				t.Fatalf("tool event leaked %q in payload %#v", forbidden, event.Payload)
			}
		}
		if event.Payload["toolCallId"] != "call-1" || event.Payload["tool"] != "search_knowledge" || event.Payload["iterationNo"] != 1 {
			t.Fatalf("unexpected safe tool payload: %#v", event.Payload)
		}
		if event.Payload["modelInvocationId"] != "invocation-1" {
			t.Fatalf("tool event missing model invocation id: %#v", event.Payload)
		}
	}
	if !seenToolEvent {
		t.Fatal("expected tool progress events")
	}
}

func TestAskDocumentReportToolProgressCarriesReportArtifact(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: documentReportToolRunner{}, prompt: "system prompt"})
	if err != nil {
		t.Fatal(err)
	}
	var events []ProgressEvent
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "export report", Mode: "report_generation"}, func(event ProgressEvent) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatal(err)
	}

	var artifact map[string]any
	for _, event := range events {
		if event.Type != "tool.completed" {
			continue
		}
		result, _ := event.Payload["result"].(map[string]any)
		artifact, _ = result["reportArtifact"].(map[string]any)
	}
	if artifact == nil {
		t.Fatalf("missing reportArtifact in events: %+v", events)
	}
	if artifact["downloadPath"] != "/api/v1/report-files/rf-1/content" || artifact["fileStatus"] != "succeeded" {
		t.Fatalf("artifact=%#v", artifact)
	}
	if len(repository.savedEvents) == 0 {
		t.Fatal("expected persisted stream events")
	}
	assertProgressPayloadsDoNotLeakSensitiveData(t, events)
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskPersistsCitationSnapshotsFromKnowledgeToolResults(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: citationToolRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	qa.SetCitationSourceChecker(&fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}})
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "find citation", Mode: "knowledge_qa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 1 || len(repository.finalization.Citations) != 1 {
		t.Fatalf("result citations=%+v finalization=%+v", result.Citations, repository.finalization.Citations)
	}
	citation := repository.finalization.Citations[0]
	if citation.CitationNo != 1 || citation.MessageID != result.AssistantMessage.ID || citation.ResponseRunID != "run-id" {
		t.Fatalf("unexpected saved citation identity: %+v", citation)
	}
	if !uuidPattern.MatchString(citation.ID) {
		t.Fatalf("citation ID must be a UUID for postgres persistence: %q", citation.ID)
	}
	if citation.DocumentID != "doc-1" || citation.DocID != "doc-1" || citation.DocumentName != "Boiler Manual" || citation.DocName != "Boiler Manual" {
		t.Fatalf("unexpected citation document fields: %+v", citation)
	}
	if citation.Source == nil || !citation.Source.Available || citation.Source.DownloadEndpoint != "/api/v1/documents/doc-1/content?knowledgeBaseId=kb-1" {
		t.Fatalf("unexpected citation source: %+v", citation.Source)
	}
	if strings.Contains(fmt.Sprintf("%#v", result.Citations), "FULL RAW DOCUMENT BODY") || strings.Contains(fmt.Sprintf("%#v", repository.savedEvents), "FULL RAW DOCUMENT BODY") {
		t.Fatalf("raw tool content leaked through Ask result or events: result=%+v events=%+v", result.Citations, repository.savedEvents)
	}
	if citation.Content != "inspect the valve before startup" || citation.ContentPreview != "inspect the valve before startup" {
		t.Fatalf("citation should expose only snapshot text, got %+v", citation)
	}
	if citation.Metadata["pageLabel"] != "12" {
		t.Fatalf("safe metadata not preserved: %#v", citation.Metadata)
	}
	for _, forbidden := range []string{"objectKey", "internalUrl", "vector"} {
		if _, ok := citation.Metadata[forbidden]; ok {
			t.Fatalf("citation metadata leaked %q: %#v", forbidden, citation.Metadata)
		}
	}
	citationSeq, citationCount, completedSeq := 0, 0, 0
	for _, event := range repository.savedEvents {
		if event.EventType == "citation.delta" {
			citationCount++
			citationSeq = event.EventSeq
		}
		if event.EventType == "answer.completed" {
			completedSeq = event.EventSeq
		}
	}
	if citationCount != 1 {
		t.Fatalf("citation.delta event count=%d events=%+v", citationCount, repository.savedEvents)
	}
	if citationSeq == 0 || completedSeq == 0 || citationSeq > completedSeq {
		t.Fatalf("citation event sequence=%d completed=%d events=%+v", citationSeq, completedSeq, repository.savedEvents)
	}
}

func TestAskStreamsCitationDeltaFromKnowledgeMCPSearchTool(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: knowledgeMCPSearchCitationToolRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	qa.SetCitationSourceChecker(&fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}})

	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "find citation", Mode: "knowledge_qa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 1 || len(repository.finalization.Citations) != 1 {
		t.Fatalf("result citations=%+v finalization=%+v", result.Citations, repository.finalization.Citations)
	}

	citationSeq, citationCount, completedSeq := 0, 0, 0
	var eventCitation Citation
	for _, event := range repository.savedEvents {
		if event.EventType == "citation.delta" {
			citationCount++
			citationSeq = event.EventSeq
			payload, ok := event.Payload["citation"].(Citation)
			if !ok {
				t.Fatalf("citation.delta payload=%#v, want Citation", event.Payload["citation"])
			}
			eventCitation = payload
		}
		if event.EventType == "answer.completed" {
			completedSeq = event.EventSeq
		}
	}
	if citationCount != 1 {
		t.Fatalf("citation.delta event count=%d events=%+v", citationCount, repository.savedEvents)
	}
	if citationSeq == 0 || completedSeq == 0 || citationSeq > completedSeq {
		t.Fatalf("citation event sequence=%d completed=%d events=%+v", citationSeq, completedSeq, repository.savedEvents)
	}
	if eventCitation.ID != repository.finalization.Citations[0].ID || eventCitation.DocumentID != "doc-1" || eventCitation.MessageID != result.AssistantMessage.ID {
		t.Fatalf("citation event=%+v finalization=%+v", eventCitation, repository.finalization.Citations[0])
	}
}

func TestAskDeduplicatesStreamingCitationSnapshots(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: duplicateCitationToolRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	qa.SetCitationSourceChecker(&fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}})

	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "find repeated citation", Mode: "knowledge_qa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 1 || len(repository.finalization.Citations) != 1 {
		t.Fatalf("result citations=%+v finalization=%+v", result.Citations, repository.finalization.Citations)
	}
	citation := repository.finalization.Citations[0]
	if citation.CitationNo != 1 || citation.DocumentID != "doc-1" || citation.ChunkID != "chunk-7" {
		t.Fatalf("unexpected deduplicated citation: %+v", citation)
	}

	citationCount := 0
	var eventCitation Citation
	for _, event := range repository.savedEvents {
		if event.EventType != "citation.delta" {
			continue
		}
		citationCount++
		payload, ok := event.Payload["citation"].(Citation)
		if !ok {
			t.Fatalf("citation.delta payload=%#v, want Citation", event.Payload["citation"])
		}
		eventCitation = payload
	}
	if citationCount != 1 {
		t.Fatalf("citation.delta event count=%d events=%+v", citationCount, repository.savedEvents)
	}
	if eventCitation.ID != citation.ID || eventCitation.CitationNo != 1 || eventCitation.ChunkID != "chunk-7" {
		t.Fatalf("citation event=%+v finalization=%+v", eventCitation, citation)
	}
}

func TestAskSSEPayloadsDoNotLeakPromptRawToolOrProviderSecrets(t *testing.T) {
	t.Run("tool and citation events", func(t *testing.T) {
		repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
		qa, err := NewQAService(repository, fakeRuntimeProvider{
			runner: citationToolRunner{},
			prompt: "system prompt with PRIVATE_CHAIN_OF_THOUGHT and raw MCP arguments",
		})
		if err != nil {
			t.Fatal(err)
		}
		qa.SetCitationSourceChecker(&fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}})
		var observed []ProgressEvent
		_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "find citation", Mode: "knowledge_qa"}, func(event ProgressEvent) {
			observed = append(observed, event)
		})
		if err != nil {
			t.Fatal(err)
		}

		assertSSEEventTypesSeen(t, observed, "message.created", "agent.iteration.started", "reasoning.step", "tool.started", "tool.completed", "answer.delta", "citation.delta", "answer.completed")
		assertProgressPayloadsDoNotLeakSensitiveData(t, observed)
		assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
	})

	t.Run("provider error event", func(t *testing.T) {
		repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
		qa, err := NewQAService(repository, fakeRuntimeProvider{
			runner: errorAgentRunner{err: errors.New("provider raw error: token=secret http://internal.provider/v1 prompt leaked")},
			prompt: "system prompt with PRIVATE_CHAIN_OF_THOUGHT",
		})
		if err != nil {
			t.Fatal(err)
		}
		var observed []ProgressEvent
		_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "fail safely"}, func(event ProgressEvent) {
			observed = append(observed, event)
		})
		appErr, ok := Classify(err)
		if !ok || appErr.Code != CodeDependency {
			t.Fatalf("error=%v, want dependency_error", err)
		}

		assertSSEEventTypesSeen(t, observed, "message.created", "agent.iteration.started", "reasoning.step", "error")
		assertProgressPayloadsDoNotLeakSensitiveData(t, observed)
		assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
	})
}

func TestAskEmitsReasoningDeltaBeforeAnswerDelta(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: reasoningDeltaRunner{deltas: []string{"checked source A", "compared public result B"}},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []ProgressEvent
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, func(event ProgressEvent) {
		observed = append(observed, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSSEEventTypesSeen(t, observed, "reasoning.delta", "answer.delta", "answer.completed")
	reasoningSeq, answerSeq, reasoningCount := 0, 0, 0
	for _, event := range repository.savedEvents {
		switch event.EventType {
		case "reasoning.delta":
			reasoningCount++
			if reasoningSeq == 0 {
				reasoningSeq = event.EventSeq
			}
			if event.Payload["messageId"] == "" || event.Payload["text"] == "" {
				t.Fatalf("invalid reasoning payload: %+v", event.Payload)
			}
		case "answer.delta":
			answerSeq = event.EventSeq
		}
	}
	if reasoningCount == 0 {
		t.Fatalf("reasoning.delta count=%d events=%+v", reasoningCount, repository.savedEvents)
	}
	if reasoningSeq == 0 || answerSeq == 0 || reasoningSeq > answerSeq {
		t.Fatalf("reasoning sequence=%d answer sequence=%d events=%+v", reasoningSeq, answerSeq, repository.savedEvents)
	}
	assertProgressPayloadsDoNotLeakSensitiveData(t, observed)
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskStreamsAnswerDeltasFromAgentEvents(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: answerDeltaRunner{deltas: []string{"first ", "second"}, final: "first second"},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed []ProgressEvent
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "stream answer"}, func(event ProgressEvent) {
		observed = append(observed, event)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantMessage.Content != "first second" || repository.finalization.AssistantMessage.Content != "first second" {
		t.Fatalf("result=%+v finalization=%+v", result.AssistantMessage, repository.finalization.AssistantMessage)
	}
	var answerTexts []string
	var answerIndexes []int
	answerSeq, completedSeq := 0, 0
	for _, event := range repository.savedEvents {
		switch event.EventType {
		case "answer.delta":
			answerSeq = event.EventSeq
			answerTexts = append(answerTexts, fmt.Sprint(event.Payload["text"]))
			index, ok := event.Payload["index"].(int)
			if !ok {
				t.Fatalf("answer.delta index payload=%#v", event.Payload["index"])
			}
			answerIndexes = append(answerIndexes, index)
		case "answer.completed":
			completedSeq = event.EventSeq
		}
	}
	if len(answerTexts) != 2 || strings.Join(answerTexts, "") != "first second" {
		t.Fatalf("answer texts=%q events=%+v", answerTexts, repository.savedEvents)
	}
	if answerIndexes[0] != 0 || answerIndexes[1] != 1 {
		t.Fatalf("answer indexes=%v events=%+v", answerIndexes, repository.savedEvents)
	}
	if answerSeq == 0 || completedSeq == 0 || answerSeq > completedSeq {
		t.Fatalf("answer seq=%d completed seq=%d events=%+v", answerSeq, completedSeq, repository.savedEvents)
	}
	assertSSEEventTypesSeen(t, observed, "answer.delta", "answer.completed")
	assertProgressPayloadsDoNotLeakSensitiveData(t, observed)
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskFailsMismatchedAnswerDeltas(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: answerDeltaRunner{deltas: []string{"intermediate"}, final: "final answer"},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "stream mismatched answer"}, nil)
	appErr, ok := Classify(err)
	if !ok || appErr.Code != CodeDependency || appErr.Message != "answer stream did not match final answer" {
		t.Fatalf("error=%v, want answer stream mismatch dependency error", err)
	}
	if result.ResponseRun.Status != "failed" || repository.finalization.Status != "failed" {
		t.Fatalf("result=%+v finalization=%+v", result.ResponseRun, repository.finalization)
	}
	for _, event := range repository.savedEvents {
		if event.EventType == "answer.completed" {
			t.Fatalf("answer.completed emitted after answer stream mismatch: %+v", repository.savedEvents)
		}
	}
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskDoesNotReplayToolTurnAnswerDeltas(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	runner, err := agent.NewRunner(&streamingToolThenAnswerModel{}, streamingToolClient{}, agent.Config{
		MaxIterations:      3,
		ToolTimeout:        time.Second,
		MaxToolResultBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: runner,
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "stream answer with tool"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.AssistantMessage.Content != "final answer" || repository.finalization.AssistantMessage.Content != "final answer" {
		t.Fatalf("result=%+v finalization=%+v", result.AssistantMessage, repository.finalization.AssistantMessage)
	}
	var answerTexts []string
	var sawCompleted bool
	for _, event := range repository.savedEvents {
		switch event.EventType {
		case "answer.delta":
			answerTexts = append(answerTexts, fmt.Sprint(event.Payload["text"]))
		case "answer.completed":
			sawCompleted = true
		}
	}
	joined := strings.Join(answerTexts, "")
	if joined != "final answer" {
		t.Fatalf("answer texts=%q events=%+v", answerTexts, repository.savedEvents)
	}
	if !sawCompleted {
		t.Fatalf("missing answer.completed after final answer: %+v", repository.savedEvents)
	}
	if strings.Contains(joined, "checking") {
		t.Fatalf("tool-call turn content leaked into answer deltas: %q", answerTexts)
	}
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskDropsUnsafeReasoningDelta(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: reasoningDeltaRunner{deltas: []string{
			"safe visible reasoning",
			"system prompt PRIVATE_CHAIN_OF_THOUGHT token=secret http://internal.provider/v1",
			"tool arguments raw MCP result should stay private",
		}},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reasoningTexts []string
	for _, event := range repository.savedEvents {
		if event.EventType == "reasoning.delta" {
			reasoningTexts = append(reasoningTexts, fmt.Sprint(event.Payload["text"]))
		}
	}
	if len(reasoningTexts) != 0 {
		t.Fatalf("unsafe reasoning deltas were not filtered: %v events=%+v", reasoningTexts, repository.savedEvents)
	}
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskPreservesReasoningDeltaWhitespace(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: reasoningDeltaRunner{deltas: []string{"plan", " ", "next\n"}},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reasoningTexts []string
	for _, event := range repository.savedEvents {
		if event.EventType == "reasoning.delta" {
			reasoningTexts = append(reasoningTexts, fmt.Sprint(event.Payload["text"]))
		}
	}
	if strings.Join(reasoningTexts, "") != "plan next\n" {
		t.Fatalf("reasoning texts=%q events=%+v", reasoningTexts, repository.savedEvents)
	}
}

func TestAskDropsReasoningDeltaSplitUnsafeMarker(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: reasoningDeltaRunner{deltas: []string{"safe prefix", " s", "k-secret", "safe suffix"}},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var reasoningTexts []string
	for _, event := range repository.savedEvents {
		if event.EventType == "reasoning.delta" {
			reasoningTexts = append(reasoningTexts, fmt.Sprint(event.Payload["text"]))
		}
	}
	if len(reasoningTexts) != 0 {
		t.Fatalf("split unsafe marker leaked: %q events=%+v", reasoningTexts, repository.savedEvents)
	}
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskDropsReasoningDeltaLongPrefixBeforeUnsafeMarker(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: reasoningDeltaRunner{deltas: []string{
			strings.Repeat("safe looking prefix ", 20),
			"system prompt should block the whole reasoning block",
		}},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range repository.savedEvents {
		if event.EventType == "reasoning.delta" {
			t.Fatalf("long unsafe reasoning prefix leaked: %+v", repository.savedEvents)
		}
	}
	assertStreamPayloadsDoNotLeakSensitiveData(t, repository.savedEvents)
}

func TestAskBuffersReasoningDeltaUntilModelCompleted(t *testing.T) {
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active"}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{
		runner: interleavedReasoningDeltaRunner{},
		prompt: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "show reasoning"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reasoningSeq, toolSeq := 0, 0
	for _, event := range repository.savedEvents {
		switch event.EventType {
		case "reasoning.delta":
			if reasoningSeq == 0 {
				reasoningSeq = event.EventSeq
			}
		case "tool.started":
			if toolSeq == 0 {
				toolSeq = event.EventSeq
			}
		}
	}
	if reasoningSeq == 0 || toolSeq == 0 || reasoningSeq <= toolSeq {
		t.Fatalf("reasoning sequence=%d tool sequence=%d events=%+v", reasoningSeq, toolSeq, repository.savedEvents)
	}
}

func TestContainsUnsafeReasoningContentRejectsInternalConnectionReferences(t *testing.T) {
	tests := []string{
		"postgres://user:pass@localhost:5432/app",
		"redis://redis:6379/0",
		"http://knowledge-vendor:9380/api/v1/system/healthz",
		"http://ai-gateway:8086/internal/v1/chat/completions",
		"http://internal.provider/v1",
		"http://qa.default.svc.cluster.local/v1",
		"http://postgres.service.consul:5432",
		"http://minio.local:9000",
		"localhost:9000",
		"elasticsearch:9200",
		"postgres.service.consul:5432",
		"DATABASE_URL=postgres://postgres:postgres@localhost:5432/app",
		"connection_string=host=knowledge-vendor port=9380",
		"host=ai-gateway port=8086",
		"provider raw error: upstream failed",
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if !containsUnsafeReasoningContent(tt) {
				t.Fatalf("containsUnsafeReasoningContent(%q)=false, want true", tt)
			}
		})
	}
}

func TestContainsUnsafeReasoningContentAllowsPublicReferenceText(t *testing.T) {
	value := "checked the public docs at https://example.com/reference and compared the summary"
	if containsUnsafeReasoningContent(value) {
		t.Fatalf("containsUnsafeReasoningContent(%q)=true, want false", value)
	}
}

func TestAskEmitsCitationDeltaForFallbackAgentMessageCitations(t *testing.T) {
	now := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	repository := &fakeRepository{conversation: Conversation{ID: "conversation-id", OwnerUserID: "user-id", Status: "active", CreatedAt: now, UpdatedAt: now}}
	qa, err := NewQAService(repository, fakeRuntimeProvider{runner: fallbackCitationToolRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	qa.now = func() time.Time { return now }
	qa.SetCitationSourceChecker(&fakeCitationSourceChecker{availability: map[string]bool{CitationSourceRefKey("kb-1", "doc-1"): true}})

	result, err := qa.Ask(context.Background(), "user-id", "conversation-id", AskInput{Message: "find citation", Mode: "knowledge_qa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Citations) != 1 || len(repository.finalization.Citations) != 1 {
		t.Fatalf("result citations=%+v finalization=%+v", result.Citations, repository.finalization.Citations)
	}

	citationSeq, citationCount, completedSeq := 0, 0, 0
	var eventCitation Citation
	for _, event := range repository.savedEvents {
		if event.EventType == "citation.delta" {
			citationCount++
			citationSeq = event.EventSeq
			payload, ok := event.Payload["citation"].(Citation)
			if !ok {
				t.Fatalf("citation.delta payload=%#v, want Citation", event.Payload["citation"])
			}
			eventCitation = payload
		}
		if event.EventType == "answer.completed" {
			completedSeq = event.EventSeq
		}
	}
	if citationCount != 1 {
		t.Fatalf("citation.delta event count=%d events=%+v", citationCount, repository.savedEvents)
	}
	if citationSeq == 0 || completedSeq == 0 || citationSeq > completedSeq {
		t.Fatalf("citation event sequence=%d completed=%d events=%+v", citationSeq, completedSeq, repository.savedEvents)
	}
	if eventCitation.ID != repository.finalization.Citations[0].ID || eventCitation.DocumentID != "doc-1" || eventCitation.MessageID != result.AssistantMessage.ID {
		t.Fatalf("citation event=%+v finalization=%+v", eventCitation, repository.finalization.Citations[0])
	}
}

func TestExtractCitationsFromToolResultSupportsKnowledgeSummaryFields(t *testing.T) {
	result := `{"results":[{"citation_no":3,"knowledge_base_id":"kb-1","document_id":"doc-1","document_name":"Doc","chunk_id":"chunk-1","preview":"safe preview","context":"safe context","page_number":7,"score":0.5,"rerank_score":0.4,"chunk_type":"paragraph"}]}`
	citations := extractCitationsFromToolResult(result, 3)

	if len(citations) != 1 {
		t.Fatalf("citations=%+v", citations)
	}
	citation := citations[0]
	if citation.CitationNo != 3 || citation.KnowledgeBaseID != "kb-1" || citation.DocumentID != "doc-1" || citation.DocumentName != "Doc" {
		t.Fatalf("unexpected citation=%+v", citation)
	}
	if citation.Text != "" || citation.ContentPreview != "safe preview" || citation.Context != "safe context" {
		t.Fatalf("unexpected citation text fields=%+v", citation)
	}
}

func TestKnowledgeRetrievalStopPolicySuppressesKnowledgeToolsAfterCitationHit(t *testing.T) {
	policy := NewKnowledgeRetrievalStopPolicy("knowledge")
	decision := policy(agent.ToolObservation{
		Type:     agent.EventToolCompleted,
		ToolName: "knowledge__search",
		Result:   citationToolResultContent,
	})

	if decision.AppendSystemMessage == "" {
		t.Fatal("expected final-answer directive")
	}
	if !containsStringInSlice(decision.SuppressToolPrefixes, "knowledge__") {
		t.Fatalf("suppressed prefixes=%v, want knowledge__", decision.SuppressToolPrefixes)
	}
	if !containsStringInSlice(decision.SuppressToolNames, "search_knowledge") {
		t.Fatalf("suppressed names=%v, want search_knowledge", decision.SuppressToolNames)
	}
}

func TestKnowledgeRetrievalStopPolicyIgnoresEmptyKnowledgeResults(t *testing.T) {
	decision := KnowledgeRetrievalStopPolicy(agent.ToolObservation{
		Type:     agent.EventToolCompleted,
		ToolName: "knowledge__search",
		Result:   `{"results":[]}`,
	})

	if decision.AppendSystemMessage != "" || len(decision.SuppressToolNames) != 0 || len(decision.SuppressToolPrefixes) != 0 {
		t.Fatalf("unexpected policy decision for empty result: %+v", decision)
	}
}

func TestNormalizeCitationMarksUnavailableSourceAndSanitizesMetadata(t *testing.T) {
	citation := NormalizeCitation(Citation{
		ID:           "citation-id",
		MessageID:    "message-id",
		CitationNo:   1,
		DocumentName: "Deleted source",
		Text:         "saved quote",
		Context:      "saved context",
		Metadata: map[string]any{
			"pageLabel": "8",
			"objectKey": "secret/object",
			"nested": map[string]any{
				"internalUrl": "http://internal/source",
				"safe":        "ok",
			},
		},
	})
	if citation.IsSourceAvailable || citation.Source == nil || citation.Source.Available || citation.Source.Reason != citationSourceUnavailableReason {
		t.Fatalf("unexpected unavailable source mapping: %+v", citation)
	}
	if citation.Content != "saved quote" || citation.ContentPreview != "saved quote" {
		t.Fatalf("snapshot text was not preserved: %+v", citation)
	}
	if _, ok := citation.Metadata["objectKey"]; ok {
		t.Fatalf("object key leaked in metadata: %#v", citation.Metadata)
	}
	nested, ok := citation.Metadata["nested"].(map[string]any)
	if !ok || nested["safe"] != "ok" {
		t.Fatalf("safe nested metadata not preserved: %#v", citation.Metadata)
	}
	if _, ok := nested["internalUrl"]; ok {
		t.Fatalf("internal URL leaked in nested metadata: %#v", nested)
	}
}

func containsStringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertSSEEventTypesSeen(t *testing.T, events []ProgressEvent, expected ...string) {
	t.Helper()
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, eventType := range expected {
		if !seen[eventType] {
			t.Fatalf("missing SSE event type %q; seen=%v", eventType, seen)
		}
	}
}

func assertProgressPayloadsDoNotLeakSensitiveData(t *testing.T, events []ProgressEvent) {
	t.Helper()
	for _, event := range events {
		assertPayloadDoesNotLeakSensitiveData(t, event.Type, event.Payload)
	}
}

func assertStreamPayloadsDoNotLeakSensitiveData(t *testing.T, events []StreamEvent) {
	t.Helper()
	for _, event := range events {
		assertPayloadDoesNotLeakSensitiveData(t, event.EventType, event.Payload)
	}
}

func TestListMessagesReturnsAttachmentIDs(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		conversation: Conversation{ID: "sess-1", OwnerUserID: "user-1", Status: "active", CreatedAt: now, UpdatedAt: now},
		messages: []Message{
			{ID: "msg-1", ConversationID: "sess-1", Role: "user", Content: "question", Status: "completed", CreatedAt: now, AttachmentIDs: []string{"att-1", "att-2"}},
		},
	}
	qa, err := NewQAService(repo, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := qa.ListMessages(context.Background(), "user-1", "sess-1", MessageListOptions{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(page.Items))
	}
	msg := page.Items[0]
	if len(msg.AttachmentIDs) != 2 || msg.AttachmentIDs[0] != "att-1" || msg.AttachmentIDs[1] != "att-2" {
		t.Fatalf("AttachmentIDs = %v, want [att-1 att-2]", msg.AttachmentIDs)
	}
}

func TestMessageAttachmentIDsOmittedWhenEmpty(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{
		conversation: Conversation{ID: "sess-1", OwnerUserID: "user-1", Status: "active", CreatedAt: now, UpdatedAt: now},
		messages: []Message{
			{ID: "msg-1", ConversationID: "sess-1", Role: "user", Content: "no attachments", Status: "completed", CreatedAt: now},
		},
	}
	qa, err := NewQAService(repo, fakeRuntimeProvider{runner: &fakeAgentRunner{}, prompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := qa.ListMessages(context.Background(), "user-1", "sess-1", MessageListOptions{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items[0].AttachmentIDs) != 0 {
		t.Fatalf("AttachmentIDs = %v, want empty", page.Items[0].AttachmentIDs)
	}
}

func assertPayloadDoesNotLeakSensitiveData(t *testing.T, eventType string, payload map[string]any) {
	t.Helper()
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("%s payload is not JSON encodable: %v", eventType, err)
	}
	payloadText := strings.ToLower(string(payloadJSON))
	for _, marker := range []string{
		"private_chain_of_thought",
		"raw mcp arguments",
		"full raw document body",
		"rawresult",
		"objectkey",
		"internalurl",
		"http://internal",
		"provider raw error",
		"token=secret",
		"prompt leaked",
	} {
		if strings.Contains(payloadText, strings.ToLower(marker)) {
			t.Fatalf("%s payload leaked sensitive marker %q: %s", eventType, marker, payloadJSON)
		}
	}
	assertPayloadKeysDoNotLeakSensitiveData(t, eventType, payload)
}

func assertPayloadKeysDoNotLeakSensitiveData(t *testing.T, eventType string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalizedKey := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(key))
			for _, marker := range []string{"chainofthought", "rawresult", "objectkey", "internalurl", "providerrawerror", "prompttokens"} {
				if strings.Contains(normalizedKey, marker) {
					t.Fatalf("%s payload leaked sensitive key %q in %#v", eventType, key, value)
				}
			}
			assertPayloadKeysDoNotLeakSensitiveData(t, eventType, nested)
		}
	case []any:
		for _, nested := range typed {
			assertPayloadKeysDoNotLeakSensitiveData(t, eventType, nested)
		}
	}
}

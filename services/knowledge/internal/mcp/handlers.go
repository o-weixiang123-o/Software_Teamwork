package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/aigateway"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/service"
)

const defaultRAGSystemPrompt = "You are a helpful assistant. Answer the user's question using only the provided context. Cite sources using [n] notation matching the context chunk numbers. If the context does not contain enough information, say so clearly."

type toolHandlers struct {
	bridge *Bridge
	caller CallerContext
	chat   *aigateway.ChatClient
}

func (h *toolHandlers) effectiveCaller() CallerContext {
	caller := h.caller
	if strings.TrimSpace(caller.RequestID) == "" {
		caller.RequestID = newRequestID()
	}
	return caller
}

func (h *toolHandlers) requireWriteCaller() (CallerContext, error) {
	caller := h.effectiveCaller()
	for _, permission := range strings.Split(caller.Permissions, ",") {
		if strings.TrimSpace(permission) == service.PermissionKnowledgeWrite {
			return caller, nil
		}
	}
	return CallerContext{}, fmt.Errorf("knowledge:write permission is required")
}

func (h *toolHandlers) chatRequestContext(caller CallerContext) aigateway.RequestContext {
	var roles, perms []string
	if caller.Roles != "" {
		roles = strings.Split(caller.Roles, ",")
	}
	if caller.Permissions != "" {
		perms = strings.Split(caller.Permissions, ",")
	}
	return aigateway.RequestContext{
		RequestID:   caller.RequestID,
		UserID:      caller.UserID,
		Roles:       roles,
		Permissions: perms,
	}
}

func (h *toolHandlers) searchKnowledge(ctx context.Context, _ *sdkmcp.CallToolRequest, input searchKnowledgeInput) (*sdkmcp.CallToolResult, searchKnowledgeOutput, error) {
	output, err := h.runKnowledgeSearch(ctx, h.effectiveCaller(), knowledgeSearchParams{
		Query:            input.Query,
		KnowledgeBaseIDs: input.KnowledgeBaseIDs,
		DocumentIDs:      input.DocumentIDs,
		TopK:             input.TopK,
		ScoreThreshold:   input.ScoreThreshold,
		Rerank:           input.Rerank,
		RerankTopN:       input.RerankTopN,
		Tags:             input.Tags,
		MetadataFilter:   input.MetadataFilter,
	})
	if err != nil {
		return nil, searchKnowledgeOutput{}, err
	}
	return nil, output, nil
}

type knowledgeSearchParams struct {
	Query            string
	KnowledgeBaseIDs []string
	DocumentIDs      []string
	TopK             int
	ScoreThreshold   *float64
	Rerank           bool
	RerankTopN       *int
	Tags             []string
	MetadataFilter   map[string]string
}

func (h *toolHandlers) runKnowledgeSearch(ctx context.Context, caller CallerContext, params knowledgeSearchParams) (searchKnowledgeOutput, error) {
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return searchKnowledgeOutput{}, fmt.Errorf("query is required")
	}

	payload := map[string]any{
		"query": query,
	}
	if len(params.KnowledgeBaseIDs) > 0 {
		payload["knowledgeBaseIds"] = params.KnowledgeBaseIDs
	}
	if params.TopK > 0 {
		payload["topK"] = params.TopK
	}
	if params.ScoreThreshold != nil {
		payload["scoreThreshold"] = *params.ScoreThreshold
	}
	if params.Rerank {
		payload["rerank"] = true
	}
	if params.RerankTopN != nil {
		payload["rerankTopN"] = *params.RerankTopN
	}
	if len(params.Tags) > 0 {
		payload["tags"] = params.Tags
	}
	if len(params.MetadataFilter) > 0 {
		payload["metadataFilter"] = params.MetadataFilter
	}
	if len(params.DocumentIDs) > 0 {
		payload["documentIds"] = params.DocumentIDs
	}

	status, respBody, _, err := h.bridge.DoJSON(ctx, caller, http.MethodPost, "/internal/v1/knowledge-queries", payload)
	if err != nil {
		return searchKnowledgeOutput{}, err
	}
	if status != http.StatusCreated {
		return searchKnowledgeOutput{}, adapterErrorMessage(status, respBody)
	}

	data, err := decodeAdapterSuccess(respBody)
	if err != nil {
		return searchKnowledgeOutput{}, err
	}

	var summary struct {
		ID      string `json:"id"`
		Results []struct {
			Score           float64  `json:"score"`
			KnowledgeBaseID string   `json:"knowledgeBaseId"`
			DocumentID      string   `json:"documentId"`
			ChunkID         string   `json:"chunkId"`
			DocumentName    string   `json:"documentName"`
			ContentPreview  string   `json:"contentPreview"`
			SectionPath     *string  `json:"sectionPath,omitempty"`
			ChunkIndex      *int     `json:"chunkIndex,omitempty"`
			ChunkType       *string  `json:"chunkType,omitempty"`
			Tags            []string `json:"tags,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return searchKnowledgeOutput{}, fmt.Errorf("decode knowledge query summary: %w", err)
	}

	results := make([]searchKnowledgeResult, 0, len(summary.Results))
	for _, item := range summary.Results {
		results = append(results, searchKnowledgeResult{
			Score:           item.Score,
			KnowledgeBaseID: item.KnowledgeBaseID,
			DocumentID:      item.DocumentID,
			ChunkID:         item.ChunkID,
			DocumentName:    item.DocumentName,
			ContentPreview:  item.ContentPreview,
			Content:         item.ContentPreview,
			SectionPath:     item.SectionPath,
			ChunkIndex:      item.ChunkIndex,
			ChunkType:       item.ChunkType,
			Tags:            item.Tags,
		})
	}

	return searchKnowledgeOutput{
		QueryID: summary.ID,
		Results: results,
	}, nil
}

func (h *toolHandlers) answerFromKnowledge(ctx context.Context, _ *sdkmcp.CallToolRequest, input answerFromKnowledgeInput) (*sdkmcp.CallToolResult, answerFromKnowledgeOutput, error) {
	if h.chat == nil {
		return nil, answerFromKnowledgeOutput{}, fmt.Errorf("answer_from_knowledge requires KNOWLEDGE_AI_GATEWAY_URL to be configured")
	}

	question := strings.TrimSpace(input.Question)
	if question == "" {
		return nil, answerFromKnowledgeOutput{}, fmt.Errorf("question is required")
	}
	profileID := strings.TrimSpace(input.ModelProfileID)
	if profileID == "" {
		return nil, answerFromKnowledgeOutput{}, fmt.Errorf("modelProfileId is required")
	}

	caller := h.effectiveCaller()
	retrieval, err := h.runKnowledgeSearch(ctx, caller, knowledgeSearchParams{
		Query:            question,
		KnowledgeBaseIDs: input.KnowledgeBaseIDs,
		DocumentIDs:      input.DocumentIDs,
		TopK:             input.TopK,
		ScoreThreshold:   input.ScoreThreshold,
	})
	if err != nil {
		return nil, answerFromKnowledgeOutput{}, err
	}

	citations := make([]answerCitation, 0, len(retrieval.Results))
	var contextBuilder strings.Builder
	for i, result := range retrieval.Results {
		index := i + 1
		excerpt := result.ContentPreview
		if excerpt == "" {
			excerpt = result.Content
		}
		citations = append(citations, answerCitation{
			Index:           index,
			KnowledgeBaseID: result.KnowledgeBaseID,
			DocumentID:      result.DocumentID,
			ChunkID:         result.ChunkID,
			Excerpt:         excerpt,
		})
		if contextBuilder.Len() > 0 {
			contextBuilder.WriteString("\n\n")
		}
		fmt.Fprintf(&contextBuilder, "[%d] (%s) %s", index, result.DocumentName, excerpt)
	}

	systemPrompt := defaultRAGSystemPrompt
	if input.SystemPrompt != nil && strings.TrimSpace(*input.SystemPrompt) != "" {
		systemPrompt = strings.TrimSpace(*input.SystemPrompt)
	}

	userPrompt := question
	if contextBuilder.Len() > 0 {
		userPrompt = fmt.Sprintf("Context:\n%s\n\nQuestion: %s", contextBuilder.String(), question)
	} else {
		userPrompt = fmt.Sprintf("No relevant context was retrieved.\n\nQuestion: %s", question)
	}

	chatResp, err := h.chat.CreateChatCompletion(ctx, h.chatRequestContext(caller), aigateway.ChatRequest{
		ProfileID: profileID,
		Model:     profileID,
		MaxTokens: input.MaxTokens,
		Messages: []aigateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	if err != nil {
		return nil, answerFromKnowledgeOutput{}, fmt.Errorf("ai gateway chat failed: %w", err)
	}

	return nil, answerFromKnowledgeOutput{
		Answer:    chatResp.Content,
		Citations: citations,
		Retrieval: answerRetrievalSummary{
			QueryID:     retrieval.QueryID,
			ResultCount: len(retrieval.Results),
		},
	}, nil
}

func (h *toolHandlers) listKnowledgeBases(ctx context.Context, _ *sdkmcp.CallToolRequest, input listKnowledgeBasesInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", fmt.Sprintf("%d", input.Page))
	}
	if input.PageSize > 0 {
		query.Set("pageSize", fmt.Sprintf("%d", input.PageSize))
	}
	return h.adapterList(ctx, h.effectiveCaller(), "/internal/v1/knowledge-bases", query)
}

func (h *toolHandlers) getKnowledgeBase(ctx context.Context, _ *sdkmcp.CallToolRequest, input getKnowledgeBaseInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	kbID := strings.TrimSpace(input.KnowledgeBaseID)
	if kbID == "" {
		return nil, nil, fmt.Errorf("knowledgeBaseId is required")
	}
	return h.adapterGet(ctx, h.effectiveCaller(), "/internal/v1/knowledge-bases/"+url.PathEscape(kbID))
}

func (h *toolHandlers) createKnowledgeBase(ctx context.Context, _ *sdkmcp.CallToolRequest, input createKnowledgeBaseInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	payload := map[string]any{"name": input.Name}
	if input.ID != nil && strings.TrimSpace(*input.ID) != "" {
		payload["id"] = strings.TrimSpace(*input.ID)
	}
	if input.Description != nil {
		payload["description"] = *input.Description
	}
	if input.DocType != nil {
		payload["docType"] = *input.DocType
	}
	if len(input.ChunkStrategy) > 0 {
		payload["chunkStrategy"] = input.ChunkStrategy
	}
	if len(input.RetrievalStrategy) > 0 {
		payload["retrievalStrategy"] = input.RetrievalStrategy
	}
	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, nil, err
	}
	return h.adapterCreate(ctx, caller, "/internal/v1/knowledge-bases", payload)
}

func (h *toolHandlers) updateKnowledgeBase(ctx context.Context, _ *sdkmcp.CallToolRequest, input updateKnowledgeBaseInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	kbID := strings.TrimSpace(input.KnowledgeBaseID)
	if kbID == "" {
		return nil, nil, fmt.Errorf("knowledgeBaseId is required")
	}
	payload := map[string]any{}
	if input.Name != nil {
		payload["name"] = *input.Name
	}
	if input.Description != nil {
		payload["description"] = *input.Description
	}
	if input.DocType != nil {
		payload["docType"] = *input.DocType
	}
	if len(input.ChunkStrategy) > 0 {
		payload["chunkStrategy"] = input.ChunkStrategy
	}
	if len(input.RetrievalStrategy) > 0 {
		payload["retrievalStrategy"] = input.RetrievalStrategy
	}
	if len(payload) == 0 {
		return nil, nil, fmt.Errorf("at least one field must be provided for update")
	}
	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, nil, err
	}
	return h.adapterUpdate(ctx, caller, "/internal/v1/knowledge-bases/"+url.PathEscape(kbID), payload)
}

func (h *toolHandlers) deleteKnowledgeBase(ctx context.Context, _ *sdkmcp.CallToolRequest, input deleteKnowledgeBaseInput) (*sdkmcp.CallToolResult, deleteResourceOutput, error) {
	kbID := strings.TrimSpace(input.KnowledgeBaseID)
	if kbID == "" {
		return nil, deleteResourceOutput{}, fmt.Errorf("knowledgeBaseId is required")
	}
	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, deleteResourceOutput{}, err
	}
	if err := h.adapterDelete(ctx, caller, "/internal/v1/knowledge-bases/"+url.PathEscape(kbID)); err != nil {
		return nil, deleteResourceOutput{}, err
	}
	return nil, deleteResourceOutput{Deleted: true, ID: kbID}, nil
}

func (h *toolHandlers) listDocuments(ctx context.Context, _ *sdkmcp.CallToolRequest, input listDocumentsInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	kbID := strings.TrimSpace(input.KnowledgeBaseID)
	if kbID == "" {
		return nil, nil, fmt.Errorf("knowledgeBaseId is required")
	}
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", fmt.Sprintf("%d", input.Page))
	}
	if input.PageSize > 0 {
		query.Set("pageSize", fmt.Sprintf("%d", input.PageSize))
	}
	if input.Status != nil && strings.TrimSpace(*input.Status) != "" {
		query.Set("status", strings.TrimSpace(*input.Status))
	}
	return h.adapterList(ctx, h.effectiveCaller(), "/internal/v1/knowledge-bases/"+url.PathEscape(kbID)+"/documents", query)
}

func (h *toolHandlers) getDocument(ctx context.Context, _ *sdkmcp.CallToolRequest, input getDocumentInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	docID := strings.TrimSpace(input.DocumentID)
	if docID == "" {
		return nil, nil, fmt.Errorf("documentId is required")
	}
	path := "/internal/v1/documents/" + url.PathEscape(docID)
	query := url.Values{}
	if kbID := strings.TrimSpace(input.KnowledgeBaseID); kbID != "" {
		query.Set("knowledgeBaseId", kbID)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return h.adapterGet(ctx, h.effectiveCaller(), path)
}

func (h *toolHandlers) createDocument(ctx context.Context, _ *sdkmcp.CallToolRequest, input createDocumentInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	kbID := strings.TrimSpace(input.KnowledgeBaseID)
	if kbID == "" {
		return nil, nil, fmt.Errorf("knowledgeBaseId is required")
	}
	fileName := strings.TrimSpace(input.FileName)
	if fileName == "" {
		return nil, nil, fmt.Errorf("fileName is required")
	}
	if strings.TrimSpace(input.FileContentBase64) == "" {
		return nil, nil, fmt.Errorf("fileContentBase64 is required")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(input.FileContentBase64))
	if err != nil {
		return nil, nil, fmt.Errorf("fileContentBase64 must be valid base64")
	}
	if len(content) == 0 {
		return nil, nil, fmt.Errorf("fileContentBase64 must not decode to empty content")
	}

	fields := map[string]string{}
	if len(input.Tags) > 0 {
		tagsJSON, err := json.Marshal(input.Tags)
		if err != nil {
			return nil, nil, fmt.Errorf("encode tags: %w", err)
		}
		fields["tags"] = string(tagsJSON)
	}

	file := MultipartFile{
		FieldName:   "file",
		FileName:    fileName,
		Content:     content,
		ContentType: "",
	}
	if input.ContentType != nil {
		file.ContentType = strings.TrimSpace(*input.ContentType)
	}

	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, nil, err
	}

	status, respBody, _, err := h.bridge.DoMultipart(ctx, caller, http.MethodPost,
		"/internal/v1/knowledge-bases/"+url.PathEscape(kbID)+"/documents", fields, []MultipartFile{file})
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusCreated {
		return nil, nil, adapterErrorMessage(status, respBody)
	}
	data, err := decodeAdapterSuccess(respBody)
	if err != nil {
		return nil, nil, err
	}
	out, err := rawToMap(data)
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (h *toolHandlers) updateDocument(ctx context.Context, _ *sdkmcp.CallToolRequest, input updateDocumentInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	docID := strings.TrimSpace(input.DocumentID)
	if docID == "" {
		return nil, nil, fmt.Errorf("documentId is required")
	}
	if input.Tags == nil {
		return nil, nil, fmt.Errorf("tags is required")
	}
	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, nil, err
	}
	return h.adapterUpdate(ctx, caller, "/internal/v1/documents/"+url.PathEscape(docID), map[string]any{
		"tags": input.Tags,
	})
}

func (h *toolHandlers) deleteDocument(ctx context.Context, _ *sdkmcp.CallToolRequest, input deleteDocumentInput) (*sdkmcp.CallToolResult, deleteResourceOutput, error) {
	docID := strings.TrimSpace(input.DocumentID)
	if docID == "" {
		return nil, deleteResourceOutput{}, fmt.Errorf("documentId is required")
	}
	caller, err := h.requireWriteCaller()
	if err != nil {
		return nil, deleteResourceOutput{}, err
	}
	if err := h.adapterDelete(ctx, caller, "/internal/v1/documents/"+url.PathEscape(docID)); err != nil {
		return nil, deleteResourceOutput{}, err
	}
	return nil, deleteResourceOutput{Deleted: true, ID: docID}, nil
}

func (h *toolHandlers) getChunk(ctx context.Context, _ *sdkmcp.CallToolRequest, input getChunkInput) (*sdkmcp.CallToolResult, map[string]any, error) {
	chunkID := strings.TrimSpace(input.ChunkID)
	if chunkID == "" {
		return nil, nil, fmt.Errorf("chunkId is required")
	}
	query := url.Values{}
	if docID := strings.TrimSpace(input.DocumentID); docID != "" {
		query.Set("documentId", docID)
	}
	if kbID := strings.TrimSpace(input.KnowledgeBaseID); kbID != "" {
		query.Set("knowledgeBaseId", kbID)
	}
	path := "/internal/v1/chunks/" + url.PathEscape(chunkID)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result, out, err := h.adapterGet(ctx, h.effectiveCaller(), path)
	if err != nil {
		return result, out, err
	}
	if _, ok := out["chunkId"]; !ok {
		out["chunkId"] = chunkID
	}
	return result, out, nil
}

func (h *toolHandlers) getDocumentContent(ctx context.Context, _ *sdkmcp.CallToolRequest, input getDocumentContentInput) (*sdkmcp.CallToolResult, getDocumentContentOutput, error) {
	docID := strings.TrimSpace(input.DocumentID)
	if docID == "" {
		return nil, getDocumentContentOutput{}, fmt.Errorf("documentId is required")
	}
	status, respBody, headers, err := h.bridge.DoGET(ctx, h.effectiveCaller(), "/internal/v1/documents/"+url.PathEscape(docID)+"/content", nil)
	if err != nil {
		return nil, getDocumentContentOutput{}, err
	}
	if status != http.StatusOK {
		return nil, getDocumentContentOutput{}, adapterErrorMessage(status, respBody)
	}
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return nil, getDocumentContentOutput{
		ContentBase64: base64.StdEncoding.EncodeToString(respBody),
		ContentType:   contentType,
		SizeBytes:     len(respBody),
	}, nil
}

func (h *toolHandlers) adapterGet(ctx context.Context, caller CallerContext, path string) (*sdkmcp.CallToolResult, map[string]any, error) {
	status, respBody, _, err := h.bridge.DoGET(ctx, caller, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return nil, nil, adapterErrorMessage(status, respBody)
	}
	data, err := decodeAdapterSuccess(respBody)
	if err != nil {
		return nil, nil, err
	}
	out, err := rawToMap(data)
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (h *toolHandlers) adapterList(ctx context.Context, caller CallerContext, path string, query url.Values) (*sdkmcp.CallToolResult, map[string]any, error) {
	status, respBody, _, err := h.bridge.DoGET(ctx, caller, path, query)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return nil, nil, adapterErrorMessage(status, respBody)
	}
	list, err := decodeAdapterList(respBody)
	if err != nil {
		return nil, nil, err
	}
	items, err := rawToSlice(list.Data)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]any{"data": items}
	if len(list.Page) > 0 {
		page, err := rawToMap(list.Page)
		if err != nil {
			return nil, nil, err
		}
		out["page"] = page
	}
	return nil, out, nil
}

func (h *toolHandlers) adapterCreate(ctx context.Context, caller CallerContext, path string, payload map[string]any) (*sdkmcp.CallToolResult, map[string]any, error) {
	status, respBody, _, err := h.bridge.DoJSON(ctx, caller, http.MethodPost, path, payload)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusCreated {
		return nil, nil, adapterErrorMessage(status, respBody)
	}
	data, err := decodeAdapterSuccess(respBody)
	if err != nil {
		return nil, nil, err
	}
	out, err := rawToMap(data)
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (h *toolHandlers) adapterUpdate(ctx context.Context, caller CallerContext, path string, payload map[string]any) (*sdkmcp.CallToolResult, map[string]any, error) {
	status, respBody, _, err := h.bridge.DoJSON(ctx, caller, http.MethodPatch, path, payload)
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return nil, nil, adapterErrorMessage(status, respBody)
	}
	data, err := decodeAdapterSuccess(respBody)
	if err != nil {
		return nil, nil, err
	}
	out, err := rawToMap(data)
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

func (h *toolHandlers) adapterDelete(ctx context.Context, caller CallerContext, path string) error {
	status, respBody, _, err := h.bridge.Do(ctx, caller, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return adapterErrorMessage(status, respBody)
	}
	return nil
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

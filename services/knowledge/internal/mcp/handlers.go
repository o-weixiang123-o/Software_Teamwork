package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolHandlers struct {
	bridge *Bridge
	caller CallerContext
}

func (h *toolHandlers) effectiveCaller() CallerContext {
	caller := h.caller
	if strings.TrimSpace(caller.RequestID) == "" {
		caller.RequestID = newRequestID()
	}
	return caller
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

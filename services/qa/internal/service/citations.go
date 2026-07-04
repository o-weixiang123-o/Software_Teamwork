package service

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/qa/internal/service/agent"
)

const (
	maxCitationSnapshotTextRunes    = 2000
	maxCitationSnapshotContextRunes = 4000
	knowledgeRetrievalStopDirective = "Knowledge retrieval already returned relevant source results. Do not call Knowledge retrieval tools again for this user request. Write the final answer now using the retrieved facts and citation numbers."
)

func citationsFromAgentMessages(messageID, runID string, messages []agent.Message) []Citation {
	citations := make([]Citation, 0)
	seen := map[string]struct{}{}
	for _, message := range messages {
		if message.Role != agent.RoleTool || !isCitationToolName(message.Name) || strings.TrimSpace(message.Content) == "" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil {
			continue
		}
		for _, record := range collectCitationRecords(payload) {
			citation, ok := citationFromRecord(record)
			if !ok {
				continue
			}
			citation.ID = newUUID()
			citation.MessageID = messageID
			citation.ResponseRunID = runID
			citation.CitationNo = len(citations) + 1
			citation = NormalizeCitation(citation)
			key := citationSnapshotKey(citation)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			citation.CitationNo = len(citations) + 1
			citations = append(citations, citation)
		}
	}
	return citations
}

func isCitationToolName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	switch name {
	case "search_knowledge", "get_citation_source", "knowledge_query", "search_session_attachments":
		return true
	}
	return strings.HasSuffix(name, "__search") ||
		strings.HasSuffix(name, ".search") ||
		strings.HasSuffix(name, "__search_knowledge") ||
		strings.HasSuffix(name, ".search_knowledge") ||
		strings.HasSuffix(name, "__get_citation_source") ||
		strings.HasSuffix(name, ".get_citation_source") ||
		strings.HasSuffix(name, "__knowledge_query") ||
		strings.HasSuffix(name, ".knowledge_query")
}

func NewKnowledgeRetrievalStopPolicy(knowledgeMCPAlias string) agent.ToolResultPolicy {
	alias := strings.TrimSpace(strings.ToLower(knowledgeMCPAlias))
	return func(observation agent.ToolObservation) agent.ToolResultPolicyDecision {
		return knowledgeRetrievalStopPolicy(observation, alias)
	}
}

func KnowledgeRetrievalStopPolicy(observation agent.ToolObservation) agent.ToolResultPolicyDecision {
	return knowledgeRetrievalStopPolicy(observation, "knowledge")
}

func knowledgeRetrievalStopPolicy(observation agent.ToolObservation, knowledgeMCPAlias string) agent.ToolResultPolicyDecision {
	if observation.Type != agent.EventToolCompleted {
		return agent.ToolResultPolicyDecision{}
	}
	name := strings.TrimSpace(strings.ToLower(observation.ToolName))
	if !isKnowledgeRetrievalToolName(name, knowledgeMCPAlias) {
		return agent.ToolResultPolicyDecision{}
	}
	if len(extractCitationsFromToolResult(observation.Result, 1)) == 0 {
		return agent.ToolResultPolicyDecision{}
	}
	decision := agent.ToolResultPolicyDecision{
		SuppressToolNames:   []string{"search_knowledge", "get_citation_source", "knowledge_query"},
		AppendSystemMessage: knowledgeRetrievalStopDirective,
	}
	if prefix := knowledgeMCPToolPrefix(name, knowledgeMCPAlias); prefix != "" {
		decision.SuppressToolPrefixes = append(decision.SuppressToolPrefixes, prefix)
	}
	if knowledgeMCPAlias != "" && knowledgeMCPAlias != "knowledge" {
		decision.SuppressToolPrefixes = append(decision.SuppressToolPrefixes, knowledgeMCPAlias+"__")
	}
	decision.SuppressToolPrefixes = append(decision.SuppressToolPrefixes, "knowledge__")
	return decision
}

func isKnowledgeRetrievalToolName(name string, knowledgeMCPAlias string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "search_knowledge", "get_citation_source", "knowledge_query":
		return true
	}
	prefix := knowledgeMCPToolPrefix(name, knowledgeMCPAlias)
	if prefix == "" {
		return false
	}
	tool := strings.TrimPrefix(name, prefix)
	switch tool {
	case "search", "get_chunk", "get_document", "list_documents", "search_knowledge", "get_citation_source", "knowledge_query":
		return true
	default:
		return false
	}
}

func knowledgeMCPToolPrefix(name string, knowledgeMCPAlias string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	knowledgeMCPAlias = strings.TrimSpace(strings.ToLower(knowledgeMCPAlias))
	if knowledgeMCPAlias != "" && strings.HasPrefix(name, knowledgeMCPAlias+"__") {
		return knowledgeMCPAlias + "__"
	}
	if strings.HasPrefix(name, "knowledge__") {
		return "knowledge__"
	}
	return ""
}

func collectCitationRecords(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, collectCitationRecords(item)...)
		}
		return items
	case map[string]any:
		if looksLikeCitationRecord(typed) {
			return []map[string]any{typed}
		}
		items := []map[string]any{}
		for _, key := range []string{"data", "results", "items", "citations", "references", "documents", "chunks"} {
			if child, ok := typed[key]; ok {
				items = append(items, collectCitationRecords(child)...)
			}
		}
		return items
	default:
		return nil
	}
}

func looksLikeCitationRecord(record map[string]any) bool {
	for _, key := range []string{
		"documentId", "docId", "externalDocId", "external_doc_id",
		"document_id", "documentName", "docName", "document_name", "chunkId", "chunk_id", "externalChunkId",
		"attachmentId", "attachment_id", "filename",
		"contentPreview", "content_preview", "quoteText", "quote_text", "text",
	} {
		if _, ok := record[key]; ok {
			return true
		}
	}
	return false
}

func citationFromRecord(record map[string]any) (Citation, bool) {
	citation := Citation{
		DocumentID:              firstString(record, "documentId", "document_id", "docId", "externalDocId", "external_doc_id"),
		DocumentName:            firstString(record, "documentName", "docName", "document_name", "doc_name", "title", "filename"),
		KnowledgeBaseID:         firstString(record, "knowledgeBaseId", "knowledge_base_id", "externalKbId", "external_kb_id", "kbId"),
		ChunkID:                 firstString(record, "chunkId", "chunk_id", "externalChunkId", "external_chunk_id"),
		AttachmentID:            firstString(record, "attachmentId", "attachment_id"),
		SectionPath:             firstString(record, "sectionPath", "section_path"),
		Text:                    firstString(record, "quoteText", "quote_text", "text"),
		ContentPreview:          firstString(record, "contentPreview", "content_preview", "preview"),
		Context:                 firstString(record, "context", "surroundingText", "surrounding_text"),
		ChunkType:               firstString(record, "chunkType", "chunk_type"),
		SourceUnavailableReason: firstString(record, "sourceUnavailableReason", "source_unavailable_reason"),
		Metadata:                firstMap(record, "metadata", "meta"),
	}
	citation.Text = truncateRunes(citation.Text, maxCitationSnapshotTextRunes)
	citation.ContentPreview = truncateRunes(citation.ContentPreview, maxCitationSnapshotTextRunes)
	citation.Context = truncateRunes(citation.Context, maxCitationSnapshotContextRunes)
	if page, ok := firstInt(record, "pageNumber", "page_number", "page"); ok && page > 0 {
		citation.PageNumber = &page
	}
	if score, ok := firstFloat(record, "score", "vectorScore", "vector_score"); ok && validScore(score) {
		citation.Score = &score
	}
	if score, ok := firstFloat(record, "rerankScore", "rerank_score"); ok && validScore(score) {
		citation.RerankScore = &score
	}
	if available, ok := firstBool(record, "isSourceAvailable", "sourceAvailable", "source_available"); ok {
		citation.IsSourceAvailable = available
	} else {
		citation.IsSourceAvailable = citation.DocumentID != "" || citation.AttachmentID != ""
	}
	if firstNonBlank(citation.DocumentID, citation.DocumentName, citation.ChunkID, citation.AttachmentID, citation.Text, citation.ContentPreview, citation.Context) == "" {
		return Citation{}, false
	}
	return NormalizeCitation(citation), true
}

func citationSnapshotKey(citation Citation) string {
	return strings.Join([]string{
		citation.KnowledgeBaseID,
		citation.DocumentID,
		citation.AttachmentID,
		citation.ChunkID,
		citation.Text,
		citation.ContentPreview,
	}, "\x00")
}

func firstString(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := stringify(record[key]); ok {
			return value
		}
	}
	return ""
}

func firstMap(record map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := record[key].(map[string]any); ok {
			return value
		}
	}
	return map[string]any{}
}

func firstInt(record map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if value, ok := intValue(record[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func firstFloat(record map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := floatValue(record[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func firstBool(record map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := boolValue(record[key]); ok {
			return value, true
		}
	}
	return false, false
}

func stringify(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed, trimmed != ""
	case json.Number:
		return typed.String(), typed.String() != ""
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10), true
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	default:
		return "", false
	}
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if math.Trunc(typed) == typed {
			return int(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed), true
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func boolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return false, false
	}
}

func validScore(value float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	return value >= 0 && value <= 1
}

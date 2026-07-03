package mcp

const (
	// Read-only tools exposed via MCP (v1 contract).
	toolSearch        = "search"
	toolListDocuments = "list_documents"
	toolGetDocument   = "get_document"
	toolGetChunk      = "get_chunk"

	// Deprecated / internal: not published in the v1 MCP tool list.
	toolSearchKnowledge     = "search_knowledge"
	toolAnswerFromKnowledge = "answer_from_knowledge"
	toolListKnowledgeBases  = "list_knowledge_bases"
	toolGetKnowledgeBase    = "get_knowledge_base"
	toolCreateKnowledgeBase = "create_knowledge_base"
	toolUpdateKnowledgeBase = "update_knowledge_base"
	toolDeleteKnowledgeBase = "delete_knowledge_base"
	toolCreateDocument      = "create_document"
	toolUpdateDocument      = "update_document"
	toolDeleteDocument      = "delete_document"
	toolListDocumentChunks  = "list_document_chunks"
	toolGetDocumentContent  = "get_document_content"
)

// ToolCatalog returns the v1 MCP read-only tool names in stable order.
func ToolCatalog() []string {
	return []string{
		toolSearch,
		toolListDocuments,
		toolGetDocument,
		toolGetChunk,
	}
}

type searchKnowledgeInput struct {
	Query            string            `json:"query" jsonschema:"required,Search query text (1-2000 characters)"`
	KnowledgeBaseIDs []string          `json:"knowledgeBaseIds,omitempty" jsonschema:"Knowledge base IDs to search"`
	DocumentIDs      []string          `json:"documentIds,omitempty" jsonschema:"Optional document IDs to restrict search"`
	TopK             int               `json:"topK,omitempty" jsonschema:"Maximum number of results to return"`
	ScoreThreshold   *float64          `json:"scoreThreshold,omitempty" jsonschema:"Minimum similarity score threshold"`
	Rerank           bool              `json:"rerank,omitempty" jsonschema:"Whether to rerank results"`
	RerankTopN       *int              `json:"rerankTopN,omitempty" jsonschema:"Top N results after reranking"`
	Tags             []string          `json:"tags,omitempty" jsonschema:"Optional document tags filter"`
	MetadataFilter   map[string]string `json:"metadataFilter,omitempty" jsonschema:"Optional metadata filter"`
}

type searchKnowledgeResult struct {
	Score           float64  `json:"score"`
	KnowledgeBaseID string   `json:"knowledgeBaseId"`
	DocumentID      string   `json:"documentId"`
	ChunkID         string   `json:"chunkId"`
	DocumentName    string   `json:"documentName"`
	ContentPreview  string   `json:"contentPreview"`
	Content         string   `json:"content"`
	SectionPath     *string  `json:"sectionPath,omitempty"`
	ChunkIndex      *int     `json:"chunkIndex,omitempty"`
	ChunkType       *string  `json:"chunkType,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

type searchKnowledgeOutput struct {
	QueryID string                  `json:"queryId"`
	Results []searchKnowledgeResult `json:"results"`
}

type answerFromKnowledgeInput struct {
	Question         string   `json:"question" jsonschema:"required,Question to answer using retrieved knowledge"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds,omitempty" jsonschema:"Knowledge base IDs to search"`
	DocumentIDs      []string `json:"documentIds,omitempty" jsonschema:"Optional document IDs to restrict search"`
	TopK             int      `json:"topK,omitempty" jsonschema:"Maximum retrieval results"`
	ScoreThreshold   *float64 `json:"scoreThreshold,omitempty" jsonschema:"Minimum similarity score threshold"`
	ModelProfileID   string   `json:"modelProfileId" jsonschema:"required,AI Gateway model profile ID"`
	SystemPrompt     *string  `json:"systemPrompt,omitempty" jsonschema:"Optional system prompt override"`
	MaxTokens        int      `json:"maxTokens,omitempty" jsonschema:"Maximum tokens for the generated answer"`
}

type answerCitation struct {
	Index           int    `json:"index"`
	KnowledgeBaseID string `json:"knowledgeBaseId"`
	DocumentID      string `json:"documentId"`
	ChunkID         string `json:"chunkId"`
	Excerpt         string `json:"excerpt"`
}

type answerRetrievalSummary struct {
	QueryID     string `json:"queryId"`
	ResultCount int    `json:"resultCount"`
}

type answerFromKnowledgeOutput struct {
	Answer    string                 `json:"answer"`
	Citations []answerCitation       `json:"citations"`
	Retrieval answerRetrievalSummary `json:"retrieval"`
}

type getDocumentContentOutput struct {
	ContentBase64 string `json:"contentBase64"`
	ContentType   string `json:"contentType"`
	SizeBytes     int    `json:"sizeBytes"`
}

type deleteResourceOutput struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

type listKnowledgeBasesInput struct {
	Page     int `json:"page,omitempty" jsonschema:"Page number (default 1)"`
	PageSize int `json:"pageSize,omitempty" jsonschema:"Page size (default 20, max 200)"`
}

type getKnowledgeBaseInput struct {
	KnowledgeBaseID string `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID"`
}

type createKnowledgeBaseInput struct {
	ID                *string       `json:"id,omitempty" jsonschema:"Optional client-specified ID"`
	Name              string        `json:"name" jsonschema:"required,Knowledge base name"`
	Description       *string       `json:"description,omitempty" jsonschema:"Knowledge base description"`
	DocType           *string       `json:"docType,omitempty" jsonschema:"Document type / chunk method"`
	ChunkStrategy     jsonRawObject `json:"chunkStrategy,omitempty" jsonschema:"Chunk strategy configuration"`
	RetrievalStrategy jsonRawObject `json:"retrievalStrategy,omitempty" jsonschema:"Retrieval strategy configuration"`
}

type updateKnowledgeBaseInput struct {
	KnowledgeBaseID   string        `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID"`
	Name              *string       `json:"name,omitempty" jsonschema:"Updated name"`
	Description       *string       `json:"description,omitempty" jsonschema:"Updated description"`
	DocType           *string       `json:"docType,omitempty" jsonschema:"Updated document type"`
	ChunkStrategy     jsonRawObject `json:"chunkStrategy,omitempty" jsonschema:"Updated chunk strategy"`
	RetrievalStrategy jsonRawObject `json:"retrievalStrategy,omitempty" jsonschema:"Updated retrieval strategy"`
}

type deleteKnowledgeBaseInput struct {
	KnowledgeBaseID string `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID"`
}

type listDocumentsInput struct {
	KnowledgeBaseID string  `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID"`
	Page            int     `json:"page,omitempty" jsonschema:"Page number (default 1)"`
	PageSize        int     `json:"pageSize,omitempty" jsonschema:"Page size (default 20, max 200)"`
	Status          *string `json:"status,omitempty" jsonschema:"Optional document status filter"`
}

type getDocumentInput struct {
	DocumentID string `json:"documentId" jsonschema:"required,Document ID"`
}

type createDocumentInput struct {
	KnowledgeBaseID   string   `json:"knowledgeBaseId" jsonschema:"required,Target knowledge base ID"`
	FileName          string   `json:"fileName" jsonschema:"required,Uploaded file name"`
	FileContentBase64 string   `json:"fileContentBase64" jsonschema:"required,Base64-encoded file bytes"`
	ContentType       *string  `json:"contentType,omitempty" jsonschema:"Optional MIME type"`
	Tags              []string `json:"tags,omitempty" jsonschema:"Optional document tags"`
}

type updateDocumentInput struct {
	DocumentID string   `json:"documentId" jsonschema:"required,Document ID"`
	Tags       []string `json:"tags" jsonschema:"required,Updated document tags"`
}

type deleteDocumentInput struct {
	DocumentID string `json:"documentId" jsonschema:"required,Document ID"`
}

type getChunkInput struct {
	KnowledgeBaseID string `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID from search results"`
	ChunkID         string `json:"chunkId" jsonschema:"required,Chunk ID from search results"`
	DocumentID      string `json:"documentId" jsonschema:"required,Document ID from search results"`
}

type getDocumentContentInput struct {
	DocumentID string `json:"documentId" jsonschema:"required,Document ID"`
}

// jsonRawObject keeps optional JSON object fields in tool schemas without importing encoding/json here.
type jsonRawObject map[string]any

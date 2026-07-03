package mcp

const (
	// Read-only tools exposed via MCP (v1 contract).
	toolSearch        = "search"
	toolListDocuments = "list_documents"
	toolGetDocument   = "get_document"
	toolGetChunk      = "get_chunk"
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
	KnowledgeBaseIDs []string          `json:"knowledgeBaseIds,omitempty" jsonschema:"Knowledge base IDs to search; empty searches all available knowledge bases"`
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

type listDocumentsInput struct {
	KnowledgeBaseID string  `json:"knowledgeBaseId" jsonschema:"required,Knowledge base ID"`
	Page            int     `json:"page,omitempty" jsonschema:"Page number (default 1)"`
	PageSize        int     `json:"pageSize,omitempty" jsonschema:"Page size (default 20, max 200)"`
	Status          *string `json:"status,omitempty" jsonschema:"Optional document status filter"`
}

type getDocumentInput struct {
	DocumentID      string `json:"documentId" jsonschema:"required,Document ID"`
	KnowledgeBaseID string `json:"knowledgeBaseId,omitempty" jsonschema:"Optional knowledge base ID from search results; server resolves it when omitted"`
}

type getChunkInput struct {
	ChunkID         string `json:"chunkId" jsonschema:"required,Chunk ID from search results"`
	DocumentID      string `json:"documentId,omitempty" jsonschema:"Optional document ID from search results; improves lookup performance"`
	KnowledgeBaseID string `json:"knowledgeBaseId,omitempty" jsonschema:"Optional knowledge base ID from search results; server resolves it when omitted"`
}

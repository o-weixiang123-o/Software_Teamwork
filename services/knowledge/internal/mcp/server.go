package mcp

import (
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/adapter"
	"github.com/Sakayori-Iroha-168/Software_Teamwork/services/knowledge/internal/aigateway"
)

const serverName = "knowledge-mcp"

// NewStreamableHTTPHandler returns an HTTP handler for MCP Streamable HTTP transport.
func NewStreamableHTTPHandler(adapterServer *adapter.Server, caller CallerContext, chatClient *aigateway.ChatClient) http.Handler {
	transport := sdkmcp.NewStreamableHTTPHandler(func(r *http.Request) *sdkmcp.Server {
		return newMCPServer(adapterServer, callerFromHTTP(r, caller), chatClient)
	}, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adapterServer == nil || !adapterServer.AuthorizeServiceToken(r.Header.Get("X-Service-Token")) {
			http.Error(w, "service authentication required", http.StatusUnauthorized)
			return
		}
		transport.ServeHTTP(w, r)
	})
}

func newMCPServer(adapterServer *adapter.Server, caller CallerContext, chatClient *aigateway.ChatClient) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: serverName, Version: "0.1.0"}, nil)
	h := &toolHandlers{
		bridge: NewBridge(adapterServer),
		caller: caller,
		chat:   chatClient,
	}

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        toolSearch,
		Description: "Semantic search across knowledge bases. Returns ranked chunks with citation fields and no LLM synthesis. Use this first, then call get_chunk for full context when needed.",
	}, h.searchKnowledge)

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        toolListDocuments,
		Description: "Paginated list of documents in a knowledge base, filterable by processing status.",
	}, h.listDocuments)

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        toolGetDocument,
		Description: "Get document metadata and processing status by ID.",
	}, h.getDocument)

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        toolGetChunk,
		Description: "Read the full text of a single chunk by its ID (obtained from search results).",
	}, h.getChunk)

	return server
}

// NewInMemoryServer creates an MCP server for unit tests without HTTP transport.
func NewInMemoryServer(adapterServer *adapter.Server, caller CallerContext, chatClient *aigateway.ChatClient) *sdkmcp.Server {
	return newMCPServer(adapterServer, caller, chatClient)
}

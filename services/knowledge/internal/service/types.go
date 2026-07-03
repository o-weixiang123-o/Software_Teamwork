package service

import (
	"context"
	"encoding/json"
	"time"
)

const (
	PermissionKnowledgeRead     = "knowledge:read"
	PermissionKnowledgeWrite    = "knowledge:write"
	PermissionKnowledgeAdmin    = "knowledge:admin"
	PermissionSystemAdmin       = "system:admin"
	PermissionAdminParserConfig = "admin:parser-config:write"
)

type ParserBackend string

const (
	ParserBackendBuiltin          ParserBackend = "builtin"
	ParserBackendTika             ParserBackend = "tika"
	ParserBackendUnstructured     ParserBackend = "unstructured"
	ParserBackendLocalOCR         ParserBackend = "local_ocr"
	ParserBackendRemoteCompatible ParserBackend = "remote_compatible"
	ParserBackendPaddleOCRCloud   ParserBackend = "paddleocr_cloud"
)

type ParserConfig struct {
	ID                    string
	Name                  string
	Backend               ParserBackend
	Enabled               bool
	IsDefault             bool
	Concurrency           int
	SupportedContentTypes []string
	EndpointURL           *string
	DefaultParameters     json.RawMessage
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

type ParserConfigList struct {
	Items []ParserConfig
}

type ParserConfigSnapshot struct {
	ParserConfigID        string          `json:"parserConfigId"`
	Backend               ParserBackend   `json:"backend"`
	Concurrency           int             `json:"concurrency"`
	SupportedContentTypes []string        `json:"supportedContentTypes,omitempty"`
	EndpointURL           *string         `json:"endpointUrl,omitempty"`
	DefaultParameters     json.RawMessage `json:"defaultParameters,omitempty"`
}

type ParserConfigAudit struct {
	ID             string
	ParserConfigID string
	ActorUserID    string
	Action         string
	Summary        json.RawMessage
	CreatedAt      time.Time
}

type CreateParserConfigInput struct {
	Name                  string
	Backend               ParserBackend
	Enabled               *bool
	IsDefault             *bool
	Concurrency           int
	SupportedContentTypes []string
	EndpointURL           *string
	DefaultParameters     json.RawMessage
}

type UpdateParserConfigInput struct {
	ID                    string
	Name                  *string
	Backend               *ParserBackend
	Enabled               *bool
	IsDefault             *bool
	Concurrency           *int
	SupportedContentTypes *[]string
	EndpointURL           **string
	DefaultParameters     *json.RawMessage
}

type DocumentStatus string

const (
	DocumentStatusUploaded  DocumentStatus = "uploaded"
	DocumentStatusParsing   DocumentStatus = "parsing"
	DocumentStatusChunking  DocumentStatus = "chunking"
	DocumentStatusEmbedding DocumentStatus = "embedding"
	DocumentStatusReady     DocumentStatus = "ready"
	DocumentStatusFailed    DocumentStatus = "failed"
)

type RequestContext struct {
	RequestID      string
	UserID         string
	CallerService  string
	ServiceToken   string
	Roles          []string
	Permissions    []string
	ForwardedFor   string
	ForwardedProto string
}

type AccessScope struct {
	UserID     string
	CanReadAll bool
	CanWrite   bool
}

type Page struct {
	Page     int
	PageSize int
	Total    int64
}

type PageInput struct {
	Page     int
	PageSize int
}

type RuntimeKnowledgeBase struct {
	ID          string
	TenantID    string
	EmbeddingID string
	ChunkCount  int64
}

type RuntimeKnowledgeBaseCatalog interface {
	ListRuntimeKnowledgeBases(ctx context.Context, ids []string) ([]RuntimeKnowledgeBase, error)
}

type RuntimeDocument struct {
	ID              string
	KnowledgeBaseID string
	TenantID        string
	ChunkCount      int64
}

type RuntimeDocumentCatalog interface {
	GetRuntimeDocument(ctx context.Context, id string) (RuntimeDocument, error)
	ListRuntimeDocuments(ctx context.Context, ids []string) ([]RuntimeDocument, error)
}

type ParserConfigRepository interface {
	ListParserConfigs(ctx context.Context, enabled *bool) ([]ParserConfig, error)
	GetParserConfig(ctx context.Context, id string) (ParserConfig, error)
	CreateParserConfig(ctx context.Context, config ParserConfig, audit ParserConfigAudit) (ParserConfig, error)
	UpdateParserConfig(ctx context.Context, config ParserConfig, audit ParserConfigAudit) (ParserConfig, error)
	SoftDeleteParserConfig(ctx context.Context, id string, deletedAt time.Time, audit ParserConfigAudit) error
	GetEffectiveParserConfig(ctx context.Context, contentType string) (ParserConfig, error)
}

# Knowledge Service 数据模型文档

版本：v0.3
日期：2026-07-03
范围：`services/knowledge/` adapter、RAGFlow runtime 映射、parser config repository、早期迁移兼容字段和检索查询逻辑模型

## 1. 文档定位

本文定义 Knowledge Service 的当前数据模型边界，用于指导 Go adapter、RAGFlow runtime 映射、PostgreSQL goose migration 兼容字段、gateway 代理契约对齐和后续测试用例编写。

本文只描述 Knowledge Service 拥有的业务数据，不替代 gateway 公开 OpenAPI，也不定义 File Service、QA Service 或 Document Service 的内部模型。前端可见字段仍以 `docs/services/gateway/api/public.openapi.yaml` 为准；服务本地内部接口以 `docs/services/knowledge/api/internal.openapi.yaml` 和 Go DTO 为准。

## 2. 技术基线

本数据模型按 [`docs/architecture/technology-decisions.md`](../../../architecture/technology-decisions.md) 中的后端选型落地：

| 领域 | 数据模型约束 |
| --- | --- |
| PostgreSQL 访问 | 当前 Go adapter 使用 `pgx/v5` 手写 repository 管理 parser configs 和迁移保留字段；本服务当前没有 `sqlc.yaml` 或生成查询包。 |
| ORM | 不使用 GORM/ent 等 ORM；schema、query 和事务边界由 migration、repository 和 service 层维护。 |
| Migration | 使用 `goose`，迁移文件放在 `services/knowledge/migrations/` 并采用有序前缀。 |
| 事务 | parser config repository 使用显式事务或单语句写入；文档、chunks、embedding、索引和 runtime task 事实由 RAGFlow runtime 管理。 |
| Runtime queue | Knowledge Go adapter 不接入 asynq；RAGFlow runtime 内部使用 Redis/worker 协调解析、切块、embedding 和索引。 |
| Runtime index | 当前 runtime 默认使用 Elasticsearch/索引后端；早期 migration 中的 `qdrant_point_id` 仅作为兼容字段和历史 fixture 背景。 |

## 3. 边界原则

- RAGFlow runtime 是知识库文档、chunks、embedding、索引、任务状态和原始文件对象的运行时事实来源；Knowledge adapter 负责项目契约映射、权限上下文和错误归一化。
- Parser configs 是当前 Go adapter 自有的 PostgreSQL 管理资源，可选启用 `DATABASE_URL` 或 `KNOWLEDGE_DATABASE_URL`。
- 原始文件二进制、MinIO object key、索引 payload、runtime 内部 URL 和 provider 原始响应归 RAGFlow runtime 边界；Knowledge 公开响应不得暴露这些字段。
- 文档解析、切块、embedding、索引和检索支持归 Knowledge 的 RAGFlow runtime 边界；Knowledge adapter 负责项目契约映射、权限上下文和状态推进。
- `knowledge-queries` 是检索请求资源。当前实现即时执行并返回结果，不持久化查询历史；如后续需要审计或调试留痕，应新增独立表。
- 删除采用软删除优先：知识库和文档在 adapter/runtime 契约中从常规列表隐藏；runtime 内部对象和索引清理由 runtime 生命周期处理。

## 4. 命名约定

| 层 | 命名风格 | 示例 |
| --- | --- | --- |
| Public/Internal HTTP JSON | camelCase | `knowledgeBaseId`, `chunkCount`, `createdAt` |
| PostgreSQL table/column | snake_case | `knowledge_base_id`, `chunk_count`, `created_at` |
| Go domain field | MixedCaps | `KnowledgeBaseID`, `ChunkCount`, `CreatedAt` |
| Runtime index payload | runtime-defined | `dataset_id`, `document_id`, `chunk_id` 等经 adapter 映射 |
| Public enum | lowercase snake/case word | `uploaded`, `delete_cleanup` |

## 5. 实体关系概览

```text
RAGFlow dataset 1 ── n runtime documents
runtime document 1 ── n runtime chunks
runtime chunks ── runtime doc engine index

knowledge_query
  └── calls RAGFlow runtime retrieval
  └── adapter maps runtime chunks to KnowledgeQueryResult

parser_configs
  └── stored by Knowledge PostgreSQL repository when DB is configured
  └── mapped to runtime parser_config during KB/document operations
```

兼容说明：

- `services/knowledge/migrations/0001_create_knowledge_core_tables.sql` 仍保留
  `knowledge_bases`、`knowledge_documents`、`processing_jobs` 和 `document_chunks`
  早期表，供迁移兼容和历史 fixture 使用。
- 当前 adapter CRUD、上传、chunks/content 和检索主路径通过 RAGFlow runtime 映射，
  不是直接读写这些早期表。
- `0002_create_parser_configs.sql` 和 `internal/repository/postgres.go` 是当前
  Go adapter 仍使用的 PostgreSQL 路径。

## 6. 核心实体

### 6.1 KnowledgeBase

知识库是知识文档、切片策略和检索策略的聚合根。

| 逻辑字段 | HTTP 字段 | PostgreSQL 字段 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `ID` | `id` | `id` | text | 业务 ID，建议使用 `kb_` 前缀。 |
| `Name` | `name` | `name` | text | 展示名，当前最大 120 字符。 |
| `Description` | `description` | `description` | text | 描述，当前最大 2000 字符。 |
| `DocType` | `docType` | `doc_type` | text | 文档领域类型，默认 `GENERAL`。 |
| `ChunkStrategy` | `chunkStrategy` | `chunk_strategy` | jsonb | 切片策略，默认语义文本切片。 |
| `RetrievalStrategy` | `retrievalStrategy` | `retrieval_strategy` | jsonb | 检索策略，默认向量检索。 |
| `DocumentCount` | `documentCount` | derived | integer | 从未删除文档聚合得出，不单独持久化。 |
| `ChunkCount` | `chunkCount` | derived | integer | 从切片聚合得出，不单独持久化。 |
| `CreatedBy` | `createdBy` | `created_by` | text | 创建人用户 ID，由 gateway/auth 上下文传入。 |
| `CreatedAt` | `createdAt` | `created_at` | timestamptz | 创建时间。 |
| `UpdatedAt` | `updatedAt` | `updated_at` | timestamptz | 最后更新时间。 |
| `DeletedAt` | hidden | `deleted_at` | timestamptz nullable | 软删除时间。 |

默认策略示例：

```json
{
  "chunkStrategy": {
    "type": "SEMANTIC_TEXT",
    "size": 1600,
    "overlap": 200
  },
  "retrievalStrategy": {
    "mode": "VECTOR",
    "topK": 10,
    "scoreThreshold": 0.35
  }
}
```

关键索引：

- `idx_knowledge_bases_created_at` 支持按创建时间倒序分页。
- `idx_knowledge_bases_doc_type` 支持按 `doc_type` 过滤未删除知识库。
- `idx_knowledge_bases_deleted_at` 支持软删除过滤和后台清理扫描。

### 6.2 KnowledgeDocument

知识文档是 Knowledge Service 拥有的可检索文档资源。它维护解析协调、切片、embedding、索引和错误状态；具体 OCR/PaddleOCR 模型加载和文档解析执行在 Knowledge 的 RAGFlow runtime 边界内。

| 逻辑字段 | HTTP 字段 | PostgreSQL 字段 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `ID` | `id` | `id` | text | 业务 ID，建议使用 `doc_` 前缀。 |
| `KnowledgeBaseID` | `knowledgeBaseId` | `knowledge_base_id` | text | 所属知识库。 |
| `FileRef` | internal only | `file_ref` | text | 早期 File Service handoff 兼容字段；当前 runtime 路径不把它作为公开权限依据，也不返回给公开 API。 |
| `Name` | `name` | `name` | text | 文件名或用户定义文档名，当前最大 255 字符。 |
| `ContentType` | `contentType` | `content_type` | text nullable | MIME type。 |
| `SizeBytes` | `sizeBytes` | `size_bytes` | bigint nullable | 原文件大小。 |
| `Status` | `status` | `status` | text | 文档处理状态，见第 7.1 节。 |
| `ErrorCode` | `errorCode` | `error_code` | text nullable | 处理失败分类。 |
| `ErrorMessage` | `errorMessage` | `error_message` | text nullable | 可展示或可排查的错误摘要。 |
| `ChunkCount` | `chunkCount` | derived | integer | 从 `document_chunks` 聚合得出。 |
| `Tags` | `tags` | `tags` | jsonb | 文档标签，当前最多 32 个，每个最多 64 字符。 |
| `ParserBackend` | `parserBackend` | `parser_backend` | text nullable | Runtime parser 标识，例如 RAGFlow parser config 中的解析器类型。 |
| `CreatedBy` | `createdBy` | `created_by` | text | 上传或 handoff 发起人。 |
| `CurrentJobID` | `jobId` | `current_job_id` | text nullable | 当前处理任务引用。 |
| `CreatedAt` | `createdAt` | `created_at` | timestamptz | 创建时间。 |
| `UpdatedAt` | `updatedAt` | `updated_at` | timestamptz | 状态或元数据更新时间。 |
| `DeletedAt` | hidden | `deleted_at` | timestamptz nullable | 软删除时间。 |

关键索引：

- `idx_knowledge_documents_knowledge_base_id` 支持按知识库列文档。
- `idx_knowledge_documents_status` 支持处理队列、管理台和状态过滤。
- `idx_knowledge_documents_file_ref` 仅用于历史兼容排查，不作为当前 runtime 业务查询主路径。
- `idx_knowledge_documents_created_at` 支持上传记录倒序分页。

### 6.3 ProcessingJob

处理任务表是早期 Go worker 设计和兼容 fixture。当前 runtime task 状态由 RAGFlow runtime 管理，adapter 只映射公开 `DocumentStatus`、错误摘要和必要 trace。

| 逻辑字段 | HTTP 字段 | PostgreSQL 字段 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `ID` | `id` | `id` | text | 业务 ID，建议使用 `job_` 前缀。 |
| `KnowledgeBaseID` | `knowledgeBaseId` | `knowledge_base_id` | text | 任务所属知识库。 |
| `DocumentID` | `documentId` | `document_id` | text nullable | 文档级任务引用；知识库级任务可为空。 |
| `JobType` | `jobType` | `job_type` | text | 任务类型，见第 7.2 节。 |
| `Status` | `status` | `status` | text | 任务状态，见第 7.3 节。 |
| `CurrentStage` | `currentStage` | `current_stage` | text nullable | 当前处理阶段，见第 7.4 节。 |
| `ProgressPercent` | `progressPercent` | `progress_percent` | integer | 0-100 的粗粒度进度。 |
| `Message` | `message` | `message` | text nullable | 当前状态说明。 |
| `ErrorCode` | `errorCode` | `error_code` | text nullable | 失败分类。 |
| `ErrorMessage` | `errorMessage` | `error_message` | text nullable | 失败摘要。 |
| `Attempts` | `attempts` | `attempts` | integer | 已执行次数。 |
| `MaxAttempts` | `maxAttempts` | `max_attempts` | integer | 最大执行次数，默认 3。 |
| `IdempotencyKey` | hidden | `idempotency_key` | text nullable | handoff 幂等键。 |
| `StartedAt` | `startedAt` | `started_at` | timestamptz nullable | 开始执行时间。 |
| `FinishedAt` | `finishedAt` | `finished_at` | timestamptz nullable | 结束时间。 |
| `CreatedAt` | `createdAt` | `created_at` | timestamptz | 创建时间。 |
| `UpdatedAt` | `updatedAt` | `updated_at` | timestamptz | 更新时间。 |

关键索引：

- `idx_processing_jobs_status_created_at` 支持 worker 获取队列任务和管理台查询。
- `idx_processing_jobs_document_id` 支持文档处理历史查询。
- `idx_processing_jobs_knowledge_base_id` 支持知识库级任务查询。
- `uniq_processing_jobs_idempotency_key` 防止重复 handoff 创建重复任务。

### 6.4 DocumentChunk

文档切片是检索命中的最小可引用单元。当前实现从 RAGFlow runtime 获取 chunks 并映射到公开 DTO；早期 PostgreSQL `document_chunks` 表和 `qdrant_point_id` 字段可用于兼容 fixture，但不是当前运行主路径。

| 逻辑字段 | HTTP 字段 | PostgreSQL 字段 | 类型 | 说明 |
| --- | --- | --- | --- | --- |
| `ID` | `id` | `id` | text | 业务 ID，建议使用 `chunk_` 前缀。 |
| `KnowledgeBaseID` | `knowledgeBaseId` | `knowledge_base_id` | text | 所属知识库。 |
| `DocumentID` | `documentId` | `document_id` | text | 所属文档。 |
| `ChunkIndex` | `chunkIndex` | `chunk_index` | integer | 文档内从 0 开始的切片序号。 |
| `SectionPath` | `sectionPath` | `section_path` | text nullable | 章节路径或解析器提供的位置。 |
| `Content` | `content` | `content` | text | 切片正文。 |
| `TokenCount` | `tokenCount` | `token_count` | integer | 估算 token 数或切片器计数。 |
| `ChunkType` | `chunkType` | `chunk_type` | text nullable | 例如 `text`、`table`。 |
| `QdrantPointID` | `qdrantPointId` | `qdrant_point_id` | text nullable | 早期 Qdrant fixture 兼容字段；当前 runtime 可填入索引后端标识或留空，不作为公开权限依据。 |
| `EmbeddingProvider` | `embeddingProvider` | `embedding_provider` | text nullable | embedding provider 标识。 |
| `EmbeddingModel` | internal only | `embedding_model` | text nullable | embedding 模型标识。 |
| `EmbeddingDimension` | `embeddingDimension` | `embedding_dimension` | integer nullable | 向量维度。 |
| `Metadata` | `metadata` | `metadata` | jsonb | 切片器产生的结构化元数据。 |
| `CreatedAt` | `createdAt` | `created_at` | timestamptz | 创建时间。 |

约束与索引：

- `UNIQUE (document_id, chunk_index)` 保证同一文档内切片顺序唯一。
- `idx_document_chunks_document_id_chunk_index` 支持按文档顺序列出 chunks。
- `idx_document_chunks_knowledge_base_id` 支持知识库级聚合。
- `idx_document_chunks_qdrant_point_id` 仅支持历史 fixture/迁移排查；当前 runtime 检索不依赖该索引。

### 6.5 KnowledgeQuery

检索查询当前不持久化，但作为资源建模以保持 RESTful 契约稳定。

请求字段：

| 逻辑字段 | HTTP 字段 | 类型 | 说明 |
| --- | --- | --- | --- |
| `Query` | `query` | string | 查询文本，当前最大 2000 字符。 |
| `KnowledgeBaseIDs` | `knowledgeBaseIds` | string[] | 限定知识库；普通 Knowledge 调用为空时按用户权限检索可访问知识库；受信任 QA RAG 调用为空时使用项目级知识库池。 |
| `TopK` | `topK` | integer | 返回候选数，默认 runtime config，范围 1-100。 |
| `ScoreThreshold` | `scoreThreshold` | number | 相似度阈值，默认 runtime config。 |
| `Tags` | `tags` | string[] | 标签过滤。 |
| `MetadataFilter` | `metadataFilter` | object | 切片 metadata 等值过滤。 |
| `Rerank` | `rerank` | boolean | 是否请求 rerank；当前通过 provider-neutral boundary 执行，未配置 reranker 时保留向量顺序。 |
| `RerankTopN` | `rerankTopN` | integer | rerank 后截断数，必须小于等于 `topK`。 |

响应字段：

| 逻辑字段 | HTTP 字段 | 类型 | 说明 |
| --- | --- | --- | --- |
| `ID` | `id` | string | 查询资源 ID，建议使用 `kq_` 前缀。 |
| `Results` | `results` | array | 检索命中列表。 |
| `Trace` | `trace` | object | embedding、collection、topK、阈值和命中数。 |

检索结果由 RAGFlow runtime 返回，adapter 将 runtime chunk 映射为 `KnowledgeQueryResult`。只有文档状态为 `ready` 且调用方有权限访问的结果才会返回；权限、状态和输出字段必须以 adapter/gateway 契约为准，不读取 runtime 内部 payload 作为前端事实。

### 6.6 RuntimeConfig

运行配置当前保存在进程内存中，用于管理台和本地开发。后续如果需要跨实例生效，应新增 `runtime_configs` 或配置中心集成，不能把配置散落到业务表。

| 字段 | HTTP 字段 | 类型 | 说明 |
| --- | --- | --- | --- |
| `EmbeddingProvider` | `embeddingProvider` | string | embedding provider。 |
| `EmbeddingModel` | `embeddingModel` | string | embedding 模型。 |
| `EmbeddingDimension` | `embeddingDimension` | integer | 向量维度。 |
| `QdrantCollection` | `qdrantCollection` | string | 兼容字段名；当前 adapter 填入 runtime doc engine 标识，例如 `elasticsearch`。 |
| `ParserBackend` | `parserBackend` | string | 默认解析器。 |
| `RerankProvider` | `rerankProvider` | string | rerank provider。 |
| `RerankModel` | `rerankModel` | string | rerank 模型。 |
| `RetrievalTopK` | `retrievalTopK` | integer | 默认 topK。 |
| `ScoreThreshold` | `scoreThreshold` | number | 默认分数阈值。 |
| `MaxConcurrentJobs` | `maxConcurrentJobs` | integer | 最大并发任务数。 |
| `ProcessingTimeoutSec` | `processingTimeoutSec` | integer | 处理超时秒数。 |
| `SecretRefs` | `secretRefs` | object | 外部密钥引用名，不保存密钥明文。 |

### 6.7 RetrievalContractFixture

检索实现和契约测试依赖的是 adapter 公开契约，而不是 runtime 内部表或旧 Go worker
进程是否已经真实跑通。测试可以 seed fake runtime 或以下早期兼容数据：

| Fixture | 必填字段 | 说明 |
| --- | --- | --- |
| `knowledge_bases` | `id`、`created_by`、`deleted_at IS NULL` | 作为权限和范围过滤根。 |
| `knowledge_documents` | `id`、`knowledge_base_id`、`name`、`status='ready'`、`tags`、`created_by`、`deleted_at IS NULL` | 只有 `ready` 且未删除文档可进入检索结果。 |
| `document_chunks` | `id`、`knowledge_base_id`、`document_id`、`chunk_index`、`content`、`token_count`、`metadata`、`qdrant_point_id` | 作为检索结果 hydrate 的正文和展示事实来源。 |
| runtime/vector hit | `pointId`、`score`、payload.`knowledge_base_id`、payload.`document_id`、payload.`chunk_id` | 可由 fake runtime 或历史 fake Qdrant adapter 直接返回；payload 只用于定位和过滤。 |

fixture 规则：

- `knowledge_documents.status != 'ready'`、`deleted_at IS NOT NULL`、缺少访问权限、低于阈值或 tag/metadata 不匹配的命中必须被过滤。
- Runtime/vector hit 找不到对应公开文档或 hydrate 后文档不可见时，跳过该 hit，不把内部不一致暴露给前端。
- 无有效命中时返回 `results: []` 和 `trace.hitCount: 0`，不得返回 500。
- fake embedding/rerank adapter 可以返回固定向量、固定 score 或固定重排序顺序，但公开响应和 trace 字段必须符合 gateway OpenAPI。
- 单元或契约测试不要求启动 RAGFlow runtime、真实 embedding provider 或真实索引后端；真实端到端 smoke 需单独显式启用。

## 7. 枚举与状态机

### 7.1 DocumentStatus

| 状态 | 说明 |
| --- | --- |
| `uploaded` | 文档资源已创建，原文件已交给 runtime 或等待 runtime 处理。 |
| `parsing` | 正在读取原文件并解析。 |
| `chunking` | 正在生成切片。 |
| `embedding` | 正在生成 embedding 并准备向量索引。 |
| `ready` | 切片和索引可用于检索。 |
| `failed` | 处理失败，查看 `errorCode` 和 `errorMessage`。 |

状态流转：

```text
uploaded -> parsing -> chunking -> embedding -> ready
                      \-> failed
parsing/chunking/embedding -> failed
failed -> parsing    # retry/reprocess 时
```

### 7.2 JobType

| 类型 | 说明 |
| --- | --- |
| `ingest` | 新文档入库处理。 |
| `reprocess` | 知识库或文档重处理。 |
| `delete_cleanup` | 历史 Go worker 类型；当前 runtime 路径由 runtime 处理内部对象和索引生命周期。 |

### 7.3 JobStatus

| 状态 | 说明 |
| --- | --- |
| `queued` | 已创建，等待 worker 执行。 |
| `running` | 正在执行。 |
| `succeeded` | 执行成功。 |
| `failed` | 执行失败，可按策略重试。 |
| `cancelled` | 已取消。 |

### 7.4 JobStage

当前实现会使用以下阶段值：

```text
handoff
parsing
chunking
embedding
indexing
reprocessing
```

`currentStage` 是诊断字段，不应作为前端强状态机判断依据；前端稳定判断应使用 `status` 和 `progressPercent`。

## 8. Runtime 索引模型

当前 adapter 的 `qdrantCollection` 公开 trace 字段是兼容字段名；当前实现填入 runtime
doc engine 标识：

```text
elasticsearch
```

早期 Qdrant fixture 使用由 `chunk_id` 稳定派生的 point ID。当前 RAGFlow runtime 的
索引 ID、payload 和向量维度由 runtime 管理，adapter 只能映射公开所需字段。

Payload 示例：

```json
{
  "knowledge_base_id": "kb_123",
  "document_id": "doc_123",
  "chunk_id": "chunk_123",
  "chunk_index": 0,
  "chunk_type": "text",
  "section_path": "root/introduction",
  "tags": ["linux", "deployment"],
  "metadata": {
    "heading": "Introduction"
  }
}
```

Runtime/fixture payload 规则：

- 必须包含 `knowledge_base_id`、`document_id`、`chunk_id`，用于权限过滤和 PostgreSQL hydrate。
- 可包含 `tags` 和 `metadata` 支持过滤。
- 不保存完整文档状态、任务状态或错误详情。
- 不依赖 runtime index payload 作为最终展示内容来源；展示正文从 runtime chunk 映射或兼容 fixture 读取。
- 契约测试可以用等价的 fake runtime/vector hit 替代真实索引命中；只要 payload 字段和 score 语义一致，不需要等待真实 runtime 完成索引写入。

## 9. PostgreSQL 当前表与兼容字段

早期 goose migration 位于 `services/knowledge/migrations/0001_create_knowledge_core_tables.sql`，包含：

```text
knowledge_bases
knowledge_documents
processing_jobs
document_chunks
```

兼容注意事项：

- 当前 Knowledge 没有 `sqlc.yaml` 或 `internal/repository/sqlc`；parser-config repository 使用 `pgx/v5` 手写 SQL。
- repository 层负责把 PostgreSQL `jsonb` 转换为 Go `map[string]any`、`[]string` 或具体策略结构。
- `created_at`、`updated_at` 统一使用 UTC。
- 列表查询默认排除 `deleted_at IS NOT NULL` 的知识库和文档。
- 删除知识库时应先阻止或清理关联文档、切片和索引；当前 runtime 路径由 RAGFlow runtime 保证内部生命周期，早期表只保留迁移兼容和测试 fixture 价值。

## 10. 公开字段与内部字段

不得向前端暴露为权限依据的字段：

- `file_ref`
- `fileId`
- MinIO bucket、object key、presigned URL
- `parsedContent`
- runtime 内部文件路径、OCR debug output、PaddleOCR 原始 provider body
- `idempotencyKey`
- provider secret 或 secret 明文

可返回用于诊断或展示的字段：

- 文档 `status`、`errorCode`、`errorMessage`
- chunk `qdrantPointId`、`embeddingProvider`、`embeddingDimension`（当前为兼容/诊断字段）
- query `trace`

如果 gateway 公开契约没有这些字段，服务本地可先保留内部字段，但 browser-facing API 必须等 gateway OpenAPI 接收后再暴露。

当前 Go baseline 和早期 migration 中如果仍存在 `file_id` 或 `file_ref` 命名，应视为兼容期实现细节；当前 runtime 路径不要求公开 API 暴露 File Service 内部引用。

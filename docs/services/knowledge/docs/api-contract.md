# 知识管理 API 契约

## 1. 契约目标

本文定义知识管理模块的 HTTP API 契约，覆盖知识库、文档、文档处理、切片、检索、报告支撑材料和知识管理配置。该模块是智能问答和报告生成的数据底座。

主责服务：

- `knowledge`：知识库、文档上传公开资源、parser config 管理、RAGFlow runtime adapter、检索协调、原始文档内容入口和 MCP bridge。
- `knowledge-runtime`：Knowledge 的 RAGFlow runtime 实现细节，负责文档保存、解析、切块、embedding、索引和检索支持，不直接暴露给前端。
- `file`：后端内部基础文件对象能力；当前 Knowledge runtime 路径不直接调用 File Service，涉及 file 的历史契约只作为兼容和迁移背景。
- `auth`：用户、角色、权限和认证上下文。
- `gateway`：外部 API 入口。
- `ai-gateway`：统一提供 OpenAI-compatible 模型调用入口，供 embedding、rerank 和后续 LLM 能力使用。

边界原则：

- `qa`、`document` 只能通过本契约的 `knowledge-queries`、Knowledge MCP 或后续稳定资源复用知识能力，不能直接读取 runtime 数据库、MinIO、Redis 或索引后端。
- 原始文件和运行时索引细节留在 RAGFlow runtime 边界；Knowledge 公开响应不得依赖或暴露 bucket、object key、MinIO URL、签名 URL、存储 backend、runtime 内部 URL、provider 原始响应或凭据。
- Knowledge Go adapter 负责项目契约、权限上下文、错误归一化和 parser config 管理；文档、chunks、embedding、索引和检索事实由 RAGFlow runtime 管理并经 adapter 映射。
- Runtime 内部可使用 PostgreSQL、Redis、MinIO 和 Elasticsearch/索引后端；这些依赖不是 gateway 或前端可见的业务事实来源。

技术基线：

- 后端通用技术栈、数据库、迁移、日志、配置、队列和测试规则以 [`docs/architecture/technology-decisions.md`](../../../architecture/technology-decisions.md) 为准。
- 文档解析、切块、embedding、索引和检索支持通过 RAGFlow runtime 接入；当前 runtime 默认 doc engine 为 Elasticsearch/索引后端。Knowledge Go 进程不引入 PaddleOCR/PaddlePaddle/OpenCV/CUDA 运行时依赖，也不维护 Go Qdrant client 或 Go ingestion worker。
- embedding、rerank 和后续 LLM 能力通过 AI Gateway profile 调用；Knowledge 不保存 provider API key 明文。

## 2. 通用约定

### 2.1 基础路径

外部 API 统一使用 `/api/v1` 作为网关前缀：

```text
/api/v1/knowledge-bases/...
/api/v1/documents/...
/api/v1/knowledge-queries
```

Knowledge 公开资源在服务内部映射到 `knowledge` adapter 和 RAGFlow runtime；File Service 只作为其他 file-backed owner resource 的基础能力，不参与当前 Knowledge 文档上传/content 主路径。前端只感知网关路径。

### 2.2 RESTful + OpenAPI + Swagger UI 规范

RESTful 路径、动作词限制、分页、时间和通用 OpenAPI 协作规则以 [`docs/architecture/frontend-backend-contract.md`](../../../architecture/frontend-backend-contract.md) 为准。本文只记录 Knowledge 资源如何落到这些通用规则上：`/knowledge-bases`、`/documents`、`/knowledge-queries`、job、attempt 和 query 等资源语义。

OpenAPI 约定：

- `knowledge` 服务维护服务内契约：[`../api/internal.openapi.yaml`](../api/internal.openapi.yaml)。
- 当前文档配套的服务级公开草案见 [`../api/public.openapi.yaml`](../api/public.openapi.yaml)；前端稳定公开契约仍以 gateway public OpenAPI 为准。
- 涉及知识库文档上传和内容读取的公开接口由 `knowledge` 通过 gateway 维护；当前主路径由 Knowledge adapter 交给 RAGFlow runtime 保存和读取原始 bytes。`file` 仅提供其他 file-backed owner resource 的内部基础文件对象契约：[`../../file/api/internal.openapi.yaml`](../../file/api/internal.openapi.yaml)。公开 API 不要求前端先申请 file 上传 URL，也不暴露 file 内部 ID。
- OpenAPI 必须声明 `securitySchemes`、通用错误响应、分页响应、资源 schema、状态枚举和 SSE/异步任务说明。
- API 文档中的 request/response 示例应与本文 Markdown 契约保持一致。

Swagger UI 约定：

- 网关应暴露聚合入口，建议为 `/api/docs`。
- 服务级 OpenAPI JSON/YAML 建议暴露为 `/api/v1/knowledge/openapi.yaml` 或通过网关聚合。
- Swagger UI 只用于开发、测试和内网验收环境；生产环境是否开放需由部署配置控制。

### 2.3 认证与权限

Knowledge 首期统一采用 opaque Bearer token，不为知识管理 API 单独设计独立会话鉴权通道。角色能力、权限字符串、知识库可见性、parser config 管理权限和首期 RBAC 边界统一维护在 [Knowledge 权限矩阵](permission-matrix.md)。

当前权限预期：

- `standard` 默认具备 `knowledge:read`，可查看有权限知识库、文档、chunks、content，并可直接创建 `knowledge-queries`。
- 知识库创建/更新/删除、文档上传/更新/删除需要 `knowledge:write`、`knowledge:admin` 或 `system:admin`；Gateway 可先预拦截这些写入口，Knowledge 仍必须在服务边界复核。
- QA 项目级 RAG 是 `POST /knowledge-queries` 的受信任检索例外，不代表用户获得知识库或文档写权限，也不绕过文档详情、chunks 或 content 的读权限复核。
- Knowledge adapter 可通过 `KNOWLEDGE_PROJECT_RUNTIME_USER_ID` 使用项目级 runtime tenant 池；这只是 runtime 数据集上下文，不是权限提升。

### 2.4 通用响应结构

除 `204 No Content` 和原始文件二进制流外，browser-facing JSON 成功、分页和错误响应使用 gateway envelope；格式和通用错误码见 [`docs/architecture/frontend-backend-contract.md`](../../../architecture/frontend-backend-contract.md)。Knowledge 文档只补充知识库、文档、切片、检索和依赖失败的业务场景。

RAGFlow runtime 的认证失败、service token 错配或 runtime 内部租户上下文错误属于下游依赖或配置问题。经 Gateway/Knowledge 暴露给前端时应归一化为 `502 dependency_error`，不得把 runtime `401` 映射成用户登录态失效。

### 2.5 枚举

知识库文档类型：

```text
REGULATION        规程规范
TECHNICAL_REPORT  技术报告论文
TERM              术语条目
GENERAL           通用文档
SUPPORT_MATERIAL  报告支撑材料
```

分段策略：

```text
SEMANTIC_TEXT  语义文本切片
HEADING        基于标题层级智能分段
FIXED_SIZE     固定字符数分段
```

检索策略：

```text
VECTOR         语义向量检索
VECTOR_RERANK  向量检索 + 重排序
```

文档状态：

```text
uploaded
parsing
chunking
embedding
ready
failed
```

`deleted`、`indexing`、`reprocessing` 等只能作为内部状态、软删除字段或 job stage；进入公开 `DocumentStatus` 前必须先更新 gateway OpenAPI 和本文档。

处理任务状态：

```text
queued
running
succeeded
failed
cancelled
```

### 2.6 Runtime、检索与契约测试解耦

当前稳定交接面是 Knowledge adapter 对外暴露的 HTTP/MCP 契约，而不是某个
runtime 内部表、Redis 队列或索引 payload。单元测试和契约测试可以使用 fake
RAGFlow runtime、seeded repository 或 fake retrieval response 验证以下语义：

- `knowledge-queries` 输入、过滤、分页、错误 envelope 和 `X-Request-Id` 传播稳定。
- 文档状态、chunks/content 和检索结果必须映射为 gateway OpenAPI 中的公开字段。
- 无命中、低分、无权限、文档未 `ready` 或已删除时，返回稳定空结果或统一错误 envelope。
- 工具、HTTP 响应和日志不得泄露 runtime PostgreSQL、Redis、MinIO、Elasticsearch/索引后端、
  provider 原始响应、bucket/object key、service token 或完整文档内容。

历史 A-11/A-12/A-14 阶段曾用 `knowledge_documents`、`document_chunks` 和 fake
Qdrant hit 作为解耦 fixture。当前这些 fixture 仍可保留为快速契约测试输入，
但不能被误读为当前 Go adapter 直接写 Qdrant/File/asynq 的生产路径。完整端到端
smoke 仍应覆盖“Gateway 上传 -> adapter -> RAGFlow runtime worker -> 解析/切块/索引
-> `knowledge-queries` -> QA answer/citation”的真实依赖链路。

## 3. 数据对象

### 3.1 KnowledgeBase

```json
{
  "id": "kb_001",
  "name": "技术监督规程库",
  "description": "电厂技术监督相关规程和术语",
  "docType": "REGULATION",
  "visibility": "private",
  "chunkStrategy": {
    "type": "SEMANTIC_TEXT",
    "chunkSize": 1600,
    "overlap": 200,
    "separators": ["\n\n", "\n", "。"]
  },
  "retrievalStrategy": {
    "mode": "VECTOR_RERANK",
    "topK": 8,
    "scoreThreshold": 0.35,
    "rerankTopN": 5
  },
  "documentCount": 128,
  "chunkCount": 9800,
  "createdBy": "user_001",
  "createdAt": "2026-06-28T10:00:00Z",
  "updatedAt": "2026-06-28T10:00:00Z"
}
```

权限说明见 [Knowledge 权限矩阵](permission-matrix.md)；本文只保留 `private`、`team`、`public` 等资源字段的 schema 语义。

### 3.2 KnowledgeDocument

```json
{
  "id": "doc_001",
  "knowledgeBaseId": "kb_001",
  "name": "技术监督规程.pdf",
  "contentType": "application/pdf",
  "sizeBytes": 1024000,
  "status": "ready",
  "tags": ["锅炉", "2026"],
  "chunkCount": 86,
  "errorCode": null,
  "errorMessage": null,
  "parserBackend": "paddleocr",
  "createdBy": "user_001",
  "jobId": "job_001",
  "createdAt": "2026-06-28T10:00:00Z",
  "updatedAt": "2026-06-28T10:10:00Z"
}
```

### 3.3 DocumentChunk

```json
{
  "id": "chunk_001",
  "documentId": "doc_001",
  "knowledgeBaseId": "kb_001",
  "chunkIndex": 1,
  "sectionPath": "1. 总则 / 1.1 适用范围",
  "chunkType": "text",
  "content": "本规程适用于...",
  "tokenCount": 320,
  "metadata": {
    "page": 3
  },
  "createdAt": "2026-06-28T10:10:00Z"
}
```

### 3.4 KnowledgeQueryResult

```json
{
  "chunkId": "chunk_001",
  "documentId": "doc_001",
  "knowledgeBaseId": "kb_001",
  "documentName": "技术监督规程.pdf",
  "sectionPath": "1. 总则 / 1.1 适用范围",
  "score": 0.82,
  "contentPreview": "本规程适用于...",
  "chunkIndex": 1,
  "chunkType": "text",
  "tags": ["锅炉", "2026"]
}
```

## 4. 知识库 API

### 4.1 创建知识库

```http
POST /api/v1/knowledge-bases
```

请求：

```json
{
  "name": "技术监督规程库",
  "description": "电厂技术监督相关规程和术语",
  "docType": "REGULATION",
  "visibility": "private",
  "chunkStrategy": {
    "type": "HEADING",
    "chunkSize": 1200,
    "overlap": 200,
    "separators": ["\n\n", "\n", "。"]
  },
  "retrievalStrategy": {
    "mode": "VECTOR_RERANK",
    "topK": 8,
    "scoreThreshold": 0.35,
    "rerankTopN": 5
  }
}
```

响应：`201 Created`

```json
{
  "data": {
    "id": "kb_001",
    "name": "技术监督规程库",
    "docType": "REGULATION",
    "visibility": "private",
    "documentCount": 0,
    "chunkCount": 0,
    "createdAt": "2026-06-28T10:00:00Z"
  },
  "requestId": "req_123"
}
```

校验规则：

- `name` 必填，建议同一可见范围内唯一。
- `docType` 必须是允许枚举。
- `chunkStrategy.type=FIXED_SIZE` 时必须提供 `chunkSize` 和 `overlap`。
- `retrievalStrategy.mode=VECTOR_RERANK` 时需要存在可用重排序模型配置。

### 4.2 查询知识库列表

```http
GET /api/v1/knowledge-bases?page=1&pageSize=20
```

规则：

- 需要 `knowledge:read`。
- `pageSize` 当前上限为 100；超过上限返回 `validation_error`。

响应：

```json
{
  "data": [
    {
      "id": "kb_001",
      "name": "技术监督规程库",
      "docType": "REGULATION",
      "visibility": "private",
      "documentCount": 128,
      "chunkCount": 9800,
      "createdBy": "user_001",
      "createdAt": "2026-06-28T10:00:00Z"
    }
  ],
  "page": {
    "page": 1,
    "pageSize": 20,
    "total": 1
  },
  "requestId": "req_123"
}
```

### 4.3 获取知识库详情

```http
GET /api/v1/knowledge-bases/{knowledgeBaseId}
```

响应：`data` 为 `KnowledgeBase`

规则：需要 `knowledge:read` 且知识库对当前用户可见。

### 4.4 更新知识库

```http
PATCH /api/v1/knowledge-bases/{knowledgeBaseId}
```

请求：

```json
{
  "name": "技术监督规程库",
  "description": "更新后的描述",
  "chunkStrategy": {
    "type": "FIXED_SIZE",
    "chunkSize": 1200,
    "overlap": 200,
    "separators": ["\n\n", "\n", "。"]
  },
  "retrievalStrategy": {
    "mode": "VECTOR_RERANK",
    "topK": 10,
    "scoreThreshold": 0.35,
    "rerankTopN": 5
  }
}
```

响应：`data` 为 `KnowledgeBase`

状态影响：

- 需要 `knowledge:write`、`knowledge:admin` 或 `system:admin`。
- 分段策略变更后，所有 `ready` 文档需要进入后台重处理。
- 检索策略变更不一定需要重建向量，但如果影响 embedding 模型或向量维度，则必须重建索引。

### 4.5 删除知识库

```http
DELETE /api/v1/knowledge-bases/{knowledgeBaseId}
```

响应：`204 No Content`

规则：

- 删除前必须校验 `knowledge:write`、`knowledge:admin` 或 `system:admin`。
- 首期采用软删除；当前 adapter 将删除语义转给 RAGFlow runtime，runtime 内部对象、chunks 和索引生命周期不得泄露给 gateway。
- 删除知识库时应处理 runtime dataset/document/index 生命周期；bucket、object key 和底层对象删除策略由 runtime 或对应 owner service 独占。

### 4.6 创建知识库删除任务

候选扩展接口，尚未进入 gateway active public OpenAPI；进入公开契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。

```http
POST /api/v1/knowledge-base-deletion-jobs
```

请求：

```json
{
  "ids": ["kb_001", "kb_002"]
}
```

响应：

```json
{
  "data": {
    "id": "kbdel_001",
    "status": "queued",
    "targetIds": ["kb_001", "kb_002"],
    "failed": [
      {
        "id": "kb_002",
        "code": "forbidden",
        "message": "no permission"
      }
    ]
  },
  "requestId": "req_123"
}
```

规则：

- 批量删除建模为 deletion job 资源，不使用 `batch-delete` 动作路径。

## 5. 文档 API

### 5.1 上传文档

```http
POST /api/v1/knowledge-bases/{knowledgeBaseId}/documents
```

请求使用 `multipart/form-data`。Knowledge adapter 创建知识库文档资源，并通过 RAGFlow runtime 保存原始文件、触发解析和索引；当前路径不直接调用 File Service。上传需要 `knowledge:write`、`knowledge:admin` 或 `system:admin`，普通 `knowledge:read` 用户不能上传。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | binary | 是 | 原始文件。 |
| `tags` | string[]/json string | 否 | 文档标签；multipart 中可用 JSON 字符串编码。 |

响应：`201 Created`

```json
{
  "data": {
    "id": "doc_001",
    "knowledgeBaseId": "kb_001",
    "name": "技术监督规程.pdf",
    "status": "uploaded",
    "jobId": "job_001",
    "createdAt": "2026-06-28T10:00:00Z"
  },
  "requestId": "req_123"
}
```

### 5.2 查询文档列表

```http
GET /api/v1/knowledge-bases/{knowledgeBaseId}/documents?page=1&pageSize=20&status=ready&q=规程
```

规则：

- 需要 `knowledge:read` 且知识库对当前用户可见。
- `pageSize` 当前上限为 100；超过上限返回 `validation_error`。

响应：

```json
{
  "data": [
    {
      "id": "doc_001",
      "name": "技术监督规程.pdf",
      "status": "ready",
      "tags": ["锅炉", "2026"],
      "chunkCount": 86,
      "createdAt": "2026-06-28T10:00:00Z"
    }
  ],
  "page": {
    "page": 1,
    "pageSize": 20,
    "total": 1
  },
  "requestId": "req_123"
}
```

### 5.3 获取文档详情

```http
GET /api/v1/documents/{documentId}?knowledgeBaseId=kb_001
```

响应：`data` 为 `KnowledgeDocument`

规则：需要 `knowledge:read`；`knowledgeBaseId` 是必填查询参数，用于定位 runtime dataset，不允许由 adapter 扫描所有知识库推断。

### 5.4 更新文档标签

```http
PATCH /api/v1/documents/{documentId}?knowledgeBaseId=kb_001
```

请求：

```json
{
  "tags": ["锅炉", "2026"]
}
```

响应：`data` 为 `KnowledgeDocument`

规则：需要 `knowledge:write`、`knowledge:admin` 或 `system:admin`。

### 5.5 删除文档

```http
DELETE /api/v1/documents/{documentId}?knowledgeBaseId=kb_001
```

响应：`204 No Content`

规则：

- 删除文档必须同步完成 Knowledge adapter 的软删除语义，并转交 runtime 处理内部文档、chunks 和索引生命周期。
- 删除文档需要 `knowledge:write`、`knowledge:admin` 或 `system:admin`，且必须携带 `knowledgeBaseId`。
- 如果历史问答引用了该文档，引用详情应返回“原文已删除或无权限访问”的 fallback。
- 历史 delete cleanup worker、`file_ref` 和 Qdrant point 清理只适用于旧 Go worker 路径；当前实现不得把这些内部细节暴露给前端或作为 runtime 成功条件。
- Runtime 内部对象或索引不存在、重复删除和权限隐藏都应按幂等/脱敏语义处理，不恢复文档可见性。

### 5.6 创建文档删除任务

候选扩展接口，尚未进入 gateway active public OpenAPI；进入公开契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。

```http
POST /api/v1/document-deletion-jobs
```

请求：

```json
{
  "ids": ["doc_001", "doc_002"]
}
```

响应结构同知识库删除任务。

### 5.7 创建文档处理任务

候选扩展接口，尚未进入 gateway active public OpenAPI；进入公开契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。

```http
POST /api/v1/documents/{documentId}/processing-jobs
```

响应：

```json
{
  "data": {
    "id": "job_002",
    "documentId": "doc_001",
    "status": "queued"
  },
  "requestId": "req_123"
}
```

规则：

- 仅 `failed` 或管理员允许重处理的状态可创建新的处理任务。
- 重新处理需要保留上一次失败原因供排查。
- 自动尝试已满 3 次后仍允许管理员通过任务或任务尝试资源手动排队。

### 5.8 获取文档切片

```http
GET /api/v1/documents/{documentId}/chunks?knowledgeBaseId=kb_001&page=1&pageSize=50
```

响应：

```json
{
  "data": [
    {
      "id": "chunk_001",
      "chunkIndex": 1,
      "sectionPath": "1. 总则",
      "chunkType": "text",
      "content": "本规程适用于...",
      "tokenCount": 320
    }
  ],
  "page": {
    "page": 1,
    "pageSize": 50,
    "total": 86
  },
  "requestId": "req_123"
}
```

规则：

- 需要 `knowledge:read`；`knowledgeBaseId` 是必填查询参数。
- `pageSize` 当前上限为 100；超过上限返回 `validation_error`。

### 5.9 读取原文内容

```http
GET /api/v1/documents/{documentId}/content?knowledgeBaseId=kb_001
```

响应：原始文件二进制流。

规则：

- 必须先校验文档访问权限。
- `knowledgeBaseId` 是必填查询参数；QA citation 原文下载也必须保存并传递该字段。
- 不得返回 `file_ref`、file 内部 ID、bucket、object key、MinIO URL、签名 URL 或内部存储 URL。
- 审计日志首期暂缓，后续可接入独立审计服务。

## 6. 文档处理任务 API

本节为候选扩展接口，尚未进入 gateway active public OpenAPI；当前服务内实现优先走 [`../api/internal.openapi.yaml`](../api/internal.openapi.yaml) 的 `/internal/v1/**` contract。进入 browser-facing 契约前必须先更新 gateway OpenAPI。

### 6.1 获取处理任务

```http
GET /api/v1/knowledge-processing-jobs/{jobId}
```

响应：

```json
{
  "data": {
    "id": "job_001",
    "documentId": "doc_001",
    "status": "running",
    "stage": "embedding",
    "attemptCount": 1,
    "maxAttempts": 3,
    "progress": {
      "current": 42,
      "total": 86
    },
    "errorMessage": null,
    "attempts": [
      {
        "attempt": 1,
        "stage": "embedding",
        "status": "running",
        "startedAt": "2026-06-28T10:04:00Z"
      }
    ],
    "createdAt": "2026-06-28T10:00:00Z",
    "updatedAt": "2026-06-28T10:05:00Z"
  },
  "requestId": "req_123"
}
```

规则：

- PostgreSQL 中的 processing job 是权威状态；Redis 只用于队列投递、短期进度和并发协调。
- 自动尝试最多 3 次，超过后进入 `failed`；手动排队通过 `POST /api/v1/knowledge-processing-jobs/{jobId}/attempts` 创建新的任务尝试并递增 `attemptCount`。
- `attempts` 最多返回最近 10 次尝试摘要，包含阶段、状态、错误信息和时间字段。

### 6.2 创建知识库处理任务

```http
POST /api/v1/knowledge-bases/{knowledgeBaseId}/processing-jobs
```

请求：

```json
{
  "documentIds": ["doc_001"],
  "reason": "segmentation_changed"
}
```

响应：

```json
{
  "data": [
    {
      "id": "job_010",
      "documentId": "doc_001",
      "status": "queued"
    }
  ],
  "requestId": "req_123"
}
```

### 6.3 创建处理任务尝试

```http
POST /api/v1/knowledge-processing-jobs/{jobId}/attempts
```

响应：

```json
{
  "data": {
    "id": "attempt_002",
    "processingJobId": "job_001",
    "attempt": 2,
    "status": "queued"
  },
  "requestId": "req_123"
}
```

## 7. 知识查询 API

### 7.1 创建知识查询

```http
POST /api/v1/knowledge-queries
```

请求：

```json
{
  "query": "锅炉技术监督有哪些检查要求？",
  "knowledgeBaseIds": ["kb_001", "kb_002"],
  "topK": 8,
  "scoreThreshold": 0.35,
  "tags": ["锅炉", "2026"],
  "metadataFilter": {
    "专业": "锅炉",
    "年份": "2026"
  },
  "rerank": true,
  "rerankTopN": 5
}
```

响应：

```json
{
  "data": {
    "id": "kq_001",
    "query": "锅炉技术监督有哪些检查要求？",
    "results": [
      {
        "chunkId": "chunk_001",
        "documentId": "doc_001",
        "knowledgeBaseId": "kb_001",
        "documentName": "技术监督规程.pdf",
        "sectionPath": "1. 总则 / 1.1 适用范围",
        "score": 0.82,
        "contentPreview": "本规程适用于...",
        "chunkIndex": 1,
        "chunkType": "text",
        "tags": ["锅炉", "2026"]
      }
    ],
    "trace": {
      "embeddingProvider": "ai-gateway",
      "embeddingModel": "embedding-model-name",
      "embeddingDimension": 1024,
      "qdrantCollection": "elasticsearch",
      "searchTopK": 8,
      "scoreThreshold": 0.35,
      "hitCount": 1,
      "rerank": true,
      "rerankTopN": 5
    }
  },
  "requestId": "req_123"
}
```

规则：

- 普通调用需要 `knowledge:read`，必须过滤用户无权限访问的知识库和文档。
- QA 受信任项目级检索只适用于创建 `knowledge-query` 资源；citation source lookup、chunk 原文和 document content 仍需按当前用户 `knowledge:read` 回到 Knowledge 复核。
- browser-facing API 返回 `contentPreview`，不得返回原始向量、完整 runtime index payload、prompt、`file_ref`、bucket、object key、MinIO URL、签名 URL 或 provider 原始响应体。`qdrantCollection` 是兼容字段名，当前 adapter 可填入 runtime doc engine 标识。
- `qa` 和 `document` 应通过该接口复用检索能力。
- 检索建模为创建 `knowledge-query` 资源，不使用 `search` 动作路径。

### 7.2 创建知识查询测试

管理员接口：

候选扩展接口，尚未进入 gateway active public OpenAPI；进入公开契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。

```http
POST /api/v1/knowledge-query-tests
```

请求同 `knowledge-queries`，可额外包含：

```json
{
  "name": "锅炉召回测试"
}
```

响应：

```json
{
  "data": {
    "id": "rt_001",
    "results": [],
    "createdAt": "2026-06-28T10:00:00Z"
  },
  "requestId": "req_123"
}
```

## 8. 报告支撑材料 API

报告支撑材料指报告生成复用的专业业务文档，例如厂级专业报告、技术文档、检查报告。它不是 UI 素材，也不是普通附件。

报告支撑材料首期作为候选扩展资源建模，尚未进入 gateway active public OpenAPI。当前 active 报告素材资源由 `document` 拥有，并在内部复用 File Service；如未来另设 Knowledge-owned 支撑材料，必须先明确它走 RAGFlow runtime 还是 File-backed 对象能力，不能复用旧 `file_ref` 假设或和普通知识库文档混淆。

### 8.1 创建报告支撑材料

```http
POST /api/v1/knowledge-support-materials
```

请求使用 `multipart/form-data`。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | binary | 是 | 支撑材料原始文件。 |
| `name` | string | 是 | 材料名称。 |
| `materialType` | string | 是 | 材料类型。 |
| `tags` | string[]/json string | 否 | 标签；multipart 中可用 JSON 字符串编码。 |

响应：

```json
{
  "data": {
    "id": "mat_001",
    "name": "某电厂迎峰度夏检查材料",
    "materialType": "plant_report",
    "status": "uploaded",
    "jobId": "job_020"
  },
  "requestId": "req_123"
}
```

### 8.2 查询报告支撑材料

```http
GET /api/v1/knowledge-support-materials?page=1&pageSize=20&type=plant_report&tag.专业=锅炉
```

响应：分页列表。

### 8.3 更新标签

```http
PATCH /api/v1/knowledge-support-materials/{materialId}
```

请求：

```json
{
  "tags": ["A电厂", "锅炉", "2026"]
}
```

### 8.4 删除材料

```http
DELETE /api/v1/knowledge-support-materials/{materialId}
```

响应：`204 No Content`

## 9. 配置 API

本节为候选扩展接口。当前公开 runtime/admin 配置能力以 gateway 的 admin-runtime-config 契约为准；Knowledge 仅维护 parser/processing 相关服务内配置。

### 9.1 获取知识管理配置

```http
GET /api/v1/knowledge-settings
```

响应：

```json
{
  "data": {
    "embeddingModel": {
      "provider": "ai-gateway",
      "profileId": "mp_embedding_default",
      "model": "embedding-model-name",
      "dimension": 1024
    },
    "rerankModel": {
      "provider": "ai-gateway",
      "profileId": "mp_rerank_default",
      "model": "rerank-model-name",
      "topN": 20
    },
    "parser": {
      "backend": "external_api",
      "baseUrl": "https://parser.example.com",
      "maxConcurrency": 4
    }
  },
  "requestId": "req_123"
}
```

### 9.2 更新知识管理配置

```http
PATCH /api/v1/knowledge-settings
```

请求：

```json
{
  "embeddingModel": {
    "provider": "ai-gateway",
    "profileId": "mp_embedding_default",
    "model": "embedding-model-name",
    "dimension": 1024
  },
  "rerankModel": {
    "provider": "ai-gateway",
    "profileId": "mp_rerank_default",
    "model": "rerank-model-name",
    "topN": 20
  },
  "parser": {
    "backend": "external_api",
    "baseUrl": "https://parser.example.com",
    "apiKey": "<write-only secret>",
    "timeoutSeconds": 120,
    "maxConcurrency": 4
  }
}
```

响应：

```json
{
  "data": {
    "updatedAt": "2026-06-28T10:00:00Z"
  },
  "requestId": "req_123"
}
```

规则：

- 模型配置中的 `profileId` 指向 AI Gateway 中的 embedding 或 rerank profile；`knowledge` 不保存 provider `baseUrl` 或 `apiKey`，也不直接适配多个模型供应商。
- 配置变更应记录变更人和时间。
- `parser.backend` 首期固定为 `external_api`；`parser.apiKey` 只允许写入，不允许明文读取。
- embedding 维度、模型族或 runtime doc engine 配置变化时必须通过 RAGFlow runtime 重建索引；旧索引保留到切换完成后清理。

## 10. 统计 API

候选扩展接口，尚未进入 gateway active public OpenAPI；进入公开契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。

```http
GET /api/v1/knowledge-statistics/overview
```

响应：

```json
{
  "data": {
    "knowledgeBaseCount": 12,
    "documentCount": 128,
    "chunkCount": 9800,
    "uploadTrend30d": [
      {
        "date": "2026-06-28",
        "count": 6
      }
    ]
  },
  "requestId": "req_123"
}
```

## 11. 存储与数据归属

| 数据 | 存储 | 所有者 |
| --- | --- | --- |
| 知识库元数据 | RAGFlow runtime dataset + adapter 映射；parser config 可选保存在 Knowledge PostgreSQL | `knowledge` |
| 文档元数据和状态 | RAGFlow runtime document/task 状态 + adapter 映射 | `knowledge` |
| 文件对象和内容读取授权 | RAGFlow runtime 内部对象存储；Knowledge 只返回公开文档内容流，不暴露 bucket、object key、storage backend 或凭据 | `knowledge-runtime` |
| 切片元数据 | RAGFlow runtime chunks + adapter 映射 | `knowledge-runtime` / `knowledge` |
| 向量和检索 payload | RAGFlow runtime doc engine（当前 Elasticsearch/索引后端） | `knowledge-runtime` |
| 处理任务状态 | RAGFlow runtime PostgreSQL/Redis/worker；Knowledge adapter 只暴露脱敏状态和公开枚举 | `knowledge-runtime` / `knowledge` |
| 模型配置 | PostgreSQL 保存业务默认参数和 AI Gateway profile 引用；provider 密钥由 AI Gateway 管理 | `knowledge` / `ai-gateway` |

## 12. 已确认决策与后续跟踪

| 编号 | 结论 |
| --- | --- |
| K1 | 首期采用角色级 RBAC 和知识库可见性，不做组织/电厂/专业多维权限。 |
| K2 | 报告支撑材料是独立资源，复用 `file` service 和必要的 `knowledge` 检索能力。 |
| K3 | 文档删除首期按软删除设计；当前 runtime adapter 路径由 RAGFlow runtime 处理内部对象和索引生命周期，旧 `file_ref`/Qdrant cleanup 仅为历史路径。 |
| K4 | 文档解析/OCR 当前由 RAGFlow runtime API/worker 提供；Knowledge Go 进程只管理 parser config 到 runtime `parser_config` 的映射。 |
| K5 | Knowledge Go adapter 不接入 asynq；runtime 内部使用 Redis/worker 协调任务，Document 等其他服务可继续使用 asynq。 |
| K6 | embedding 维度、模型族或 doc engine 变化后通过 RAGFlow runtime 重建索引。 |
| K7 | 任务自动重试最多 3 次，PostgreSQL job 保留最近 10 次尝试摘要。 |
| K8 | Owner service 不依赖 bucket 分类或 object key 规则；当前 Knowledge runtime 对象存储细节由 RAGFlow runtime 独占，File Service 的 bucket/object key 规则不进入 Knowledge 公开响应。 |
| K9 | 审计日志首期暂缓，不作为知识管理 API 的强制验收项；首期保留配置变更、任务失败和删除结果等排查字段。 |

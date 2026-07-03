# Knowledge MCP 工具接口与参数规范

## 1. 命名、版本与通用约定

本文是 Knowledge MCP 首期四个只读模型工具的目标字段级契约，用于约束 #528
工具 schema 和后续 #505 收敛实现。

> **当前状态（2026-07-03，`develop@ce0b4774`）**：当前代码已经有
> `services/knowledge/internal/mcp`，但实际 `tools/list` 仍发布
> `search_knowledge`、`answer_from_knowledge`、`list_knowledge_bases` 等 14 个原生
> 工具，并通过 `KNOWLEDGE_MCP_ADDR` 启动独立 Streamable HTTP 监听器。本文下表的
> `search`、`list_documents`、`get_document`、`get_chunk` 是四个只读模型工具的目标
> 收敛契约；当前运行拓扑、实际工具目录和缺口见
> [`mcp-server.md`](mcp-server.md)。

目标收敛后，Knowledge Server 在 MCP `tools/list` 中发布原生工具名，QA 默认以
alias `knowledge` 注册后向模型暴露带前缀名称：

| 原生工具名 | 模型侧默认名称 |
| --- | --- |
| `search` | `knowledge__search` |
| `list_documents` | `knowledge__list_documents` |
| `get_document` | `knowledge__get_document` |
| `get_chunk` | `knowledge__get_chunk` |

> **实现与配置前置条件**：本目标契约依赖
> [#505](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/505)
> 的 Knowledge MCP Server、QA discovery 和默认工具白名单变更。当前 `develop`
> 已有 Knowledge MCP server 基础实现，但默认 QA 知识检索路径仍保留内置
> `search_knowledge`；四个 `knowledge__*` 模型侧名称进入默认 Agent 配置前，已有
> 或自定义的 `enabledToolNames` 必须显式加入这四个名称，否则
> `Policy.FilterTools` 会将它们过滤掉。

所有输入均为 JSON object，字段使用 lowerCamelCase，且
`additionalProperties=false`。未知字段、类型错误、缺少必填项或越界值都返回
`validation_error`，不做静默纠正。字符串在执行前去除首尾空白；仅含空白的必填
标量字符串视为缺失。`knowledgeBaseIds` 是唯一的数组归一化例外：其中的字符串会
先 trim，空项被忽略，重复项去重。

四个工具均为只读、幂等、封闭世界调用：

```json
{
  "readOnlyHint": true,
  "destructiveHint": false,
  "idempotentHint": true,
  "openWorldHint": false
}
```

## 2. 通用输出结构

成功和失败共享以下顶层 schema：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `requestId` | string | 是 | 调用链 ID；优先沿用 `X-Request-Id`。 |
| `toolName` | string | 是 | Knowledge 收到的原生工具名，不包含 QA alias。 |
| `status` | `succeeded \| failed` | 是 | 工具执行状态。 |
| `data` | object | 成功时 | 各工具的数据结构，失败时省略。 |
| `error` | object | 失败时 | 安全错误结构，成功时省略。 |

错误对象：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `code` | string | 是 | 稳定机器错误码。 |
| `message` | string | 是 | 可交给模型的脱敏摘要。 |
| `fields` | object<string,string> | 否 | 仅参数错误使用的字段提示。 |

服务器在四个工具上声明的通用 output JSON Schema 为：

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["requestId", "toolName", "status"],
  "properties": {
    "requestId": {"type": "string"},
    "toolName": {"type": "string"},
    "status": {"type": "string", "enum": ["succeeded", "failed"]},
    "data": {"type": "object", "additionalProperties": true},
    "error": {
      "type": "object",
      "additionalProperties": false,
      "required": ["code", "message"],
      "properties": {
        "code": {"type": "string"},
        "message": {"type": "string"},
        "fields": {
          "type": "object",
          "additionalProperties": {"type": "string"}
        }
      }
    }
  },
  "oneOf": [
    {
      "properties": {"status": {"const": "succeeded"}},
      "required": ["data"],
      "not": {"required": ["error"]}
    },
    {
      "properties": {"status": {"const": "failed"}},
      "required": ["error"],
      "not": {"required": ["data"]}
    }
  ]
}
```

稳定错误码为 `validation_error`、`unauthorized`、`forbidden`、`not_found`、
`conflict`、`rate_limited`、`dependency_error`、`internal_error`。底层错误、凭据、SQL、内部 URL、
runtime doc engine、File 或 AI provider 原始响应不得进入输出。

失败示例：

```json
{
  "requestId": "req_01JZ8JH9N5",
  "toolName": "search",
  "status": "failed",
  "error": {
    "code": "validation_error",
    "message": "knowledge tool arguments are invalid",
    "fields": {
      "topK": "must be between 1 and 20"
    }
  }
}
```

## 3. `knowledge__search`

在固定 MCP caller 或项目级 QA RAG 知识库池中做语义检索，返回按相关度排序的安全 chunk 摘要和后续读取
所需的资源 ID。模型应优先调用此工具，再按需调用 `knowledge__get_chunk`。

这里的参数边界是 **MCP wrapper 有意收窄的模型工具契约**，不继承现有
`/knowledge-queries` HTTP API 的默认值。HTTP API 仍使用 `topK=10`、最大 `100`、
`scoreThreshold=0.35`；#505 的 `MCPToolService.search` 独立应用下表中的
`topK=5`、最大 `20`、`scoreThreshold=0` 和 `0..1` 边界，再把规范化后的显式值传给
Knowledge 检索服务。该限制用于约束单次 Agent 工具结果规模，不修改 REST 契约；
#505 的 MCP service/schema 测试必须锁定这些独立默认值和越界行为。

### 3.1 输入参数

| 字段 | 类型 | 必填 | 默认值 | 约束与语义 |
| --- | --- | --- | --- | --- |
| `query` | string | 是 | - | 非空自然语言查询。 |
| `knowledgeBaseIds` | string[] | 否 | `[]` | 限定检索范围；每项 trim 后，空 ID 被忽略、重复 ID 去重。归一化后为空表示使用项目级 QA RAG 知识库池。 |
| `topK` | integer | 否 | `5` | `1..20`，最大返回候选数。 |
| `scoreThreshold` | number | 否 | `0` | `0..1`，低于阈值的结果不返回。 |
| `rerank` | boolean | 否 | `false` | 是否请求 Knowledge 使用已配置 reranker。 |
| `rerankTopN` | integer | 否 | 未设置 | `1..topK`；限制进入 rerank/最终候选的数量。 |

输入 JSON Schema：

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "minLength": 1},
    "knowledgeBaseIds": {
      "type": "array",
      "items": {"type": "string"}
    },
    "topK": {"type": "integer", "minimum": 1, "maximum": 20, "default": 5},
    "scoreThreshold": {"type": "number", "minimum": 0, "maximum": 1, "default": 0},
    "rerank": {"type": "boolean", "default": false},
    "rerankTopN": {"type": "integer", "minimum": 1, "maximum": 20}
  }
}
```

`rerankTopN <= topK` 是 JSON Schema 之外的跨字段运行时约束。

### 3.2 成功数据

`data` 字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `query` | string | 是 | 规范化后的查询。 |
| `results` | SearchHit[] | 是 | 排序结果；无命中时为空数组。 |
| `hitCount` | integer | 是 | 本次返回的结果数。 |

`SearchHit`：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `rank` | integer | 是 | 从 1 开始的返回顺序。 |
| `score` | number | 是 | 最终相关度分数。 |
| `knowledgeBaseId` | string | 是 | 知识库 ID。 |
| `documentId` | string | 是 | 文档 ID。 |
| `documentName` | string | 是 | 用户可见文档名。 |
| `chunkId` | string | 是 | 可传给 `knowledge__get_chunk`。 |
| `chunkIndex` | integer | 否 | chunk 在文档中的序号。 |
| `chunkType` | string | 否 | chunk 类型，例如 `text`。 |
| `sectionPath` | string | 否 | 标题/章节路径。 |
| `contentPreview` | string | 是 | 脱敏的命中摘要。 |
| `tags` | string[] | 否 | 文档标签。 |

成功示例：

```json
{
  "requestId": "req_01JZ8JH9N5",
  "toolName": "search",
  "status": "succeeded",
  "data": {
    "query": "断路器合闸前检查什么",
    "results": [
      {
        "rank": 1,
        "score": 0.92,
        "knowledgeBaseId": "kb_safety",
        "documentId": "doc_manual",
        "documentName": "高压设备操作手册.pdf",
        "chunkId": "chunk_0042",
        "chunkIndex": 42,
        "chunkType": "text",
        "sectionPath": "第三章/合闸前检查",
        "contentPreview": "合闸前应确认保护装置、接地开关和指示状态……",
        "tags": ["安全规程"]
      }
    ],
    "hitCount": 1
  }
}
```

## 4. `knowledge__list_documents`

分页列出某个固定 MCP caller 或项目 runtime 身份可见知识库中的文档，可按处理状态过滤。

### 4.1 输入参数

| 字段 | 类型 | 必填 | 默认值 | 约束与语义 |
| --- | --- | --- | --- | --- |
| `knowledgeBaseId` | string | 是 | - | 要查看的知识库 ID。 |
| `status` | string | 否 | 未设置 | `uploaded`、`parsing`、`chunking`、`embedding`、`ready`、`failed`。 |
| `page` | integer | 否 | `1` | 最小值 `1`。 |
| `pageSize` | integer | 否 | `20` | `1..100`。 |

输入 JSON Schema：

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["knowledgeBaseId"],
  "properties": {
    "knowledgeBaseId": {"type": "string", "minLength": 1},
    "status": {
      "type": "string",
      "enum": ["uploaded", "parsing", "chunking", "embedding", "ready", "failed"]
    },
    "page": {"type": "integer", "minimum": 1, "default": 1},
    "pageSize": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}
  }
}
```

### 4.2 成功数据

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `knowledgeBaseId` | string | 是 | 本次查询的知识库。 |
| `documents` | DocumentSummary[] | 是 | 当前页文档；无结果时为空数组。 |
| `totalCount` | integer | 是 | 过滤后的文档总数。 |
| `page` | integer | 是 | 当前页。 |
| `pageSize` | integer | 是 | 页大小。 |

成功示例：

```json
{
  "requestId": "req_01JZ8K3D1A",
  "toolName": "list_documents",
  "status": "succeeded",
  "data": {
    "knowledgeBaseId": "kb_safety",
    "documents": [
      {
        "id": "doc_manual",
        "knowledgeBaseId": "kb_safety",
        "name": "高压设备操作手册.pdf",
        "status": "ready",
        "tags": ["安全规程"],
        "chunkCount": 126,
        "createdAt": "2026-07-03T09:30:00Z",
        "updatedAt": "2026-07-03T09:32:10Z"
      }
    ],
    "totalCount": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

## 5. `knowledge__get_document`

读取单篇固定 MCP caller 或项目 runtime 身份可见文档的安全元数据与处理状态。

### 5.1 输入参数与 schema

| 字段 | 类型 | 必填 | 约束与语义 |
| --- | --- | --- | --- |
| `documentId` | string | 是 | 非空 Knowledge 文档 ID。 |

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["documentId"],
  "properties": {
    "documentId": {
      "type": "string",
      "minLength": 1,
      "description": "Knowledge document ID."
    }
  }
}
```

### 5.2 `DocumentSummary`

`get_document.data` 以及 `list_documents.data.documents[]` 使用同一结构：

MCP 不直接复用 OpenAPI 中 `updatedAt` 可空的 REST `DocumentSummary`。#505 定义独立
`MCPDocumentSummary`，从 Knowledge service 的非指针 `KnowledgeDocument.UpdatedAt`
映射并统一格式化为 UTC RFC 3339，因此 MCP 输出中的 `updatedAt` 是必填字符串；
这不会收紧现有 REST response schema。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | string | 是 | 文档 ID。 |
| `knowledgeBaseId` | string | 是 | 所属知识库 ID。 |
| `name` | string | 是 | 用户可见文档名。 |
| `status` | string | 是 | 文档处理状态。 |
| `tags` | string[] | 否 | 文档标签。 |
| `chunkCount` | integer | 是 | 已持久化 chunk 数。 |
| `errorCode` | string | 否 | 处理失败时的稳定错误码。 |
| `errorMessage` | string | 否 | 仅在 `failed` 状态返回固定脱敏摘要。 |
| `createdAt` | RFC 3339 string | 是 | 创建时间，UTC 输出。 |
| `updatedAt` | RFC 3339 string | 是 | 最后更新时间，UTC 输出。 |

成功示例：

```json
{
  "requestId": "req_01JZ8M1N8Q",
  "toolName": "get_document",
  "status": "succeeded",
  "data": {
    "id": "doc_manual",
    "knowledgeBaseId": "kb_safety",
    "name": "高压设备操作手册.pdf",
    "status": "ready",
    "tags": ["安全规程"],
    "chunkCount": 126,
    "createdAt": "2026-07-03T09:30:00Z",
    "updatedAt": "2026-07-03T09:32:10Z"
  }
}
```

输出不包含 `fileRef`、content type、对象存储信息、parser 内部配置或当前 job 内部字段。

## 6. `knowledge__get_chunk`

读取一个固定 MCP caller 或项目 runtime 身份可见 chunk 的完整文本。通常只应使用
`knowledge__search.results[].chunkId` 作为输入，避免模型猜测 ID。

`chunkId` 不是授权凭证。实现必须使用带当前用户 `AccessScope` 的 chunk/document
查询，并同时校验所属知识库和文档未删除、调用者可读且文档状态为 `ready`；在组装
`documentName`、`tags` 等字段时还应通过 scoped `GetDocument` 做纵深校验。严禁直接
复用仅按 `id = ANY(...)` 取行的 `FindChunksByIDs` 返回完整 `content`，因为该查询是
检索 hydrate 的内部批处理路径，自身不承担 owner、软删除或状态过滤。不可见、已删除
或非 `ready` 的资源不得返回正文，并按统一安全错误语义处理。

### 6.1 输入参数与 schema

| 字段 | 类型 | 必填 | 约束与语义 |
| --- | --- | --- | --- |
| `chunkId` | string | 是 | 非空 Knowledge chunk ID。 |
| `documentId` | string | 是 | `knowledge__search` 同一条命中的 document ID，用于 scoped chunk 查询。 |

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["chunkId", "documentId"],
  "properties": {
    "chunkId": {
      "type": "string",
      "minLength": 1,
      "description": "Knowledge chunk ID from a search result."
    },
    "documentId": {
      "type": "string",
      "minLength": 1,
      "description": "Knowledge document ID from the same search result."
    }
  }
}
```

### 6.2 成功数据

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `chunkId` | string | 是 | chunk ID。 |
| `documentId` | string | 是 | 所属文档 ID。 |
| `documentName` | string | 是 | 用户可见文档名。 |
| `knowledgeBaseId` | string | 是 | 所属知识库 ID。 |
| `chunkIndex` | integer | 是 | chunk 在文档中的序号。 |
| `chunkType` | string | 否 | chunk 类型。 |
| `sectionPath` | string | 否 | 标题/章节路径。 |
| `content` | string | 是 | 完整 chunk 文本。 |
| `tags` | string[] | 否 | 所属文档标签。 |

成功示例：

```json
{
  "requestId": "req_01JZ8N0V4R",
  "toolName": "get_chunk",
  "status": "succeeded",
  "data": {
    "chunkId": "chunk_0042",
    "documentId": "doc_manual",
    "documentName": "高压设备操作手册.pdf",
    "knowledgeBaseId": "kb_safety",
    "chunkIndex": 42,
    "chunkType": "text",
    "sectionPath": "第三章/合闸前检查",
    "content": "合闸前应确认保护装置、接地开关和指示状态均符合操作票要求。",
    "tags": ["安全规程"]
  }
}
```

输出不包含 token count、embedding provider/model/dimension、向量、runtime index point ID、
内部 metadata 或文件存储引用。

## 7. Agent 选用规则

| 用户意图 | 首选工具 | 后续动作 |
| --- | --- | --- |
| 基于知识库回答事实问题 | `knowledge__search` | 摘要不足时用命中的 `chunkId` 调 `knowledge__get_chunk`。 |
| 查看某知识库有哪些文档 | `knowledge__list_documents` | 按需用 `documentId` 查看详情。 |
| 查看文档是否已处理完成 | `knowledge__get_document` | `status=ready` 后再检索；失败时只陈述安全摘要。 |
| 展开某条检索命中的完整上下文 | `knowledge__get_chunk` | 将内容用于回答并保留 document/chunk citation。 |

Agent 不应枚举或猜测资源 ID，不应把 `get_chunk` 当作无界全文下载工具，也不应把
模型生成的用户身份或权限放入工具参数；身份只能来自 QA 注入的受信任请求上下文。

`knowledge__search` 是 citation-producing tool。#505 的 QA 消费方必须同步更新
`services/qa/internal/service/citations.go`，让 citation 工具名识别覆盖 alias-qualified
`knowledge__search`（以及受支持的 Knowledge alias 形式），并以 MCP
`data.results[]` 的 `knowledgeBaseId`、`documentId`、`chunkId`、`documentName`、
`contentPreview` 等字段生成 citation snapshot。consumer 测试必须使用
`knowledge__search` 工具消息断言引用能够落库/展示，不能只保留旧
`search_knowledge` 用例。

## 8. 兼容性检查清单

- `tools/list` 必须同时返回四个原生工具及 input/output schema。
- QA 默认 alias 后的目标工具名必须与本文一致；在 #505 完成收敛前，不得把本文
  误读为当前 `develop` 已默认启用这些 `knowledge__*` 工具。
- 历史或自定义 QA Agent 配置的 `enabledToolNames` 必须显式包含
  `knowledge__search`、`knowledge__list_documents`、`knowledge__get_document` 和
  `knowledge__get_chunk`；新默认配置由 #505 提供这些名称。
- QA citation 识别和 consumer 测试必须覆盖 `knowledge__search`，确保 MCP 检索结果
  继续生成 document/chunk citation snapshot。
- 未提供可选参数时，默认值必须与本文一致。
- 所有 object 输入拒绝未知字段。
- 搜索和列表的空结果必须返回空数组而不是 `null`。
- 成功输出不得出现 `error`，失败输出不得出现 `data`。
- `status=failed` 时 MCP `isError` 必须为 `true`。
- 私有/不可见资源必须在 Knowledge 服务层完成鉴权，不能依赖 Agent 自律。
- `get_chunk` 必须走 `AccessScope` + 未删除 + `ready` 状态过滤，禁止使用
  `FindChunksByIDs` 作为面向模型的正文读取路径。
- schema 或字段语义发生破坏性变化时，必须同步 QA 消费方测试并引入版本迁移。

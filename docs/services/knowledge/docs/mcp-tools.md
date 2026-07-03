# Knowledge MCP 工具接口与参数规范

## 1. 当前工具目录

Knowledge MCP 当前发布四个只读原生工具。QA 以 alias `knowledge` 注册该 MCP
server 时，模型侧看到的是带前缀的工具名：

| 原生工具名 | 模型侧默认名称 | 用途 |
| --- | --- | --- |
| `search` | `knowledge__search` | 语义检索知识库 chunk 摘要 |
| `list_documents` | `knowledge__list_documents` | 分页查看知识库文档 |
| `get_document` | `knowledge__get_document` | 查看单篇文档元数据与处理状态 |
| `get_chunk` | `knowledge__get_chunk` | 读取单个 chunk 正文 |

这些工具由 `services/knowledge/internal/mcp.ToolCatalog()` 定义，当前没有发布写入型工具、
`answer_from_knowledge`、全文下载工具或 Knowledge-local chat 工具。

## 2. 通用约定

- 传输使用 MCP Streamable HTTP，访问令牌 header 固定为 `X-Service-Token`。
- 客户端只能透传 `X-Request-Id`；服务端不信任客户端提供的 `X-User-Id`、
  `X-User-Roles` 或 `X-User-Permissions`。
- 工具成功时，业务结果直接位于 MCP `structuredContent`，没有额外
  `requestId/toolName/status/data/error` 自定义 envelope。
- 工具失败时返回 MCP `CallToolResult.isError=true`。错误信息必须保持脱敏，不能包含
  SQL、凭据、内部 URL、object key、向量、provider 原始响应或原始文档全文。
- 工具 schema 由 Go SDK 根据 handler 输入类型生成。必填字段由 `jsonschema` tag
  和 handler 运行时校验共同约束；更细的分页、权限和资源可见性校验由 Knowledge
  REST adapter 与 runtime 承担。

## 3. `knowledge__search`

按固定 MCP caller 可见范围执行 Knowledge 查询，返回可用于 citation 的命中摘要。

### 输入

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `query` | string | 是 | 检索问题或关键词，空白字符串会被拒绝。 |
| `knowledgeBaseIds` | string[] | 否 | 限定知识库范围；为空时使用项目级可见范围。 |
| `documentIds` | string[] | 否 | 限定文档范围。 |
| `topK` | integer | 否 | 最大候选数；未提供时沿用 Knowledge query 默认值。 |
| `scoreThreshold` | number | 否 | 最小相似度阈值。 |
| `rerank` | boolean | 否 | 是否请求 Knowledge 使用已配置 reranker。 |
| `rerankTopN` | integer | 否 | rerank 后候选数量。 |
| `tags` | string[] | 否 | 文档标签过滤。 |
| `metadataFilter` | object<string,string> | 否 | metadata 过滤条件。 |

### 输出

`structuredContent` 形态：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `queryId` | string | Knowledge query ID。 |
| `results` | SearchHit[] | 命中结果；无命中时为空数组。 |

`SearchHit`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `score` | number | 最终相关度分数。 |
| `knowledgeBaseId` | string | 知识库 ID。 |
| `documentId` | string | 文档 ID。 |
| `documentName` | string | 用户可见文档名。 |
| `chunkId` | string | 可传给 `knowledge__get_chunk` 的 chunk ID。 |
| `contentPreview` | string | 命中摘要。 |
| `content` | string | 当前实现与 `contentPreview` 相同，用于兼容 citation consumer。 |
| `sectionPath` | string | 可选章节路径。 |
| `chunkIndex` | integer | 可选 chunk 序号。 |
| `chunkType` | string | 可选 chunk 类型。 |
| `tags` | string[] | 可选文档标签。 |

示例：

```json
{
  "queryId": "kq_01JZ8JH9N5",
  "results": [
    {
      "score": 0.92,
      "knowledgeBaseId": "kb_safety",
      "documentId": "doc_manual",
      "documentName": "高压设备操作手册.pdf",
      "chunkId": "chunk_0042",
      "chunkIndex": 42,
      "chunkType": "text",
      "sectionPath": "第三章/合闸前检查",
      "contentPreview": "合闸前应确认保护装置、接地开关和指示状态……",
      "content": "合闸前应确认保护装置、接地开关和指示状态……",
      "tags": ["安全规程"]
    }
  ]
}
```

## 4. `knowledge__list_documents`

分页列出某个知识库下的文档。

### 输入

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `knowledgeBaseId` | string | 是 | 要查看的知识库 ID。 |
| `page` | integer | 否 | 页码。 |
| `pageSize` | integer | 否 | 页大小。 |
| `status` | string | 否 | 文档处理状态过滤。 |

### 输出

`structuredContent` 直接透出 adapter 列表结果：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `data` | object[] | 文档摘要数组。 |
| `page` | object | 可选分页信息，字段由 Knowledge REST adapter 返回。 |

文档摘要字段以 Knowledge REST adapter 当前 `DocumentSummary` 为准，常见字段包括
`id`、`knowledgeBaseId`、`name`、`status`、`tags`、`chunkCount`、`createdAt`、
`updatedAt`、`errorCode` 和 `errorMessage`。

## 5. `knowledge__get_document`

读取单篇文档的安全元数据与处理状态。

### 输入

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `documentId` | string | 是 | Knowledge 文档 ID。 |
| `knowledgeBaseId` | string | 否 | 搜索结果中的知识库 ID；提供后可帮助 adapter 做 scoped lookup。 |

### 输出

`structuredContent` 为 Knowledge REST adapter 返回的文档对象，常见字段包括
`id`、`knowledgeBaseId`、`name`、`status`、`tags`、`chunkCount`、`createdAt`、
`updatedAt`、`errorCode` 和 `errorMessage`。

输出不包含 `fileRef`、对象存储 key、runtime object key、parser 内部配置或当前 job
内部字段。

## 6. `knowledge__get_chunk`

读取单个 chunk 的正文。模型通常应先调用 `knowledge__search`，再使用同一命中的
`chunkId`、`documentId` 和 `knowledgeBaseId` 调用本工具。

### 输入

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `chunkId` | string | 是 | Knowledge chunk ID。 |
| `documentId` | string | 否 | 同一搜索命中的文档 ID；提供后 lookup 更精确。 |
| `knowledgeBaseId` | string | 否 | 同一搜索命中的知识库 ID；提供后 lookup 更精确。 |

### 输出

`structuredContent` 为 Knowledge REST adapter 返回的 chunk 对象。当前 handler 会确保
输出中存在 `chunkId` 字段。常见字段包括 `id`、`chunkId`、`documentId`、
`knowledgeBaseId`、`documentName`、`chunkIndex`、`chunkType`、`sectionPath`、
`content` 和 `tags`。

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
模型生成的用户身份或权限放入工具参数；身份只能来自 QA/Knowledge 配置的受信任
caller。

`knowledge__search` 是 citation-producing tool。QA citation consumer 必须识别
alias-qualified `knowledge__search`，并从 `structuredContent.results[]` 的
`knowledgeBaseId`、`documentId`、`chunkId`、`documentName`、`contentPreview` 等字段
生成 citation snapshot。

## 8. 兼容性检查清单

- `tools/list` 必须返回四个原生工具：`search`、`list_documents`、`get_document`、
  `get_chunk`。
- QA 默认 alias 后的目标工具名必须与本文一致。
- 历史或自定义 QA Agent 配置的 `enabledToolNames` 必须显式包含
  `knowledge__search`、`knowledge__list_documents`、`knowledge__get_document` 和
  `knowledge__get_chunk`。
- 搜索和列表的空结果必须返回空数组而不是 `null`。
- 私有/不可见资源必须由 Knowledge 服务层完成鉴权，不能依赖 Agent 自律。
- schema 或字段语义发生破坏性变化时，必须同步 Knowledge MCP 测试、QA
  discovery/fallback 测试、QA citation consumer 测试和本文档。

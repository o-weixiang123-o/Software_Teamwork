# Knowledge MCP Server 协议与运行说明

## 1. 当前边界

Knowledge MCP 是 `services/knowledge` contract adapter 内的 Streamable HTTP
服务器。它复用 adapter 的 Knowledge API、权限与错误处理，不允许 QA 直接访问
PostgreSQL、Elasticsearch、MinIO 或 Python Knowledge Runtime。

当前实现由 `services/knowledge/cmd/adapter` 启动两个监听器：

- `KNOWLEDGE_HTTP_ADDR`：Knowledge REST adapter，默认 `:8083`；
- `KNOWLEDGE_MCP_ADDR`：可选 MCP 监听地址；为空时不启动，local 模板使用
  `127.0.0.1:8093`。

两者属于同一宿主机进程，但不是同一端口。Gateway 不代理 MCP 监听器。

## 2. 工具目录与命名

Knowledge MCP 当前通过官方 Go SDK 发布以下原生工具：

| 分类 | 原生工具 |
| --- | --- |
| 检索与回答 | `search_knowledge`、`answer_from_knowledge` |
| 知识库 | `list_knowledge_bases`、`get_knowledge_base`、`create_knowledge_base`、`update_knowledge_base`、`delete_knowledge_base` |
| 文档 | `list_documents`、`get_document`、`create_document`、`update_document`、`delete_document` |
| 内容 | `list_document_chunks`、`get_document_content` |

QA 用 alias 隔离服务器命名空间。alias 为 `knowledge` 时，模型侧名称为
`knowledge__search_knowledge`、`knowledge__list_documents` 等，QA 调用前会移除
alias 前缀。

首期 QA 自动切换至少要求以下只读工具全部可发现：

- `search_knowledge`
- `list_documents`
- `get_document`
- `list_document_chunks`

任一工具缺失、initialize 失败或 `tools/list` 失败时，QA 关闭该 MCP session，并
保留内置 `search_knowledge` HTTP 工具作为检索回退。

> [MCP 工具字段规范](mcp-tools.md)记录 issue #528 提出的四工具目标契约。当前
> `develop` 已随 Knowledge Runtime adapter 落地上述 14 工具目录，工具改名、缩减
> 或增加单 chunk 读取能力时，必须同时更新实现、QA allowlist、citation consumer 和
> 两份文档，不能把目标契约误写成当前事实。

## 3. 传输与鉴权

- 传输协议：MCP Streamable HTTP；
- 服务端：`github.com/modelcontextprotocol/go-sdk/mcp`；
- 访问令牌 header：固定为 `X-Service-Token`；
- 有效令牌：与 Knowledge adapter 的 `KNOWLEDGE_SERVICE_TOKEN` 一致；
- 未授权请求：MCP transport 处理前返回 HTTP `401`；
- 运行时不启用 stdio，不通过公共 Gateway 暴露。

当前 MCP listener 使用配置生成的受信任 caller：

| 变量 | 用途 |
| --- | --- |
| `KNOWLEDGE_MCP_USER_ID` | adapter 内部请求的固定用户身份 |
| `KNOWLEDGE_PROJECT_RUNTIME_USER_ID` | 项目级 QA RAG 知识库池使用的 runtime 身份；默认继承 `KNOWLEDGE_MCP_USER_ID` |
| `KNOWLEDGE_MCP_ROLES` | 固定角色集合 |
| `KNOWLEDGE_MCP_PERMISSIONS` | 固定权限，默认 `knowledge:read` |

客户端只能透传 `X-Request-Id`。服务端不会信任客户端提供的
`X-User-Id`、`X-User-Roles` 或 `X-User-Permissions`，因此不能通过伪造 header
把只读 listener 提升为写权限。若后续改为最终用户级授权，必须先设计可验证的
Gateway/QA 身份签名或 token exchange，不能直接信任普通 HTTP header。

## 4. Agent 调用流程

```text
Knowledge adapter 启动
  -> KNOWLEDGE_MCP_ADDR 非空时启动 Streamable HTTP listener
  -> QA 使用 KNOWLEDGE_MCP_URL + service token initialize
  -> QA tools/list 并加 knowledge__ alias
  -> 校验四个必需只读工具
  -> 模型调用 knowledge__search_knowledge
  -> QA 移除 alias 并发送 tools/call
  -> Knowledge MCP 用固定 caller 调 adapter
  -> adapter 调 Knowledge Runtime 并返回 structuredContent
  -> QA 从检索结果生成 citation snapshot
  -> 模型按需列文档或 chunk，最后生成回答
```

QA 的 `citations.go` 已识别原生 `search_knowledge` 以及
`*__search_knowledge` / `*.search_knowledge`，所以 alias 后的 MCP 检索仍会生成
document/chunk citation。

## 5. 输入、输出与错误

每个工具的 input/output schema 由 Go SDK 根据 handler 的类型生成，并通过
`tools/list` 发布。调用成功时业务结果位于 MCP `structuredContent`；失败时返回
`CallToolResult.isError=true`。当前实现不额外包裹
`requestId/toolName/status/data/error` 自定义 envelope。

`search_knowledge` 返回 `queryId` 和 `results[]`；每条结果包含 score、
knowledgeBaseId、documentId、chunkId、documentName、contentPreview，以及可选的
章节、chunk 序号、类型和 tags。

`answer_from_knowledge` 只有在 Knowledge adapter 配置 AI Gateway client 时可用，
并要求调用方提供 `modelProfileId`。QA 自己已经拥有回答循环，默认只把
`search_knowledge` 作为 citation-producing 检索工具。

## 6. 配置

Knowledge 侧：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `KNOWLEDGE_MCP_ADDR` | 空 | MCP 监听地址；local 模板为 `127.0.0.1:8093` |
| `KNOWLEDGE_MCP_USER_ID` | `knowledge_mcp_service` | 固定受信任 caller |
| `KNOWLEDGE_PROJECT_RUNTIME_USER_ID` | `KNOWLEDGE_MCP_USER_ID` | 项目级 QA RAG 知识库池使用的 runtime 身份 |
| `KNOWLEDGE_MCP_ROLES` | 空 | 固定角色 |
| `KNOWLEDGE_MCP_PERMISSIONS` | `knowledge:read` | 固定权限 |
| `KNOWLEDGE_SERVICE_TOKEN` | 无 | REST 与 MCP 共用的内部服务令牌 |

QA 侧：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `KNOWLEDGE_MCP_URL` | 空 | 非空时启用 MCP 优先；local 为 `http://localhost:8093/mcp` |
| `KNOWLEDGE_MCP_TOKEN` | `INTERNAL_SERVICE_TOKEN` | MCP 服务令牌 |
| `KNOWLEDGE_MCP_TOKEN_HEADER` | `X-Service-Token` | token header |
| `KNOWLEDGE_MCP_ALIAS` | `knowledge` | 模型侧命名空间 |
| `KNOWLEDGE_MCP_TIMEOUT` | `MCP_TOOL_TIMEOUT` | 单次工具调用上限 |

## 7. 安全与兼容性

- 写工具除 service token 外还要求受信任 caller 包含
  `knowledge:write`；local 默认 listener 只有读权限。
- 工具不得返回凭据、内部 URL、对象存储 key、向量或 provider 原始错误。
- `get_document_content` 返回 base64 原文，默认 QA allowlist 不应启用它。
- `search_knowledge` 是 citation-producing tool；改名时必须同步 QA citation 测试。
- QA 默认 allowlist、历史配置和自定义配置是独立状态；新增工具不会自动进入已有
  `enabledToolNames`。
- 工具目录、schema 或错误语义发生破坏性变化时，必须同步 Knowledge MCP 测试、
  QA discovery/fallback 测试和文档。

## 8. 联调检查

1. 启动 Knowledge adapter，确认日志包含 MCP listener 地址；
2. 无 token 或错误 token 调用 initialize，确认返回 `401`；
3. 正确 token 调用 `tools/list`，确认 14 个原生工具；
4. QA 使用 alias `knowledge` 后，确认模型看到
   `knowledge__search_knowledge` 等名称；
5. 缺少任一必需只读工具时，确认 QA 关闭 MCP client 并保留内置
   `search_knowledge`；
6. 使用检索工具回答，确认 citation snapshot 包含 knowledge base、document 和
   chunk ID；
7. 伪造用户或写权限 header，确认服务端仍使用配置中的固定 caller。

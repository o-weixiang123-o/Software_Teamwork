# QA 权限矩阵

本文档说明 `qa` 服务在会话、消息、回答运行、引用、配置、检索测试和指标上的权限边界。稳定公开路径以 Gateway OpenAPI 为准。

## 权限来源

| 项 | 说明 |
| --- | --- |
| 认证入口 | 前端经 Gateway 调用，QA active paths 均需要 bearer auth。 |
| 用户上下文 | Gateway 注入 `X-User-Id`、`X-User-Roles`、`X-User-Permissions`。 |
| 角色 | `standard`、`admin`、`super_admin`。 |
| 权限字符串 | `qa:use`、`qa:settings:read`、`qa:settings:write`，以及被 MCP 工具消费的跨服务权限。 |
| 资源事实 | QA PostgreSQL 保存 conversation、message、response run、tool call、citation、settings、test run 和 metrics facts。 |
| 下游权限 | Knowledge、Document、File、AI Gateway 和 MCP server 仍在各自边界校验资源权限。 |

## 业务能力矩阵

| 能力 | `standard` | `admin` | `super_admin` | 额外约束 |
| --- | --- | --- | --- | --- |
| 创建和查看自己的 QA 会话 | 允许，需 `qa:use`。 | 允许。 | 允许。 | 标准用户只能访问自己的会话。 |
| 更新和删除自己的 QA 会话 | 允许，需 `qa:use`。 | 允许。 | 允许。 | 标准用户只能管理自己的会话。 |
| 查看和软删除全站 QA 会话 | 不允许。 | 已决策但未落地。 | 已决策但未落地。 | 当前普通 `qa-sessions/{sessionId}` active paths 仍按 owner-only 执行；落地前必须补独立管理路径、Gateway OpenAPI、实现、测试和审计。 |
| 创建消息和回答运行 | 允许，需 `qa:use`。 | 允许。 | 允许。 | 工具可见性必须按配置、白名单和用户权限裁剪。 |
| 查看 response run 和 tool calls | 允许，仅限自己会话关联资源。 | 允许，默认仍按 owner 约束。 | 允许，默认仍按 owner 约束。 | 不返回完整 prompt、chain-of-thought、MCP 原始参数或完整工具结果。 |
| 查看引用和 citation lookup | 允许，仅限自己消息或可访问引用。 | 允许。 | 允许。 | 知识库原文 hydrate 必须回 Knowledge 复核权限；file-backed 引用回到对应 owner 服务复核。 |
| 上传和删除 QA 会话附件 | 允许，仅限自己会话。 | 允许。 | 允许。 | QA 拥有附件元数据、解析状态和临时 chunks；File 保存原始 bytes；不得恢复独立 Parser 或写入 Knowledge 长期索引。 |
| 读取当前 QA/LLM settings | 默认不允许，除非授予 `qa:settings:read`。 | 允许。 | 允许。 | 管理端响应可包含完整 `systemPrompt`，但必须脱敏模型和 provider secret；普通 QA 公开资源仍禁止返回完整提示词。 |
| 创建 QA/LLM settings version | 默认不允许，除非授予 `qa:settings:write`。 | 允许。 | 允许。 | 可写入版本化 `systemPrompt`；admin audit logs 只记录长度和配置元数据，不保存完整提示词。 |
| LLM connection test | 默认不允许，除非授予 `qa:settings:write`。 | 允许。 | 允许。 | 不返回 provider raw body 或 secret。 |
| Retrieval test runs | 默认不允许，除非授予管理权限。 | 允许。 | 允许。 | Knowledge retrieval 仍过滤无权限知识库和文档。 |
| QA metrics | 默认不允许，除非授予管理权限。 | 允许。 | 允许。 | 指标不得包含 prompt、原始文档全文或敏感工具参数。 |

## 工具和服务间权限矩阵

| 调用目标 | QA 调用前必须校验 | 目标服务继续负责 |
| --- | --- | --- |
| AI Gateway chat | 用户可创建回答运行、profile/config 可用、调用摘要可审计。 | Provider 调用、API key 保护和错误归一化。 |
| MCP tools | 工具白名单、用户权限、参数 schema、超时、幂等键。 | 具体工具的业务权限和数据脱敏。 |
| Knowledge retrieval | 用户是否可在当前 QA 上下文使用检索工具；请求级 `knowledgeBaseIds` 必须受 QA 默认知识库 allowlist 约束。默认 allowlist 为空表示 QA 使用项目全局长期知识库，不要求最终用户具备 Knowledge 管理面的 `knowledge:read`。 | 知识库管理、文档管理、chunk 原文读取和直接 Knowledge 资源权限。 |
| Citation source / document download | QA 必须保存并传递引用的 `knowledgeBaseId`、`documentId`，只暴露当前用户通过 Knowledge 普通读权限可访问的来源。`standard` 角色默认具备 `knowledge:read`，因此普通用户可以查看引用来源和下载原文。 | Knowledge 复核 `knowledge:read`、具体知识库/文档可见性和 content/chunk 资源读取。 |
| Document report tools | 用户是否可调用报告工具。 | 报告、模板、文件、settings 和操作日志权限。 |
| File / QA attachment processing | 会话 owner、附件状态和本次消息绑定的附件 IDs。 | File 保存 bytes；QA 负责附件解析状态和会话临时 chunks；附件不得写入 Knowledge 长期索引。 |

QA 回答时长期 Knowledge RAG 和会话附件检索是互补来源：`search_knowledge` /
`knowledge__search` 查询长期知识库，`search_session_attachments` 只查询当前会话已绑定、
ready 的临时附件 chunks。上传到 QA 会话的文件不得因为被问答使用而自动进入 Knowledge
长期知识库；只有用户明确执行 Knowledge 文档上传/加入知识库流程时，才由 Knowledge
拥有长期文档、解析、embedding 和索引生命周期。

QA 配置的 `defaultKnowledgeBaseIds` 只表达默认检索范围：非空时限制模型的长期知识库检索
范围，空数组表示搜索项目全局长期知识库。QA 检索权限由 QA 使用权限授权，不能等同于
Knowledge 管理面的 `knowledge:read`。引用来源可见性不是检索权限的一部分，必须回到
Knowledge 以当前用户的 `knowledge:read` 权限复核；缺少 `knowledgeBaseId` 的历史引用应保留
快照但不提供原文下载。禁用长期 RAG 应通过工具白名单移除
`search_knowledge` / `knowledge__search`，不要把空默认知识库列表解释为“无可用知识库”。

## 拒绝规则

| 条件 | 响应 |
| --- | --- |
| 未认证 | `401 unauthorized`。 |
| 缺少 `qa:use` 或 settings 管理权限 | `403 forbidden`。 |
| 访问他人会话、消息、run 或附件 | `403 forbidden` 或隐藏为 `404 not_found`，按接口契约执行。 |
| 工具未授权、参数越界或策略拒绝 | `policy_denied` / `403 forbidden`，并写入脱敏工具摘要。 |
| 下游依赖失败 | `dependency_error`，不得返回 provider raw body、prompt、MCP 原始参数、object key 或内部 URL。 |

## Owner 隐藏规则

| 资源 | 非 owner 响应语义 | 说明 |
| --- | --- | --- |
| QA 会话详情、会话更新、会话删除、会话消息列表、消息创建 | 访问他人会话时返回 `403 forbidden`；会话不存在或已软删除时返回 `404 not_found`。 | 当前普通 QA session active paths 不允许管理员绕过 owner 检查；管理员跨用户查看和软删除必须通过后续独立管理路径落地。 |
| Message、response run、tool call、citation 单资源或子资源 | 标准用户非 owner 返回 `404 not_found`。 | 始终带当前用户 owner 过滤，不通过单资源响应泄露他人消息、run、tool call 或引用是否存在；管理员跨用户查看必须走明确的管理会话路径并写审计。 |
| Citation lookup 批量查询 | 只返回当前用户可见记录；被过滤 ID 不出现在结果和错误详情中。 | 不披露被省略 ID 是否存在或属于其他用户。 |
| QA 会话附件和临时附件 chunk | 返回 `404 not_found`。 | 附件非 owner 必须隐藏存在性；`file_ref`、object key、bucket 和内部 URL 不进入公开响应。 |

## 当前缺口

- 管理员跨用户 QA 会话查看和软删除是已决策但未落地能力；落地前需补独立管理路径、Gateway OpenAPI、实现、测试和审计，普通 QA session active paths 保持 owner-only。
- MCP 原始 tool schema、完整工具参数/结果和内部审计细节不属于前端稳定公开契约。
- QA 不保存用户、角色、权限源数据，只消费 Gateway 注入的上下文。

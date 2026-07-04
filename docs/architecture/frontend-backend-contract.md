# 前后端集成契约

本文档定义 frontend 与 gateway 的基础集成约定。详细 endpoint 以 [`docs/services/gateway/api/public.openapi.yaml`](../services/gateway/api/public.openapi.yaml) 为准。

## API 入口

前端只调用 gateway：

```text
/api/v1
```

前端不得直接调用 `auth`、`file`、`knowledge`、`qa`、`document`、`ai-gateway` 的内部地址。内部服务地址只应存在于 gateway、领域服务或部署配置中。

管理端、其他后端模块和 MCP 工具等 HTTP 调用方同样必须通过 gateway `/api/v1` 访问公开业务接口，不得绕过 gateway 直连内部服务。

AI Gateway 是内部模型服务，只提供 `/internal/v1/**` 给 `qa`、`knowledge`、`document` 和 public `gateway` 等后端服务使用。前端即使需要问答、报告生成或模型配置能力，也必须先调用 public gateway 的 `/api/v1/**`，不能直接调用 AI Gateway 的 OpenAI-compatible endpoint 或内部配置 endpoint。

## OpenAPI 作为协作源

- `docs/services/gateway/api/public.openapi.yaml` 是前端与 gateway 的第一版契约源。
- [`docs/services/gateway/docs/active-api-owner-map.md`](../services/gateway/docs/active-api-owner-map.md) 是从 gateway OpenAPI 审计得到的 active operation 与 owner service 清单，便于各组按路径分工。
- `docs/services/ai-gateway/api/internal.openapi.yaml` 是 AI Gateway 内部服务契约源，不生成前端 API client。
- 前端统一使用 `openapi-typescript` 从 gateway OpenAPI 生成类型，并通过项目封装的 typed fetch wrapper 调用 gateway；不得继续扩展旧的手写 `{ code, message, data }` API client。
- 后端实现 endpoint 前，应先更新 OpenAPI。
- 破坏性字段变更必须同步更新 OpenAPI 和本契约文档。
- 所有前端到 gateway、gateway 到下游服务的 HTTP API 必须使用 RESTful 资源路径，由 HTTP method 表达动作；健康检查是唯一已允许的非 `/api/v1` 例外。
- 本轮把 gateway 健康检查、auth、自助注册、个人资料、必需改密、管理员用户管理、knowledge-owned 知识库/文档上传/文档处理/原文件内容/切片/检索接口、`document` 拥有的报告生成接口、`qa` 拥有的会话/消息/SSE/引用/配置/检索体验测试/统计接口，以及 admin-facing runtime model/parser configuration 接口列为已确定公开契约；`file` 只作为后端内部基础文件能力，不直接拥有前端公开 API。`GET /api/v1/admin/overview` 和 `GET /api/v1/admin/metrics` 均已是 active contract；逐项 active operation 以 Gateway OpenAPI 和 owner map 为准。
- AI Gateway 的 chat、Function Calling 透传、embedding 和 rerank 契约已经作为内部服务契约补齐，但不改变前端只能调用 gateway 的约束。

## 接口文档编写标准

本文是 RESTful 路径、OpenAPI 协作、请求/响应 envelope、分页、错误、SSE、上传和 request id 的权威位置。服务 README 只应解释该服务拥有的资源、字段业务含义、状态枚举、工作流和特殊错误场景，不应重复定义通用标准。

服务文档中如需提到通用规则，使用链接而不是复制：

| 通用规则 | 权威位置 |
| --- | --- |
| RESTful 资源路径、动作词限制和 owner service | 本文、[`service-boundaries.md`](service-boundaries.md) |
| 成功响应、分页响应和错误响应 envelope | 本文的“成功响应”“错误响应”章节 |
| `Authorization`、用户上下文 header 和 request id | 本文的“认证约定”“Request ID”章节 |
| OpenAPI 先行、前端类型生成和缺失契约处理 | 本文的“OpenAPI 作为协作源”“Mock 与并行开发”章节 |
| 技术栈、日志、测试、数据库和队列选型 | [`technology-decisions.md`](technology-decisions.md) |
| 文档维护和归属规则 | [`../collaboration/documentation-workflow.md`](../collaboration/documentation-workflow.md) |

## 认证约定

- 用户创建和会话创建接口不要求认证。
- `POST /api/v1/users` 是公开自助注册入口。它只收集用户名和密码，
  创建默认 `standard` 用户并返回会话；自助注册用户不进入首次强制改密。
- 管理员创建用户使用独立资源 `POST /api/v1/admin/users`。该入口要求
  管理员认证，不返回被创建用户的会话，并要求被创建用户首次登录后进入
  `/password/change-required` 完成临时密码修改。
- 业务接口默认要求认证，OpenAPI 中使用 `bearerAuth` 标记。
- 用户创建或会话创建成功后，前端从响应的 `data.session.accessToken` 读取访问令牌。该 token 是 opaque Bearer token，不是 JWT，前端不得解析其内容。
- 前端后续请求使用 `Authorization: Bearer <accessToken>`。
- 前端只发送认证凭据，不发送 `X-User-Id`、`X-User-Roles`、`X-User-Permissions`。
- 用户身份、角色和权限由 gateway 从 Redis 会话缓存读取后传递给下游服务。
- Redis 会话缓存由 gateway 在 auth 返回身份/会话信息后写入；前端不直接访问 Redis 或 auth 内部地址。
- Auth 返回的当前用户摘要可包含 `mustChangePassword`。前端在普通业务和
  管理端路由渲染前应先把该用户带到 `/password/change-required`；该页面
  调用 `POST /api/v1/users/me/password-changes`，提交当前临时密码、新密码和确认值。
- `401 unauthorized` 表示未登录或认证失效；前端应回到登录流程。
- `403 forbidden` 表示已登录但权限不足；前端应展示权限不足状态。

## 请求约定

| 项目 | 约定 |
| --- | --- |
| JSON request | `Content-Type: application/json` |
| JSON response | `Content-Type: application/json` |
| File upload | `multipart/form-data` |
| Streaming response | `text/event-stream` |
| Timestamp | RFC 3339 / OpenAPI `date-time` |
| ID | Public API 使用 string ID |
| Page index | `page` 从 1 开始 |
| Page size | `pageSize`，默认值和上限由 endpoint 细化；Knowledge 列表和 chunk 列表当前上限为 100 |

## 成功响应

单资源响应：

```json
{
  "data": {
    "id": "kb_123"
  },
  "requestId": "req_123"
}
```

列表响应：

```json
{
  "data": [],
  "page": {
    "page": 1,
    "pageSize": 20,
    "total": 0
  },
  "requestId": "req_123"
}
```

前端应从 `data` 读取业务数据，不依赖响应中的内部服务字段。

## 错误响应

错误响应固定为：

```json
{
  "error": {
    "code": "validation_error",
    "message": "request validation failed",
    "requestId": "req_123",
    "fields": {
      "name": "is required"
    }
  }
}
```

前端逻辑应优先匹配 `error.code`，不要解析 `message` 文案。

| Code | Frontend behavior |
| --- | --- |
| `validation_error` | 显示字段错误或表单级错误。 |
| `unauthorized` | 清理本地登录态并进入登录流程。 |
| `forbidden` | 展示权限不足。 |
| `not_found` | 展示资源不存在或已删除。 |
| `conflict` | 展示状态冲突并刷新当前数据。 |
| `rate_limited` | 展示稍后重试。 |
| `dependency_error` | 展示服务暂不可用。 |
| `internal_error` | 展示通用系统错误。 |

## 分页、过滤和查询

分页、过滤和查询属于下游服务契约的一部分。Knowledge 相关列表和检索参数已经进入 OpenAPI；其他下游接口后续补齐时优先使用以下约定：

```text
?page=1&pageSize=20&keyword=xxx&status=ready
```

约定：

- `keyword` 表示模糊查询关键词。
- 多值过滤可使用逗号分隔字符串，具体字段由 OpenAPI endpoint 定义。
- 排序参数后续统一为 `sort`，例如 `sort=-createdAt`，本轮只保留扩展空间。
- `GET /api/v1/admin/overview` 和 `GET /api/v1/admin/metrics` 均已是 active contract，
  schema 已定义，前端可基于此生成 typed client 并开始 dashboard UI 开发。
  当前路由返回 501 Not Implemented，后端聚合实现由单独 issue 追踪。
  其余知识库、问答、报告生成和 admin runtime configuration 接口以当前 OpenAPI active paths 为准。

## Auth 与用户管理接口

Auth 拥有用户、凭证、角色、权限、会话、个人资料和强制改密状态。
Gateway 是唯一公开入口，前端不得直连 Auth `/internal/v1/**`。

Auth 公开资源族包括：

- `POST /api/v1/users`：公开自助注册，保持用户名/密码短表单，返回会话，
  不设置首次强制改密。
- `POST /api/v1/sessions`、`DELETE /api/v1/sessions/current`、
  `GET /api/v1/users/me`：会话创建、删除和当前用户。
- `/api/v1/users/me/profile`：当前用户资料读取和自助编辑。只允许编辑
  `displayName`、`email`、`phone`；用户名、角色、权限、状态只读。
- `/api/v1/users/me/password-changes`：当前用户必需的临时密码修改流程。
  请求必须包含当前临时密码、新密码和确认值。
- `/api/v1/admin/users`：管理员用户管理。列表必须由 Gateway/Auth 通过
  `page`、`pageSize`、`username`、`role`、`status` 参数过滤和分页，不允许
  前端只对单页结果做假过滤。

管理员用户管理权限语义：

- `standard` 用户不能访问用户管理。
- `admin` 只能看见和管理 `standard` 用户。
- `super_admin` 或具备 `system:admin` 的调用方可以看见和管理 `standard`
  与 `admin` 用户。
- `super_admin` 用户不出现在管理列表中，任何人都不能通过公开 UI/API 创建、
  授予或移除 `super_admin`。
- 角色更新是单角色替换，只允许目标角色为 `standard` 或 `admin`。
- 管理员和超管不能通过用户管理接口禁用自己、重置自己的密码或修改自己的角色。

资料字段规则：

- `displayName`、`email`、`phone` 都是可选资料字段。
- `email` 和 `phone` 不唯一，不作为登录标识，不参与找回密码、验证码或通知投递。
- 密码策略为 8 到 1024 个字符；不要求大小写、数字或符号复杂度。该策略适用于
  自助注册、管理员临时密码、管理员密码重置和强制改密。

## Knowledge 接口

知识库管理、文档处理状态、切片详情、原始文件内容和知识检索已经进入 gateway OpenAPI。前端只能调用 gateway active paths，不能直接调用 `services/knowledge`。逐项 method/path/schema 以 [`docs/services/gateway/api/public.openapi.yaml`](../services/gateway/api/public.openapi.yaml) 和 [Gateway Active API Owner Map](../services/gateway/docs/active-api-owner-map.md) 为准；本文只规定跨前后端协作规则。

Knowledge 公开资源族包括 `knowledge-bases`、知识库下的 `documents`、独立 `documents` 子资源、`documents/{documentId}/chunks`、`documents/{documentId}/content` 和 `knowledge-queries`。检索使用 `knowledge-queries` 资源，不使用 `/search`、`/retrieval/search` 或其他动作路径。`standard` 默认具备 `knowledge:read`，可以直接使用前端知识检索入口，也可以查看可见知识库、文档详情、chunks 和原文内容；知识库创建/更新/删除、文档上传/更新/删除和 parser config 仍按 Knowledge 管理权限控制。文档详情、chunks 和 content 路径必须携带 `knowledgeBaseId`，让 Knowledge adapter 使用明确的 runtime dataset 上下文。返回字段、分页结构和错误响应以 Gateway OpenAPI 为准；Knowledge runtime 认证失败或 service token 错配应作为 `dependency_error` 展示服务暂不可用，不应触发前端清理登录态。

## QA 接口

智能问答会话、消息、回答运行、引用、配置、检索体验测试和统计已经进入 gateway OpenAPI。前端只能调用 gateway active paths，不能直接调用 `services/qa` 或 AI Gateway。逐项 method/path/schema 以 Gateway OpenAPI 和 owner map 为准。

QA 公开资源族包括 `qa-sessions`、会话下的 `messages` 和 `events`、`response-runs`、消息引用和引用 lookup、QA/LLM 配置版本、连接测试、检索体验测试和 `qa-metrics`。检索体验测试是普通 QA 使用能力，主导航可显示为“检索”，但稳定 API 资源名仍是 `retrieval-test-runs`；`standard` 通过 `qa:use` 可创建和查看自己的测试运行。QA/LLM settings、LLM connection test 和 metrics 仍是管理面。前端只展示 QA 返回的 `thinking` / `reasoning.step` 安全摘要和 tool-call summary，不展示或缓存完整 prompt、私有 chain-of-thought、MCP 原始参数/结果、内部 URL、provider 原始错误或存储 object key。

> **管理员配置例外**：管理员 QA config 端点（`GET/POST /api/v1/qa-config-versions`）是可返回完整 `systemPrompt` 的唯一公开资源，仅限持有 `qa:settings:read`/`qa:settings:write` 权限的管理员客户端访问。普通 QA 会话/消息/SSE/工具摘要/错误/日志/指标仍然禁止返回完整提示词。

## QA 报告生成工具产物

QA 调用 Document 报告生成 MCP 工具后，只能通过脱敏后的 `reportArtifact` 向前端暴露用户可见产物摘要。该字段不是 Document MCP 原始返回值，也不是完整报告内容；它是 QA 工具结果的公开摘要，结构以 Gateway OpenAPI `QAReportArtifact` schema 为准。

允许出现位置：

- QA SSE `tool.completed` / `tool.failed` 事件的 `payload.result.reportArtifact`。
- `GET /api/v1/response-runs/{responseRunId}/tool-calls` 返回的 `resultSummary.reportArtifact`，用于断线回放和历史消息恢复。
- 助手消息的 `thinking` / `reasoning.step` 只能引用同一安全摘要，不得另行暴露 MCP 原始参数或结果。

字段规则：

- `artifactType` 固定为 `report_generation`。
- `jobId`、`jobType`、`jobStatus` 用于展示异步进度；job 未完成时前端只展示进度类 preview，不显示下载按钮。
- `reportFileId`、`filename`、`format`、`fileStatus`、`fileSize` 只在 Document 可向当前用户暴露导出文件元数据时返回。
- `downloadPath` 只能是 `/api/v1/report-files/{reportFileId}/content`，且仅当 `fileStatus=succeeded`、`reportFileId` 存在并且当前用户有读取权限时返回。
- `preview` 只能包含标题、章节标题、短摘要、进度百分比和用户可见状态，不得包含完整章节正文、prompt、provider 原始输出、MCP 原始参数/结果、File internal ID、bucket、object key、内部 URL 或向量 payload。
- 无 report 读取权限、导出未完成、文件失败或上游依赖错误时，不得伪造 `downloadPath` 或内部文件位置。

前端展示规则：

- 看到 `reportArtifact.preview` 时，可在 assistant 消息中展示报告产物卡片。
- `jobStatus=accepted|pending|running` 时展示进度或状态文案，下载按钮禁用或不渲染。
- `fileStatus=succeeded` 且 `downloadPath` 存在时展示下载按钮，并直接请求该 Gateway 路径。
- `jobStatus=failed` 或 `fileStatus=failed` 时展示失败状态和安全错误摘要，不展示 provider 原始错误或内部依赖响应。

进行中示例：

```json
{
  "reportArtifact": {
    "artifactType": "report_generation",
    "reportId": "rpt_123",
    "reportName": "迎峰度夏检查报告",
    "reportType": "summer_peak_inspection",
    "jobId": "job_123",
    "jobType": "content_generation",
    "jobStatus": "running",
    "reportStatus": "outline_generated",
    "preview": {
      "title": "正在生成报告正文",
      "outlineTitles": ["一、总体情况", "二、风险分析", "三、整改建议"],
      "progressPercent": 45,
      "statusText": "已生成 3/7 个章节"
    },
    "detailPath": "/api/v1/reports/rpt_123"
  }
}
```

导出成功示例：

```json
{
  "reportArtifact": {
    "artifactType": "report_generation",
    "reportId": "rpt_123",
    "reportName": "迎峰度夏检查报告",
    "reportType": "summer_peak_inspection",
    "jobId": "job_456",
    "jobType": "report_file_creation",
    "jobStatus": "succeeded",
    "reportStatus": "generated",
    "reportFileId": "rf_123",
    "filename": "summer_peak_inspection.docx",
    "format": "docx",
    "fileStatus": "succeeded",
    "fileSize": 245760,
    "preview": {
      "title": "DOCX 已生成",
      "summary": "报告文件已准备好下载。",
      "sectionTitles": ["总体情况", "风险分析", "整改建议"],
      "progressPercent": 100,
      "statusText": "导出成功"
    },
    "downloadPath": "/api/v1/report-files/rf_123/content",
    "detailPath": "/api/v1/reports/rpt_123"
  }
}
```

导出失败示例：

```json
{
  "reportArtifact": {
    "artifactType": "report_generation",
    "reportId": "rpt_123",
    "reportName": "迎峰度夏检查报告",
    "jobId": "job_789",
    "jobType": "report_file_creation",
    "jobStatus": "failed",
    "reportStatus": "generated",
    "fileStatus": "failed",
    "preview": {
      "title": "DOCX 导出失败",
      "summary": "报告内容已生成，但文件导出暂不可用，请稍后重试。",
      "progressPercent": 100,
      "statusText": "导出失败"
    },
    "detailPath": "/api/v1/reports/rpt_123"
  }
}
```

无权限示例：

```json
{
  "reportArtifact": {
    "artifactType": "report_generation",
    "jobStatus": "failed",
    "preview": {
      "title": "无法访问报告产物",
      "summary": "当前账号没有该报告或报告文件的读取权限。",
      "statusText": "权限不足"
    }
  }
}
```

## Admin Runtime Configuration 接口

模型配置管理由 public `gateway` 暴露给前端，实际 provider 配置、API key 写入状态和模型 profile 校验仍由 `ai-gateway` 拥有。文档解析器配置由 `knowledge` 拥有，gateway 只提供统一公开入口、管理员鉴权和响应归一化。当前 active 管理资源族是 `admin/model-profiles` 和 `admin/parser-configs`；逐项 method/path/schema 以 Gateway OpenAPI 为准。

`apiKey` 是 write-only 字段，只允许在创建或更新模型 profile 时发送；任何响应、日志、错误文案和前端缓存都不得包含明文 key。前端只能依赖 `apiKeyConfigured` 判断是否已配置密钥。模型调用能力仍由后端领域服务通过 AI Gateway 内部调用完成，前端不得用这些 admin 配置接口发起 chat、embedding 或 rerank 请求。

## SSE 与流式 UI

问答的公开流式接口已经进入 gateway OpenAPI。前端通过 `POST /api/v1/qa-sessions/{sessionId}/messages` 创建消息；当请求头包含 `Accept: text/event-stream` 时，gateway 返回 QA SSE 流。`GET /api/v1/qa-sessions/{sessionId}/events?responseRunId=...` 用于短期事件回放和断线恢复。AI Gateway 内部 `POST /internal/v1/chat/completions` 支持 OpenAI-compatible streaming chunk 和 tool-call delta，但该能力只供 `qa`、`document` 等后端领域服务使用，不等同于前端可直接调用的 gateway SSE contract。报告生成当前可使用 `GET /api/v1/reports/{reportId}/events` 轮询事件列表；后续如需报告 SSE，必须先补 OpenAPI 契约。QA SSE 前端处理原则如下：

- 根据 `Content-Type: text/event-stream` 进入流式读取。
- `message.created` 事件用于创建消息和运行占位。
- `agent.iteration.started` 事件用于展示 Agent 正在进入下一轮模型/工具循环。
- `reasoning.step` 事件用于展示安全的处理步骤摘要，不展示私有 chain-of-thought。
- `tool.started`、`tool.completed`、`tool.failed` 事件用于展示脱敏后的工具调用状态。
- `answer.delta` 事件用于最终回答文本增量。
- `citation.delta` 事件用于问答引用。
- `answer.completed` 事件表示回答完成。
- `error` 事件表示本次流式任务失败。

QA SSE 不得返回完整工具参数、MCP 原始响应、内部 URL、原始文档全文、prompt、provider 原始错误或存储 object key。断线重连时，前端应使用当前 OpenAPI 中的事件回放资源，而不是直接调用内部 QA 或 AI Gateway 地址。

## 文件上传与内容读取

- 上传使用 `multipart/form-data`。
- 上传 endpoint 由 gateway 暴露，知识库文档公开资源归 `knowledge` 服务管理；当前 Knowledge 文档主路径由 `services/knowledge-runtime` 保存和读取底层原始 bytes，不经过 File Service。
- 文档处理状态、知识库列表、切片详情、原文件内容入口和知识检索归 `knowledge` 服务并已进入 gateway OpenAPI。
- 前端读取原文件内容时，只使用 gateway 提供的 `GET /api/v1/documents/{documentId}/content?knowledgeBaseId=...`。
- 生成报告和报告文件内容接口由 `document` 契约提供；前端只通过 `POST /api/v1/reports/{reportId}/jobs` 创建生成类任务，通过 `GET /api/v1/report-files/{reportFileId}/content` 获取生成文件内容。
- 前端不得依赖 file 内部 ID、MinIO object key、内部 URL 或内部存储路径。

## Request ID

- 前端可以在请求头中传递 `X-Request-Id`，不传时由 gateway 生成。
- Gateway 应在响应头和响应体中返回 request id。
- 用户反馈问题时，前端可展示或复制 request id 便于排查。

## Mock 与并行开发

并行开发时：

- 前端以 OpenAPI 中已存在的 active paths 为准，不等待所有内部服务完成。
- Gateway OpenAPI 当前只保留 `status: resolved` 的 `x-missing-contracts` 记录，`placeholderOperations` 为空；若后续重新出现未解决的缺失项，其中列出的范围只能作为待办，不应生成可调用 API client 方法。QA、admin model/parser configuration、admin overview/metrics 均按 active paths 处理，但前端调用 admin overview/metrics 时必须处理稳定 `not_implemented` 响应。
- 各后端服务以 gateway OpenAPI 和服务边界矩阵确认自己需要提供的能力。
- 领域服务需要模型能力时，以 [AI Gateway 服务接口文档](../services/ai-gateway/README.md) 和 [AI Gateway OpenAPI 契约](../services/ai-gateway/api/internal.openapi.yaml) 为准，不把 provider 细节暴露给前端。QA 需要工具能力时，应通过自己的 Agent Host 和 MCP Client 契约暴露安全摘要，而不是把 MCP server、tool schema 或工具原始结果直接暴露给前端。
- 如果实现发现契约不合理，先更新 OpenAPI 和相关文档，再改代码。

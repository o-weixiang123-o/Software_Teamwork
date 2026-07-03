# Gateway 数据模型文档

## 1. 文档说明

本文定义 `gateway` 服务的逻辑数据模型，用于支撑公开 API envelope、认证上下文、Redis 会话缓存、下游调用上下文、SSE 转发、健康检查和运行时审计。

本文只描述逻辑数据模型，不提供具体 SQL 建表语句。`gateway` 首期不拥有业务数据库；稳定前端公开契约以 [`../api/public.openapi.yaml`](../api/public.openapi.yaml) 为准，职责边界见 [`../README.md`](../README.md)，技术选型以 [`../../../architecture/technology-decisions.md`](../../../architecture/technology-decisions.md) 为基线。

## 2. 存储边界

### 2.1 Gateway 自有运行时数据

Gateway 只拥有运行时边缘数据：

- Redis 会话缓存，用于从 bearer token 派生当前用户、角色和权限上下文。
- 请求上下文，用于 request id、客户端来源、认证结果、下游调用和日志关联。
- 下游调用上下文，用于把 gateway path、owner service、用户上下文和超时策略传递给内部服务。
- 统一响应 envelope、分页 envelope 和错误 envelope。
- 健康检查和就绪检查的临时状态。
- 结构化日志、Prometheus 风格指标和后续 tracing 中的脱敏观测字段。

这些数据不替代任何领域服务的持久化模型。除 Redis 会话缓存外，Gateway 上下文默认只存在于单次请求生命周期、日志或指标系统中。

Gateway 首期不拥有 PostgreSQL 业务表、`sqlc` 查询文件或 `goose` 迁移。后续如果新增自有持久化模型，必须先在 Gateway README 和 OpenAPI 中确认 owner 边界，再按项目技术基线使用 `pgx` + `sqlc` 和 `goose`。

### 2.2 Gateway 不拥有的数据

Gateway 不保存以下数据：

- 用户、密码、角色、权限源数据和会话撤销源数据。
- 文件对象、MinIO bucket、object key、内部下载 URL 或文件二进制内容。
- 知识库、文档切片、embedding、runtime doc-engine point、检索策略和解析结果。
- QA 会话、消息、Agent run、MCP 工具调用、引用快照和问答配置版本。
- 报告、模板、素材、章节、任务、报告文件和报告统计。
- AI provider API key、provider secret、完整 prompt、provider 原始错误和模型调用原文。
- JWT claims、JWT signing key 或其他可解析 token 声明。项目公开 access token 是 opaque token，不是 JWT。

公开 API 中出现的业务资源只是在 Gateway 契约中暴露的代理视图，权威存储仍归对应 owner service。

### 2.3 Redis 会话缓存

Redis 是 Gateway 的运行时缓存，不是 auth 的持久化数据库。Gateway 实现使用 `go-redis` 访问 Redis。缓存键使用：

```text
gateway:session:<accessTokenHash>
```

`accessTokenHash` 必须由原始 opaque bearer token 通过不可逆 hash 派生。Gateway 不得把原始 token 写入 Redis key、Redis value、日志、错误响应或指标标签，也不得把 access token 当作 JWT 解析或信任其中的任何声明。

每条会话缓存必须设置 TTL，TTL 不得晚于 `expires_at`。Redis 未命中、缓存过期、缓存字段不完整或 hash 不匹配时，Gateway 返回 `401 unauthorized`。

## 3. 设计原则

- Gateway 内部字段使用 snake_case；公开 API 字段使用 camelCase。
- 时间字段使用 RFC 3339 / OpenAPI `date-time`。
- 公开响应字段必须以 gateway OpenAPI 为准；本文只补充内部模型和边界约束。
- Gateway-owned 模型不能创建跨服务物理外键；跨服务关系只通过公开 ID、owner service 和 request id 关联。
- Gateway 可以做认证、路由、响应归一化、脱敏和传输层策略，不能复制领域服务的业务状态机。
- 所有日志、审计和错误摘要都必须先脱敏再记录。
- Gateway 日志使用 Go 标准库 `log/slog` 的结构化字段；生产默认 JSON 输出。
- 指标使用 Prometheus 风格暴露，label 不得包含用户输入正文、prompt、token、object key、API key 指纹或内部 URL。

## 4. 模型关系概览

```text
Bearer token
  -> accessTokenHash
  -> GatewaySessionCacheEntry
  -> GatewayAuthContext
  -> DownstreamCallContext
  -> owner service

GatewayRequestContext
  -> GatewayResponseEnvelope | GatewayPageEnvelope | GatewayErrorEnvelope

GatewayRequestContext
  -> GatewaySseProxyContext
  -> qa-owned QASseEvent stream
```

## 5. Gateway 自有模型

### 5.1 GatewaySessionCacheEntry

存储位置：Redis
键：`gateway:session:<accessTokenHash>`

Gateway 在 `POST /api/v1/users` 或 `POST /api/v1/sessions` 成功后写入该缓存。Auth 仍是用户、角色、权限和会话撤销状态的权威来源。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `session_id` | string | Auth 签发的会话 ID；对应公开 `session.sessionId`。 |
| `user_id` | string | 已认证用户 ID；来自 `UserSummary.id`。 |
| `username` | string | 用户名；用于展示、审计和调试。 |
| `status` | string | Auth 用户状态快照；禁用用户不得继续通过 Gateway 授权。 |
| `must_change_password` | boolean | Auth 强制改密状态快照；为 true 时只能进入完成改密所需路由。 |
| `roles` | string[] | 角色列表；来自 auth 计算结果。 |
| `permissions` | string[] | 权限字符串列表；来自 auth 计算结果。 |
| `token_type` | string | 固定为 `Bearer`。 |
| `access_token_hash` | string | bearer token 不可逆 hash。 |
| `issued_at` | datetime | 会话签发时间。 |
| `expires_at` | datetime | 会话过期时间；Redis TTL 不得晚于该时间。 |
| `cached_at` | datetime | Gateway 写入缓存时间。 |
| `request_id` | string | 写入该缓存的 Gateway 请求 ID。 |

约束建议：

- `session_id`、`user_id`、`username`、`status`、`must_change_password`、`roles`、`permissions`、`access_token_hash`、`expires_at` 必填。
- `expires_at` 必须晚于 `cached_at`。
- Redis value 不得包含 `access_token`、`password`、`session_secret`、provider secret 或内部服务 URL。
- 角色和权限为空数组时必须显式保存为空数组，不能省略字段。
- `access_token_hash` 只能用于查找和一致性校验，不能作为前端、日志或指标中的可展示 token 标识。

公开 API 映射：

| 缓存字段 | 公开 API 字段 |
| --- | --- |
| `user_id` | `data.user.id` |
| `username` | `data.user.username` |
| `status` | `data.user.status` |
| `must_change_password` | `data.user.mustChangePassword` |
| `roles` | `data.user.roles` |
| `permissions` | `data.user.permissions` |
| `session_id` | `data.session.sessionId` |
| `token_type` | `data.session.tokenType` |
| `expires_at` | `data.session.expiresAt` |

`data.session.accessToken` 只在创建用户或创建会话响应中返回给前端，不进入 Redis value。

### 5.2 GatewayAuthContext

存储位置：单次请求内存上下文

Gateway 从 Redis 命中会话后构造该模型，并基于它向下游服务注入用户上下文。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | 本次请求 ID。 |
| `session_id` | string | 当前会话 ID。 |
| `user_id` | string | 当前用户 ID。 |
| `username` | string | 当前用户名。 |
| `status` | string | 当前用户状态。 |
| `must_change_password` | boolean | 当前用户是否必须完成临时密码修改。 |
| `roles` | string[] | 当前角色列表。 |
| `permissions` | string[] | 当前权限列表。 |
| `authenticated_at` | datetime | Gateway 认证完成时间。 |
| `expires_at` | datetime | 当前会话过期时间。 |

下游 header 映射：

| Auth context 字段 | 下游 header |
| --- | --- |
| `request_id` | `X-Request-Id` |
| `user_id` | `X-User-Id` |
| `roles` | `X-User-Roles`，逗号分隔。 |
| `permissions` | `X-User-Permissions`，逗号分隔。 |

前端不得设置 `X-User-Id`、`X-User-Roles` 或 `X-User-Permissions`。Gateway 必须忽略或覆盖来自公网请求的这些 header。

### 5.3 GatewayRequestContext

存储位置：单次请求内存上下文、脱敏日志和指标

该模型描述 Gateway 入口请求，不是公开 API 资源。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | Gateway 生成或透传的请求 ID。 |
| `method` | string | HTTP method。 |
| `path` | string | Gateway public path，不含敏感 query value。 |
| `operation_id` | string | OpenAPI operationId，可选。 |
| `owner_service` | string | 该 path 的下游 owner，例如 `auth`、`knowledge`、`qa`。 |
| `auth_required` | boolean | 该接口是否要求认证。 |
| `auth_context` | GatewayAuthContext | 已认证请求的用户上下文。 |
| `client_ip` | string | 脱敏或规范化后的客户端地址。 |
| `user_agent` | string | 客户端 user agent，可截断。 |
| `content_type` | string | 请求 Content-Type。 |
| `accept` | string | 请求 Accept。 |
| `started_at` | datetime | Gateway 收到请求时间。 |
| `deadline_at` | datetime | Gateway 为本次请求设置的截止时间。 |

日志约束：

- 不记录 `Authorization` 原文。
- 不记录 multipart 文件内容、文档正文、prompt、工具参数、provider API key 或 password。
- Query string 如包含 token、key、secret、password 等字段名，必须脱敏或不记录 value。

### 5.4 DownstreamCallContext

存储位置：单次下游调用内存上下文、脱敏日志和指标

Gateway 调用内部服务时使用该模型。它记录“如何转发”，不记录业务资源内容。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | 与入口请求一致。 |
| `owner_service` | string | 下游服务名。 |
| `gateway_operation_id` | string | Gateway OpenAPI operationId。 |
| `gateway_method` | string | 入口 HTTP method。 |
| `gateway_path` | string | 入口 path 模板。 |
| `downstream_method` | string | 内部服务 HTTP method。 |
| `downstream_path` | string | 内部服务 path 模板。 |
| `timeout_ms` | int | 本次下游调用超时。 |
| `attempt_no` | int | 调用尝试次数，从 1 开始。 |
| `auth_context` | GatewayAuthContext | 需要传递给下游的认证上下文。 |
| `started_at` | datetime | 下游调用开始时间。 |
| `finished_at` | datetime | 下游调用结束时间。 |
| `status_code` | int | 下游 HTTP 状态码，可为空。 |
| `error_code` | string | 归一化后的错误码，可为空。 |

约束建议：

- 只有幂等或明确允许重试的下游调用可自动重试。
- 下游 base URL、连接字符串和内部 token 不进入公开响应。
- 下游错误必须映射为 gateway `ErrorDetail`，不能把 provider 原始错误、内部 URL、SQL 错误或对象存储 key 透传给前端。

### 5.5 GatewayResponseEnvelope

公开响应模型：OpenAPI 中所有普通成功响应必须使用统一 envelope。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `data` | object | 单个资源、聚合对象或创建结果。 |
| `requestId` | string | Gateway 请求 ID。 |

示例：

```json
{
  "data": {
    "id": "kb_123",
    "name": "运行规程"
  },
  "requestId": "req_123"
}
```

业务字段由 owner service 决定。Gateway 只负责 envelope、字段命名一致性和脱敏。

### 5.6 GatewayPageEnvelope

公开分页响应模型。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `data` | object[] | 当前页资源列表。 |
| `page.page` | int | 当前页码，从 1 开始。 |
| `page.pageSize` | int | 每页数量。 |
| `page.total` | int | 总数。 |
| `requestId` | string | Gateway 请求 ID。 |

Gateway 可以校验公共分页参数范围，但排序、过滤和可见性规则由 owner service 执行。

### 5.7 GatewayErrorEnvelope

公开错误响应模型，对应 OpenAPI `ErrorResponse` 和 `ErrorDetail`。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `error.code` | string | 稳定错误码。 |
| `error.message` | string | 面向调用方的脱敏错误信息。 |
| `error.requestId` | string | Gateway 请求 ID。 |
| `error.fields` | object<string,string> | 字段级校验错误，可选。 |

稳定错误码：

| code | HTTP status |
| --- | --- |
| `validation_error` | `400` |
| `unauthorized` | `401` |
| `forbidden` | `403` |
| `not_found` | `404` |
| `conflict` | `409` |
| `rate_limited` | `429` |
| `dependency_error` | `502` |
| `unsupported_intent` | `400` |
| `unsupported_mode` | `400` |
| `internal_error` | `500` |

错误信息不得包含原始 token、password、provider API key、prompt、MCP 原始参数、内部服务 URL、Redis key、SQL、对象存储 key 或文件系统路径。

### 5.8 GatewaySseProxyContext

存储位置：单次 SSE 连接内存上下文

Gateway 对 QA 消息创建接口提供 SSE 转发能力，但 QA 拥有事件语义和事件回放数据。Gateway 不保存 `QASseEvent` 权威记录。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | SSE 请求 ID。 |
| `session_id` | string | QA session ID，来自 path。 |
| `response_run_id` | string | QA response run ID，可在下游返回后获得。 |
| `auth_context` | GatewayAuthContext | 当前用户上下文。 |
| `accept` | string | 应为 `text/event-stream`。 |
| `connected_at` | datetime | SSE 连接建立时间。 |
| `last_event_id` | string | 客户端断线恢复 ID，可选。 |
| `downstream_owner` | string | 固定为 `qa`。 |

转发约束：

- Gateway 可以转发 `event`、`id` 和 `data`，但不能改写 QA 事件语义。
- Gateway 可将传输错误归一化为 `error` 事件或标准错误响应，具体行为以 OpenAPI 和 QA 文档为准。
- Gateway 日志不得记录完整 answer delta、工具参数、MCP 原始响应、prompt 或原始文档全文。

### 5.9 GatewaySecretTransit

存储位置：单次请求内存，不得持久化

管理端模型 profile 创建或更新时，请求可能包含 `apiKey`。Gateway 只把该字段转发给 `ai-gateway`，不得保存、记录或返回。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | 当前请求 ID。 |
| `owner_service` | string | 固定为 `ai-gateway`。 |
| `profile_id` | string | 更新时的 profile ID，可选。 |
| `secret_field_names` | string[] | 例如 `["apiKey"]`。 |
| `forwarded_at` | datetime | 转发时间。 |

约束建议：

- 只允许记录 secret 字段名，不允许记录 secret value、长度、前后缀或 hash，除非后续安全设计明确要求。
- Gateway 响应只能包含 `apiKeyConfigured` 等脱敏状态。
- 空字符串或 `null` 不应被 Gateway 解释为清空密钥；密钥清空语义必须由 AI Gateway 契约明确声明。

### 5.10 GatewayHealthSnapshot

存储位置：请求内存，可进入指标系统

对应 `/healthz` 和 `/readyz`。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `status` | string | `ok`、`degraded` 或 `unavailable`。公开 OpenAPI 当前只要求 `ok` 字符串形态。 |
| `request_id` | string | 健康检查请求 ID。 |
| `checked_at` | datetime | 检查时间。 |
| `checks` | object | 内部检查结果，可选，不作为当前公开契约。 |

`/healthz` 只表示 Gateway 进程可响应。`/readyz` 是轻量 readiness
快照：当前用于检查 Redis session cache、Auth readiness 和 owner service base
URL 配置；不请求 Knowledge、QA、Document 或 AI Gateway 的 `/readyz`，也不证明
业务级上传、检索、问答、报告生成、模型 profile 或真实 provider 调用可用。完整
跨服务依赖诊断应通过 runbook smoke、#125 和 #352 承担。公开响应不得泄露内部地址、
连接串或凭据。

### 5.11 GatewayObservabilityRecord

存储位置：结构化日志、指标系统和后续 tracing

Gateway 观测数据只记录请求和下游调用的脱敏摘要，不作为业务事实来源。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `service` | string | 固定为 `gateway`。 |
| `request_id` | string | 本次请求 ID。 |
| `operation` | string | OpenAPI operationId 或内部操作名。 |
| `status` | string | `ok`、`error`、`timeout`、`canceled` 等归一化状态。 |
| `http_status` | int | Gateway 返回状态码。 |
| `owner_service` | string | 下游 owner service，可为空。 |
| `duration_ms` | int | Gateway 总耗时。 |
| `downstream_duration_ms` | int | 下游调用耗时，可为空。 |
| `error_code` | string | 归一化错误码，可为空。 |

约束建议：

- 日志由 `slog` 输出结构化字段，生产默认 JSON。
- Prometheus label 只能使用低基数字段，例如 `operation`、`owner_service`、`status`、`error_code`。
- 不记录 password、原始 token、token hash、provider API key、prompt、文档全文、multipart 内容、MCP 原始参数、内部 URL、SQL、Redis key 或对象存储 key。
- 后续接入 OpenTelemetry tracing 时，span attribute 遵循同一脱敏规则。

## 6. 代理资源模型边界

Gateway OpenAPI 暴露的下列 schema 不是 Gateway 的持久化模型：

| Schema 范围 | Owner service | Gateway 行为 |
| --- | --- | --- |
| `UserSummary`、`SessionSummary` | `auth` | 接收 auth 返回，写入 Redis 会话缓存，按公开 envelope 返回。 |
| `KnowledgeBase*`、`Document*`、`KnowledgeQuery*` | `knowledge` | 转发请求、注入用户上下文、归一化响应和错误。 |
| `Report*`、`ReportTemplate*`、`ReportMaterial*`、`ReportSettings*` | `document` | 转发请求、注入用户上下文、归一化响应和错误。 |
| `ModelProfile*` | `ai-gateway` | 转发管理请求，处理管理员鉴权和密钥脱敏边界。 |
| `ParserConfig*` | `knowledge` | 转发管理请求，归一化响应和错误。 |
| `QASession*`、`QAMessage*`、`QAResponseRun*`、`QACitation*`、`QAConfig*`、`QAMetrics*` | `qa` | 转发普通请求和 SSE，归一化响应和错误。 |

Gateway 不应为这些资源建立自己的业务表，也不应把下游返回数据复制成长期缓存。确需缓存读结果时，必须单独定义缓存键、TTL、失效条件、脱敏规则和 owner service，不得绕过下游权限校验。

## 7. 管理后台聚合预留模型

`GET /api/v1/admin/overview` 和 `GET /api/v1/admin/metrics` 已转为 active contracts。Gateway 负责轻量聚合读，schema 定义见 OpenAPI `AdminOverview` / `AdminMetrics` 及对应 Response envelope。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `request_id` | string | 聚合请求 ID。 |
| `generated_at` | datetime | 聚合生成时间。 |
| `sources` | object[] | 参与聚合的 owner service 和数据新鲜度。 |
| `data` | object | 聚合后的公开展示数据。 |

聚合模型只能保存或返回前端已确认需要的摘要字段，不能把各服务内部表结构直接暴露为 Gateway API。

## 8. 数据生命周期

| 数据 | 创建时机 | 删除或过期 |
| --- | --- | --- |
| `GatewaySessionCacheEntry` | 用户创建或会话创建成功后。 | Redis TTL 到期、当前会话删除、auth 撤销会话、用户禁用或权限安全事件。 |
| `GatewayAuthContext` | 每次认证成功请求。 | 请求结束。 |
| `GatewayRequestContext` | 每次 Gateway 收到请求。 | 请求结束；脱敏摘要可进入日志和指标。 |
| `DownstreamCallContext` | 每次调用内部服务。 | 调用结束；脱敏摘要可进入日志和指标。 |
| `GatewaySseProxyContext` | SSE 连接建立。 | SSE 连接关闭或超时。 |
| `GatewaySecretTransit` | 含密钥字段的管理请求进入 Gateway。 | 下游调用完成后立即释放，不持久化。 |
| `GatewayObservabilityRecord` | 请求、下游调用、错误映射或健康检查完成时。 | 按日志、指标和 tracing 后端保留策略过期。 |

## 9. 实现检查清单

- 新增公开 schema 前，先确认 owner service 和 gateway OpenAPI。
- 新增 Gateway 缓存前，先定义 key、value、TTL、失效条件和脱敏规则。
- 新增下游调用前，先定义 owner service、超时、错误映射和用户上下文 header。
- 新增日志字段前，确认不会记录 token、密钥、prompt、文件内容、内部 URL 或对象存储 key。
- 新增指标 label 前，确认字段是低基数字段，且不包含用户输入、token、hash、object key 或 provider 信息中的敏感部分。
- 新增 Gateway 自有持久化模型前，先确认是否真的属于 Gateway；确需落库时使用 `pgx` + `sqlc` 和 `goose`。
- 新增聚合接口前，同步更新 gateway OpenAPI、服务边界文档和对应 owner service 文档。

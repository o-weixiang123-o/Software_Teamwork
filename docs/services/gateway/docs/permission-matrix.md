# Gateway 权限矩阵

本文档说明 `gateway` 在公开 API 层如何处理认证、角色、权限和下游上下文。机器可读路径、owner service、operationId 和 `bearerAuth` 状态以 [`active-api-owner-map.md`](active-api-owner-map.md) 和 [`../api/public.openapi.yaml`](../api/public.openapi.yaml) 为准；本文只整理权限口径。

## 权限来源

| 项 | 说明 |
| --- | --- |
| 认证凭据 | 前端使用 `Authorization: Bearer <accessToken>`。Access token 是 opaque token，不是 JWT。 |
| 身份来源 | `auth` 签发 session identity，`gateway` 写入并读取 Redis session cache。 |
| 角色来源 | `auth` 计算 `standard`、`admin`、`super_admin` 等角色，`gateway` 不持久化角色源数据。 |
| 权限来源 | `auth` 计算权限字符串，`gateway` 只缓存和透传运行时快照。 |
| 下游上下文 | `gateway` 向 owner service 注入 `X-User-Id`、`X-User-Roles`、`X-User-Permissions`、`X-Request-Id`、`X-Forwarded-For`、`X-Forwarded-Proto`。 |
| 领域授权 | owner service 必须在自己的边界复核业务权限和资源归属。 |

## 公开入口矩阵

| 能力 | 路径范围 | 认证 | Gateway 判定 | Owner service 复核 |
| --- | --- | --- | --- | --- |
| 健康检查 | `GET /healthz`、`GET /readyz` | 不需要 | 直接返回 Gateway 状态。 | 无。 |
| 用户创建 | `POST /api/v1/users` | 不需要 | 只做公开入口、响应 envelope 和错误归一化。 | `auth` 校验用户创建规则和默认角色。 |
| 会话创建 | `POST /api/v1/sessions` | 不需要 | 转发凭证，成功后缓存 session identity。 | `auth` 校验密码、账号状态和会话签发。 |
| 当前会话删除 | `DELETE /api/v1/sessions/current` | `bearerAuth` | 校验 session cache，删除 Gateway 缓存。 | `auth` 撤销 session 或 token。 |
| 当前用户 | `GET /api/v1/users/me` | `bearerAuth` | 从 session cache 返回当前身份。 | `auth` 是身份和权限源数据。 |
| 当前用户资料 | `/api/v1/users/me/profile` | `bearerAuth` | 校验 session cache，使用 Gateway 专用 Auth token 转发当前用户上下文。 | `auth` 只允许当前用户编辑 `displayName`、`email`、`phone`。 |
| 当前用户必需改密 | `POST /api/v1/users/me/password-changes` | `bearerAuth` | 只允许已认证用户完成改密所需调用，使用 Gateway 专用 Auth token 转发；成功后刷新当前用户状态。 | `auth` 校验当前临时密码、更新密码哈希并清除 `must_change_password`。 |
| 管理端用户管理 | `/api/v1/admin/users/**` | `bearerAuth` | 限制为 `admin` / `super_admin` 角色入口，使用 Gateway 专用 Auth admin service token 转发调用方身份和 request id；裸 `system:admin` 权限不提升用户管理层级。 | `auth` 复核 Auth-owned 角色、管理范围、列表过滤、创建、禁用/启用、单角色替换、密码重置和会话刷新。 |
| Knowledge 资源 | `/api/v1/knowledge-bases/**`、`/api/v1/documents/**`、`/api/v1/knowledge-queries` | `bearerAuth` | 注入用户上下文，统一 envelope；标准用户默认有 `knowledge:read`，可直接使用 `knowledge-queries` 检索。 | `knowledge` 校验知识库、文档、查询范围和可见性。 |
| 管理端 parser config | `/api/v1/admin/parser-configs/**` | `bearerAuth` | 只允许带认证上下文的管理员入口。 | `knowledge` 校验 `admin` / `super_admin` 或 `admin:parser-config:write`。 |
| 管理端 model profile | `/api/v1/admin/model-profiles/**` | `bearerAuth` | 只允许带认证上下文的管理员入口，不保存 API key。 | `ai-gateway` 保存配置；Gateway 应要求管理员角色或 `admin:model-profile:write`。 |
| QA 资源 | `/api/v1/qa-sessions/**`、`/api/v1/response-runs/**`、`/api/v1/citations/**`、QA attachments、retrieval tests、QA settings、metrics | `bearerAuth` | 注入用户上下文，转发 SSE 和普通响应；检索测试随 `qa:use` 对普通用户开放；QA/LLM settings 和 metrics 端点必须具备对应管理权限，且 settings 是唯一可返回完整 `systemPrompt` 的公开面。 | `qa` 校验 owner、检索测试发起人、管理员配置权限、附件归属和工具权限裁剪；普通 QA 公开资源不得返回完整提示词。 |
| Document 资源 | `/api/v1/report-*`、`/api/v1/reports/**` | `bearerAuth` | 注入用户上下文，统一 envelope。 | `document` 校验 owner、报告权限、管理员 settings 权限。 |
| 管理概览和指标 | `/api/v1/admin/overview`、`/api/v1/admin/metrics` | `bearerAuth` | 聚合读入口，应限制为 `admin` / `super_admin` 或等价管理权限。 | 各 owner service 仍只暴露自己的安全指标。 |

## 角色与权限口径

| 调用方 | Gateway 行为 |
| --- | --- |
| 未认证调用方 | 只允许健康检查、用户创建和会话创建；其他公开路径返回 `401 unauthorized`。 |
| `standard` | 可进入业务路径，但具体资源可见性和写权限由 owner service 决定。 |
| `admin` | 可进入管理入口；用户管理最终由 Auth 限制为只管理 `standard`。Gateway 仍以 Auth 输出的运行时权限快照和 owner service 复核为准。 |
| `super_admin` | 可进入管理入口；用户管理最终由 Auth 限制为只管理 `standard` 和 `admin`，且不能管理 `super_admin`。具体敏感操作仍应由 owner service 记录审计。 |
| 服务间调用方 | Public Gateway 不信任前端自填 `X-User-*`；服务间 token 只用于 Gateway 到下游，不向前端公开。 |

## 拒绝规则

| 条件 | 响应 |
| --- | --- |
| 缺少或无效 bearer token | `401 unauthorized`。 |
| Redis session miss、过期或缓存 payload 无效 | `401 unauthorized`。 |
| 已认证但缺少管理权限 | `403 forbidden`。 |
| 下游返回资源隐藏或不存在 | 按 owner service 语义归一化为 `404 not_found` 或 `403 forbidden`。 |
| 下游依赖失败 | `502 dependency_error`，不得透传 token、SQL、object key、provider body、prompt 或内部 URL。 |

## 用户管理边界

- `POST /api/v1/users` 保持公开自助注册，不要求管理员认证，不触发首次强制改密。
- `POST /api/v1/admin/users` 是管理员创建用户入口，不返回被创建用户的 session，并要求目标用户下次登录改密。
- Gateway 可以先拒绝明显不是管理员的用户管理请求，但 Auth 是最终权限裁判；Gateway 转发用户管理、当前用户资料写入和必需改密路由时必须使用 `GATEWAY_AUTH_ADMIN_SERVICE_TOKEN`，不能复用共享内部 token。
- Gateway 不持久化用户、角色、权限、资料字段或 `mustChangePassword`。
- 用户禁用、管理员密码重置和角色变化后，Gateway 必须配合 Auth 撤销或刷新 Redis session cache，避免继续使用旧状态和旧权限。

## 当前缺口

- Gateway 只拥有运行时 session cache，不是角色和权限的持久化源数据。
- 管理端权限最终应以 `auth` 输出的权限字符串和 owner service 复核为准；本文不替代 Gateway OpenAPI。
- 本地 demo seed 已创建 `admin` 和 `superadmin` 用户；生产环境仍应通过受控初始化流程管理超级管理员凭据。

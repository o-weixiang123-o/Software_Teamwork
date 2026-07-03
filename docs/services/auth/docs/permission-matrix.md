# Auth 权限矩阵

本文档说明 `auth` 服务拥有的用户、角色、权限、会话和内部权限查询边界。稳定前端入口仍通过 Gateway；服务级机器可读契约见 [`../api/internal.openapi.yaml`](../api/internal.openapi.yaml)。

## 权限来源

| 项 | 说明 |
| --- | --- |
| 用户源数据 | `auth_users`。 |
| 凭证源数据 | `auth_credentials`，密码使用 `argon2id-v1` hash。 |
| 角色源数据 | `auth_roles`，当前已合入迁移后的默认角色包含 `standard`、`admin`、`super_admin`。 |
| 权限源数据 | `auth_permissions` 和 `role_permissions`。 |
| 用户授权 | `user_roles` 将用户绑定到角色。 |
| 会话身份 | `auth` 签发 opaque bearer token 和 session identity；Gateway 缓存运行时快照。 |

## 角色权限矩阵

| 角色 | 基础权限 | 管理权限 | 系统级权限 |
| --- | --- | --- | --- |
| `standard` | `knowledge:read`、`document:read`、`document:upload`、`report:read`、`report:write`、`qa:use` | 无。 | 无。 |
| `admin` | `knowledge:*`、`document:*`、`report:*`、`qa:use` | `admin:model-profile:write`、`admin:parser-config:write`、`qa:settings:read`、`qa:settings:write` | `system:admin`。 |
| `super_admin` | 与 `admin` 相同。 | 与 `admin` 相同。 | `system:admin`。 |

上表按 `0002` 基础 seed、`0003` QA settings 权限、`0004` 标准用户报告/文档增量迁移和 `0006` 标准用户知识读权限补丁迁移全部应用后的有效默认授权维护；不要只按单个基础 seed 文件判断运行时权限。`standard` 默认可直接调用 `knowledge-queries`（`knowledge:read`）并使用 QA/检索测试（`qa:use`），但不能读取 QA settings、metrics 或模型/provider 管理配置。`*` 表示当前迁移中该领域的 read/write/update/delete/upload 等已列权限集合，不表示通配符权限实现。

## API 权限矩阵

| 能力 | 路径范围 | 认证 | 允许调用方 | Auth 负责 |
| --- | --- | --- | --- | --- |
| 创建用户 | `POST /internal/v1/users` | `X-Service-Token` | Gateway 或受信内部调用方。 | 创建用户、分配默认角色、返回用户摘要。 |
| 创建 session | `POST /internal/v1/sessions` | `X-Service-Token` | Gateway。 | 校验用户名/密码、账号状态，签发 session identity 和 opaque access token。 |
| 查询用户 | `GET /internal/v1/users/{userId}` | `X-Service-Token` | Gateway 或受信内部调用方。 | 返回用户基础信息、角色和权限快照。 |
| 更新当前用户资料 | `PATCH /internal/v1/users/{userId}/profile` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 只允许当前用户更新自己的 `displayName`、`email`、`phone`。 |
| 当前用户必需改密 | `POST /internal/v1/users/{userId}/password-changes` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 验证当前临时密码，替换密码哈希，清除 `must_change_password`。 |
| 管理员列出用户 | `GET /internal/v1/admin/users` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 按调用方管理范围分页、搜索和过滤用户。 |
| 管理员创建用户 | `POST /internal/v1/admin/users` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 创建受管用户、分配单一目标角色、设置必需改密。 |
| 管理员更新用户 | `PATCH /internal/v1/admin/users/{userId}` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 更新资料、active/disabled 状态或单一管理角色。 |
| 管理员重置密码 | `POST /internal/v1/admin/users/{userId}/password-resets` | Gateway 专用 `X-Service-Token` + Gateway 用户上下文 | Gateway。 | 使用管理员输入的临时密码替换哈希，并设置必需改密。 |
| 查询用户权限 | `GET /internal/v1/users/{userId}/permissions` | `X-Service-Token` | Gateway、权限缓存刷新任务、受信内部调用方。 | 返回 roles、permissions 和更新时间。 |
| 查询 session | `GET /internal/v1/sessions/{sessionId}` | `X-Service-Token` | Gateway。 | 返回 session identity 或失效状态。 |
| 删除 session | `DELETE /internal/v1/sessions/{sessionId}` | `X-Service-Token` | Gateway、安全任务、管理任务。 | 撤销 session 并记录安全事件。 |

## 边界规则

| 场景 | 规则 |
| --- | --- |
| 前端直连 auth | 不允许；前端只能调用 Gateway。 |
| 前端自填角色/权限 header | 不可信；只有 Gateway 认证后注入的上下文可供下游消费。 |
| 用户禁用、权限变更、密码重置 | 应撤销或刷新受影响 session，避免 Gateway 继续使用旧权限快照。 |
| 下游业务权限判断 | Auth 只提供角色和权限字符串，不判断知识库、报告、QA 会话等业务资源归属。 |
| 账号枚举风险 | 登录失败不得暴露用户名是否存在、密码 hash、token hash 或内部原因。 |

## 管理员用户管理矩阵

| 调用方 | 列表可见用户 | 可创建角色 | 可管理操作 |
| --- | --- | --- | --- |
| `standard` | 无。 | 无。 | 不允许访问用户管理。 |
| `admin` | `standard` 用户。 | `standard`。 | 创建、禁用、重新启用、重置密码、更新资料；不能授予 `admin` 或 `super_admin`。 |
| `super_admin` | `standard` 与 `admin` 用户。 | `standard`、`admin`。 | 创建、禁用、重新启用、重置密码、更新资料、在 `standard`/`admin` 间单角色替换。 |

强制规则：

- `super_admin` 用户不出现在管理列表中，也不能通过用户管理 API 创建、授予或移除。
- 角色编辑是单角色替换，不合并多个管理角色。
- 用户管理层级按角色判定；默认 `admin` 角色即使带有 `system:admin` 业务权限，也不能创建、列出或管理其他 `admin`。
- 用户管理授权不信任转发的 `X-User-Roles`；Auth 根据 `X-User-Id` 重新读取 Auth-owned 角色数据后判定管理层级。
- `/internal/v1/admin/**`、`PATCH /internal/v1/users/{userId}/profile` 和 `POST /internal/v1/users/{userId}/password-changes` 要求 `AUTH_GATEWAY_ADMIN_SERVICE_TOKEN` 对应的 Gateway 专用凭据，不能只用共享内部 service token。
- 管理员和超管不能通过用户管理接口禁用自己、重置自己的密码或修改自己的角色。
- 管理员创建用户和管理员重置密码都要求管理员手动输入临时密码，并设置
  `must_change_password=true`。
- 自助注册用户创建时 `must_change_password=false`，不进入首次强制改密。
- 资料字段 `displayName`、`email`、`phone` 可为空；`email` 和 `phone` 不唯一，
  不作为登录、验证、找回密码或通知投递依据。

## 当前缺口

- 本地 demo seed 已创建 `superadmin` 用户；生产环境仍应通过受控初始化流程创建或轮换超级管理员凭据。
- 管理员用户管理已形成稳定公开契约，但完整角色/权限 catalog 编辑 UI/API 仍未形成稳定公开契约。
- 细粒度组织、电厂、专业等权限模型不属于首期范围。

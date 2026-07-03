# AI Gateway 数据模型文档

## 1. 文档说明

本文定义 `ai-gateway` 服务的逻辑数据模型，用于支撑运行时模型 profile、provider 凭据写入状态、配置变更审计、模型调用脱敏记录、健康检查和错误归一化。

本文只描述逻辑数据模型，不提供具体 SQL 建表语句。后续实现应根据服务代码、PostgreSQL 规范和迁移策略转换为 migration。服务级 API 契约见 [`../api/internal.openapi.yaml`](../api/internal.openapi.yaml)，领域说明见 [`../README.md`](../README.md)。前端稳定公开模型配置入口仍以 [`../../gateway/api/public.openapi.yaml`](../../gateway/api/public.openapi.yaml) 中的 `/api/v1/admin/model-profiles` 为准。

## 2. 存储边界

### 2.1 AI Gateway 持久化数据库

AI Gateway 数据库只保存模型运行和配置所需的服务内状态：

- 模型 profile，包括用途、provider 类型、base URL、模型名、默认参数、超时、默认 profile 标记和启用状态。
- Provider API key 的写入状态、密钥系统引用或加密密文元数据。
- 模型 profile 配置变更审计事件。
- 脱敏后的 provider 调用记录、重试尝试和用量统计。
- 健康检查和就绪检查所需的配置摘要。

AI Gateway 数据库不得保存领域服务业务状态，包括但不限于：

- QA 会话、消息、Agent Run、MCP 工具调用记录、引用快照和 QA 配置版本。
- Knowledge 文档、chunk、embedding 向量、runtime doc-engine point、检索结果和 rerank 业务过滤结果。
- Document 报告、模板、素材、大纲、章节、报告任务和生成文件。
- 用户、角色、权限源数据和前端会话缓存。
- 完整 prompt、完整生成答案、完整 embedding payload、用户上传文档全文、MCP 原始参数/结果或 provider 原始响应体。

### 2.2 Secret 存储

Provider API key 是敏感配置。推荐由外部 secret manager 保存明文，AI Gateway 数据库只保存 secret 引用；如果第一阶段必须落库，也只能保存加密密文和密钥版本，不能保存明文。

公开响应、错误响应、普通日志、指标标签和 gateway admin model-profile 响应都只能暴露：

| 字段 | 说明 |
| --- | --- |
| `apiKeyConfigured` | 是否已配置 provider 凭据。 |

以下字段不得出现在任何公开响应中：

- `api_key`
- `api_key_ciphertext`
- `api_key_secret_ref`
- `api_key_fingerprint`
- `provider_bearer_token`

### 2.3 外部服务标识

跨服务关系只通过调用上下文记录，不创建跨服务物理外键：

| 字段 | 外部 owner | 说明 |
| --- | --- | --- |
| `caller_service` | `qa` / `knowledge` / `document` / `gateway` | 发起内部调用的服务名。 |
| `external_user_id` | `auth` | Gateway 或领域服务透传的用户 ID，用于审计和配额。 |
| `request_id` | `gateway` | 贯穿一次公开请求或后台任务的追踪 ID。 |

AI Gateway 不根据 `external_user_id` 判断业务权限；权限仍由调用方领域服务负责。

## 3. 设计原则

- 数据库字段使用 snake_case；项目自有 API 字段使用 camelCase；OpenAI-compatible 模型调用字段保持 snake_case。
- 主键建议使用 UUID 或带业务前缀的字符串 ID；公开和内部 API 始终按 string 暴露。
- 时间字段使用 `TIMESTAMPTZ`，API 映射为 RFC 3339 / OpenAPI `date-time`。
- `model_profiles` 中每个 `purpose` 最多只能有一个 `enabled = true AND is_default = true` 的 profile。
- 所有错误摘要、调用日志、审计事件和 metrics 维度必须先脱敏再写入。
- 模型调用传输体默认不持久化。确需调试采样时必须另行设计受控、脱敏、短 TTL 的采样机制，不能混入本文核心表。
- AI Gateway 只归一化 provider 错误和模型调用协议，不保存领域 prompt、业务上下文或工具执行结果。

### 3.1 技术选型映射

AI Gateway 的物理实现以 [`docs/architecture/technology-decisions.md`](../../../architecture/technology-decisions.md) 为基线：

| 领域 | 数据模型落地规则 |
| --- | --- |
| PostgreSQL client | 使用 `pgx` 运行时连接 PostgreSQL。Repository 层接收 `pgx.Tx` 或抽象后的 querier，事务由 service/use-case 层发起。 |
| SQL 生成 | 使用 `sqlc` 生成类型安全查询代码。生成代码只供 `internal/repository` 适配层使用，不能泄露到 HTTP handler 或 provider client。 |
| 查询文件 | SQL 查询放在 `services/ai-gateway/internal/repository/queries/`，生成代码放在 `services/ai-gateway/internal/repository/sqlc/`。 |
| 迁移 | 使用 `goose`，迁移文件放在 `services/ai-gateway/migrations/`，文件名使用 `0001_*.sql` 形式。 |
| SQL 风格 | 查询显式列名，不使用 `SELECT *`；所有用户输入和请求字段都通过参数绑定进入 SQL。 |
| JSONB | `default_parameters_json`、审计快照和低敏 metadata 可用 JSONB，但必须在应用层做字段黑名单、大小限制和脱敏。 |
| Secret | Provider API key 不落明文。优先保存 secret manager 引用；第一阶段如使用加密列，只保存密文、加密密钥版本和脱敏状态。 |
| 调用日志 | PostgreSQL 只保存调用摘要、用量、耗时和归一化错误，不保存完整请求/响应体。 |

建议 `sqlc.yaml` 覆盖 `model_profiles`、`provider_credentials`、`model_profile_revisions`、`provider_invocations` 和 `provider_invocation_attempts` 的基础 CRUD 查询；业务组合逻辑例如“切换默认 profile”必须由 service 层开启事务并调用 repository 方法完成。

### 3.2 迁移基线

当前 `goose` migration 基线按以下顺序拆分，减少后续回滚和审查成本：

| 迁移 | 内容 |
| --- | --- |
| `0001_create_model_profiles.sql` | 创建模型 profile 表、枚举检查约束、默认 profile 部分唯一索引。 |
| `0002_create_provider_credentials.sql` | 创建凭据元数据表、active 凭据唯一约束和 storage mode 检查约束。 |
| `0003_create_model_profile_revisions.sql` | 创建配置变更审计表和 profile 内版本号索引。 |
| `0004_create_provider_invocations.sql` | 创建调用摘要表和排障索引。 |
| `0005_create_provider_invocation_attempts.sql` | 创建 provider invocation attempt 表。 |
| `0006_create_model_usage_aggregates.sql` | 创建按小时聚合的 request/success/failure/token/duration 用量表。 |

迁移必须能在空库重复从头 apply。首期允许 forward-only migration；如果提供 down migration，必须在本地和 CI 中验证可执行。涉及密钥表的 down migration 不得把密文或 secret ref 打印到错误信息、日志或注释示例中。

## 4. 实体关系概览

```text
ModelProfile 1 ── 0..1 ProviderCredential
ModelProfile 1 ── N ModelProfileRevision
ModelProfile 1 ── N ProviderInvocation

ProviderInvocation 1 ── N ProviderInvocationAttempt

ProviderInvocation -> NormalizedProviderError
ModelProfile -> ReadinessCheck
```

`NormalizedProviderError` 和 `ReadinessCheck` 可以是内存/响应模型，不一定需要持久化表。

物理外键建议只在 AI Gateway 自有表之间建立，例如 `provider_credentials.profile_id -> model_profiles.id`、`model_profile_revisions.profile_id -> model_profiles.id`、`provider_invocations.profile_id -> model_profiles.id` 和 `provider_invocation_attempts.invocation_id -> provider_invocations.id`。对 `external_user_id`、`caller_service`、QA 会话、知识库文档或报告资源不得建立跨服务外键。

## 5. 通用字段约定

| 字段 | 说明 |
| --- | --- |
| `id` | 主键。 |
| `created_at` | 创建时间。 |
| `updated_at` | 更新时间。 |
| `deleted_at` | 软删除时间，仅用于可软删除资源。 |
| `created_by_user_id` | 配置创建人外部用户 ID，可为空。 |
| `updated_by_user_id` | 最近修改人外部用户 ID，可为空。 |
| `request_id` | Gateway 或内部服务请求追踪 ID。 |
| `metadata` | JSONB 扩展字段；不得包含密钥、完整 prompt、完整文档内容、完整 provider 响应或内部 URL。 |

## 6. 核心配置实体

### 6.1 ModelProfile

表名建议：`model_profiles`

模型 profile 是 AI Gateway 的核心配置资源。一个 profile 绑定一种用途、一个 provider 和一个模型名。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | Profile ID，对应 API `ModelProfile.id`。 |
| `name` | string | 管理展示名。 |
| `purpose` | string | `chat`、`embedding` 或 `rerank`。 |
| `provider` | string | `openai_compatible`、`siliconflow` 或 `local_compatible`。 |
| `base_url` | string | Provider API base URL。不得包含 query secret。 |
| `model` | string | Provider 模型名或本地兼容模型名。 |
| `enabled` | boolean | 是否允许用于新请求。 |
| `is_default` | boolean | 是否为该用途默认 profile。 |
| `timeout_ms` | int | Provider 请求超时时间。 |
| `supports_streaming` | boolean | 是否支持 chat streaming。非 chat 用途应为 false。 |
| `dimensions` | int | Embedding 向量维度。非 embedding 用途为空。 |
| `top_n` | int | Rerank 默认返回数量。非 rerank 用途为空。 |
| `default_parameters_json` | jsonb | Provider 特定默认参数，例如 `temperature`、`top_p`、`max_tokens`。 |
| `api_key_configured` | boolean | 是否存在可用 provider 凭据。由 ProviderCredential 派生或同步更新。 |
| `credential_id` | string / uuid | 当前凭据记录 ID，可空。 |
| `created_by_user_id` | string | 创建人外部用户 ID，可空。 |
| `updated_by_user_id` | string | 最近修改人外部用户 ID，可空。 |
| `created_at` | datetime | 创建时间。 |
| `updated_at` | datetime | 更新时间。 |
| `deleted_at` | datetime | 软删除时间，可空。 |

枚举约束：

| 字段 | 允许值 |
| --- | --- |
| `purpose` | `chat`、`embedding`、`rerank` |
| `provider` | `openai_compatible`、`siliconflow`、`local_compatible` |

业务约束建议：

- `timeout_ms >= 1000`。
- `purpose = 'chat'` 时 `supports_streaming` 可为 true；其他用途必须为 false。
- `purpose = 'embedding'` 时 `dimensions` 建议非空且 `dimensions > 0`。
- `purpose = 'rerank'` 时 `top_n` 建议非空且 `top_n > 0`。
- 每个 `purpose` 最多一个默认 profile：部分唯一索引 `UNIQUE (purpose) WHERE enabled = true AND is_default = true AND deleted_at IS NULL`。
- `name` 在同一 `purpose` 下建议唯一。
- `base_url` 必须是 URI，不得包含 `api_key`、`token`、`secret`、`password` 等敏感 query 参数。

API 字段映射：

| 数据库字段 | AI Gateway API 字段 | Gateway admin API 字段 |
| --- | --- | --- |
| `id` | `id` | `id` |
| `name` | `name` | `name` |
| `purpose` | `purpose` | `purpose` |
| `provider` | `provider` | `provider` |
| `base_url` | `baseUrl` | `baseUrl` |
| `model` | `model` | `model` |
| `enabled` | `enabled` | `enabled` |
| `is_default` | `isDefault` | `isDefault` |
| `timeout_ms` | `timeoutMs` | `timeoutMs` |
| `supports_streaming` | `supportsStreaming` | `supportsStreaming` |
| `dimensions` | `dimensions` | `dimensions` |
| `top_n` | `topN` | `topN` |
| `default_parameters_json` | `defaultParameters` | `defaultParameters` |
| `api_key_configured` | `apiKeyConfigured` | `apiKeyConfigured` |
| `credential_id` | 不返回 | 不返回 |

### 6.2 ProviderCredential

表名建议：`provider_credentials`

Provider 凭据记录。该表只保存 secret 引用或加密密文元数据，不能保存明文 API key。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | 凭据记录 ID。 |
| `profile_id` | string / uuid | 所属 `model_profiles.id`。 |
| `storage_mode` | string | `secret_ref` 或 `encrypted_column`。 |
| `secret_ref` | string | 外部 secret manager 引用；`storage_mode=secret_ref` 时必填。 |
| `ciphertext` | bytea / text | 加密后的 API key；`storage_mode=encrypted_column` 时必填。 |
| `encryption_key_version` | string | 加密密钥版本，可空。 |
| `fingerprint_sha256` | string | API key 指纹，用于判断是否变更；字段名兼容存量 schema，写入值为 HMAC-SHA-256 指纹而不是裸 SHA-256。 |
| `key_last4` | string | 内部排障用后四位，可选；不返回公开 API。 |
| `status` | string | `active`、`rotated`、`disabled`、`deleted`。 |
| `created_by_user_id` | string | 创建人外部用户 ID，可空。 |
| `created_at` | datetime | 创建时间。 |
| `rotated_at` | datetime | 被新凭据替换时间，可空。 |
| `disabled_at` | datetime | 禁用时间，可空。 |
| `deleted_at` | datetime | 删除时间，可空。 |

约束建议：

- 同一个 profile 最多一个 `status = 'active'` 的凭据。
- `storage_mode = 'secret_ref'` 时 `secret_ref` 必填且 `ciphertext` 为空。
- `storage_mode = 'encrypted_column'` 时 `ciphertext` 必填且必须由应用层加密后写入。
- `storage_mode` 只能取 `secret_ref` 或 `encrypted_column`；不得新增 `plaintext`、`env` 或其他会暴露明文的模式。
- `fingerprint_sha256` 不得作为认证用途，只用于审计和变更检测；应用层必须用凭据加密密钥派生出的 HMAC key 计算 HMAC-SHA-256，不能保存明文 API key 的裸 SHA-256。
- 响应、日志和错误不得输出 `secret_ref`、`ciphertext`、`fingerprint_sha256` 或 `key_last4`。
- `encryption_key_version` 只记录版本标识，不记录原始密钥、KMS token 或本地密钥文件路径。

密钥轮换语义：

1. 管理端通过 gateway admin API 发送新的 write-only `apiKey`。
2. Gateway 不保存、不记录该字段，只转发给 AI Gateway。
3. AI Gateway 创建新的 `ProviderCredential(status=active)`。
4. 旧凭据标记为 `rotated`，保留最小审计元数据。
5. `model_profiles.credential_id` 指向新凭据，并更新 `api_key_configured = true`。

### 6.3 ModelProfileRevision

表名建议：`model_profile_revisions`

配置变更审计表。用于追踪管理员运行时配置变更，支持回溯但不要求自动回滚。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | Revision ID。 |
| `profile_id` | string / uuid | 所属 profile。 |
| `revision_no` | int | Profile 内递增版本号。 |
| `change_type` | string | `created`、`updated`、`credential_rotated`、`disabled`、`deleted`、`default_changed`。 |
| `changed_fields_json` | jsonb | 字段名列表或字段差异摘要。不得包含密钥明文。 |
| `before_snapshot_json` | jsonb | 脱敏前置快照，可空。 |
| `after_snapshot_json` | jsonb | 脱敏后置快照，可空。 |
| `changed_by_user_id` | string | 操作人外部用户 ID，可空。 |
| `caller_service` | string | 通常为 `gateway`。 |
| `request_id` | string | 请求追踪 ID。 |
| `created_at` | datetime | 变更时间。 |

脱敏规则：

- `before_snapshot_json` 和 `after_snapshot_json` 可包含 `apiKeyConfigured`，不得包含 `apiKey`、`secret_ref`、`ciphertext`、`fingerprint` 或 provider bearer token。
- `base_url` 如包含敏感 query 参数，必须在入库前拒绝或脱敏。

## 7. 调用与观测实体

### 7.1 ProviderInvocation

表名建议：`provider_invocations`

一次 AI Gateway 模型调用的脱敏摘要。该表用于排障、用量统计和依赖健康分析，不保存完整输入和输出。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | 调用记录 ID。 |
| `request_id` | string | 请求追踪 ID。 |
| `caller_service` | string | `qa`、`knowledge`、`document` 或 `gateway`。 |
| `external_user_id` | string | 触发调用的用户 ID，可空。 |
| `operation` | string | `chat_completion`、`embedding`、`reranking`。 |
| `profile_id` | string / uuid | 使用的 model profile ID。 |
| `provider` | string | Provider 类型快照。 |
| `model` | string | 实际调用的模型名。 |
| `stream` | boolean | 是否流式调用。 |
| `status` | string | `succeeded`、`failed`、`cancelled`、`timeout`。 |
| `provider_status_code` | int | Provider HTTP 状态码，可空。 |
| `prompt_tokens` | int | 输入 token 数，可空。 |
| `completion_tokens` | int | 输出 token 数，可空。 |
| `total_tokens` | int | 总 token 数，可空。 |
| `input_count` | int | embedding 输入条数或 rerank 文档数，可空。 |
| `embedding_dimensions` | int | embedding 维度，可空。 |
| `rerank_top_n` | int | rerank 请求 top_n，可空。 |
| `duration_ms` | int | 总耗时。 |
| `attempt_count` | int | provider 尝试次数。 |
| `normalized_error_code` | string | 归一化错误码，可空。 |
| `normalized_error_type` | string | OpenAI-style error type，可空。 |
| `error_message` | string | 脱敏错误摘要，可空。 |
| `created_at` | datetime | 调用开始时间。 |
| `finished_at` | datetime | 调用结束时间。 |

禁止保存：

- `messages` 原文。
- `input` 原文或 embedding payload。
- rerank `documents[].text` 原文。
- 完整 provider response body。
- tool call arguments 完整值。
- prompt、系统提示词、用户上传文档全文。

建议索引：

- `(created_at DESC)`。
- `(caller_service, created_at DESC)`。
- `(profile_id, created_at DESC)`。
- `(operation, status, created_at DESC)`。
- `(request_id)`。

### 7.2 ProviderInvocationAttempt

表名建议：`provider_invocation_attempts`

一次模型调用可能包含一次或多次 provider 尝试。第一阶段如果不做 retry/fallback，可只记录一次 attempt 或暂不落表。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | Attempt ID。 |
| `invocation_id` | string / uuid | 所属 `provider_invocations.id`。 |
| `attempt_no` | int | 尝试序号，从 1 开始。 |
| `provider` | string | Provider 类型快照。 |
| `base_url_host` | string | 脱敏后的 host，不含 path query。 |
| `model` | string | 模型名。 |
| `status` | string | `succeeded`、`failed`、`timeout`、`cancelled`。 |
| `provider_status_code` | int | Provider HTTP 状态码，可空。 |
| `duration_ms` | int | 本次尝试耗时。 |
| `error_code` | string | 脱敏错误码，可空。 |
| `error_message` | string | 脱敏错误摘要，可空。 |
| `started_at` | datetime | 尝试开始时间。 |
| `finished_at` | datetime | 尝试结束时间。 |

`base_url_host` 只能记录 host，例如 `api.siliconflow.cn`；不得记录完整 URL、query、token 或 path 中的敏感租户信息。

### 7.3 UsageAggregate

表名建议：`model_usage_aggregates`

可选聚合表，用于后续配额、成本统计或后台图表。第一阶段也可以由日志/指标系统承担。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | 聚合记录 ID。 |
| `bucket_start_at` | datetime | 时间桶开始。 |
| `bucket_granularity` | string | `minute`、`hour`、`day`。 |
| `caller_service` | string | 调用方服务。 |
| `profile_id` | string / uuid | 模型 profile ID。 |
| `operation` | string | `chat_completion`、`embedding`、`reranking`。 |
| `request_count` | int | 请求数。 |
| `success_count` | int | 成功数。 |
| `failure_count` | int | 失败数。 |
| `prompt_tokens` | bigint | 输入 token 汇总。 |
| `completion_tokens` | bigint | 输出 token 汇总。 |
| `total_tokens` | bigint | 总 token 汇总。 |
| `total_duration_ms` | bigint | 总耗时。 |
| `created_at` | datetime | 创建时间。 |
| `updated_at` | datetime | 更新时间。 |

聚合表不得使用 `external_user_id`、prompt hash、文档 ID 或 query 文本作为高基数指标标签。

## 8. 传输模型

以下模型主要来自 OpenAI-compatible / OpenAI-style API，默认不持久化。

### 8.1 ChatCompletionRequest

内部 API：`POST /internal/v1/chat/completions`

| 字段 | 持久化策略 |
| --- | --- |
| `model` | 可在 `ProviderInvocation.model` 中保存。 |
| `profile_id` | 可在 `ProviderInvocation.profile_id` 中保存。 |
| `messages` | 不持久化完整内容。 |
| `tools` | 不持久化完整 schema；QA 可保存自己的脱敏工具摘要。 |
| `tool_choice` | 可作为脱敏 metadata 保存，不能包含完整参数。 |
| `temperature`、`top_p`、`max_tokens` | 可保存为调用参数摘要。 |
| `stream` | 可保存为 `ProviderInvocation.stream`。 |
| `metadata` | 默认不持久化；如保存必须脱敏。 |

AI Gateway 不读取历史会话，不保存多轮上下文。调用方必须传入本次请求完整上下文并自行持久化业务状态。

### 8.2 EmbeddingRequest

内部 API：`POST /internal/v1/embeddings`

| 字段 | 持久化策略 |
| --- | --- |
| `model` | 可在调用摘要中保存。 |
| `profile_id` | 可在调用摘要中保存。 |
| `input` | 不持久化原文。 |
| `dimensions` | 可保存为 `embedding_dimensions`。 |
| `encoding_format` | 可保存为低敏参数摘要。 |

Embedding response 中的 `embedding` 在 `encoding_format=float` 时是 float array，在 `encoding_format=base64` 时是 base64 string；完整 embedding payload 不得保存到 AI Gateway 数据库或日志。向量持久化只归 `knowledge` 或向量数据库所有。

### 8.3 RerankingRequest

内部 API：`POST /internal/v1/rerankings`

| 字段 | 持久化策略 |
| --- | --- |
| `model` | 可在调用摘要中保存。 |
| `profile_id` | 可在调用摘要中保存。 |
| `query` | 不持久化原文。 |
| `documents[].text` | 不持久化原文。 |
| `documents[].id` | 默认不持久化；如为排障采样必须脱敏并受短 TTL 控制。 |
| `top_n` | 可保存为 `rerank_top_n`。 |

Reranking response 只返回排序分数和输入 ID/index；引用标题、章节路径、文件下载和业务过滤仍归 `knowledge` 或 `qa`。

## 9. 运行时响应模型

### 9.1 ReadinessCheck

`GET /readyz` 返回的检查项可以来自内存计算，不需要持久化。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `name` | string | `chat_profile`、`embedding_profile`、`rerank_profile`、`config_store` 等。 |
| `status` | string | `ok`、`missing`、`failed`。 |
| `message` | string | 脱敏诊断摘要。 |

检查规则以 [AI Gateway README 的配置与就绪语义](../README.md#配置与就绪语义) 为准，本数据模型文档只描述检查项结构。

`message` 不得包含 API key、secret ref、完整 base URL query、provider 原始错误体或内部堆栈。

### 9.2 NormalizedProviderError

AI Gateway 对 provider 错误做归一化后返回给调用方。该模型可作为内存模型、错误响应模型和调用日志摘要。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `code` | string | 项目错误码，例如 `dependency_error`、`rate_limited`。 |
| `openai_error_type` | string | OpenAI-style `error.type`，例如 `upstream_error`。 |
| `http_status` | int | 对调用方返回的 HTTP 状态。 |
| `provider_status_code` | int | Provider 原始状态码，可写入脱敏日志。 |
| `retryable` | boolean | 是否可重试。 |
| `message` | string | 稳定、简短、脱敏的错误文案。 |

不得保存或返回 provider 原始 body、认证 header、内部堆栈、完整 URL、prompt、输入文本或 embedding payload。

## 10. 状态机

### 10.1 ModelProfile lifecycle

```text
created -> enabled -> disabled -> enabled
created -> deleted
disabled -> deleted
```

约束：

- `deleted` profile 不应用于新请求。
- 已被业务配置引用的 profile 是否允许删除由 AI Gateway 返回 `conflict` 决定；实现也可以将删除降级为禁用。
- 切换默认 profile 必须在同一事务内取消同用途旧默认 profile，避免出现两个默认项。

### 10.2 ProviderCredential lifecycle

```text
active -> rotated
active -> disabled
rotated -> deleted
disabled -> deleted
```

约束：

- `active` 凭据用于新 provider 请求。
- `rotated` 和 `disabled` 凭据不得用于新请求。
- 删除凭据不应删除 profile；只会使 `api_key_configured = false` 或阻止新调用。

### 10.3 ProviderInvocation status

```text
running -> succeeded
running -> failed
running -> timeout
running -> cancelled
```

如果实现只在请求结束后写调用记录，可以省略 `running` 状态。

## 11. 索引建议

| 表 | 索引 |
| --- | --- |
| `model_profiles` | `purpose`、`enabled`、`is_default`、`deleted_at`、`updated_at DESC` |
| `provider_credentials` | `profile_id`、`status`、`created_at DESC` |
| `model_profile_revisions` | `profile_id, revision_no DESC`、`created_at DESC`、`request_id` |
| `provider_invocations` | `created_at DESC`、`request_id`、`caller_service, created_at DESC`、`profile_id, created_at DESC`、`operation, status, created_at DESC` |
| `provider_invocation_attempts` | `invocation_id, attempt_no`、`started_at DESC` |
| `model_usage_aggregates` | `bucket_start_at DESC`、`caller_service, bucket_start_at DESC`、`profile_id, bucket_start_at DESC` |

## 12. 安全与脱敏规则

- 数据库、日志、错误响应和 metrics 不得保存或输出 API key 明文。
- 数据库连接串、内部服务 token、secret ref、加密密钥引用和 provider bearer token 与 API key 同级处理，不得出现在日志、响应、审计快照或指标标签中。
- Provider base URL 入库前必须校验并拒绝含敏感 query 参数的 URL。
- 调用日志不得保存完整 prompt、完整 generated answer、完整 embedding payload、用户上传文档全文、MCP 原始参数或 provider 原始响应体。
- `default_parameters_json` 不得包含密钥、token、内部 URL、prompt 模板或文档内容。
- `metadata` 字段必须进行字段名黑名单和长度限制校验。
- 错误摘要只能保存稳定错误码和脱敏 message，不保存 provider 原始 body。
- 指标标签不得使用用户输入文本、prompt hash、document text、API key fingerprint 或 object key。

推荐字段黑名单至少包含以下大小写不敏感关键词：`api_key`、`apikey`、`authorization`、`bearer`、`token`、`secret`、`password`、`credential`、`connection_string`、`database_url`、`object_key`、`prompt`、`document_text`、`provider_response`。

## 13. 边界对齐

| 数据或行为 | Owner | AI Gateway 处理方式 |
| --- | --- | --- |
| Provider API key | `ai-gateway` | 保存 secret 引用或加密密文元数据，响应只返回 `apiKeyConfigured`。 |
| Chat/embedding/rerank 调用适配 | `ai-gateway` | 归一化请求/响应和错误，不保存业务状态。 |
| QA 会话、消息、工具调用、引用 | `qa` | 只接收调用请求并返回 OpenAI-compatible 输出。 |
| Knowledge chunk、embedding/index 持久化 | `knowledge` | 只生成 embedding 或 rerank 分数，不写向量库。 |
| Report 任务、大纲、章节、文件 | `document` | 只提供 chat/streaming 输出，不保存报告状态。 |
| Public admin model-profile API | `gateway` + `ai-gateway` | Gateway 负责公开入口和管理员鉴权；AI Gateway 负责配置存储和密钥状态。 |

新增字段或表时，应先确认它是否属于 provider/model invocation 边界；如果字段描述的是 QA、knowledge、document 或 auth 的业务状态，应放回对应 owner service。

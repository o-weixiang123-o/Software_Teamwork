# File 服务数据模型文档

版本：v0.2
日期：2026-06-29
范围：`services/file/` 负责的基础文件对象元数据、对象存储引用、删除清理任务和内部服务调用逻辑模型

## 1. 文档说明

本文定义 `file` 服务的逻辑数据模型，用于支撑基础文件对象元数据、对象存储引用、删除清理和内部服务调用。

本文只描述逻辑数据模型，不提供具体 SQL 建表语句。后续实现应根据 `services/file` 的 PostgreSQL repository 和 migration 规范转换为 forward-only migration。服务级内部 API 契约见 [`../api/internal.openapi.yaml`](../api/internal.openapi.yaml)，前端稳定公开契约仍以 [`docs/services/gateway/api/public.openapi.yaml`](../../gateway/api/public.openapi.yaml) 为准。

本文按 [`docs/architecture/technology-decisions.md`](../../../architecture/technology-decisions.md) 的最新技术基线编写：

- PostgreSQL 访问使用 `pgx` + `sqlc`，不使用 ORM。
- 数据库迁移使用 `goose`，迁移文件由 `services/file/migrations/` 拥有。
- 对象存储使用官方 MinIO Go SDK，File Service 统一封装 bucket、object key 和存储凭据。
- 物理删除可由 `asynq` worker 执行，但 PostgreSQL 中的状态、错误摘要和重试次数是权威来源。
- 日志使用 `slog`，日志和指标不得记录 object key、内部 URL、存储凭据、token 或原始文件内容。

## 2. 存储边界

### 2.1 File 持久化数据库

File 数据库只保存基础文件对象元数据：

- file 服务内部文件 ID。
- 展示用文件名。
- 内容类型。
- 文件大小。
- checksum。
- 内部对象存储位置。
- 创建、删除和清理状态。

File 数据库不得保存知识库、报告或权限领域的业务数据，包括但不限于：

- `knowledge_base_id`、`document_id`、文档处理状态、parser 配置、chunk、embedding 或 runtime doc-engine point。
- `report_id`、`template_id`、`material_id`、`report_file_id`、报告状态、模板结构或素材引用关系。
- 业务标签、业务 ACL、用户可见性规则、租户配额归属、审计日志主数据。

### 2.2 对象存储

MinIO 或等价对象存储保存原始文件二进制。Bucket、object key、etag、version id 和内部 URL 都是 file 服务内部实现细节，不得通过 gateway 或 owner service 的公开响应返回。

Owner service 只能保存 file 服务返回的内部 `file_ref`，并在自己的资源边界内暴露业务资源 ID。例如：

| Owner service | 业务资源 | 公开内容读取路径 |
| --- | --- | --- |
| `knowledge` | knowledge document | `/api/v1/documents/{documentId}/content` |
| `document` | report template | `/api/v1/report-templates/{reportTemplateId}` |
| `document` | report material | `/api/v1/report-materials/{materialId}` |
| `document` | report file | `/api/v1/report-files/{reportFileId}/content` |

## 3. 设计原则

- 数据库字段使用 snake_case；API 字段使用 camelCase。
- file 服务自己的内部资源 ID 可以在服务间契约中表示为 `fileId`，但不得作为前端业务资源 ID 暴露。
- `storage_bucket`、`storage_object_key`、`storage_version_id`、内部 URL 和存储凭据不得进入公开 API、错误响应或业务事件。
- PostgreSQL 是基础文件元数据、删除状态、清理失败摘要和重试计数的事实来源；Redis、asynq、MinIO etag 或缓存都不能替代 PostgreSQL 状态。
- 上传文件名来自用户输入，只能作为展示名保存，不能直接作为 object key。
- file 服务不判断知识库文档、报告模板、报告素材或报告导出文件的业务权限；这些权限由 owner service 判断。
- 删除采用元数据软删除加对象清理状态，物理删除失败必须可重试。
- `sqlc` query 必须显式列名，不使用 `SELECT *`；用户输入只能通过参数绑定进入 SQL。

## 4. 实体关系概览

```text
FileObject 1 -- N FileObjectEvent
FileObject 1 -- 0..1 FileDeletionTask
```

`FileObjectEvent` 和 `FileDeletionTask` 可按实现复杂度后续添加；第一阶段可以只落 `file_objects`。

## 5. 通用字段约定

| 字段 | 说明 |
| --- | --- |
| `id` | 主键。建议使用 UUID 或带 `file_` 前缀的字符串 ID。 |
| `created_at` | 创建时间。 |
| `updated_at` | 更新时间。 |
| `deleted_at` | 软删除时间。 |
| `request_id` | Gateway 或服务间调用传入的请求追踪 ID。 |

## 6. 核心实体

### 6.1 FileObject

表名建议：`file_objects`

基础文件对象记录。该表是 file 服务能否定位原始对象的事实来源。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | file 服务内部文件 ID，对应内部 API `FileObject.id`。 |
| `filename` | string | 展示用原始文件名或规范化文件名。 |
| `content_type` | string | 内容类型；缺失或不可信时使用 `application/octet-stream`。 |
| `size_bytes` | bigint | 文件大小。 |
| `checksum_sha256` | string | SHA-256 checksum，可空但建议保存。 |
| `storage_backend` | string | 存储后端，例如 `minio`、`local`、`memory`。 |
| `storage_bucket` | string | 内部 bucket 名，不对外返回。 |
| `storage_object_key` | string | 内部 object key，不对外返回。 |
| `storage_version_id` | string | 对象版本 ID，可空，不对外返回。 |
| `storage_etag` | string | 对象存储 etag，可空。 |
| `status` | string | 文件状态，见状态枚举。 |
| `created_by_service` | string | 调用方服务，例如 `knowledge` 或 `document`。 |
| `request_id` | string | 创建文件对象时的请求追踪 ID，可空。 |
| `created_at` | datetime | 创建时间。 |
| `updated_at` | datetime | 更新时间。 |
| `deleted_at` | datetime | 软删除时间，可空。 |
| `delete_requested_at` | datetime | 请求删除时间，可空。 |
| `purged_at` | datetime | 物理对象清理完成时间，可空。 |
| `last_error_code` | string | 最近一次存储或清理错误码，可空。 |
| `last_error_message` | string | 脱敏后的最近错误摘要，可空。 |

状态枚举建议：

| status | 说明 |
| --- | --- |
| `available` | 元数据和对象均可读取。 |
| `delete_requested` | owner service 已请求删除，后续不应再允许普通读取。 |
| `purging` | 正在物理清理对象。 |
| `purged` | 物理对象已清理。 |
| `failed` | 写入或清理失败，需要重试或人工介入。 |

公开 API 字段映射：

| 数据库字段 | 内部 API 字段 |
| --- | --- |
| `id` | `id` |
| `filename` | `filename` |
| `content_type` | `contentType` |
| `size_bytes` | `sizeBytes` |
| `checksum_sha256` | `checksumSha256` |
| `created_at` | `createdAt` |
| `deleted_at` | `deletedAt` |
| `storage_bucket` | 不返回 |
| `storage_object_key` | 不返回 |
| `storage_version_id` | 不返回 |

PostgreSQL 类型建议：

| 字段 | PostgreSQL 类型建议 | 说明 |
| --- | --- | --- |
| `id` | `text` 或 `uuid` | 如果使用字符串 ID，建议统一 `file_` 前缀。 |
| `filename` | `text` | 写入前限制长度，建议不超过 255 字符。 |
| `content_type` | `text` | 缺失或不可信时落 `application/octet-stream`。 |
| `size_bytes` | `bigint` | 使用 `CHECK (size_bytes >= 0)`。 |
| `checksum_sha256` | `text` | 使用 `CHECK` 约束为空或 64 位十六进制。 |
| `storage_backend` | `text` | 第一阶段枚举 `memory`、`local`、`minio`，生产必须使用 `minio` 或等价持久后端。 |
| `status` | `text` | 第一阶段可用 `CHECK` 约束枚举；引入 PostgreSQL enum 前需评估迁移成本。 |
| 时间字段 | `timestamptz` | 统一保存 UTC 语义，API 输出 RFC 3339 / OpenAPI `date-time`。 |

约束建议：

- `size_bytes >= 0`。
- `checksum_sha256` 为空或满足 64 位十六进制字符串。
- `status` 只能取允许枚举。
- `storage_backend`、`storage_bucket`、`storage_object_key` 对 `available` 文件必须非空。
- `deleted_at` 非空后，普通读取接口不得继续返回内容流。
- 索引：`created_at DESC`、`status`、`checksum_sha256`、`deleted_at`、`created_by_service`。

### 6.2 FileObjectEvent

表名建议：`file_object_events`

可选事件表，用于排查上传、读取、删除和清理流程。第一阶段如果已有统一审计服务，可不落本表。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | 事件 ID。 |
| `file_id` | string / uuid | 所属 file object。 |
| `event_type` | string | 事件类型。 |
| `caller_service` | string | 调用方服务。 |
| `request_id` | string | 请求追踪 ID。 |
| `status` | string | 事件结果，例如 `succeeded`、`failed`。 |
| `error_code` | string | 脱敏错误码，可空。 |
| `error_message` | string | 脱敏错误摘要，可空。 |
| `created_at` | datetime | 事件时间。 |

事件类型建议：

```text
created
content_read
delete_requested
purge_started
purge_succeeded
purge_failed
```

事件不得记录 object key、内部 URL、存储凭据或原始文件内容。

### 6.3 FileDeletionTask

表名建议：`file_deletion_tasks`

可选清理任务表，用于对象存储物理删除的重试和恢复。即使执行层使用 `asynq`，该表仍用于保存业务可追溯状态；asynq 只负责排队、调度和执行。

| 字段 | 类型建议 | 说明 |
| --- | --- | --- |
| `id` | string / uuid | 清理任务 ID。 |
| `file_id` | string / uuid | 目标 file object。 |
| `task_type` | string | 任务类型，固定建议 `file:object:purge`。 |
| `asynq_task_id` | string | 队列任务 ID，可空；不得作为业务状态唯一来源。 |
| `status` | string | 清理任务状态。 |
| `attempts` | int | 已尝试次数。 |
| `max_attempts` | int | 最大尝试次数。 |
| `next_attempt_at` | datetime | 下次尝试时间。 |
| `locked_at` | datetime | worker 锁定任务时间，可空。 |
| `finished_at` | datetime | 清理完成或终止时间，可空。 |
| `last_error_code` | string | 最近错误码，可空。 |
| `last_error_message` | string | 脱敏错误摘要，可空。 |
| `request_id` | string | 触发删除请求的追踪 ID，可空。 |
| `created_at` | datetime | 创建时间。 |
| `updated_at` | datetime | 更新时间。 |

任务状态建议：

```text
queued
running
succeeded
failed
cancelled
```

asynq payload 必须是 JSON，并包含可追踪字段：

```json
{
  "requestId": "req_123",
  "jobId": "del_123",
  "fileId": "file_123",
  "callerService": "knowledge"
}
```

payload 不得包含 bucket、object key、内部 URL、MinIO access key、原始 token 或文件内容。

## 7. 服务间引用约定

Owner service 保存的内部 `file_ref` 应只用于服务间调用，不作为公开 API 字段返回。

建议 owner service 存储字段统一使用：

| 字段 | 说明 |
| --- | --- |
| `file_ref` | file 服务返回的内部文件引用。当前实现可等价保存 file ID，后续也可扩展为不透明引用。 |
| `filename` 或 `file_name` | owner resource 对外展示用文件名快照。 |
| `file_size` | 文件大小快照。 |
| `content_type` | 内容类型快照。 |

`file_ref` 不得用于跨服务业务权限判断。业务权限必须基于 owner service 的资源 ID，例如 `document_id`、`report_template_id`、`material_id` 或 `report_file_id`。

## 8. 关键约束

- File 服务不能保存知识库或报告业务字段。
- File 服务不能向前端暴露任何 `/api/v1/files/**` 能力。
- File 服务不能返回 MinIO bucket、object key、内部 URL、预签名 URL 或 access key。
- Owner service 删除业务资源后，应协调内部 `file_ref` 进入删除或延迟清理流程。
- 物理对象删除失败不能恢复业务资源可见性，只能进入清理重试或人工处理。
- 日志和事件必须脱敏，不记录原始文件内容、内部 URL 或存储凭据。

## 9. sqlc 与迁移约定

`services/file` 后续落地 PostgreSQL repository 时应使用以下目录约定：

```text
services/file/
  sqlc.yaml
  internal/repository/queries/
  internal/repository/sqlc/
  migrations/
```

`sqlc` 生成代码只能被 repository 适配层调用，不能直接暴露给 HTTP handler 或 owner service client。service/use-case 层发起事务时，repository 应接收 `pgx.Tx` 或抽象后的 querier。

首批 query 建议覆盖：

| Query | 说明 |
| --- | --- |
| `CreateFileObject` | 插入基础文件元数据和内部存储引用。 |
| `GetFileObject` | 按 file ID 读取未删除或指定状态的元数据。 |
| `MarkFileDeleteRequested` | 将文件标记为删除请求，写入 `delete_requested_at` 和 `deleted_at`。 |
| `MarkFilePurging` | 标记进入物理清理。 |
| `MarkFilePurged` | 标记物理对象已清理。 |
| `MarkFileFailed` | 写入脱敏后的失败码和失败摘要。 |
| `CreateFileDeletionTask` | 创建可追溯清理任务。 |
| `ListDueFileDeletionTasks` | 扫描到期清理任务，供同步 worker 或 asynq worker 使用。 |

迁移使用 `goose`，文件名示例：

```text
0001_create_file_objects.sql
0002_create_file_deletion_tasks.sql
0003_create_file_object_events.sql
```

第一阶段允许 forward-only migration。若写 down migration，必须能在本地和 CI 验证。

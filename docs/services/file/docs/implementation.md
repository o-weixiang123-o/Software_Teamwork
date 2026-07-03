# File 服务实现说明

版本：v0.6
日期：2026-07-04
范围：`services/file/` 当前实现、契约对齐、缺口和后续实现约束

## 1. 文档定位

本文档描述 `file` 当前实现状态和后续实现约束。它只补充服务 README、OpenAPI、架构和技术选型文档，不覆盖这些上游契约。

权威来源：

| 类型 | 权威来源 | 本文档关系 |
| --- | --- | --- |
| 服务公开说明 | `docs/services/file/README.md` | 只能补充，不能覆盖 |
| 服务 OpenAPI | `docs/services/file/api/internal.openapi.yaml`；`services/file/api/openapi.yaml` 是实现本地路由副本 | 只能跟随，不能另起契约 |
| Gateway 公开契约 | `docs/services/gateway/api/public.openapi.yaml` | 前端稳定契约以 gateway 为准 |
| 服务边界 | `docs/architecture/service-boundaries.md` | 必须遵守 |
| 技术基线 | `docs/architecture/technology-decisions.md` | 必须跟随 |
| 代码实现 | `services/file/` | 本文档记录当前状态和差距 |

凡是本文档与上表文件冲突，以上游文件为准；发现冲突时，在“文档与实现出入”中记录并生成回写或实现任务。

## 2. 当前结论

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| 文档状态 | active | README、数据模型和内部 OpenAPI 已存在。 |
| 代码状态 | partial | Go service、`/internal/v1/files/**`、memory/local/MinIO object store、file_objects migration、PostgreSQL metadata runtime、`pgx/v5@v5.9.2`、service-token 校验、caller allowlist、MIME sniffing allowlist、service-layer streaming upload 和同步 purge 收口已存在。 |
| 契约对齐 | active | 内部 OpenAPI 声明 `/internal/v1/files/**`；代码已移除 knowledge-document 兼容路由，并增加 docs/service OpenAPI 漂移测试。PostgreSQL metadata runtime 已按 `FILE_DATABASE_URL` 接入；MinIO adapter 已落地，根级 Compose 已提供 MinIO server/mc bucket 初始化。 |
| 数据持久化 | memory / postgres metadata + memory/local/MinIO objects | `FILE_DATABASE_URL` 为空时 metadata 使用 memory repository；配置后使用 PostgreSQL repository。object bytes 可用 memory、local 或 MinIO adapter。非本地 `FILE_ENV` 拒绝 memory object storage 和空 metadata DB。 |
| 测试状态 | partial | `go test ./...` 覆盖 service、handler、local storage、MinIO adapter、config、caller policy、MIME policy、默认跳过的 PostgreSQL repository smoke 和默认跳过的 PostgreSQL + MinIO 联合 smoke。 |
| 建议动作 | 联调 / 回写文档 | 在可用 PostgreSQL/MinIO 环境中运行 env-gated smoke，并继续由 #125/#289 等任务补跨服务 smoke。 |

## 3. 已实现

| 能力 | 代码位置 | 契约来源 | 验证方式 | 备注 |
| --- | --- | --- | --- | --- |
| 健康检查 | `services/file/internal/http/server.go` | `docs/services/file/api/internal.openapi.yaml` | `cd services/file && go test ./...` | `GET /healthz`、`GET /readyz`。 |
| 基础文件创建 | `services/file/internal/http/server.go`、`services/file/internal/service/service.go` | `docs/services/file/api/internal.openapi.yaml` | `TestFileCreateGetContentDeleteFlow` | `POST /internal/v1/files` 接收 multipart，计算或校验 SHA-256。 |
| 基础文件元数据读取 | `services/file/internal/service/service.go` | `docs/services/file/api/internal.openapi.yaml` | `TestFileCreateGetContentDeleteFlow` | 返回 file ID、文件名、content type、大小、checksum，不返回 object key。 |
| 基础文件内容读取 | `services/file/internal/http/server.go` | `docs/services/file/api/internal.openapi.yaml` | `TestFileCreateGetContentDeleteFlow` | 成功时返回二进制流，不包 JSON envelope。 |
| 基础文件删除 | `services/file/internal/service/service.go` | `docs/services/file/README.md` | `TestDeleteFileHidesMetadataAndContent` | 删除后 metadata/content 读取返回 not found。 |
| 上传校验 | `services/file/internal/service/service.go`、`services/file/internal/http/server_test.go` | `docs/services/file/README.md` | handler/service tests | 覆盖缺少文件、空文件、超限、checksum 格式和 mismatch。 |
| service-layer streaming upload | `services/file/internal/service/service.go` | File README / 安全基线 | `TestCreateFileStreamsThroughObjectStore` | service 只读取 bounded sniff prefix，然后通过 reader wrapper 流式写入 `ObjectStore.Put` 并计算 SHA-256。memory adapter 仍为 test/local adapter 内部缓冲。 |
| MIME sniffing allowlist | `services/file/internal/service/service.go`、`services/file/internal/config/config.go` | File README / 权限矩阵 | service/config tests | `FILE_ALLOWED_CONTENT_TYPES` 为空时兼容；配置后按嗅探后的有效 MIME 校验，未知二进制按 `application/octet-stream`。 |
| local object store | `services/file/internal/platform/storage/local.go` | `docs/services/file/README.md` | `TestLocalStorePutGetDelete` | 可用于本地持久化 smoke；metadata 仍非持久。 |
| MinIO object store | `services/file/internal/platform/storage/minio.go` | `docs/services/file/README.md`、`docs/architecture/technology-decisions.md` | `TestMinIOStorePutSendsContentTypeChecksumAndSize` 等 adapter tests | 使用官方 `github.com/minio/minio-go/v7@v7.2.1` SDK；隐藏 bucket、object key、内部 URL 和凭据。 |
| PostgreSQL metadata runtime | `services/file/cmd/server/main.go`、`services/file/internal/repository/postgres.go` | 数据模型 / 技术选型 | `go test ./...`、`FILE_TEST_DATABASE_URL` smoke（可选） | `FILE_DATABASE_URL` 配置后使用 PostgreSQL repository；未配置时保留 memory 模式。 |
| PostgreSQL + MinIO 联合 smoke | `services/file/internal/integration/minio_postgres_smoke_test.go`、`deploy/docker-compose.yml`（root local infra baseline） | #286 / 本地联调手册 | `FILE_MINIO_POSTGRES_SMOKE=1 ... go test ./internal/integration -run TestFileMinIOPostgresSmoke -count=1 -v` | 默认跳过；显式启用后通过 HTTP API 验证上传、metadata 写入、内容读取、删除和清理。 |
| service token 校验 | `services/file/internal/config/config.go`、`services/file/internal/http/server.go` | File README / 内部服务契约 | handler/config tests | DB 模式要求 `FILE_INTERNAL_SERVICE_TOKEN` 或 `INTERNAL_SERVICE_TOKEN`；保护 `/internal/v1/files/**`。 |
| caller operation allowlist | `services/file/internal/config/config.go`、`services/file/internal/http/server.go` | File 权限矩阵 | `TestFileRoutesEnforceCallerPolicyByOperation` | `FILE_ALLOWED_CREATE_CALLERS`、`FILE_ALLOWED_READ_CALLERS`、`FILE_ALLOWED_DELETE_CALLERS` 为空时兼容；配置后按 create/read/delete 分别返回 `401` 或 `403`。 |
| 非本地 backend guard | `services/file/internal/config/config.go` | File README / 技术基线 | config tests | `FILE_ENV` fallback `ENV` fallback `local`；非 local/test/development 时拒绝 memory object storage 和空 `FILE_DATABASE_URL`。 |
| readyz runtime 状态 | `services/file/internal/http/server.go` | `docs/services/file/api/internal.openapi.yaml` | handler tests | 返回 metadata/storage backend、service token 配置状态和 PostgreSQL dependency 状态。 |
| legacy knowledge-document 路由移除 | `services/file/internal/http/server.go`、`services/file/internal/http/server_test.go` | File 只保留目标内部 OpenAPI | `TestLegacyKnowledgeDocumentRoutesReturnNotFound` | 旧 `/internal/v1/knowledge-bases/{knowledgeBaseId}/documents` 和 `/internal/v1/documents/{documentId}` 调用面返回 `404 not_found`。 |
| file_objects migration | `services/file/migrations/0001_create_file_objects.sql` | `docs/services/file/docs/data-models.md` | 需手工 goose apply；env-gated test 会应用 migration | PostgreSQL runtime 使用该 schema。 |
| OpenAPI 漂移测试 | `services/file/internal/http/openapi_contract_test.go` | `docs/services/file/api/internal.openapi.yaml`、`services/file/api/openapi.yaml` | `cd services/file && go test ./...` | 解析并归一化比较两份 File internal OpenAPI，避免 docs/code contract 漂移。 |
| purge 收口 | `services/file/internal/service/service.go`、`services/file/internal/repository/*` | File README / data models | service/repository tests | 删除路径标记 delete requested/purging/purged；对象不存在视为成功，存储失败写入脱敏 failure summary 并允许重试。 |

## 4. 未实现

| 缺口 | 文档来源 | 影响范围 | 建议任务 |
| --- | --- | --- | --- |
| 跨服务 owner smoke 仍默认不跑 | `docs/services/file/README.md`、`docs/testing/reports/2026-07-01/file-module-test-report.md` | integration | 由 #125/#289 等跨服务任务继续覆盖 owner service 到 File 的真实链路。 |

## 5. 文档与实现出入

| 出入点 | 文档要求 | 当前实现 | 风险 | 建议处理 |
| --- | --- | --- | --- | --- |
| 生产对象存储 | 文档要求 File Service 封装 MinIO | 已有 MinIO adapter；根级 Compose 已固定 MinIO server/mc 镜像并初始化本地实现细节 bucket `software-teamwork-local`；File 联合 smoke 需显式 env 启用 | 普通 CI 不跑真实对象存储；跨服务链路仍需 #125/#289 等任务覆盖 | 使用 env-gated smoke 记录本机验证结果，不把根级 Compose 误写成完整 E2E。 |
| 服务 OpenAPI 与代码路由 | `docs/services/file/api/internal.openapi.yaml` 只声明 `/internal/v1/files/**` | 代码只注册 `/internal/v1/files/**`、`/healthz`、`/readyz`；`services/file/api/openapi.yaml` 是实现本地副本 | 两份 OpenAPI 若漂移会影响调用方 | `openapi_contract_test.go` 在 `go test ./...` 中自动比较。 |
| 配置说明 | 早期实现说明列出 `FILE_DATABASE_URL`、MinIO、Redis 等 | runtime 已解析 `FILE_DATABASE_URL`、service token 和 MinIO 配置；当前 cleanup 通过删除路径同步收口，不依赖 Redis/asynq | 部署若只配置 Redis 不会改变 File cleanup 行为 | README 和 implementation 只把已接入项写成 runtime 能力，未落地项放在缺口。 |

## 6. MVP / mock / memory backend / 占位

| 项目 | 当前用途 | 退出条件 | 关联任务 |
| --- | --- | --- | --- |
| memory repository | handler tests 和无 DB 本地运行 | 部署/真实联调统一配置 `FILE_DATABASE_URL` 后，memory 仅保留测试和早期本地用途；非本地 `FILE_ENV` 已拒绝空 DB | PostgreSQL runtime 已接入；仍需部署联调 |
| memory object store | 单元测试和无文件系统本地运行 | local/MinIO smoke 成为默认验证路径；非本地 `FILE_ENV` 已拒绝 `FILE_STORAGE_BACKEND=memory` | MinIO adapter / smoke 任务 |
| local object store | 本地持久化 smoke | MinIO adapter 可在 Compose/部署中使用 | MinIO adapter / smoke 任务 |
| MinIO object store | 持久对象存储 adapter，可接已有 MinIO/S3 兼容端点；根级 Compose 提供本地 MinIO 和 File Service 内部 bucket 初始化 | 真实依赖 smoke 成为 PR/联调时的显式验证项；完整跨服务 E2E 仍由 #125/#289 等任务承接 | #286 / 跨服务 smoke 任务 |
| legacy knowledge-document 路由 | 已从 File service 删除 | owner service 只调用 `/internal/v1/files/**` | #341 |

## 7. 运行与配置

| 项目 | 当前状态 | 缺口 |
| --- | --- | --- |
| 启动命令 | `cd services/file && go run ./cmd/server` | host-run。 |
| 环境变量 | `FILE_ENV`、`ENV`、`FILE_HTTP_ADDR`、`FILE_MAX_UPLOAD_BYTES`、`FILE_STORAGE_BACKEND`、`FILE_LOCAL_STORAGE_DIR`、`FILE_MINIO_ENDPOINT`、`FILE_MINIO_ACCESS_KEY`、`FILE_MINIO_SECRET_KEY`、`FILE_MINIO_BUCKET`、`FILE_MINIO_USE_SSL`、`FILE_MINIO_REGION`、`FILE_MINIO_TIMEOUT`、`FILE_DATABASE_URL`、`FILE_INTERNAL_SERVICE_TOKEN`、`INTERNAL_SERVICE_TOKEN`、`FILE_ALLOWED_CONTENT_TYPES`、`FILE_ALLOWED_CREATE_CALLERS`、`FILE_ALLOWED_READ_CALLERS`、`FILE_ALLOWED_DELETE_CALLERS`、`FILE_SHUTDOWN_TIMEOUT` | Redis 尚未被 runtime 使用。 |
| PostgreSQL / migration | `migrations/0001_create_file_objects.sql`、`sqlc.yaml`、`queries/file_objects.sql`、runtime repository、`internal/repository/sqlc` | 真实 PostgreSQL smoke 需要本地 DB。 |
| Redis / queue | 未使用 | 当前 cleanup 在 File delete path 中同步收口；如后续引入独立 async worker，仍需以 PostgreSQL metadata 为最终状态源。 |
| Object storage / vector store / AI provider | memory/local/MinIO object store；根级 Compose 启动 MinIO 并通过 `minio-init` 初始化本地 File Service 内部 bucket `software-teamwork-local` | File 不涉及 vector/AI provider；File 容器在根级 Compose 中仍默认 `local` storage，MinIO 路径通过显式 smoke 验证。 |

## 8. 测试与验证

| 验证项 | 命令或步骤 | 当前结果 | 缺口 |
| --- | --- | --- | --- |
| 单元测试 | `cd services/file && go test ./...` | pass（2026-07-04） | 不覆盖真实 MinIO server；env-gated DB smoke 默认跳过。 |
| 集成测试 | `FILE_TEST_DATABASE_URL=... go test ./internal/repository` | available / not run by default | 需要真实 DB；测试会创建独立 schema、应用 migration 并验证 repository metadata lifecycle。 |
| 联合 smoke | `FILE_MINIO_POSTGRES_SMOKE=1 FILE_TEST_DATABASE_URL=... FILE_MINIO_ENDPOINT=... FILE_MINIO_ACCESS_KEY=... FILE_MINIO_SECRET_KEY=... FILE_MINIO_BUCKET=... go test ./internal/integration -run TestFileMinIOPostgresSmoke -count=1 -v` | blocked locally（2026-07-01：`localhost:5432` PostgreSQL connection refused） | 需要真实 PostgreSQL 和 MinIO；测试会创建独立 schema，上传/读取/删除对象，并验证 metadata 清理状态。 |
| 契约测试 | handler tests against `/internal/v1/files/**` + OpenAPI drift test | active | `openapi_contract_test.go` 自动校验 docs/service OpenAPI 一致。 |
| 手工 smoke | `FILE_STORAGE_BACKEND=local go run ./cmd/server` 后上传/读取/删除 | pass（既有记录，2026-06-30：healthz、上传、读取、删除、删除后 404；本轮文档审计未重跑） | local 路径仍可用于无 MinIO 的快速验证；真实 MinIO 路径使用 env-gated 联合 smoke。 |

## 9. 建议任务

| 任务 | 类型 | 优先级 | 依据 | 说明 |
| --- | --- | --- | --- | --- |
| 将 PostgreSQL smoke 纳入稳定 CI 或 runbook | 新任务 | P1 | env-gated 测试已存在但默认跳过 | 在服务级 Compose 或 CI DB 可用后固定执行方式。 |
| 扩展跨服务 smoke | 新任务 | P1 | File service smoke 已存在但 owner-service 链路仍由并行任务覆盖 | 覆盖 Knowledge/Document owner service 调 File 的真实运行链路。 |

## 10. 最近检查记录

| 日期 | 检查人/工具 | 代码基准 | 结论 |
| --- | --- | --- | --- |
| 2026-06-29 | Codex goal | `eddf917` + working tree | File 已有可用基础文件 API 和本地存储雏形，但生产持久化、MinIO、service token 和兼容路由退出仍未完成。 |
| 2026-06-30 | Codex task #154 | `upstream/develop` + `L1nggTeam/feat/file-minio-storage` | File 已接入官方 MinIO Go SDK adapter 和 runtime 配置；metadata runtime、异步清理 worker、service token 和 MinIO Compose smoke 仍未完成。 |
| 2026-06-30 | Codex goal #235 | working tree | PostgreSQL metadata runtime、`X-Service-Token` 校验、readyz backend/dependency 状态和 env-gated repository smoke 已补齐；异步清理、MinIO Compose smoke 和兼容路由退出仍未完成。 |
| 2026-06-30 | Codex issue #286 | `Special/test/file-minio-pg-smoke` | 新增默认跳过的 PostgreSQL + MinIO 联合 smoke；根级 Compose 的 MinIO bucket 初始化和运行命令已写入 README/runbook。 |
| 2026-06-30 | Codex full-day audit | `develop@92d3afc` | 复核今日 PR/issue：File 已包含 MinIO adapter、PostgreSQL metadata runtime、service-token 校验、`pgx/v5@v5.9.2` 和默认跳过的 PostgreSQL + MinIO 联合 smoke；剩余为兼容 knowledge-document 路由退出、异步清理 worker、sqlc 生成代码和跨服务 smoke。 |
| 2026-07-01 | Codex issue #341 | working tree | 移除 File legacy knowledge-document 路由和 repository 方法；新增 OpenAPI 漂移测试；删除/purge 路径可重试且 failure summary 脱敏；`sqlc@v1.31.1` 生成产物已落地；`go test ./...`、`go build ./cmd/server`、`git diff --check` 通过，MinIO+PostgreSQL smoke 因本机 PostgreSQL 未监听 `localhost:5432` 阻断。 |
| 2026-07-04 | Codex task `07-04-file-service-production-hardening` | `L1nggTeam/feat/file-service-production-hardening` | 新增 service-layer streaming upload、MIME sniffing allowlist、非本地 backend guard、create/read/delete caller allowlist 和对应文档/测试；`go test ./...` 已通过。 |

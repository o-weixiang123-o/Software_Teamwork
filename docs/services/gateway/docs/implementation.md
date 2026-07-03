# Gateway 实现说明

版本：v0.3
日期：2026-07-02
范围：`services/gateway/` 当前实现、契约对齐、缺口和后续实现约束

## 1. 文档定位

本文档描述 `gateway` 当前实现状态和后续实现约束。它只补充服务 README、OpenAPI、架构和技术选型文档，不覆盖这些上游契约。

权威来源：

| 类型 | 权威来源 | 本文档关系 |
| --- | --- | --- |
| 服务公开说明 | `docs/services/gateway/README.md` | 只能补充，不能覆盖 |
| 服务 OpenAPI | `docs/services/gateway/api/public.openapi.yaml` | 前端公开契约权威来源 |
| Active owner map | `docs/services/gateway/docs/active-api-owner-map.md` | 路由审计清单 |
| 服务边界 | `docs/architecture/service-boundaries.md` | 必须遵守 |
| 技术基线 | `docs/architecture/technology-decisions.md` | 必须跟随 |
| 代码实现 | `services/gateway/` | 本文档记录当前状态和差距 |

凡是本文档与上表文件冲突，以上游文件为准；发现冲突时，在“文档与实现出入”中记录并生成回写或实现任务。

## 2. 当前结论

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| 文档状态 | active | README、OpenAPI、active owner map 和数据模型文档存在。 |
| 代码状态 | partial | Go gateway、auth public routes、Redis session cache、proxy route matrix、中间件、错误归一化、Auth profile/password-change/admin-users routes 和 Prometheus HTTP metrics baseline 已实现。 |
| 契约对齐 | guarded / partial | Gateway OpenAPI 现有 112 个 active operations；当前 route matrix 覆盖全部 112 个 active operations。app-version freshness 由 Gateway 代理 GitHub compare 语义判断当前构建是否包含 develop 最新提交，并做缓存/fallback；admin parser-configs、Knowledge document lifecycle、chunks、content、knowledge-queries、QA session attachments 和 Auth profile/password-change/admin-users 已转为 owner proxy；admin overview/metrics 是 active contract，但实现仍返回稳定 `not_implemented`。 |
| 数据持久化 | redis / none | Gateway 不持久化业务数据库；使用 Redis 保存 session cache。 |
| 测试状态 | partial | 单元测试覆盖 route matrix、QA active OpenAPI schema contract、auth proxy、headers、binary/SSE proxy、中间件和 metrics middleware；缺真实 Redis/downstream 集成测试。 |
| 建议动作 | 联调 / 复核 | Auth/Gateway/Redis full smoke 已脚本化；继续补真实 owner services 端到端联调验证。 |

## 3. 已实现

| 能力 | 代码位置 | 契约来源 | 验证方式 | 备注 |
| --- | --- | --- | --- | --- |
| 健康检查 | `services/gateway/internal/http/server.go` | `docs/services/gateway/api/public.openapi.yaml` | `cd services/gateway && go test ./...` | `GET /healthz`、`GET /readyz`。 |
| 用户/会话公开入口 | `services/gateway/internal/http/auth.go` | Gateway OpenAPI auth paths | `TestCreateSessionCachesSessionWithoutRawToken` | 转发 auth，成功后写 Redis session cache。 |
| Redis session cache | `services/gateway/internal/platform/redis/session_store.go` | `docs/services/gateway/README.md` | config/auth proxy tests | 使用 token hash key，不缓存原始 token。 |
| 认证上下文注入 | `services/gateway/internal/http/proxy.go` | `frontend-backend-contract.md` | `TestProxyInjectsAuthenticatedContextHeaders` | 注入 `X-User-*`、`X-Request-Id`、`X-Service-Token`。 |
| active route matrix | `services/gateway/internal/http/routes.go` | Gateway OpenAPI / owner map | `TestActiveRouteMatrixCoversGatewayOwnerMap` | 覆盖 112 个 active operations，含 app-version freshness 与 Auth profile/password-change/admin-users routes。 |
| binary content proxy | `services/gateway/internal/http/proxy.go` | Gateway OpenAPI file content paths | `TestProxyStreamsBinaryContentWithoutJSONEnvelope` | 文件流成功响应不包 JSON。 |
| SSE proxy | `services/gateway/internal/http/proxy.go` | QA SSE contract | `TestProxyStreamsSSEWithoutFixedTimeout` | `Accept: text/event-stream` 使用 streaming client。 |
| CORS / body limit / timeout / recover / request id | `services/gateway/internal/middleware/` | 前后端集成契约 | middleware/server tests | 覆盖基础 edge policy。 |
| Prometheus HTTP metrics | `services/gateway/internal/metrics/metrics.go`、`internal/middleware/metrics.go`、`cmd/server` | #308 / #322 observability baseline | metrics middleware tests | 通过独立 metrics listener 暴露 gateway HTTP request count/duration，不在 Gateway 内聚合业务指标。 |
| 服务边界导入守卫 | `services/gateway/internal/http/routes_internal_test.go` | 服务边界 / 技术基线 | `TestGatewayDoesNotImportBusinessInfrastructureClients` | 防止 Gateway 生产代码引入 SQL、MinIO、runtime doc engine 或 provider SDK。 |
| QA active path schema contract | `services/gateway/internal/http/qa_schema_contract_test.go` | #343 / Gateway OpenAPI QA paths | `cd services/gateway && go test ./internal/http -run QA` | 解析 OpenAPI YAML，校验 29 个 QA-owned active operations 的 owner/auth/schema/content type、ErrorResponse、分页参数、SSE 唯一路径、QA session attachments、QA settings `systemPrompt` contract、QA internal `$ref` 和默认 `/internal/v1` proxy drift。 |
| Admin parser config proxy | `services/gateway/internal/http/routes.go`、`parser_config_test.go` | Gateway OpenAPI admin runtime config | `cd services/gateway && go test ./...` | 转发 `/api/v1/admin/parser-configs` 到 Knowledge `/internal/v1/parser-configs`，支持管理员权限、request id、validation/conflict/error 归一化。 |
| Knowledge document lifecycle proxy | `services/gateway/internal/http/routes.go`、`gateway_auth_proxy_test.go` | Gateway OpenAPI document paths | `TestKnowledgeDocumentLifecycleRoutesProxyToKnowledge` | 转发 `PATCH/DELETE /api/v1/documents/{documentId}` 到 Knowledge，保留认证上下文和 request id。 |
| Knowledge chunks/content/query proxy | `services/gateway/internal/http/routes.go`、`gateway_auth_proxy_test.go` | Gateway OpenAPI Knowledge active paths | `TestKnowledgeDocumentChunkAndContentRoutesProxyToKnowledge`、`TestKnowledgeQueriesRouteProxiesToKnowledge` | chunks/query 返回 JSON envelope，content 保持二进制透明代理。 |
| Auth profile/password-change/admin-users routes | `services/gateway/internal/http/auth.go`、`routes.go`、`authclient/client.go` | Gateway OpenAPI Auth active paths | `TestAdminUsersRouteProxiesToAuthWithActorContext`、`TestAdminUsersRouteUsesDedicatedAuthAdminServiceToken`、`TestMustChangePasswordBlocksProtectedRoutesButAllowsPasswordChange` | 当前用户资料/改密通过 Auth client 刷新 Redis cache；当前用户写接口和 admin-users proxy 均使用专用 Auth admin service token，并透传 actor context 供 Auth 复核。 |

## 4. 未实现

| 缺口 | 文档来源 | 影响范围 | 建议任务 |
| --- | --- | --- | --- |
| 管理概览/跨服务指标聚合路由待实现 | OpenAPI active paths 已定义 | backend / deploy | 契约已补齐，路由注册由单独后端 issue 追踪。 |
| 真实 owner service 跨服务 smoke 仍需按 slice 验证 | README / deploy expectation | deploy / integration | `/readyz` 不承担完整业务链路验证；#125 承担跨服务 smoke 汇总，#352 的 Auth/Gateway/Redis full smoke 已由 `scripts/run_issue_352_smoke.sh` 脚本化，并提供手动 optional workflow。 |

## 5. 文档与实现出入

| 出入点 | 文档要求 | 当前实现 | 风险 | 建议处理 |
| --- | --- | --- | --- | --- |
| readyz 依赖 | Gateway README 要求轻量接流量门禁，完整 owner service 可用性由 smoke/diagnostics 承担 | `gatewayReadyCheck` 检查 Redis、Auth `/readyz`，并要求 knowledge、qa、document、ai-gateway base URL 全配置；不请求这些 owner services 的 `/readyz` | 只启动 gateway 时 `/readyz` 会因 Redis/Auth 或 owner base URL 配置缺失失败；即使 `/readyz` 通过，也不能证明上传、检索、QA、报告生成、模型 profile 或真实 provider 调用可用 | 当前行为符合 #353 选择的拆分语义；保持文档一致，完整链路继续由 #125/#352 和 runbook smoke 验证。 |
| 下游错误归一化 | 前后端契约要求统一 error envelope | proxy 会丢弃非公开错误细节并归一化 | 有利于安全，但可能隐藏调试信息 | 在日志/trace 中补 request id 和 dependency 信息。 |
| Gateway 不写业务逻辑 | 服务边界要求 Gateway 不访问 SQL/MinIO/runtime doc engine/LLM | 当前代码符合 | 无 | 持续通过 review/测试防回归。 |
| metrics 边界 | #308/#322 要求 observability baseline | Gateway 暴露自身 HTTP request count/duration；跨服务业务指标契约已补齐（admin-overview / admin-metrics），当前 Gateway route 仍返回稳定 `not_implemented`，聚合实现待后续 backend issue | 前端可基于 active schema 并行开发，但不能把聚合运行证据视为已完成 | 保持 active contract 与实现状态分开记录。 |
| 用户管理契约 | OpenAPI 已新增 Auth-owned profile、required password-change 和 admin-users active paths | Gateway 已注册这些新增路由并转发 actor context | 剩余主要是前端 UI 与真实依赖联调 | 前端子任务继续接入这些 active contract。 |

## 6. MVP / mock / memory backend / 占位

| 项目 | 当前用途 | 退出条件 | 关联任务 |
| --- | --- | --- | --- |
| test memory session store | Gateway auth/proxy 单元测试 | 保留测试用 | 无 |

## 7. 运行与配置

| 项目 | 当前状态 | 缺口 |
| --- | --- | --- |
| 启动命令 | `cd services/gateway && go run ./cmd/server` | 需要 Redis、auth 和 owner base URLs 才能 ready；不要求 owner service 业务 smoke 已通过。 |
| 环境变量 | `GATEWAY_HTTP_ADDR`、`GATEWAY_METRICS_ADDR`、Redis、token hash secret、`GATEWAY_INTERNAL_SERVICE_TOKEN`、`GATEWAY_AUTH_ADMIN_SERVICE_TOKEN`、auth/knowledge/qa/document/ai-gateway base URLs、CORS、timeouts | 缺根级 Compose 串联验证。 |
| PostgreSQL / migration | 不拥有 PostgreSQL | 无。 |
| Redis / queue | Redis session cache | 缺真实 Redis 集成测试。 |
| Object storage / vector store / AI provider | 不直接访问 | 必须继续由 owner services / ai-gateway 处理。 |

## 8. 测试与验证

| 验证项 | 命令或步骤 | 当前结果 | 缺口 |
| --- | --- | --- | --- |
| 单元测试 | `cd services/gateway && go test ./...` | pass（2026-07-03） | 不覆盖真实 Redis/downstreams。 |
| 集成测试 | Gateway + Redis + Auth smoke；Gateway -> owner service smoke | partial | `bash scripts/run_issue_352_smoke.sh` 或手动 workflow `Auth Gateway Redis Smoke` 可执行 Auth/Gateway/Redis full smoke；#125 继续负责更完整的 owner service 跨服务 smoke。 |
| 契约测试 | `TestActiveRouteMatrixCoversGatewayOwnerMap`、`TestQAActiveOpenAPIContractsHaveSchemasAndAuth`、`TestQAInternalOpenAPIRefsCoverGatewayActivePaths`、`TestQASseEventSchemaCoversSafePublicEvents`、`TestNotImplementedRoutesReturnStableGatewayError`、`TestGatewayDoesNotImportBusinessInfrastructureClients` | pass（2026-07-03，`cd services/gateway && go test ./...`；Gateway Contract CI verify 通过） | 仍不覆盖真实 Redis/downstreams。 |
| 手工 smoke | 登录、访问 knowledge/report/qa route | not run | 需要完整依赖环境。 |

## 9. 建议任务

| 任务 | 类型 | 优先级 | 依据 | 说明 |
| --- | --- | --- | --- | --- |
| 增加 Gateway integration smoke | 已有任务 | P1 | readyz 和 proxy 依赖真实服务 | #352 覆盖 Redis/Auth/Gateway 核心链路；#125 覆盖 owner service 业务链路。 |

## 10. 最近检查记录

| 日期 | 检查人/工具 | 代码基准 | 结论 |
| --- | --- | --- | --- |
| 2026-06-29 | Codex goal | `eddf917` + working tree | Gateway 架构边界清晰，route matrix 覆盖 103 active operations；主要风险是 active contract 中仍有多条 501 占位。 |
| 2026-06-30 | Codex | `8f294ec` + 本分支改动 | route matrix 已显式覆盖 gateway/auth 直接路由和 owner proxy routes，并与 OpenAPI 的 method/path/owner/operationId 对齐；501 占位和 Gateway 不直连业务基础设施均有回归测试。 |
| 2026-06-30 | Codex | A-13 PR #249 | admin parser-configs 已完成 Gateway proxy 和权限/error 测试，不再属于 501 占位范围。 |
| 2026-06-30 | Codex | PR #273 | Knowledge document lifecycle、chunks、content 和 knowledge-queries 已完成 Gateway proxy，不再属于 501 占位范围；真实 Redis/downstream smoke 仍待补齐。 |
| 2026-07-03 | Codex docs watch | `develop@58fc6eb2` | 复核 #440/#504/#527/#535 后 Gateway current facts：route matrix 仍覆盖 110 个 active operations；Knowledge RAGFlow runtime、Auth profile/password-change/admin-users、Document MCP registration 和 report settings/profile 变更已通过 owner service 暴露；admin overview/metrics 仍是 active contract + `not_implemented` 实现缺口。 |
| 2026-06-30 | Codex full-day audit | `develop@92d3afc` | 复核今日 PR/issue：#322 已补 Gateway Prometheus HTTP metrics baseline 和 middleware 测试；route matrix 当时覆盖 103 active operations，真实 Redis/downstream smoke 和跨服务指标聚合契约仍待补齐。 |
| 2026-07-03 | Codex docs watch | `develop@736acde0` | 复核 PR #520：Auth/Gateway/Redis full smoke 已脚本化，验证 Gateway auth public routes、Redis session cache 脱敏、logout invalidation 和 owner proxy context header 注入；owner service 业务链路仍由 #125 smoke slices 继续覆盖。 |
| 2026-07-01 | Codex #343 branch | `develop@96b5ad8f` + 本分支改动 | 新增 QA active path schema-level contract tests，覆盖当时 25 个 QA-owned operations、SSE 唯一路径、ErrorResponse envelope、分页 schema、QA internal `$ref` drift 和默认 proxy namespace/query 映射；后续 develop 已扩展到 29 个 QA active operations。 |
| 2026-07-02 | Codex backend task | `develop@cda73a10` + user-management working tree | route matrix 覆盖 110 个 active operations；新增 Auth profile/password-change/admin-users direct/proxy routes、actor context forwarding 和 mustChangePassword route guard；Gateway `go test ./...`、`go build ./cmd/server`、active API verifier 通过。 |
| 2026-07-02 | Codex docs refresh | `develop@58fc6eb2` | Gateway active owner map 当前为 110 个 active operations；QA session attachments、QA config `systemPrompt` 权限契约、Auth profile/password-change/admin-users、前端附件上传状态已经进入当前事实，admin overview/metrics 仍是 active contract + `not_implemented` 实现缺口。 |

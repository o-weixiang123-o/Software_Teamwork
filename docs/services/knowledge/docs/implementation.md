# Knowledge Service 实现说明

版本：v0.9
日期：2026-07-03
范围：`services/knowledge/` 当前实现、契约对齐、缺口和后续实现约束

## 1. 文档定位

本文档描述 `knowledge` 当前实现状态和后续实现约束。它只补充服务 README、OpenAPI、架构和技术选型文档，不覆盖这些上游契约。

权威来源：

| 类型 | 权威来源 | 本文档关系 |
| --- | --- | --- |
| 服务公开说明 | `docs/services/knowledge/README.md` | 只能补充，不能覆盖 |
| 服务 OpenAPI | `docs/services/knowledge/api/internal.openapi.yaml`；`services/knowledge/api/openapi.yaml` 是实现本地路由副本 | 只能跟随，不能另起契约 |
| Gateway 公开契约 | `docs/services/gateway/api/public.openapi.yaml` | 前端稳定契约以 gateway 为准 |
| 服务边界 | `docs/architecture/service-boundaries.md` | 必须遵守 |
| 技术基线 | `docs/architecture/technology-decisions.md` | 必须跟随 |
| 代码实现 | `services/knowledge/` | 本文档记录当前状态和差距 |

凡是本文档与上表文件冲突，以上游文件为准；发现冲突时，在“文档与实现出入”中记录并生成回写或实现任务。

## 2. 当前结论

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| 文档状态 | active | README、公开草案、数据模型、内部 OpenAPI 和实现说明存在。 |
| 代码状态 | partial | Go adapter 已实现知识库 CRUD、文档列表/上传/详情、parser-configs 运行时管理、RAGFlow runtime client、文档 chunks/content API、`knowledge-queries` 检索、Knowledge MCP server、MCP bridge 和 runtime contract tests。旧 Parser Service、旧 Go ingestion worker 和 File 直连路径已由 RAGFlow runtime 方案取代。 |
| 契约对齐 | partial | Gateway OpenAPI 已声明 document lifecycle、chunks、content、knowledge-queries、parser configs；这些 active routes 已由 Knowledge adapter 和 Gateway proxy 落地。Knowledge PDF E2E 路径、Gateway -> Knowledge owner route smoke、Gateway -> Knowledge -> QA RAG env-gated smoke 和 MCP 单元/HTTP handler tests 已存在；前端和 #125 完整一键 Gateway/MCP/QA E2E 仍待后续 smoke。 |
| 数据持久化 | adapter / runtime | adapter 通过 RAGFlow runtime 管理文档、chunks、embedding、索引和检索；parser configs 可选使用 Knowledge goose PostgreSQL 表；runtime 内部使用 PostgreSQL、MinIO 和 Elasticsearch/索引后端。当前 adapter 不直接调用 File Service 或 Redis/asynq。 |
| 测试状态 | partial | 单元、adapter contract、repository integration、runtime route/config tests、MCP tests 和真实 PDF E2E 验证路径覆盖 CRUD、权限、runtime config 映射、chunks/content、`knowledge-queries`、MCP service token/固定上下文、错误 envelope 和 request id；完整 Gateway/MCP/QA #125 E2E 仍需后续联调。 |
| 依赖解耦 | documented | A-12 检索和 A-14 契约测试可依赖 `docs/api-contract.md` 2.6 与 `docs/data-models.md` 6.7 的 seeded chunk/vector fixture，不再要求 A-11 worker runtime 先完成。 |
| 建议动作 | 联调 / 人工复审 | 继续补完整 Gateway/MCP/#125 E2E、真实 provider 运维和并发/外部副作用一致性加固；人工复审任务幂等、失败状态收敛和敏感数据不泄漏。 |

## 3. 已实现

| 能力 | 代码位置 | 契约来源 | 验证方式 | 备注 |
| --- | --- | --- | --- | --- |
| 健康检查 | `services/knowledge/internal/adapter/server.go` | `docs/services/knowledge/api/internal.openapi.yaml` | `cd services/knowledge && go test ./...` | `GET /healthz`、`GET /readyz`。 |
| 知识库 CRUD | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/knowledge/api/internal.openapi.yaml` | adapter contract tests | 通过 RAGFlow runtime dataset API 映射列表、创建、详情、更新和软删除。 |
| 用户上下文和权限校验 | `services/knowledge/internal/adapter/auth.go`、`internal/adapter/context.go` | `docs/services/knowledge/README.md` | adapter and MCP tests | 依赖 gateway 注入的 user/permission context；MCP HTTP 使用服务端固定 caller context，不信任调用方伪造 `X-User-*`。 |
| 文档列表和详情 | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/knowledge/api/internal.openapi.yaml` | adapter contract tests | 文档元数据和状态由 runtime 响应映射为项目契约。 |
| 文档上传 handoff | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/knowledge/README.md`、`docs/runbooks/local-integration.md` | adapter contract tests、PDF E2E | multipart 上传后通过 RAGFlow runtime 保存文档并按 `KNOWLEDGE_AUTO_START_INGESTION` 触发 parse，不再调用 File Service。 |
| Parser configs 运行时管理 | `services/knowledge/internal/adapter/handlers_parser.go`、`internal/service/parser_config.go`、`internal/repository/postgres.go` | `docs/services/gateway/api/public.openapi.yaml`、`docs/architecture/service-boundaries.md` | `cd services/knowledge && go test ./...`；repository integration CI | 支持 list/get/create/update/delete、默认 builtin seed、上传 parser snapshot、重复名称 conflict、空配置 fallback 和 MIME 匹配选择；创建 KB 时映射为 RAGFlow `parser_config`。 |
| RAGFlow runtime adapter | `services/knowledge/internal/adapter`、`internal/vendorclient`、`services/knowledge-runtime` | `docs/services/knowledge/README.md`、`docs/runbooks/local-integration.md` | adapter contract tests、runtime route tests、PDF E2E | 通过 runtime API/worker 完成 dataset/document、解析、切块、embedding、索引和检索；Go adapter 负责项目内部 HTTP 契约、权限上下文和错误归一化。 |
| 文档 chunks HTTP API | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/gateway/api/public.openapi.yaml`、`docs/services/knowledge/api/internal.openapi.yaml` | adapter contract tests、Gateway proxy tests | 支持 `GET /internal/v1/documents/{documentId}/chunks`，分页返回 Knowledge-owned chunk DTO，不暴露原始向量或 runtime 内部索引 payload。 |
| 原始文档 content API | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/gateway/api/public.openapi.yaml`、`docs/architecture/service-boundaries.md` | adapter contract tests、Gateway binary proxy tests | 先校验 Knowledge 文档可见性，再从 RAGFlow runtime/adapter 映射 raw bytes；响应为二进制流，不包 JSON envelope，不暴露 object key 或内部 URL。 |
| 文档 lifecycle API | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient` | `docs/services/gateway/api/public.openapi.yaml`、`docs/services/knowledge/api/internal.openapi.yaml` | adapter contract tests、Gateway proxy tests | 支持 `PATCH /internal/v1/documents/{documentId}` 更新 tags，`DELETE /internal/v1/documents/{documentId}` 软删除；响应不暴露 runtime 内部索引或 provider 细节。 |
| Runtime parser config mapping | `services/knowledge/internal/adapter/map.go`、`internal/service/parser_config.go` | `docs/services/knowledge/README.md`、`docs/architecture/service-boundaries.md` | adapter map tests、contract tests | `parser-configs` 仍是 Knowledge 管理的业务配置资源；创建 KB 时映射为 RAGFlow `parser_config`，不再调用独立 `services/parser`。 |
| embedding / vector index | `services/knowledge-runtime`、`services/knowledge/internal/vendorclient` | `docs/architecture/service-boundaries.md` | runtime route/config tests、PDF E2E | runtime 通过 provider 配置、MinIO 和 Elasticsearch/索引后端完成 embedding、索引和检索支持；adapter 不再维护 local hashing 平台代码。 |
| `knowledge-queries` 检索 | `services/knowledge/internal/adapter/handlers.go`、`internal/vendorclient`、Gateway proxy route | `docs/services/knowledge/docs/api-contract.md`、`docs/services/gateway/api/public.openapi.yaml` | adapter contract tests、Gateway RAG smoke | 通过 RAGFlow runtime 检索并映射成项目 `KnowledgeQuerySummary`；默认 RAG smoke 已覆盖最小 answer/citation，真实 provider/rerank 和完整 #125 仍需显式环境。 |
| PostgreSQL migration/repository | `services/knowledge/migrations/0001_create_knowledge_core_tables.sql`、`0002_create_parser_configs.sql`、`internal/repository/postgres.go` | `docs/services/knowledge/docs/data-models.md` | `go test ./...`；CI 用 `KNOWLEDGE_TEST_DATABASE_URL` 跑 repository lifecycle integration test | Go adapter 仍保留 goose migration；当前 repository 主要服务 parser-config admin 和迁移保留字段。文档、job、chunks 等运行时事实由 RAGFlow runtime 内部持久化并经 adapter 映射。分页 limit/offset 转 `int32` 前在 repository 层做显式范围校验，非法页码或溢出 offset 返回 validation error，不静默截断到 `MaxInt32`。 |
| Knowledge PDF E2E | local runtime stack | #440、`docs/runbooks/local-integration.md` | 手动启动宿主机 runtime API/worker 和 host-run adapter 后上传 `DL_T_673-1999.pdf` | 覆盖真实 PDF 上传、runtime 解析、切块、embedding、索引和 `knowledge-queries` 检索；需要可用 runtime、对象存储、索引后端和 provider credential。 |
| Gateway -> Knowledge -> QA RAG smoke | `services/knowledge/internal/integration/gateway_rag_e2e_smoke_test.go` | #304、`docs/runbooks/local-integration.md` | `GATEWAY_RAG_E2E_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v` | 默认跳过；启用后通过 Gateway 创建 session/KB、上传 Markdown fixture，轮询文档 ready 和 chunkCount，调用 `knowledge-queries` 断言命中 `calibrate relay RAG-E2E-304` 和 rerank trace，再通过 QA config/session/message 验证 answer 包含 `RAG-E2E-304` 且 citations 匹配本轮 KB/doc/chunk。需要可用 AI Gateway chat profile/provider；默认 local hashing/in-memory vector 只证明等价检索数据，真实 Elasticsearch/runtime doc engine 和 AI Gateway embedding/rerank provider 需显式 env。 |
| Knowledge MCP server | `services/knowledge/internal/mcp`、`cmd/adapter` | [`mcp-server.md`](mcp-server.md)、[`mcp-tools.md`](mcp-tools.md) | `go test ./internal/mcp/...`；MCP HTTP handler tests | `KNOWLEDGE_MCP_ADDR` 非空时启动独立 Streamable HTTP server；当前 ToolCatalog 为四个只读原生工具 `search`、`list_documents`、`get_document`、`get_chunk`，使用 `X-Service-Token` 和服务端固定 caller context。 |

## 4. 未实现

| 缺口 | 文档来源 | 影响范围 | 建议任务 |
| --- | --- | --- | --- |
| Knowledge MCP 默认 QA/前端 E2E 证据未闭环 | #505、[`mcp-server.md`](mcp-server.md)、[`mcp-tools.md`](mcp-tools.md) | QA / MCP / citation | MCP server 已收敛四个只读工具，仍需要真实 provider 下 QA Agent 选择工具、citation 展示和 #125 前端 E2E 证据。 |
| 真实 AI Gateway embedding/rerank smoke 未闭环 | `docs/architecture/service-boundaries.md`、AI Gateway invocation contract | retrieval / AI Gateway | runtime 已提供 `AI_GATEWAY` embedding/rerank provider；还需要带真实 provider credential 的跨服务 smoke，并以 AI Gateway `provider_invocations` 作为验收证据。 |

## 5. 文档与实现出入

| 出入点 | 文档要求 | 当前实现 | 风险 | 建议处理 |
| --- | --- | --- | --- | --- |
| AI Gateway embedding/rerank 接入状态 | AI Gateway 已实现 embeddings/rerankings endpoint；Knowledge runtime 已提供 `AI_GATEWAY` provider 接入它 | RAGFlow runtime 负责 embedding/index/rerank；默认推荐路径从 runtime 直连 provider 收敛到 AI Gateway，未配置真实 provider 时不能声称真实 rerank 已验收 | 容易把 no-op fallback 或直连 provider 成功误读为 AI Gateway 已治理 | 保留 fake/seeded 契约测试，同时补带真实 provider credential 和 `provider_invocations` 证据的跨服务 smoke。 |
| Runtime host 访问前置条件 | host-run Knowledge adapter 需要能访问 RAGFlow runtime API | 如果 `VENDOR_RUNTIME_URL` 指向 Docker bridge IP 且代理环境未把该 IP 加入 `NO_PROXY`，adapter 请求会被代理截走并返回 502 | 容易把代理问题误判为 runtime 或解析失败 | 默认配置使用发布到宿主机的 `127.0.0.1:9380`；临时使用容器 IP 时必须清理代理或补 `NO_PROXY`。 |
| 公开 Knowledge 草案范围 | `docs/services/knowledge/api/public.openapi.yaml` 是服务级 public 设计草案，覆盖 deletion jobs、processing jobs、query tests、support materials、settings、statistics | runtime 已实现 KB CRUD、文档 upload/list/detail/tags/soft delete、chunks/content 和 knowledge-queries；deletion job 查询、processing job 查询、query tests、support materials、settings、statistics 仍是草案/缺口；前端稳定契约以 gateway public OpenAPI 为准 | 文件名里的 `public` 可能被误读为 active browser-facing contract | 草案文件已加说明；进入前端稳定契约前必须先更新 `docs/services/gateway/api/public.openapi.yaml`。 |
| Knowledge MCP 工具目录 | #528/#529 定义四个只读模型工具，#525/#531 定义协议说明 | 当前 `ToolCatalog()` 发布 `search`、`list_documents`、`get_document`、`get_chunk`；alias 后是 `knowledge__search` 等名称；`KNOWLEDGE_MCP_ADDR` 是独立 endpoint，非 `/mcp` path | 容易把旧 `search_knowledge` 或 14 工具目录误读为当前实现 | 以 [`mcp-server.md`](mcp-server.md) 的当前运行说明为准；后续若改名或扩展工具，必须同步 QA 白名单、citation 和文档。 |

## 6. MVP / mock / memory backend / 占位

| 项目 | 当前用途 | 退出条件 | 关联任务 |
| --- | --- | --- | --- |
| memory repository | 单元测试 | PostgreSQL integration tests 覆盖关键 CRUD 后仍可保留测试用 | 保留测试用 |
| fake runtime/vendor client | adapter、MCP 和 contract tests 的 runtime 响应输入 | 真实 runtime E2E 稳定后仍可保留为快速契约测试 | Runtime contract tests |
| seeded chunk/vector fixture | A-12 retrieval 和 A-14 contract tests | 真实 RAGFlow runtime + 索引后端 + AI Gateway smoke 稳定后仍可保留为快速契约测试 | A-12/A-14 历史并行开发 |
| fake AI / vendor adapter | 检索、MCP、错误 envelope 和 request id 测试 | 真实依赖集成测试补齐；不替代端到端 smoke | Retrieval/MCP contract tests |

## 7. 运行与配置

| 项目 | 当前状态 | 缺口 |
| --- | --- | --- |
| 启动命令 | `cd services/knowledge && go run ./cmd/adapter` | 需要可访问的 host-run `services/knowledge-runtime` API。 |
| 环境变量 | adapter 模式需要 `VENDOR_RUNTIME_URL`、`VENDOR_RUNTIME_SERVICE_TOKEN`、`KNOWLEDGE_SERVICE_TOKEN` 或 `INTERNAL_SERVICE_TOKEN`、`KNOWLEDGE_AUTO_START_INGESTION`；`DATABASE_URL` 或 `KNOWLEDGE_DATABASE_URL` 用于 parser-config admin；`KNOWLEDGE_MCP_ADDR` 可启用独立 MCP server。runtime 依赖 PostgreSQL、Redis、MinIO、Elasticsearch 和 provider 配置 | 仍需按部署环境补真实依赖连通性检查；所有 `/internal/v1/**` 和 MCP 调用必须带匹配的 `X-Service-Token`。 |
| PostgreSQL / migration | `migrations/0001_create_knowledge_core_tables.sql`、`0002_create_parser_configs.sql`，runtime `pgx/v5` | goose apply CI 已覆盖 migration；repository lifecycle 由 `KNOWLEDGE_TEST_DATABASE_URL` 集成测试覆盖。 |
| MCP server | `KNOWLEDGE_MCP_ADDR` 非空时由 `cmd/adapter` 启动独立 Streamable HTTP server；`KNOWLEDGE_MCP_USER_ID`/roles/permissions 决定固定 caller context | QA 默认 seed/allowlist/citation 仍需后续收敛。 |
| Object storage / vector store / AI provider | RAGFlow runtime 通过 MinIO、Elasticsearch/向量索引和 provider 配置完成文档保存、embedding、索引和检索 | 本地 PDF E2E 覆盖上传、解析、切块、索引和查询；仍需完整 Gateway/MCP/QA 联调。 |
| Knowledge runtime | Knowledge 通过 `VENDOR_RUNTIME_URL` 调 `services/knowledge-runtime` 的 RAGFlow API；worker 负责后台 parse/index 任务 | runtime image rebuild 在网络异常时可能卡在资源仓库 clone；本地可先复用已构建健康容器验证链路。 |

当 Knowledge runtime 使用 `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=AI_GATEWAY` 或 `KNOWLEDGE_RUNTIME_RERANK_FACTORY=AI_GATEWAY` 时，runtime 请求中的 `model` 必须匹配 AI Gateway 对应 profile 的 `model`。`KNOWLEDGE_RUNTIME_AI_GATEWAY_*_PROFILE_ID` 可留空以使用 AI Gateway 默认启用的 embedding/rerank profile，但 AI Gateway 调用前仍会强制校验 model 匹配。

### 7.1 历史 delete cleanup worker 说明

以下约束描述 2026-07-01 前的旧 Go worker 路径，用于排查历史任务和迁移记录。当前
`develop` 的生产路径已由 RAGFlow runtime adapter 取代，`cmd/server`、
`internal/worker`、`internal/platform/queue` 和 `internal/platform/vector` 不再存在。
如果后续恢复独立 cleanup worker，必须重新写入当前运行手册和 smoke 证据。

排查 SQL：

```sql
SELECT id, document_id, status, current_stage, attempts, max_attempts,
       error_code, error_message, updated_at
FROM processing_jobs
WHERE job_type = 'delete_cleanup'
ORDER BY updated_at DESC
LIMIT 20;

SELECT d.id AS document_id, d.knowledge_base_id, d.current_job_id,
       j.status, j.attempts, j.max_attempts, j.error_code, j.error_message
FROM knowledge_documents d
JOIN processing_jobs j ON j.id = d.current_job_id
WHERE d.deleted_at IS NOT NULL
  AND j.job_type = 'delete_cleanup'
  AND j.status IN ('queued', 'running', 'failed')
ORDER BY j.updated_at DESC;
```

## 8. 测试与验证

| 验证项 | 命令或步骤 | 当前结果 | 缺口 |
| --- | --- | --- | --- |
| 单元测试 | `cd services/knowledge && go test ./...` | pass（2026-07-01，本地 Go 1.26.4；需允许 `httptest` 本地端口监听） | 主要使用 fake vendor/runtime 依赖，覆盖 adapter CRUD、parser-configs 管理、runtime mapping、chunks/content、`knowledge-queries`、MCP tools、service-token 和 request id；env-gated Gateway/RAG smoke 默认跳过，不破坏普通 CI。 |
| Repository 集成测试 | `KNOWLEDGE_TEST_DATABASE_URL=... go test ./internal/repository -count=1` | CI 覆盖 parser-config repository lifecycle；无 env 时本地跳过 | 当前 repository 主要服务 parser-config admin 和迁移保留字段，不覆盖 RAGFlow runtime 内部 PostgreSQL/MinIO/Elasticsearch。 |
| Knowledge runtime targeted tests | `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest <targeted tests> -q` | pass（2026-07-02，本地 targeted route/config tests） | 不替代真实 PDF E2E。 |
| Knowledge PDF E2E | 启动 runtime API/worker 和 host-run adapter 后上传 `DL_T_673-1999.pdf` | available / manual | 覆盖真实 PDF 上传、解析、切块、embedding、索引和检索；需要真实 provider credential 和本地 runtime。 |
| Gateway -> Knowledge owner route smoke | `GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayKnowledgeOwnerRouteSmoke$' -count=1 -v` | available（2026-07-01 新增；默认 skip） | 覆盖伪造 `X-User-*` 未认证请求拒绝、Gateway session 创建、KB `createdBy` 真实 session user 断言，以及依赖 ready 前置检查；不覆盖完整 Gateway route matrix。 |
| Gateway -> Knowledge -> QA RAG smoke | `GATEWAY_RAG_E2E_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v` | available（2026-07-01 新增；默认 skip；本轮只跑默认 skip 编译检查） | 覆盖最小 RAG 样例的 Gateway 上传、runtime ready/chunks、`knowledge-queries` 命中、QA answer 和 citations；需要可用 RAGFlow runtime、AI Gateway chat profile/provider，真实 provider 不进入普通 CI。 |
| Knowledge MCP tests | `cd services/knowledge && go test ./internal/mcp/...` | available | 覆盖 ToolCatalog、in-memory MCP 调用、HTTP service token 校验和伪造 caller context 不被信任；不等于 QA 默认白名单或四个 `knowledge__*` 目标工具已收敛。 |
| 契约测试 | gateway route matrix + Knowledge adapter tests | pass（2026-06-30 起持续补充） | document lifecycle、chunks、content、knowledge-queries、parser-configs 等 active path 已补 contract/request-id/error envelope 覆盖。 |
| 手工 smoke | 启动 Knowledge runtime API/worker 和 host-run adapter 后上传文档 | available / manual | 需要可复现脚本或共享环境证据；普通 CI 不启动 RAGFlow runtime/OCR/索引/provider。 |

## 9. 建议任务

| 任务 | 类型 | 优先级 | 依据 | 说明 |
| --- | --- | --- | --- | --- |
| 补 Knowledge MCP 默认 QA/前端 E2E 证据 | 后续任务 | P0 | 当前 MCP server 已发布四个只读工具，但完整 QA Agent/前端路径仍缺真实 provider 证据 | 复验 QA default allowlist、citation 识别和 #125 smoke，确保 `knowledge__search` 等工具被真实模型稳定调用。 |
| 补真实 runtime/provider retrieval-rerank 证据 | 后续任务 | P0 | #304 已提供最小 Gateway RAG smoke，但普通路径仍缺真实 provider 运行记录 | 在真实 RAGFlow runtime、Elasticsearch/索引后端、AI Gateway embedding/rerank provider 环境下记录 `knowledge-queries` search/rerank 证据，并继续由 #125 覆盖 MCP/前端完整 E2E。 |

## 10. 最近检查记录

| 日期 | 检查人/工具 | 代码基准 | 结论 |
| --- | --- | --- | --- |
| 2026-07-03 | Codex AI Gateway runtime task | `L1nggTeam/feat/ragflow-runtime-vendor` | 复核 #440/#529/#531：旧独立 `services/parser` 已退役，Knowledge Go adapter 通过 `services/knowledge-runtime` 的 RAGFlow runtime API/worker 完成 dataset/document、解析、切块、embedding、索引和检索支持；当前代码已有 `KNOWLEDGE_MCP_ADDR` 独立 Streamable HTTP MCP server 和四个只读原生工具，完整 QA 默认接入、citation、前端/#125 E2E 和真实 provider 运维仍是缺口。 |
| 2026-07-01 | Codex | Issue #342 branch | 历史路径：当时实现 Knowledge 文档 delete cleanup worker，删除文档后软删并投递 `knowledge:document:delete_cleanup`，worker 幂等调用 File DELETE 和按 `document_id` 清理 vector points，失败摘要脱敏写入 `processing_jobs`；当前 RAGFlow runtime adapter 已取代旧 Go worker，后续如恢复 cleanup worker 需重写运行手册和 smoke。 |
| 2026-07-01 | Codex | Issue #304 branch | 新增 env-gated `TestGatewayRAGE2ESmoke`，默认 skip；启用后通过 Gateway 上传最小 Markdown fixture，验证 Knowledge ingestion ready/chunkCount、`knowledge-queries` 命中、QA answer 包含 `RAG-E2E-304`，并校验 citation 摘要匹配本轮 KB/doc/chunk。 |
| 2026-07-01 | Codex | A-021 working tree | 历史路径：新增 env-gated `TestKnowledgeIngestionRealDepsSmoke`，当时验证 File Service、Parser Service、Knowledge worker 和 local hashing embedding 的旧路径；当前 RAGFlow runtime 方案已取代独立 Parser Service。 |
| 2026-07-01 | Codex CodeQL follow-up | working tree | 继续收敛合并后仍 open 的 rerank allocation 告警：rerank result ordering 的 slice/map capacity 改为 `maxRetrievalTopK` 常量，`limit` 仅作为业务截断条件，避免用户控制值继续流入 allocation size。 |
| 2026-07-01 | Codex | A-021 scope update | 历史路径：新增 env-gated `TestGatewayKnowledgeOwnerRouteSmoke` 和 Parser image 构建/缓存前置说明；当前 RAGFlow runtime 方案不再恢复 Parser image。 |
| 2026-06-30 | Codex full-day audit | `develop@92d3afc` | 历史路径：Knowledge 当时包含 ingestion worker、Parser Service client、parser-configs runtime management、chunks/content、`knowledge-queries`、旧文档所称的 AI Gateway embedding/rerank adapter、document PATCH/DELETE lifecycle 和 Gateway proxy；当前 RAGFlow runtime 方案已取代独立 Parser Service，runtime 统一走 AI Gateway 的实现以 2026-07-03 记录为准。 |
| 2026-06-30 | Codex | A-014 working tree | 补齐 chunks/content internal route、Gateway proxy、seeded/fake-backed `knowledge-queries` contract、错误 envelope 和 request id 测试；当时 document PATCH/DELETE 与真实 AI Gateway smoke 仍待后续任务。 |
| 2026-06-30 | Codex | PR #273 | 文档 PATCH/DELETE lifecycle 已落地：tags 更新、软删除、cleanup job 创建、Gateway proxy 和 PostgreSQL repository lifecycle 集成测试；真实 File cleanup worker 和跨依赖 smoke 仍待后续任务。 |
| 2026-06-30 | Codex | working tree | 补充 A-11/A-12/A-14 解耦契约：A-12/A-14 可用 seeded chunks、fake vector/AI adapter 做契约和 handler 测试；完整 ingestion runtime 仍由 A-11 交付。 |
| 2026-06-30 | Codex | A-13 PR #249 | parser-configs 运行时管理已落地并合入：Knowledge 内部 API、Gateway proxy、默认 builtin seed、上传 snapshot、conflict 映射和前端管理入口均已覆盖。 |
| 2026-06-30 | Codex | PR #226 docs extraction | 历史路径：当时从 PR #226 单独抽出 Parser Runtime 文档和 OpenAPI 契约；当前分支已删除旧 Parser 文档。 |
| 2026-06-29 | Codex goal | `eddf917` + working tree | Knowledge 已完成 KB CRUD 和文档上传 handoff；当时 parser config 与入库 worker、chunks、content、retrieval 均为关键缺口，其中 parser config 已由 A-13 补齐。 |
| 2026-06-29 | Codex | A11 branch | 历史路径：Knowledge 当时完成文档上传 handoff、入库 worker、Parser Service 解析调用、切片、embedding、vector 写入和 chunk 持久化；当前 RAGFlow runtime 方案已替代独立 Parser Service。 |

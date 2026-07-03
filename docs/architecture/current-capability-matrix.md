# 当前能力矩阵

日期：2026-07-03

本文根据截至当前 `develop`、今天的 issue/PR 状态和各服务实现说明，汇总项目当前真实能力。合并到 `develop` 的能力才标为“已实现”；open PR 只作为待合入或下一步，不当作当前能力。

## 状态标记

| 标记 | 含义 |
| --- | --- |
| 已实现 | 代码、契约和基础测试已经在当前 `develop` 可见。 |
| 部分实现 | 核心资源或状态机存在，但关键下游、真实集成或部分 active paths 未闭环。 |
| 占位 | 契约或 route 已存在，但返回 `not_implemented` 或只做安全占位。 |
| 缺失 | 当前没有可执行实现或稳定契约。 |

## 今日输入

| 来源 | 状态 | 对能力判断的影响 |
| --- | --- | --- |
| Gateway active API owner map | current develop | Gateway active operations 为 110 个；QA active operations 为 29 个，包含 session attachments；admin overview/metrics 已转为 active contracts，但 Gateway route 仍按 `not_implemented` 占位，聚合运行证据待补。 |
| QA implementation docs | current develop | QA sessions/messages/SSE、ResponseRun Agent Loop、function-calling adapter、session attachments、Document report artifact 摘要映射、QA -> AI Gateway smoke 入口、Gateway -> Knowledge -> QA RAG 最小 smoke 和 QA -> Document MCP report tools env-gated smoke 已落地；完整 #125、真实 provider、持久化回放恢复和前端端到端仍缺。 |
| QA system prompt contract | current develop | Gateway OpenAPI、QA docs 和前端生成类型已同步 versioned/global `systemPrompt` 契约：`QAConfigVersion.systemPrompt` 为返回必填，创建配置时可继承现有 prompt，完整 prompt 只允许 `qa:settings:read/write` 管理员配置端点返回；普通 QA 响应、SSE、tool summary、错误、日志和指标不得泄露完整 prompt。 |
| Knowledge implementation docs | current develop + local RAG/MCP fix | Knowledge RAGFlow runtime adapter、真实 PDF E2E 路径、Gateway owner route smoke、Gateway -> Knowledge -> QA RAG env-gated smoke 和 Knowledge MCP server 基础实现已补齐；Knowledge MCP 当前暴露四个只读工具，QA alias 后为 `knowledge__search`、`knowledge__list_documents`、`knowledge__get_document`、`knowledge__get_chunk`。真实 provider Agent 选择工具、前端和 #125 证据仍需单独记录。 |
| Document implementation docs | current develop | report jobs/attempts/events、report files/content、基础内置 DOCX 导出、settings、statistics、operation logs、`summer_peak_inspection` 基础 AI 大纲/正文生成编排、AI prompt/DOCX formatting/polling retry 修复、服务内 Document MCP 工具适配层和 Streamable HTTP MCP server 已落地；QA 侧已有 env-gated MCP 子场景，更多报告类型、真实 provider 运行记录、富 DOCX 运行时和 #125 完整 smoke 仍缺。 |
| Local integration runbook / README | current develop | 默认联调路径是 Docker infra -> host backend -> frontend；根级 Compose 只启动 PostgreSQL、Redis、Qdrant、MinIO 和 minio-init，业务服务、migration、seed 和 Knowledge RAGFlow runtime 通过 `scripts/local/dev-up.sh` / `run-backend.sh` / runtime runbook host-run；默认下载源保持官方，`--china` 显式为中国大陆网络切换 Docker/Go/uv/runtime artifact 镜像；AI Gateway placeholder profile 的 degraded/placeholder readiness 不等于真实 provider 可用。 |
| Frontend recent changes | current develop | QA 聊天页可上传 session attachments、展示解析/可用状态、切换纳入上下文并在发送消息时携带 ready attachment IDs，聊天侧栏滚动链路已收敛在组件内部；同页可展示 SSE `reportArtifact` 并按安全 `downloadPath` 下载报告；admin model profile 页面补充 enable/default/streaming、embedding dimension、rerank topN 和更新时 API key 可选的表单校验；Gateway readyz generated type 已同步；刷新后从 tool-call 回放恢复 artifact 仍是 TODO。 |

## 能力矩阵

| 能力 | 当前状态 | 已有实现证据 | 对外/内部契约 | 主要缺口 | 关联 |
| --- | --- | --- | --- | --- | --- |
| Auth 用户、会话和权限上下文 | 已实现 | `services/auth/` Go service、PostgreSQL migration、argon2id、session token hash、服务间 token、当前用户资料、必需改密和管理员用户管理；Auth/Gateway/Redis full smoke 已有 `scripts/run_issue_352_smoke.sh` 和手动 workflow。 | Gateway auth public routes；Auth service OpenAPI。 | 默认 required CI 不跑真实 Docker/host-run full smoke；完整本地 E2E、种子数据和管理端权限配置仍需联调。 | #109、#122、#125、#352、#504 |
| Gateway active route proxy、session cache 和 admin aggregation | 部分实现 | `services/gateway/` active proxy matrix、110 个 active operations、Redis session cache、Auth routes；Knowledge document lifecycle、chunks/content、`knowledge-queries`、QA attachments、Auth profile/password-change/admin-users 已进入 owner proxy；Auth/Gateway/Redis full smoke 已脚本化；admin overview/metrics 是 active contract，但当前 Gateway route 仍是稳定 `not_implemented` 占位。 | Gateway OpenAPI 是前端权威契约。 | owner service 业务跨服务 smoke、admin aggregation 后端实现和跨服务 admin metrics 运行证据仍需自动化。 | #153、#199、#125、#352、#504 |
| File 基础文件对象 | 部分实现 | `services/file/` 内部 `/internal/v1/files/**`、memory/local/MinIO object store、file metadata migration、PostgreSQL metadata runtime 和 service-token 校验。 | File service 内部 API；业务服务不得暴露 object key。 | 根级 Compose 已提供 MinIO server/mc 初始化；真实对象存储 smoke 和跨服务 smoke 仍未自动化。 | #154、#235 |
| Knowledge 知识库 CRUD、上传和 RAGFlow runtime 链路 | 部分实现 | Knowledge adapter、知识库 CRUD、文档上传、宿主机 RAGFlow runtime API/worker、parser config 到 RAGFlow `parser_config` 映射、文档状态/chunks/content、`knowledge-queries` 检索，以及真实 PDF E2E 验证路径。 | Gateway Knowledge active paths；Knowledge service OpenAPI；host-run runtime scripts。 | 完整 Gateway/MCP/#125 E2E、真实 provider 运维和并发/外部副作用一致性仍需补齐。 | #152、#200、#226、#304、#440 |
| Knowledge MCP server 与工具契约 | 部分实现 | `services/knowledge/internal/mcp` 和 `cmd/adapter` 已支持 `KNOWLEDGE_MCP_ADDR` 独立 Streamable HTTP server、`X-Service-Token` 校验、固定 caller context 和四个只读原生工具 `search`、`list_documents`、`get_document`、`get_chunk`；QA 默认白名单包含 alias 后的 `knowledge__*` 工具。 | `docs/services/knowledge/docs/mcp-server.md`、`docs/services/knowledge/docs/mcp-tools.md`、QA MCP runtime contract。 | 完整 #125/前端/真实 provider Agent E2E 仍需补齐；当前本地证据覆盖直接 MCP search 和 Gateway retrieval-test。 | #440、#505、#525、#528、#529、#531 |
| QA 会话、消息、附件、配置、引用和统计资源 | 部分实现 | `services/qa/` 会话/消息 API、session attachments、SSE 事件、config versions（含全局 `systemPrompt` 管理员配置契约）、citations、retrieval test/metrics 资源、AI Gateway chat client、ResponseRun / Agent Loop、Document report artifact 摘要映射，以及 env-gated Gateway RAG smoke 的 answer/citation 验收。 | Gateway QA active paths；QA service OpenAPI。 | 真实 provider 运行证据、`systemPrompt` 进入 provider 请求但不泄露到普通响应的完整 E2E、citation snapshot/detail/batch query、持久化 artifact 回放恢复、前端和 #125 完整 E2E 仍需收口。 | #157、#89、#217、#219、#210、#304、#428 |
| QA MCP/local tool 基础和 Document report artifact 映射 | 部分实现 | `services/qa/internal/platform/mcpclient`、local tools、安全摘要、Document report tool `reportArtifact` 映射、SSE/tool-call 安全测试，以及连接 Document Streamable HTTP MCP endpoint 的 env-gated report tools smoke。 | QA README、Gateway `QAReportArtifact` schema 和数据模型。 | 真实 provider 下的 QA Agent 端到端、Document worker/Gateway 下载探针共享环境、工具白名单运维和完整审计仍待纳入 #125。 | #151、#105、B-016、B-017 |
| AI Gateway model profile 和 credential 安全 | 已实现 | Model profile CRUD、provider credential AES-GCM 加密列、revision、service-token auth。 | AI Gateway `/internal/v1/model-profiles`；Gateway admin model profile routes。 | 真实部署的 secret manager、token 轮换和 profile 运维流程仍需补。 | #119、#204 |
| AI Gateway chat completions | 已实现 | OpenAI-compatible non-stream/stream chat、function-calling 字段透传、provider invocation 记录。 | `POST /internal/v1/chat/completions`。 | 真实 provider smoke、stream cancel 和 provider 特异行为回归仍需扩展。 | #120、#208 |
| AI Gateway embeddings | 已实现 | `POST /internal/v1/embeddings`、profile model exact-match、response count/index 校验、usage aggregate。 | AI Gateway OpenAPI embedding endpoint。 | Knowledge 已有可选 AI Gateway embedding adapter；真实 provider indexing smoke 仍缺。 | #121、#225、#152 |
| AI Gateway rerankings | 已实现 | `POST /internal/v1/rerankings`、OpenAI-style `/rerank` adapter、document_id/index 校验、usage aggregate。 | AI Gateway OpenAPI reranking endpoint。 | Knowledge 已有可选 AI Gateway rerank adapter；真实 provider retrieval/rerank smoke 仍缺。 | #121、#225 |
| Document 模板、素材、报告、大纲、章节 | 已实现 | `services/document/` 模板/材料/报告 CRUD、大纲版本、章节树、章节版本、权限和软删除测试。 | Gateway Document active paths；Document service OpenAPI。 | 模板/材料底层 File Service 的完整运行时 smoke 仍缺。 | #158、#202 |
| Document report jobs、attempts、events、AI 生成和 worker 状态机 | 部分实现 | report jobs/attempts/events handlers、asynq client/worker、PostgreSQL job tables；`summer_peak_inspection` 可通过 AI Gateway chat 生成基础大纲和逐章节正文；AI prompt 已收紧占位符/标题重复，前端 job.failed 轮询保留 retry grace window；`report_file_creation` 会执行基础 DOCX 导出。 | `/reports/{reportId}/jobs`、`/report-jobs/{jobId}`、events 等 active paths。 | 真实 provider 运行记录、AI Gateway/Knowledge/File/Redis 跨服务 smoke、更多报告类型生成策略和失败场景验收仍缺。 | #160、#220、#101、#514 |
| Document report files、settings、statistics、operation logs | 部分实现 | report files/content、基础内置 DOCX 导出、settings、statistics 和 operation logs 已在服务端实现；DOCX 导出已支持多级标题、表格 XML、脚注、中文字体、标题页和基础列表/段落格式。 | Gateway active document paths 已声明。 | report file content 依赖 File Service 内容可读；仍缺 Gateway/File/Redis/worker 跨服务 smoke 和 Pandoc/LibreOffice 富 DOCX 工具链运行时接入。 | #159、#160、#221、#223、#514 |
| Document MCP tool adapter/server | 部分实现 | 服务内 `mcp_tools.go` 已提供报告生成/状态/导出等安全摘要适配层；`internal/platform/mcpserver` 通过 Streamable HTTP `/mcp` 包装工具并执行 token 校验；QA env-gated smoke 可发现 `document__*` 工具并映射 `reportArtifact`。 | QA/Document 工具边界设计；Gateway `QAReportArtifact` schema。 | 共享环境中的 Gateway/Auth/worker/download 完整 smoke、真实 provider 触发的 Agent 调用和 #125 一键验收仍未闭环。 | #151、#158、#125、#451 |
| 前端 App shell、登录态、RBAC 导航和关键业务页面 | 部分实现 | `apps/web` auth shell、read-only report navigation 修正、QA 聊天附件上传/状态展示/ready 附件随消息发送、QA chat sidebar scroll containment、QA report artifact 预览/下载、Knowledge document upload/download、report generation/records/templates 页面、admin model/parser/QA settings 入口和 model profile UX 校验。 | 只调用 Gateway `/api/v1/**`。 | 管理端/业务页面仍以 smoke/unit 覆盖为主；QA artifact 刷新恢复、完整 RAG/MCP/Document 前端 E2E 和 #125 仍缺。 | #109、#212、#222、#110、#111、#163、#469、#481、#482、#516 |
| 前端 Gateway 类型和 typed client | 已实现 / 需持续校验 | `openapi-typescript` 已进入前端依赖，`api:generate` 脚本存在，生成类型已包含 `QAConfigVersion.systemPrompt` 和创建配置时可选 `systemPrompt`；Gateway readyz generated type 已同步。 | Gateway OpenAPI -> `apps/web/src/api/generated/`。 | 类型漂移、`systemPrompt` 字节限制和敏感字段展示边界需 CI 和 PR 前检查持续约束。 | #108、#161、#162、#499 |
| 本地联调环境 | 部分实现 | 根 `deploy/docker-compose.yml` 只提供 PostgreSQL、Redis、Qdrant、MinIO 和 `minio-init`；migration、seed、业务服务和 Knowledge RAGFlow runtime 均按 host-run 文档执行；默认源为官方，`dev-up.sh --china` 显式启用 Docker/uv/runtime artifact 镜像并自动准备 Knowledge runtime 依赖，`run-backend.sh --china` 显式启用 Go 镜像；Issue #125 runbook 汇总 Auth/Gateway、File owner、QA RAG、Document REST 和 Document MCP smoke slices，#304/#451 子场景可显式运行。 | 本地运行手册见 `deploy/README.md`、`docs/runbooks/local-integration.md` 和 `docs/runbooks/issue-125-smoke.md`。 | 现有 seed data 覆盖本地登录、基础报告类型、示例知识库和 AI profile placeholder；真实 provider/runtime doc engine 运行证据、统一前端到多服务 E2E 验收和生产部署流水线仍缺。 | #125、#150、#304、#451、#518 |
| 生产/准生产部署基线 | 缺失 | 当前仓库不保留业务服务容器或生产/准生产 Compose。 | 暂无。 | 如需生产部署能力，需后续独立任务设计。 | #306、#125 |
| 测试策略和 CI | 部分实现 | Frontend check/build/unit/E2E smoke workflow、Go services path-filtered workflow、goose migration workflow、Docker/Compose config checks、Gateway contract workflow、API type drift workflow、Issue #125 smoke slices 汇总入口，以及 Auth/Gateway/Redis full smoke 的本地脚本和手动 workflow。 | 测试策略见 `docs/testing/strategy.md`。 | 完整 DB integration jobs、Knowledge RAGFlow runtime PDF E2E 自动化、真实 provider 验证和完整前端到后端 E2E smoke 待补；Auth/Gateway/Redis full smoke 仍不是默认 required check。 | #117、#123、#125、#163、#352 |

## 当前最重要的文档缺口

1. 本地联调要继续明确根级 Compose 只负责基础设施，业务服务、migration、seed 和 Knowledge RAGFlow runtime 准备必须 host-run；默认下载源保持官方，中国大陆网络通过显式 `--china` 进入 Docker/Go/uv/runtime artifact 镜像路径；Knowledge runtime Python 包源不属于 Docker registry rewrite；生产/准生产部署流水线和完整前端到多服务 E2E smoke 仍需后续收口。
2. Knowledge 已有 RAGFlow runtime PDF E2E 路径、Gateway RAG 最小 smoke 和 MCP server 基础实现，但仍要区分当前 14 个原生 MCP 工具与 #529 四个 `knowledge__*` 目标工具；真实 provider、QA 默认接入、citation 和 #125 证据仍需单独补齐。
3. QA 已有 session attachments、ResponseRun / Agent Loop、versioned/global `systemPrompt` 管理员配置契约、Document report artifact 映射、最小 RAG smoke 和 QA -> Document MCP env-gated 子场景；真实 provider、prompt 生效且不向普通 QA 返回面泄漏的完整 E2E、citation snapshot/detail/batch query、artifact 回放恢复、前端完整 E2E 和 #125 完整跨服务 smoke 仍要单独追踪。
4. Document 已有 `summer_peak_inspection` 基础 AI 大纲/正文生成、AI prompt/DOCX formatting/polling retry 修复、基础 DOCX 导出、服务内 MCP tool adapter 和 Streamable HTTP MCP server；仍不能承诺更多报告类型、真实 provider 运行记录、真实 provider 触发的 QA Agent 端到端或 Pandoc 富 DOCX 运行时已可用。
5. 前端已能消费部分新 Gateway 契约，包括 QA session attachments、QA report artifact 预览/下载、Gateway readyz generated type 和 model profile UX 校验；聊天侧栏滚动体验已有组件级修复，但这些页面级能力不能替代真实 RAG/MCP/Document 后端跨服务 smoke。
6. 测试策略要继续区分当前 CI、env-gated smoke、手工受控 provider 验证和完整跨服务验收；路径过滤 CI 已覆盖前端、服务级 Go、Docker/Compose config，#125 已有 smoke slices 汇总入口，Auth/Gateway/Redis full smoke 已脚本化但不是默认 required check，完整前端到后端一键 E2E smoke 仍未落地。

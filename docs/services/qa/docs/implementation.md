
# QA 服务实现说明

版本：v0.2
日期：2026-07-02
范围：`services/qa/` 当前实现、契约对齐、缺口和后续实现约束

## 1. 文档定位

本文档描述 `qa` 当前实现状态和后续实现约束。它只补充服务 README、OpenAPI、架构和技术选型文档，不覆盖这些上游契约。

权威来源：

| 类型 | 权威来源 | 本文档关系 |
| --- | --- | --- |
| 服务公开说明 | `docs/services/qa/README.md` | 只能补充，不能覆盖 |
| 服务 OpenAPI | `docs/services/qa/api/internal.openapi.yaml`、`docs/services/qa/api/public.openapi.yaml`；`services/qa/api/openapi.yaml` 是实现本地路由副本 | 只能跟随，不能另起契约 |
| Gateway 公开契约 | `docs/services/gateway/api/public.openapi.yaml` | 前端稳定契约以 gateway 为准 |
| 服务边界 | `docs/architecture/service-boundaries.md` | 必须遵守 |
| 技术基线 | `docs/architecture/technology-decisions.md` | 必须跟随 |
| 代码实现 | `services/qa/` | 本文档记录当前状态和差距 |

凡是本文档与上表文件冲突，以上游文件为准；发现冲突时，在“文档与实现出入”中记录并生成回写或实现任务。

## 2. 当前结论

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| 文档状态 | active | README、数据模型、公开设计 OpenAPI 和服务内部 OpenAPI 存在。 |
| 代码状态 | partial | Go service、PostgreSQL repository、QA sessions/messages/SSE heartbeat/replay、资源查询、settings、MCP/model tooling、ResponseRun Agent Loop、function-calling adapter、QA -> AI Gateway env-gated smoke，以及 QA -> Document MCP 报告工具 env-gated smoke 已实现。 |
| 契约对齐 | partial | Gateway 29 个 QA active operations 均有 proxy route；QA 内部 routes 也注册，模型调用通过 AI Gateway chat completions；session attachments、版本化 `systemPrompt` 配置、Knowledge `knowledge-queries`、env-gated Gateway -> Knowledge -> QA RAG 最小 smoke 和 Document report tool `QAReportArtifact` 映射均已落地。完整 #125/MCP/前端跨服务闭环仍由后续任务收口。 |
| 数据持久化 | postgres | runtime 使用 PostgreSQL；配置 secret 使用本地加密 key。 |
| 测试状态 | covered / partial | 单元测试覆盖 service、repository mapping、HTTP、MCP/model/local tools、SSE/tool/citation 安全边界；QA -> AI Gateway chat、Gateway -> Knowledge -> QA RAG、QA -> Document MCP report tools 均已有 env-gated smoke。真实 provider 运行证据和完整 #125 E2E 仍需显式环境。 |
| 建议动作 | 补联调 / 回写文档 | 将 #304 opt-in RAG smoke 作为 Knowledge/QA 契约约束保留；继续在受控或真实 provider 环境补 QA + Knowledge 与 Gateway/Auth 完整联调。 |

## 3. 已实现

| 能力 | 代码位置 | 契约来源 | 验证方式 | 备注 |
| --- | --- | --- | --- | --- |
| 健康/就绪检查 | `services/qa/internal/http/server.go` | `docs/services/qa/api/internal.openapi.yaml` | `cd services/qa && go test ./...` | `/readyz` 使用 repo ping。 |
| QA session CRUD | `services/qa/internal/http/server.go`、`internal/service/qa.go` | Gateway OpenAPI QA paths | HTTP/service tests | 创建、列表、详情、更新、删除。 |
| QA owner authorization | `internal/repository/postgres.go`、`internal/repository/resources_postgres.go` | Gateway OpenAPI QA `403`/`404` responses / [QA 权限矩阵](permission-matrix.md) | HTTP/service tests；PostgreSQL integration test gated by `QA_TEST_DATABASE_URL` | 会话、message、run 和 citation 子资源按矩阵执行 owner 过滤与隐藏。 |
| 消息创建与 SSE | `services/qa/internal/http/server.go`、`internal/service/qa.go` | Gateway OpenAPI | `TestStreamUsesContractEventNames`、`TestAskSSEPayloadsDoNotLeakPromptRawToolOrProviderSecrets` | 支持 `Accept: text/event-stream`，fake-backed 测试覆盖 prompt、私有 chain-of-thought、原始工具结果、provider 原始错误、内部 URL 和 object key 不进入 SSE payload。 |
| SSE heartbeat/replay safeguards | `services/qa/internal/http/server.go`、`internal/service/qa.go`、`internal/repository` | #92 / #321 | SSE/service/repository tests | 支持 heartbeat、事件回放边界、取消后 replay record 保留和 event id 语义保护。 |
| response runs / tool calls / citations | `services/qa/internal/http/resource_handlers.go`、`internal/service/resources.go` | Gateway OpenAPI | service/repository tests | 返回脱敏资源摘要。 |
| QA/LLM config versions | `services/qa/internal/http/resource_handlers.go`、`internal/service/settings.go` | Gateway OpenAPI | config/settings tests | 配置版本持久化并加密敏感字段；创建 active QA/LLM 版本后触发 runtime reload。 |
| retrieval test / metrics | `services/qa/internal/http/resource_handlers.go` | Gateway OpenAPI | resource tests | 依赖 Knowledge retrieval client；`scoreThreshold: 0` 是有效显式配置，不应被 fallback 覆盖。 |
| ResponseRun Agent Loop | `services/qa/internal/service/qa.go`、`internal/service/agent`、`internal/repository` | #89 / QA README / QA 数据模型 | service、repository、modelclient tests | 创建用户消息、助手占位、response run、初始事件和模型调用摘要；落库 `completed`、`model_error`、`timeout`、`cancelled`、`max_iterations` 等终止原因。 |
| AI Gateway chat/function-calling client | `services/qa/internal/platform/modelclient/openai.go`、`internal/modelendpoint`、`internal/service/agent` | #90 / #253 / AI Gateway OpenAPI | modelclient/agent/config tests | 发送 OpenAI-compatible chat request，透传 `X-Caller-Service: qa` 和 request id，支持 `profile_id`、tool calls 和 streamed function-call completions。运行时 endpoint 必须指向受控 AI Gateway `/internal/v1/chat/completions`，不得包含 credentials/query/fragment、公网域名、非 loopback IP 或非标准内部端口；校验后 client 使用 canonical endpoint，避免把用户输入 host/path 直接传入 HTTP sink。 |
| QA -> AI Gateway env-gated smoke | `services/qa/internal/platform/modelclient/ai_gateway_smoke_test.go`、`services/qa/README.md` | #288 / AI Gateway seed runbook | `QA_AI_GATEWAY_SMOKE=1 go test ./internal/platform/modelclient -run '^TestAIGatewaySmoke$' -count=1 -v` | 默认 skip；显式启用时验证成功模型响应、service token 拒绝和缺失 profile 错误归一化。 |
| Gateway -> Knowledge -> QA RAG smoke | `services/knowledge/internal/integration/gateway_rag_e2e_smoke_test.go`、`docs/runbooks/local-integration.md` | #304 / QA RAG 主链路 | `GATEWAY_RAG_E2E_SMOKE=1 go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v` | 默认 skip；显式启用时通过 Gateway 配置 QA LLM/retrieval 版本，创建 QA session/message，要求模型使用 `search_knowledge` 工具并返回包含 `RAG-E2E-304` 的 answer，同时校验 response citations 和 message citation list 匹配本轮 KB/doc/chunk。需要可用 AI Gateway chat profile/provider；不替代 QA 单服务 smoke 或 #125。 |
| QA -> Document MCP report tools smoke | `services/qa/internal/platform/mcpclient/document_mcp_smoke_test.go`、`docs/runbooks/local-integration.md` | #451 / B-017 | `QA_DOCUMENT_MCP_SMOKE=1 go test ./internal/platform/mcpclient -run '^TestDocumentMCPReportToolsSmoke$' -count=1 -v` | 默认 skip；显式启用时连接 C-023 的 Document Streamable HTTP MCP endpoint，验证 `tools/list`、`document__*` 前缀、默认白名单、outline job accepted/running、status 查询、DOCX export/get_result 的 `reportArtifact`、无权限摘要和可选 Gateway 下载探针。普通 CI 不依赖真实 Document worker。 |
| MCP client/tooling | `services/qa/internal/platform/mcpclient`、`localtools` | QA README | platform tests | 支持 runtime Streamable HTTP、测试专用 exact-spec allowlisted stdio、内置工具。runtime 配置拒绝 stdio；包内 stdio 测试只映射代码内批准的 command spec 到固定 executable + argv，不把配置中的 executable/argv 直接传入 `exec.Command`；内置命令工具不再通过 shell 执行用户字符串，只运行 path-free diagnostic command，文件访问必须走 workspace-bounded file tools。 |
| PostgreSQL schema/repository | `services/qa/migrations/*.sql`、`internal/repository` | QA 数据模型 | repository tests | 有 integration tests，但依赖 `QA_TEST_DATABASE_URL`。分页、事件游标等写入 sqlc `int4` 参数前在 repository 层做 `int32` 范围校验，避免上层绕过时溢出。 |
| System prompt 版本化 | `services/qa/migrations/0014_versioned_system_prompt.sql`、`internal/service/settings.go`、`internal/repository/` | [S-048] / [B-018] | settings/service/repository tests | `system_prompt` 纳入 `qa_config_versions` 原子版本化；运行时从 active config 读取，空时回退 `AGENT_SYSTEM_PROMPT`；审计仅存长度元数据；发布触发 runtime reload。 |

## 4. 未实现

| 缺口 | 文档来源 | 影响范围 | 建议任务 |
| --- | --- | --- | --- |
| 完整 QA + Knowledge + AI Gateway RAG smoke 为显式 opt-in | `docs/services/gateway/api/public.openapi.yaml`、QA RAG 流程、#304 | QA / Knowledge / frontend | 已提供 env-gated Gateway RAG smoke 和 runbook；需要可用 AI Gateway chat profile/provider，普通 CI 默认 skip，且不覆盖 MCP/前端/#125 完整 E2E。 |
| 引用快照、引用详情和批量查询仍未完全闭环 | #93 / #325 | QA / frontend | 保留现有脱敏资源摘要，继续补 citation snapshot/detail/batch query 契约与持久化验证。 |
| QA -> AI Gateway smoke 依赖外部受控环境 | `docs/services/ai-gateway/api/internal.openapi.yaml` | QA / AI Gateway | 已提供 env-gated 入口；普通 CI 不启动 AI Gateway/provider，真实 provider 仍只允许显式手工运行。 |
| 完整 MCP/Knowledge/Model 端到端测试未证明 | QA README / local-integration runbook | integration | QA -> Document MCP 与 Gateway -> Knowledge -> QA RAG 已提供 env-gated 子场景；#125 仍需把 Auth/Gateway/File/Knowledge/QA/Document/AI Gateway/前端组合成完整一键 smoke。 |
| Knowledge MCP 默认接入未收敛 | Knowledge MCP docs / #505 / #528 | QA / MCP / citation | Knowledge MCP server 当前已有独立 endpoint 和 14 个原生工具，但 QA 默认 RAG 仍使用内置 `search_knowledge`；四个 `knowledge__*` 目标工具、默认白名单、citation 识别和 #125 smoke 仍需后续任务。 |
| AI Gateway service-token 配置需联调 | QA config / AI Gateway middleware | QA / AI Gateway / deploy | 验证 `AI_GATEWAY_TOKEN` 缺省复用 `INTERNAL_SERVICE_TOKEN` 与 AI Gateway token hashes 一致，并补 profile seed 说明。 |

## 5. 文档与实现出入

| 出入点 | 文档要求 | 当前实现 | 风险 | 建议处理 |
| --- | --- | --- | --- | --- |
| 模型调用边界 | 文档要求业务服务通过 AI Gateway 调模型 | `services/qa/internal/config/config.go` 默认 `AI_GATEWAY_URL=http://localhost:8086/internal/v1/chat/completions`，token header 默认 `X-Service-Token`，不再要求 `DEEPSEEK_API_KEY` fallback | 与架构方向一致；仍需部署联调 token hash 和 caller header | 补 QA -> AI Gateway smoke。 |
| Knowledge retrieval dependency | QA 文档将检索作为 RAG 主路径 | Knowledge 已实现 `knowledge-queries`，#304 已新增最小 Gateway RAG smoke 验证 QA answer/citation；真实 provider 和完整 #125 E2E 仍需单独记录 | 单服务测试通过不等于所有用户问答闭环已验收；env-gated smoke 默认不在普通 CI 执行 | 保留 #304 runbook，继续补 #95 retrieval tests、#93/#325 citation snapshot/detail/batch query 和 #125 完整 E2E。 |
| Knowledge MCP 工具命名 | #528/#529 定义四个 `knowledge__*` 目标模型工具 | 当前 Knowledge MCP 远程 server 原生工具仍是 `search_knowledge`、`answer_from_knowledge`、`list_knowledge_bases` 等，QA alias 后会成为 `knowledge__search_knowledge` 等名称；默认 QA config 未 seed Knowledge MCP | 容易让模型白名单、citation 识别和 PR 文档误以为 `knowledge__search` 已默认可用 | 在 #505 收敛前保持内置 `search_knowledge` 为默认 RAG smoke 路径；新增 Knowledge MCP 默认配置时同步白名单、citation 和 smoke。 |
| Gateway active QA paths | Gateway 29 个 QA operations active | QA 内部 routes 全注册，包括 session attachments 和 settings paths | route 层对齐，但业务结果依赖外部服务 | 增加跨服务 contract smoke。 |
| MCP 原始信息不得暴露 | 文档要求只返回脱敏摘要；QA 报告生成工具产物按 Gateway OpenAPI `QAReportArtifact` 暴露在 `tool.completed`/`tool.failed` 的 `payload.result.reportArtifact` 和 tool-call `resultSummary.reportArtifact` | B-016 已通过 Document MCP 工具名识别和 `GenerateResultSummary` 映射实现 `reportArtifact`，并覆盖 job pending、export succeeded、forbidden 和 SSE payload 安全测试；B-017 补充真实 Document MCP endpoint 的 env-gated smoke | 当前方向一致；QA 通过 MCP ToolClient 消费 Document 安全结果，不 import Document internal 包、不透传 MCP 原始 JSON | #125 仍需把该子场景纳入完整跨服务 smoke。 |
| Agent Run 状态 | README 描述 Agent Run、termination 和 maxIterations | develop 已包含 ResponseRun、终止原因、模型调用摘要、function-calling adapter 和基础测试 | 容易把 Agent Loop 可用误读为完整 RAG/citation 已完成 | 本文将 Agent Loop 和真实 RAG/citation smoke 分开记录。 |
| `sqlc` 生成器版本 | 技术基线固定 `sqlc` CLI 推荐版本为 `v1.31.1` | `services/qa/internal/repository/sqlc/*.go` 头部仍记录 `sqlc v1.29.0`；本次版本修复不改非 Docker 生成代码 | 代码生成器版本与文档基线出入，后续 SQL 变更时容易继续沿用旧生成器 | 下次修改 QA SQL 或 repository 生成代码时，使用 `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` 重新生成并提交。 |

## 6. QA 报告生成工具产物契约

`reportArtifact` 是 QA 面向前端的报告生成产物摘要，权威 schema 在 Gateway OpenAPI `QAReportArtifact`。QA 后端把 Document MCP tool result 映射为该结构，并只写入以下公开位置：

- SSE `tool.completed` / `tool.failed` 事件的 `payload.result.reportArtifact`。
- `agent_tool_calls.result_summary.reportArtifact`，供 `GET /api/v1/response-runs/{responseRunId}/tool-calls` 回放。

映射规则：

- 创建或查询 job 时返回 `jobId`、`jobType`、`jobStatus` 和进度类 `preview`；job 未完成时不得返回 `downloadPath`。
- 导出成功或 `get_report_result` 能确认文件就绪时，才返回 `reportFileId`、`filename`、`format`、`fileStatus=succeeded`、`fileSize` 和 `/api/v1/report-files/{reportFileId}/content` 形式的 `downloadPath`。
- 无权限、依赖错误、导出失败只返回安全 `preview` 与状态字段，不返回 File internal ID、object key、bucket、内部 URL、prompt、provider 原始错误、MCP 原始参数或完整结果。
- `preview` 只保留标题、章节标题、短摘要、进度和用户可见状态，不放完整报告正文。

当前状态：B-016 已实现 QA 侧 Document report 工具摘要映射。默认工具白名单包含 `document__generate_report_outline`、`document__generate_report_text`、`document__get_generation_status`、`document__export_report_docx` 和 `document__get_report_result`；当运行时注册 alias 为 `document` 的 MCP server 且 tools/list 返回这些工具时，Agent 可在 `report_generation` 模式选择调用。QA 仅把 Document MCP tool result 映射为 `reportArtifact`，不透传原始 JSON。

运行时配置：数据库 `mcp_servers.alias=document` 是正式配置路径；`deploy/seeds/003-qa-document-mcp.sql` 写入宿主机 endpoint 等非敏感元数据，环境变量 bootstrap 用 `MCP_TRANSPORT=streamable_http`、`MCP_SERVER_ALIAS=document`、`MCP_SERVER_URL=http://localhost:8085/mcp`、`MCP_SERVER_TOKEN`、`MCP_SERVER_TOKEN_HEADER` 提供本地运行参数与 token。数据库同名记录无 token 时回退环境 token；数据库 token 和显式 disabled 状态优先。根级 Compose 仅启动基础设施，QA 与 Document 都在宿主机运行。`MCP_TOOL_TIMEOUT` 限制单次工具调用；QA 不在工具适配层做无界轮询，模型同一 run 内继续调用 `document__get_generation_status` 时由 `AGENT_MAX_ITERATIONS` 与单次工具超时共同约束。真实 Document worker / Gateway 下载链路 smoke 需要用 `QA_DOCUMENT_MCP_SMOKE=1` 显式开启，普通 CI 仅跑 fake Document MCP / 单服务契约测试；该 smoke 覆盖 `tools/list`、prefixed tool、job pending、status 查询、export/result artifact、forbidden 摘要和可选 Gateway download probe。全部 9 个当前工具的 schema 与 Agent 工作流见 [`../../document/docs/mcp-tools.md`](../../document/docs/mcp-tools.md)。

## 7. MVP / mock / memory backend / 占位

| 项目 | 当前用途 | 退出条件 | 关联任务 |
| --- | --- | --- | --- |
| built-in/local tools | 无外部 MCP server 时支持开发调试 | 生产工具白名单和 MCP server 稳定后限制启用 | 后续工具白名单 / MCP 运维任务 |
| AI Gateway default endpoint | 未显式配置时使用本地 AI Gateway chat completions | 环境差异需要部署文档明确覆盖 | QA -> AI Gateway smoke / profile seed 任务 |
| repository integration tests gated by env | 避免无 DB 环境失败 | CI 提供 `QA_TEST_DATABASE_URL` | testing required checks 分阶段升级任务 |

## 8. 运行与配置

| 项目 | 当前状态 | 缺口 |
| --- | --- | --- |
| 启动命令 | `cd services/qa && go run ./cmd/server` | 需要 PostgreSQL、模型 endpoint、Knowledge URL。 |
| 环境变量 | `QA_DATABASE_URL`、`QA_HTTP_ADDR`、`KNOWLEDGE_SERVICE_URL`、`INTERNAL_SERVICE_TOKEN`、`AI_GATEWAY_URL`/token、MCP、tool limits、settings flags | 需统一命名和 secret 注入说明。 |
| PostgreSQL / migration | `migrations/0001` 到 `0004`，`sqlc.yaml`，runtime repository | 需要 CI migration apply 证据。 |
| Redis / queue | 当前交互式主路径不使用队列 | 后续离线任务再接 asynq。 |
| Object storage / vector store / AI provider | 通过 Knowledge/AI Gateway/MCP 间接访问 | 需补 QA -> AI Gateway/provider smoke。 |

## 9. 测试与验证

| 验证项 | 命令或步骤 | 当前结果 | 缺口 |
| --- | --- | --- | --- |
| 单元测试 | `cd services/qa && go test ./...` | pass（2026-07-01，本轮 TDD） | 真实 DB tests 可能被 env gate 跳过。 |
| 服务构建 | 同上 Docker Go 环境运行 `go build ./cmd/server` 和 `go build ./cmd/agent` | pass（2026-07-01，本轮 TDD） | 宿主机未安装 Go 时使用 Docker Go 镜像。 |
| 集成测试 | `QA_TEST_DATABASE_URL=... go test ./internal/repository` | not run | 需要 PostgreSQL。 |
| 契约测试 | Gateway QA schema contract + QA HTTP/service safety tests | partial / guarded | `cd services/gateway && go test ./internal/http -run QA` 覆盖 29 个 QA-owned Gateway active paths 的 schema/auth/content type、session attachments、settings `systemPrompt` contract 与 internal `$ref` drift；QA service fake-backed SSE 安全测试不依赖 PostgreSQL。 |
| QA -> AI Gateway smoke | `QA_AI_GATEWAY_SMOKE=1 go test ./internal/platform/modelclient -run '^TestAIGatewaySmoke$' -count=1 -v` | env-gated | 需要运行中的 AI Gateway、有效 service token、显式 chat profile 和受控或真实 provider；默认 CI skip。 |
| Gateway -> Knowledge -> QA RAG smoke | `GATEWAY_RAG_E2E_SMOKE=1 go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v` | available（2026-07-01 新增；默认 skip；本轮只跑默认 skip 编译检查） | 需要 Gateway/Auth/Redis/Knowledge/RAGFlow runtime/QA/AI Gateway 和可用 chat profile/provider；覆盖最小 answer/citation，不覆盖 MCP/前端/#125。 |
| QA -> Document MCP report tools smoke | `QA_DOCUMENT_MCP_SMOKE=1 go test ./internal/platform/mcpclient -run '^TestDocumentMCPReportToolsSmoke$' -count=1 -v` | available（默认 skip） | 需要根级 Compose 中 Document MCP、File、Redis、PostgreSQL、Gateway/seed 可用；覆盖 Document tool discovery、report artifact 和安全摘要，不替代 #125 完整跨服务 smoke 或 F-020 前端 UI。 |
| 完整手工 smoke | Gateway -> QA session -> message stream | pass（2026-07-01，本地 Compose；知识库 `123`，查询“支持向量机实验的实验目的是什么？”命中 1 条 citation 并生成回答） | 仍需在共享/受控环境复跑并记录真实 provider 证据。 |

## 10. 建议任务

| 任务 | 类型 | 优先级 | 依据 | 说明 |
| --- | --- | --- | --- | --- |
| 将 QA -> AI Gateway smoke 接入受控集成环境 | 后续任务 | P1 | #288 env-gated smoke | 当前入口默认 skip；待共享 provider fixture/CI secret 策略稳定后再升级为受控集成 job。 |
| 扩展 QA + Knowledge + AI Gateway retrieval 联调 | 后续任务 | P0 | #304 已补最小 smoke；仍缺更完整场景 | 覆盖 no result、dependency_error、真实 provider/rerank trace、citation snapshot/detail/batch query 和 SSE replay。 |
| 补 citation snapshot/detail/batch query | 新任务 | P0 | #93 / #325 | 不把现有 tool-call/resource 摘要误写成完整 citation API。 |
| 补完整 QA + Knowledge + Gateway E2E smoke | 新任务 | P0 | 单服务 fake-backed 测试不能替代跨服务验收 | 覆盖 Auth/Gateway/Knowledge/AI Gateway provider fixture 和 QA SSE replay。 |

## 11. 最近检查记录

| 日期 | 检查人/工具 | 代码基准 | 结论 |
| --- | --- | --- | --- |
| 2026-07-04 | Codex #604/#605 TDD | `JerryTeam/fix/qa-sse-stream-cancel` | QA SSE answer deltas now project provider streaming `delta.content` chunks through modelclient -> agent runner -> service events, with a final single-delta fallback for non-streaming providers. Request-context cancellation now reaches the agent/model/tool execution context; bounded cleanup still persists response runs as `cancelled`. |
| 2026-07-03 | Codex docs watch | `develop@ce0b4774` | 复核 #527/#440/#529/#531：QA 的 Document MCP report tools env-gated smoke 可连接 Document Streamable HTTP `/mcp` endpoint；Gateway -> Knowledge -> QA RAG 依赖当前 Knowledge RAGFlow runtime，而不是旧独立 Parser 服务；Knowledge MCP server 已存在但默认 QA 接入仍未从内置 `search_knowledge` 收敛到四个 `knowledge__*` 目标工具。完整前端/#125/真实 provider E2E 仍未证明。 |
| 2026-07-01 | Codex QA knowledge RAG TDD | working tree on `develop@9640bee` | 修复 QA active 配置发布不 reload、显式 `scoreThreshold: 0` 被默认化、模型工具参数把阈值调高导致 local hashing 检索无命中的问题；新增 repository/service/tool/client 单元回归，并完成本地 Gateway 用户层 SSE smoke。 |
| 2026-07-01 | Codex #337 security pass | PR #359 | Code Scanning 修复收紧模型出站边界：QA runtime/settings/modelclient 只接受受信 AI Gateway `/internal/v1/chat/completions` endpoint，存量 `direct` 配置不再可作为任意 provider URL 出口；provider base URL 和密钥继续由 AI Gateway profile 承载。 |
| 2026-07-01 | Codex #304 branch | working tree | 新增 env-gated Gateway -> Knowledge -> QA RAG smoke；通过 Gateway 配置本轮 QA LLM/retrieval，创建 QA session/message，并断言 answer/citation 使用 Knowledge `search_knowledge` 结果。普通 CI 默认 skip，真实 provider 仍按 runbook 手动执行。 |
| 2026-07-01 | Codex CodeQL follow-up | working tree | 继续收敛合并后仍 open 的 QA `go/request-forgery` 告警：AI Gateway endpoint 解析后只保留 canonical trusted URL literal，端口固定为 `8086`，单元测试用 transport rewrite 覆盖 httptest 而不放宽生产配置。 |
| 2026-06-30 | Codex #288 branch | working tree | 新增 QA -> AI Gateway env-gated chat smoke，覆盖成功响应、无效 service token、缺失 profile 和 request id 诊断；普通 CI 保持 skip，不扩展到完整 QA/Knowledge/Gateway 链路。 |
| 2026-06-30 | Codex full-day audit | `develop@92d3afc` | 复核今日 PR/issue：QA 已包含 Agent Loop、function-calling adapter、SSE heartbeat/replay safeguards、MCP SDK security update 和 QA -> AI Gateway env-gated smoke；Knowledge `knowledge-queries` 已落地，剩余为完整 RAG/citation 跨服务 smoke、citation snapshot/detail/batch query、retrieval/metrics 强化。 |
| 2026-07-01 | Codex #343 branch | `develop@96b5ad8f` + 本分支改动 | 新增 Gateway QA active path schema contract 和 QA service fake-backed SSE/tool/citation 安全边界扫描；快速测试不依赖 PostgreSQL，repository/integration 仍由 `QA_TEST_DATABASE_URL` 显式 gate。 |
| 2026-07-02 | Codex docs refresh | `develop@736acde0` | 最新 Gateway active QA paths 为 29 个，包含 session attachments 和 QA settings `systemPrompt` contract；前端已支持 QA chat 附件上传、状态展示、ready 附件随消息发送、页面刷新/切换会话后恢复附件列表，并已收敛聊天侧栏滚动链路。 |
| 2026-06-29 | Codex #89 branch | `31711d9` + working tree | B-03 非流式 Agent Run MVP 覆盖成功、模型失败、超时、取消和 max-iterations；response_run、assistant message、初始事件和模型调用摘要保持一致。剩余风险为 Knowledge retrieval、跨服务 smoke 和 env-gated DB integration。 |
| 2026-06-29 | Codex after proxy rebase | `0e402ca` + working tree | QA route 层基本对齐，config 默认走 AI Gateway chat；主要剩余风险在 Knowledge retrieval 未完成和跨服务 smoke 未跑。 |
| 2026-06-29 | Codex after rebase | `808c589` + working tree | QA route 层基本对齐，AI Gateway chat 下游已落地；当时主要剩余风险在 Knowledge retrieval 未完成、跨服务 smoke 未跑和 direct provider fallback 边界，后续 `develop` 已移除 DeepSeek fallback。 |
| 2026-06-29 | Codex goal | `eddf917` + working tree | QA 代码量已较完整，route 层基本对齐；当时主要风险在 Knowledge/AI Gateway 下游未完成和 direct provider fallback 边界。 |

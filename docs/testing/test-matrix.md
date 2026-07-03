# 全局测试矩阵与证据追踪表

日期：2026-07-01
基线：`upstream/develop`；后续维护以合入时的 `develop` 为准。

本文是测试组总测试矩阵，用于把分散在前端、后端单元、契约、集成、E2E、Smoke、部署验证和测试治理 issue 中的验证工作收口到同一张表。GitHub Issue 和 Project 仍是状态事实来源；本文只做测试覆盖、证据位置和汇总口径。

## 口径

| 分类 | 使用口径 | 证据要求 |
| --- | --- | --- |
| 本地自动化 | 不依赖外部凭据或长期环境，开发者和 PR reviewer 可重复执行。 | 记录命令、commit、环境和结果。 |
| CI 自动化 | GitHub Actions 可稳定执行，适合作为 required check 候选。 | 链接 workflow run 或 PR check，并说明覆盖边界。 |
| env-gated smoke | 需要显式环境变量、数据库、Redis、MinIO、Elasticsearch、Knowledge runtime 或 Compose 服务。 | 记录缺失环境、跳过条件、日志位置和 request id。 |
| 真实 provider smoke | 需要真实外部模型 provider key 或受保护环境。 | 不提交 key、完整 prompt、payload 或 provider raw body；只记录脱敏结果和 request id。 |
| 人工验收 | 需要人工账号、截图、演示环境或逐步操作。 | 记录 checklist、截图/trace、输入摘要、预期/实际结果和失败定位入口。 |

## 既有任务矩阵

| Issue | 测试领域与层级 | 主责 / 协作 | 状态 | 依赖与同步对象 | 必跑命令或操作 | 证据位置 | 残余风险口径 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [#372](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/372) `F-018` 知识库文档上传单元测试 | 前端本地自动化、mock-backed 单元/组件测试。 | 主责 `@AKTNL`；协作同步 #401。 | Closed；任务正文仍为 In Progress，最终以 GitHub closed 为准。 | 无上游依赖。 | `bun run --cwd apps/web check`；`bun run --cwd apps/web build`；`bun run --cwd apps/web test:unit`。 | Issue/PR 记录；如测试组复验，归档到 `docs/testing/reports/YYYY-MM-DD/frontend-upload-test-report.md`。 | 只能证明前端 mock 层行为，不等价于 File/Knowledge runtime 真实链路。 |
| [#375](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/375) `C-018` Document AI 非法 JSON 容错测试 | Document 后端单元测试、fake AI 响应。 | 主责 `@Tsuki-CARAT`；协作同步 #379、#398。 | Closed。 | 依赖 #102。 | `cd services/document && go test ./...`；`cd services/document && go build ./cmd/server`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/document-ai-malformed-json-test-report.md`。 | 不证明真实 AI Gateway provider 或跨服务报告文件链路。 |
| [#388](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/388) `C-021` Document 检索失败与空结果容错 | Document 后端单元/集成边界测试。 | 主责 `@Tina-jwt`；协作同步 #379、#402。 | Closed。 | 依赖 #101，并行 #375。 | `cd services/document && go test ./...`；`cd services/document && go build ./cmd/server`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/document-retrieval-fallback-test-report.md`。 | fake Knowledge 或空结果只能覆盖服务容错，不证明 Knowledge retrieval 质量。 |
| [#389](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/389) `C-022` Document AI Gateway client 单元测试 | Document HTTP client 单元测试、fake provider。 | 主责 `@Tina-jwt`；协作同步 #378、#398。 | Closed。 | 无上游依赖。 | `cd services/document && go test ./...`；`cd services/document && go build ./cmd/server`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/document-aigatewayclient-test-report.md`。 | 不证明真实 provider key、AI Gateway profile 或跨服务鉴权。 |
| [#385](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/385) `S-043` QA 附件 Gateway OpenAPI 契约与服务边界 | Gateway/QA 契约、OpenAPI drift、文档一致性。 | 主责 `@bingyuwu645-sudo`；协作同步 #386、#387、#401。 | Closed。 | 依赖 #118 #73 #88；阻塞 #386 #387；并行 #343 #341 #304 #378。 | `python3 -m unittest scripts.tests.test_verify_gateway_active_api`；`python3 scripts/verify_gateway_active_api.py`；如影响前端类型，执行 `bun run --cwd apps/web api:generate`；文档改动执行 `git diff --check`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/qa-attachment-contract-test-report.md`。 | 契约通过不等于 QA 附件实现、File bytes 存储、QA 临时解析或前端上传流程已验证。 |
| [#386](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/386) `B-015` QA 会话附件上传、QA 临时解析、Agent 检索与 TTL 清理 | QA 后端单元、File env-gated smoke、附件生命周期。 | 主责 `@JerryBlackoo`；协作同步 #385、#400、#401。 | Open / In Progress。 | 依赖 #385 #88 #91 #93 #80 #83；阻塞 #387 #125；并行 #304 #343 #96 #341 #378。 | `cd services/qa && go test ./...`；`cd services/qa && go build ./cmd/server`；`cd services/qa && go build ./cmd/agent`；如跑真实附件链路，记录 File 和 QA 附件 smoke 命令。 | `docs/testing/reports/YYYY-MM-DD/qa-session-attachments-test-report.md`；PR 需附 request id、日志和未运行项。 | File/AI Gateway 未启动时只能记录为 skipped 或 blocked，不能写成通过；QA 附件不得写入 Knowledge 长期索引。 |
| [#379](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/379) `C-020` Document 跨服务集成与公共接口契约测试 | Document 服务内测试、跨服务 smoke、文件/AI 下游边界。 | 主责 `@Tsuki-CARAT`；协作同步 #375、#388、#398、#400。 | Closed。 | 依赖 #307 #368；并行 #375 #376。 | `cd services/document && go test ./...`；`cd services/document && go build ./cmd/server`；真实 File/AI Gateway 路径按环境记录 env-gated 操作。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/document-cross-service-test-report.md`。 | Document-only 环境不证明 File bytes、AI provider 或前端下载完整链路。 |
| [#304](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/304) `S-029` Knowledge 到 QA RAG 端到端验收样例 | RAG E2E、Knowledge/QA/Gateway env-gated smoke。 | 主责 `@Sakayori-Iroha-168`；协作同步 #397、#401、#402、#403。 | Open / Ready。 | 依赖 #236 #84 #288 #289 #305；阻塞 #125 #306；并行 #286 #287 #282 #303。 | `cd services/knowledge && KNOWLEDGE_INGESTION_SMOKE=1 ... go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v`；按 runbook 补 Gateway/QA 操作和 request id。 | `docs/testing/reports/YYYY-MM-DD/rag-e2e-smoke-test-report.md`。 | local hashing embedding 不等于真实 embedding/rerank provider；完整 MCP/跨服务入口仍由 #125 收口。 |
| [#125](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/125) `S-008` MCP 与跨服务契约端到端冒烟脚本 | 跨服务 E2E smoke、MCP/Gateway 总入口、部署联调。 | 主责 `@Sakayori-Iroha-168`；协作同步 #304、#397、#398、#399、#400、#402、#403。 | Open / Ready。 | 依赖 #84 #91 #105 #122 #234；并行 #92 #101 #150 #160 #264。 | 启动本地栈后按 `docs/runbooks/local-integration.md` 记录 health/ready、Gateway public API、MCP/owner service 请求和日志；配置检查执行 `docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config --quiet`。 | `docs/testing/reports/YYYY-MM-DD/cross-service-smoke-test-report.md`。 | 当前仓库仍无完整一键 E2E required check；未稳定依赖只能作为 env-gated 或人工验收。 |
| [#305](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/305) `S-030` 本地联调 seed 数据统一化 | Seed/Reset/Migration 静态契约和本地联调基线。 | 主责 `@Jackeyliu37`；协作同步 #304、#399、#403。 | Closed。 | 依赖 #286 #289；阻塞 #304；并行 #287 #303。 | `python scripts/verify_local_seed_contract.py`；`python -m unittest scripts.tests.test_local_seed_contract`；`docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config --quiet`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/local-seed-baseline-test-report.md`。 | 静态契约通过不代表所有数据库已实际 migration + host-run seed；本地执行需另记环境。 |
| [#306](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/306) `S-031` Deploy/Compose 环境变量基线 | Infra-only Compose 配置、Docker policy、环境变量基线。 | 主责 `@Sakayori-Iroha-168`；协作同步 #399、#403。 | Superseded by infra-only Docker baseline。 | 依赖 #286 #289 #304；并行 #305。 | `python3 scripts/check_docker_policy.py`；`docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config --quiet`；`docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config --services`。 | `docs/testing/reports/YYYY-MM-DD/deploy-compose-baseline-test-report.md`。 | Compose config 只证明 infra 配置可解析；业务服务必须按 host-run 文档单独验证。 |
| [#378](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/378) `S-042` AI Gateway 真实 API Key 接入与跨服务验收 | AI Gateway 本地自动化、真实 provider smoke、跨服务 AI 验收。 | 主责 `@AndyXuPrime`；协作同步 #398、#402、#403。 | Closed。 | 无上游依赖；并行 #125 #304 #305 #352 #353。 | `cd services/ai-gateway && go test ./...`；受保护环境执行 `AI_GATEWAY_REAL_PROVIDER_SMOKE=1 ... go test ./internal/http -run '^TestRealProviderSmoke_ExplicitEnvOnly$' -count=1 -v`。 | Issue/PR 记录；如复验，归档到 `docs/testing/reports/YYYY-MM-DD/ai-gateway-real-provider-test-report.md`。 | 真实 provider smoke 不进入默认 CI；报告不得泄露 key、prompt、embedding payload 或 provider raw body。 |

## Batch 5 测试任务矩阵

| Issue | 测试领域与层级 | 主责 / 协作 | 状态 | 依赖与同步对象 | 必跑命令或操作 | 证据位置 | 残余风险口径 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [#396](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/396) `T-001` 全局测试矩阵与证据追踪表 | 测试治理、文档一致性。 | 主责 `@AKTNL`；协作所有测试 issue owner。 | Open / Ready。 | 依赖 #377；并行 #372 #375 #388 #389 #385 #386 #379 #304 #125 #305 #306 #378。 | `git diff --check`；新增文件执行 `git diff --no-index --check`；人工核对矩阵覆盖 issue、命令、证据和风险。 | 本文；`docs/testing/reports/2026-07-01/test-matrix-test-report.md`；`docs/testing/templates/test-evidence-record-template.md`；`docs/testing/templates/final-acceptance-summary-template.md`。 | 本文不替代各专项任务实际测试，只提供追踪和汇总入口。 |
| [#397](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/397) `T-002` 全站权限矩阵与越权访问回归测试 | 权限矩阵、Gateway/Auth/owner service 回归、env-gated smoke。 | 主责 `@bingyuwu645-sudo`；协作 #304 #125 owner。 | Open / In Progress。 | 依赖 #352 #343 #385 #386 #379；并行 #304 #125。 | `cd services/gateway && go test ./internal/http -run QA`；按影响服务执行 `go test ./...`；真实 owner route 执行 `GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayKnowledgeOwnerRouteSmoke$' -count=1 -v`。 | `docs/testing/reports/YYYY-MM-DD/permission-regression-test-report.md`。 | 未启动 Auth/Gateway/Redis/owner service 时只能记录未运行原因；前端隐藏不等于后端权限通过。 |
| [#398](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/398) `T-003` API 响应与日志敏感信息泄露检查 | 安全回归、错误 envelope、日志/测试输出脱敏。 | 主责 `@up998`；协作 #378 #386 #379 #125 owner。 | Open / In Progress。 | 依赖 #378 #386 #379 #125；并行 #343 #352。 | 按命中服务执行 `go test ./...` 和 focused negative tests；检查 API response、SSE event、日志摘要、测试输出；文档或脚本改动执行 `git diff --check`。 | `docs/testing/reports/YYYY-MM-DD/sensitive-data-regression-test-report.md`。 | 没有稳定日志捕获的服务必须记录人工检查步骤；不能提交 token、object key、内部 URL 或完整 prompt。 |
| [#399](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/399) `T-004` Seed、Reset 与 Migration 升级验收 | Seed/Reset/Migration、本地联调、infra Compose 或 env-gated DB。 | 主责 `@Tina-jwt`；协作 #305 #306 #352 owner。 | Open / In Progress。 | 依赖 #305 #306 #352；并行 #304 #125 #378。 | `python scripts/verify_local_seed_contract.py`；`python -m unittest scripts.tests.test_local_seed_contract`；`docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config --quiet`；真实 DB apply 需记录 `goose@v3.27.1` 和 `psql` 命令。 | `docs/testing/reports/YYYY-MM-DD/seed-reset-migration-test-report.md`。 | 静态校验不等价于真实空库 apply；未启动 PostgreSQL/Redis/Elasticsearch/MinIO 时要列明残余风险。 |
| [#400](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/400) `T-005` File 与 Knowledge runtime 各自边界测试 | File 负向测试、Knowledge runtime 负向测试、上传/下载边界、服务响应脱敏。 | 主责 `@wanghanliang666`；协作 #386 #379 #125 owner。 | Open / In Progress。 | 依赖 #386 #379 #125；并行 #304 #372 #387。 | `cd services/file && go test ./...`；`cd services/knowledge && go test ./...`；`cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest <targeted tests> -q`；真实对象存储执行 `FILE_MINIO_POSTGRES_SMOKE=1 ... go test ./internal/integration -run TestFileMinIOPostgresSmoke -count=1 -v`。 | `docs/testing/reports/YYYY-MM-DD/file-knowledge-runtime-boundary-test-report.md`。 | Runtime、真实 OCR 和 MinIO/PostgreSQL 依赖不可用时不能写成通过；公开响应不得暴露 bucket、object key 或 runtime 原始错误。 |
| [#401](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/401) `T-006` 前端登录、知识库、QA、报告核心流程 E2E | 前端 check/build/unit、Playwright mock-backed 或 local Gateway-backed E2E。 | 主责 `@AndyXuPrime`；协作 #372 #387 #304 owner。 | Open / In Progress。 | 依赖 #372 #387 #304；并行 #386 #385。 | `bun install --frozen-lockfile`；`bun run --cwd apps/web check`；`bun run --cwd apps/web build`；`bun run --cwd apps/web test:unit`；关键流程执行 `bun run --cwd apps/web test:e2e`。 | `docs/testing/reports/YYYY-MM-DD/frontend-core-e2e-test-report.md`，并保存截图或 trace 路径。 | mock E2E 不能声明跨服务真实验收；真实 Gateway 模式需记录后端环境和 request id。 |
| [#402](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/402) `T-007` Knowledge 检索质量与 rerank 排序回归 | Knowledge retrieval、rerank 质量基线、QA citation 检查。 | 主责 `@Tsuki-CARAT`；协作 #304 #125 #378 #386 owner。 | Open / In Progress。 | 依赖 #304 #125；并行 #378 #386。 | `cd services/knowledge && go test ./...`；有真实依赖时执行 `KNOWLEDGE_INGESTION_SMOKE=1 ... go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v`；真实 rerank/provider 另按 #378 记录。 | `docs/testing/reports/YYYY-MM-DD/knowledge-rerank-quality-test-report.md`。 | local hashing 和 fake provider 只能作为稳定回归基线，不代表真实语义排序质量。 |
| [#403](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/403) `T-008` 最终人工验收脚本与演示 checklist | 最终人工验收、演示脚本、跨模块证据汇总。 | 主责 `@AKTNL`；协作所有测试 issue owner。 | Open / Ready。 | 依赖 #377 #304 #125 #305 #306 #378；并行 #372 #375 #388 #389 #385 #386 #379。 | `git diff --check`；按 `docs/runbooks/local-integration.md` 记录 infra Compose/host-run readyz/关键 API 操作；人工 checklist 需覆盖登录、Knowledge、QA、Document、File、AI Gateway/Gateway。 | `docs/testing/reports/YYYY-MM-DD/final-acceptance-test-report.md`；最终汇总使用 `docs/testing/templates/final-acceptance-summary-template.md`。 | 人工演示不替代各专项自动化；真实 provider、完整 E2E 和外部凭据缺口必须单独列出。 |
| [#454](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/454) `T-010` 前端无障碍与键盘导航轻量回归 | 前端 RTL 键盘/a11y smoke、Playwright mock-backed E2E、测试报告归档。 | 主责 `@AKTNL`；协作 #401 #432。 | Review / 本地完成。 | 无上游依赖；并行 #401 #432。 | `bun install --frozen-lockfile`；`bun run --cwd apps/web check`；`bun run --cwd apps/web build`；`bun run --cwd apps/web test:unit`；`bun run --cwd apps/web test:e2e`；`git diff --check`。 | `docs/testing/reports/2026-07-02/frontend-a11y-keyboard-test-report.md`。 | mock-backed/RTL 通过不等于真实 Gateway-backed 验收；Windows 工作树既有 CRLF 行尾会让 full `format:check` 本地失败，变更范围已单独通过 Prettier check。 |
| [#455](https://github.com/Sakayori-Iroha-168/Software_Teamwork/issues/455) `T-011` Go 后端 race detector 抽样检查 | Gateway/QA/Knowledge/Document focused `go test -race` 抽样、普通 focused `go test` 对照。 | 主责 `@AKTNL`；协作 #397 #398 #402。 | Review / 本地完成。 | 无上游依赖；并行 #397 #398 #402。 | Docker Linux Go 1.25.6 在 rebased 快照上执行 `go test -race -count=1` focused packages；相同范围执行普通 `go test -count=1`；`git diff --check`。 | `docs/testing/reports/2026-07-02/go-race-sample-test-report.md`。 | focused 抽样不替代全量 `go test ./...`、服务 build 或真实依赖 smoke；Windows native 缺 GCC/cgo，实际 race 抽样在 Docker Linux 环境完成；未把 `-race` 升级为默认 CI。 |

## 证据记录格式

每个测试 issue 至少保留一类可复核证据；复杂测试还必须补完整测试报告：

1. 轻量执行记录：纯单元测试、组件测试和静态检查可只使用 `docs/testing/templates/test-evidence-record-template.md` 填写到 issue 评论或 PR body，记录命令、环境、结果、未运行原因、失败证据和缺陷处理。
2. 完整测试报告：集成测试、E2E、权限/安全边界、文件/Knowledge runtime 边界、migration、环境验收、人工验收、回归测试或缺陷复现必须复制 `docs/testing/templates/test-report-template.md` 到 `docs/testing/reports/YYYY-MM-DD/<scope>-test-report.md`，填写命令、环境、结果、缺陷、未运行项和最终结论。
3. 快速证据摘要：复杂测试即使已有完整报告，也应在 issue/PR 中链接报告并保留快速证据摘要，便于 reviewer 不打开完整报告也能看到已运行命令、未运行原因、截图/日志/trace、失败复现和关联 PR。

证据必须区分以下结论：

- 测试通过。
- 测试失败且已修复。
- 测试失败已转 issue。
- 因环境缺失未运行。

禁止只写“已测试”。未运行项必须写清缺失环境、跳过条件、残余风险和后续归属。

## 阶段性更新规则

| 时机 | 必须更新的内容 |
| --- | --- |
| 认领或转让 issue 后 | 更新主责人、协作对象和当前状态。 |
| 开始测试设计后 | 确认测试层级、必跑命令、env-gated 条件和证据路径。 |
| PR 前 | 补齐已运行命令、未运行原因、失败证据、关联 PR 和报告路径。 |
| 发现缺陷后 | 判断小问题或大问题；小问题可在当前测试 PR 修复，大问题新建 owner issue 并回填链接。 |
| PR 合并或 issue 关闭后 | 将 GitHub 状态、报告路径、最终结论和残余风险同步到矩阵。 |
| 周报或最终汇总前 | 使用 `docs/testing/templates/final-acceptance-summary-template.md` 汇总覆盖范围、环境依赖、已知缺口和风险。 |

## 维护原则

- GitHub Issue/Project 是状态事实来源；矩阵状态落后时以 GitHub 为准，并尽快更新本文。
- 矩阵中的命令是最低证据要求。任务实际改动触碰更多服务、契约、Docker 或前端时，按 `docs/testing/strategy.md` 扩大验证范围。
- mock/fake、轻量容器、env-gated、真实 provider 和人工验收必须分开记录，不得混写成同一种通过结论。
- open PR、未合入 issue 和草案不能写成当前 `develop` 已实现。
- 任何报告、截图、日志或 trace 都不得泄露 token、API key、object key、内部 URL、完整 prompt、embedding payload、provider raw body 或生产凭据。

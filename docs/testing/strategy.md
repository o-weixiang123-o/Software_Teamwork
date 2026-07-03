# 测试策略

日期：2026-07-03

本文把当前仓库已经可执行的检查、CI 覆盖和仍缺的测试能力放在一起，作为 PR 前验证基线。具体服务实现状态仍以各服务 `docs/implementation.md` 为准。

本文中的检查分为三类：

- 当前 CI 覆盖：已经由 GitHub Actions 执行，可作为 required checks 候选。
- PR 前建议：本地应按改动范围尽量执行，并在 PR body 写明结果。
- 缺口：当前缺少稳定环境或脚本，不能写成 required，落地后再升级。

## 总体原则

- 改什么跑什么，但跨契约、跨服务或共享文档变更要扩大检查范围。
- OpenAPI 是协作源；改 Gateway active API 时必须跑契约校验和前端类型同步检查。
- 数据库 migration 必须能从空库 apply。
- env-gated integration tests 默认可能跳过；如果本次改动触碰 repository、SQL 或 migration，应尽量提供本地数据库执行记录。
- 测试组 `T-*` 任务必须实际运行测试并留下可复核证据；纯单元/组件自动化或静态检查可在 issue/PR 中保留轻量执行记录，集成、E2E、权限/安全边界、文件/Knowledge runtime 边界、migration、环境验收、人工验收、回归或缺陷复现必须按 `docs/testing/templates/test-report-template.md` 生成完整报告并归档到 `docs/testing/reports/YYYY-MM-DD/`。
- 当前有前端 Playwright 基础 smoke，但没有后端跨服务完整 E2E smoke；不要用单服务测试或前端 mock E2E 替代跨服务验收。
- 旧 `services/parser` 已退役；PDF 解析、切块、embedding、索引和检索由 Knowledge 的 RAGFlow runtime API/worker 链路覆盖。相关变更优先运行 Knowledge、knowledge-runtime、Docker policy、Compose config 和真实 PDF E2E。
- 当前有前端 Playwright 基础 smoke 和 #125 后端/跨服务 smoke slices，但没有完整前端到后端 E2E smoke；不要用单服务测试、局部 smoke 或前端 mock E2E 替代完整跨服务验收。
- open PR、未合入 issue 和草案不能写成当前 `develop` 已实现；测试记录也不能把未稳定依赖的检查写成 required。

## 自动化测试分层

自动化测试按“本地先发现、CI 再兜底”的思路分层。新增测试或调整触发范围时，应先明确它属于本地自动化、CI 自动化还是显式 opt-in smoke，避免把需要外部依赖或长时间运行的检查直接放进默认路径。

### 本地自动化

本地自动化用于开发者在 PR 前快速验证改动，优先覆盖确定性强、依赖少、失败后容易定位的问题。前端以 typecheck、lint、format、build、Vitest/React Testing Library 和必要的 Playwright smoke 为主；后端以服务内 `go test ./...`、handler/service unit tests、fake dependency tests、OpenAPI/active route contract checks 和必要的 env-gated repository tests 为主。需要数据库、Redis、Elasticsearch/runtime doc engine、MinIO、Knowledge RAGFlow runtime 或真实模型 provider 的检查可以作为本地命令记录，但必须写清楚依赖环境、跳过条件和残余风险。

### CI 自动化

CI 自动化用于保护 `develop`，只放入可以在 GitHub Actions 中稳定重现、耗时可控、依赖可准备的检查。路径过滤可以减少无关服务运行，但不能降低受影响模块的最低验证要求。CI 中的 mock、fake backend 或轻量服务容器只证明该层级契约稳定，不等价于完整跨服务验收；如果 CI 只跑 fake dependency，PR 说明中不能写成真实依赖已经验证。

### 触发原则

触发范围以改动影响面为准：改文档只需要文档一致性和 `git diff --check`；改前端页面或 API client 需要前端 check/build/unit，触碰关键流程时再加 E2E smoke；改 Gateway OpenAPI、owner map 或 route 时需要契约校验和前端类型生成检查；改后端 service、repository、SQL 或 migration 时需要对应服务测试，并按风险补充 repository 或 migration apply；改 Docker infra Compose、基础设施镜像源、runtime dependency 或 CI 配置时，需要同步 runbook/技术决策文档并运行对应 policy/config 检查。跨契约、跨服务或共享能力变更要扩大验证范围，不能只跑离改动最近的一层。

### 暂不纳入默认自动化的内容

真实外部 provider 调用、完整后端跨服务 E2E、大型文档解析质量评测、需要人工凭证或长期运行环境的检查，暂不纳入默认自动化。当前阶段也不引入 Testcontainers for Go 作为默认后端集成测试工具；后端集成测试继续优先使用显式 env-gated 数据库/服务地址和 CI 中已配置的轻量服务容器。若后续要升级为默认工具链，应先更新技术决策、测试策略、CI 资源预算和服务 runbook，再进入实现任务。

## 当前 CI 覆盖

| Workflow | 覆盖 | 当前说明 |
| --- | --- | --- |
| `frontend.yml` | `apps/web/**`、根前端依赖文件和 workflow | 执行 `bun install --frozen-lockfile`、`bun run --cwd apps/web check`、`build`、`test:unit` 和 Playwright E2E smoke。 |
| `go-services.yml` | `services/{ai-gateway,auth,document,file,gateway,knowledge,qa}` | 根据变更路径只选择受影响服务，执行 `go test ./...`、`go build ./cmd/server`；QA 额外 build `./cmd/agent`；Knowledge 或 workflow 变更时额外用 PostgreSQL 16 和 `KNOWLEDGE_TEST_DATABASE_URL` 执行 repository lifecycle integration test。 |
| `go-migrations.yml` | 有 SQL migration 的后端服务 | 校验 migration 文件名并用 `goose@v3.27.1` 对 PostgreSQL 16 apply。 |
| `docker-deploy-checks.yml` | `deploy/docker-compose.yml`、Docker infra runbook/spec、Docker policy/environment scripts | 执行 `python3 scripts/check_docker_policy.py`、Docker policy/environment 单元测试、`check_docker_environment.py --skip-network --clean-env`，并对根级 infra-only Compose 执行 `docker compose ... config --quiet`；不处理业务服务容器。 |
| `gateway-contract.yml` | Gateway OpenAPI active API | 执行 verifier unit tests 和 `python3 scripts/verify_gateway_active_api.py`。 |
| `check-api-types.yml` | 前端 Gateway 类型漂移 | 在 `apps/web` 执行 `bun run api:generate`，本地等价命令为 `bun run --cwd apps/web api:generate`，并要求 generated diff 干净。 |
| `commitlint.yml` / `pr-guard.yml` | 协作规则 | 检查提交格式、PR body、issue 关联和 base 更新要求。 |

所有 GitHub Actions workflow 都应显式声明最小 `permissions`。只读取仓库内容的校验类 workflow，例如 API type drift check，应使用 `contents: read`，不得依赖默认 token 权限。
当前可作为 required checks 的优先候选是 Frontend、Go service tests、goose migration apply、Docker/Compose config、Gateway contract/API drift 和 API type drift。完整 DB integration jobs 和后端跨服务 E2E smoke 仍未落地；在 CI 提供稳定依赖前只能作为 PR 前建议或缺口登记。

## 本地命令矩阵

| 改动范围 | 必跑命令 |
| --- | --- |
| 文档 | `git diff --check`；检查新增链接和实现事实。 |
| Gateway OpenAPI / owner map | `python3 -m unittest scripts.tests.test_verify_gateway_active_api`；`python3 scripts/verify_gateway_active_api.py`。 |
| Gateway QA active path schema contract | `cd services/gateway && go test ./internal/http -run QA`；覆盖 29 个 QA-owned active paths、OpenAPI schema/auth/content type、session attachments、settings `systemPrompt` contract、QA internal `$ref` drift 和 proxy namespace/query 映射。 |
| 前端 | `bun install --frozen-lockfile`；`bun run --cwd apps/web check`；`bun run --cwd apps/web build`；`bun run --cwd apps/web test:unit`；关键页面改动再跑 `bun run --cwd apps/web test:e2e`。 |
| 前端 API 类型 | `bun run --cwd apps/web api:generate`；确认 generated diff 符合预期。 |
| 单个 Go 服务 | `cd services/<service> && go test ./...`；`go build ./cmd/server`。 |
| QA 服务 | `cd services/qa && go test ./...`；`go build ./cmd/server`；`go build ./cmd/agent`。 |
| Docker policy | `python3 scripts/check_docker_policy.py`；验证根级 Compose 默认服务只包含 `postgres`、`redis`、`minio`、`minio-init`、`elasticsearch`；根 Compose 不包含 `build:`，基础设施镜像默认值不使用 `latest`，`deploy/.env.example` 不启用第三方镜像源。 |
| 本地启动脚本 / 文档 | `bash -n scripts/local/dev-up.sh scripts/local/run-backend.sh scripts/local/stop-backend.sh`；`python3 scripts/verify_local_seed_contract.py`；确认 README/deploy/runbook 第一屏仍是 `cp deploy/.env.example deploy/.env`、`./scripts/local/dev-up.sh`、`./scripts/local/run-backend.sh`、`cd apps/web && bun install && bun run dev`，默认源保持官方，`--china` 明确覆盖 Docker/Go/uv/runtime artifact 下载且不改写 `deploy/.env`，本地入口脚本都有开始/成功/失败命令行摘要，`dev-up.sh` 在 migration/seed 前等待 infra health、检查 Go module 配置，`run-backend.sh` 预检 Go module 下载并汇总早退服务日志，Go module proxy 排障说明区分 `GOPROXY`、Docker registry rewrite、GitHub release/raw 镜像和 `UV_DEFAULT_INDEX`，AI Gateway 本地 seed 使用 `localhost:11434`，stop 脚本按进程组停止 host-run 服务。 |
| Docker environment | `python3 scripts/check_docker_environment.py --profile all --clean-env`；用于区分 registry rewrite、daemon mirror、Docker Hub direct 和 shell proxy 的问题。CI 只跑 `--skip-network`，真实 manifest 探测作为本地/排障检查。 |
| Compose | `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`；默认服务清单只能是 `postgres`、`redis`、`minio`、`minio-init`、`elasticsearch`。 |
| Knowledge repository / SQL | `cd services/knowledge && KNOWLEDGE_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./internal/repository -count=1`。 |
| Knowledge ingestion real deps | 启动 PostgreSQL/Knowledge runtime/Redis/MinIO/Elasticsearch 后执行 `cd services/knowledge && KNOWLEDGE_INGESTION_SMOKE=1 ... go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v`；默认无 env 时跳过，不进入普通 CI required check。当前 Knowledge 主路径不依赖 File Service。 |
| Gateway -> Knowledge owner route | 启动 Gateway/Auth/Redis/Knowledge/PostgreSQL 和 Knowledge runtime 后执行 `cd services/knowledge && GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayKnowledgeOwnerRouteSmoke$' -count=1 -v`；默认无 env 时跳过，不进入普通 CI required check。 |
| Auth/Gateway/Redis full smoke | `bash scripts/run_issue_352_smoke.sh`；脚本只从 Compose 启动 `postgres` 和 `redis`，随后在宿主机执行 Auth migration、启动 Auth/Gateway、创建用户、登录、`/users/me`、登出、Redis 脱敏检查和 fake owner header capture。 |
| Knowledge RAGFlow runtime / PDF E2E | 启动宿主机 Knowledge runtime API/worker 和 host-run Knowledge adapter 后，使用真实 PDF 走上传、解析、切块、索引、检索链路；默认不进入普通 CI required check。 |
| migration | `go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$DATABASE_URL" up`。 |
| Knowledge runtime route / contract | `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio python -m pytest <targeted tests> -q`；配合 `cd services/knowledge && go test ./...` 和 `go build ./cmd/adapter`。 |
| AI Gateway provider adapter | `cd services/ai-gateway && go test ./...`；尽量加 fake provider case 和真实 provider smoke 记录。 |
| Document worker/job | `cd services/document && go test ./...`；如改 repository，设置 `DOCUMENT_TEST_DATABASE_URL` 跑 repository integration tests。 |
| Code Scanning / 安全告警 | 按告警影响范围运行对应服务全量 `go test ./...` 和 `go build ./cmd/server`；QA 额外 `go build ./cmd/agent`。PR body 必须列出 alert 编号、规则 ID、文件位置、验证命令和剩余风险。命令执行、URL trust、allocation upper bound、integer conversion、credential hashing 和 workflow permissions 都需要有 focused unit tests 或静态验证记录。 |

## 后端测试层级

| 层级 | 当前做法 | 适用场景 |
| --- | --- | --- |
| Unit tests | Go `testing`、fake repository、fake provider、httptest。 | service rules、handler validation、脱敏、错误归一化。 |
| Repository tests | 部分服务有 SQL/repository tests；Knowledge/QA/Document 有 env-gated PostgreSQL integration tests；Knowledge repository lifecycle 已接入 CI PostgreSQL job。 | repository、SQL、transaction、migration 相关改动。 |
| Migration apply | CI 使用 PostgreSQL 16 和 goose apply。 | 新增或修改 migration。 |
| Contract tests | Gateway active API verifier、route coverage tests、QA active path schema contract tests。 | OpenAPI、owner map、active path、RESTful path、owner/auth/schema/content type 和 QA internal `$ref` drift。 |
| Auth/Gateway 用户管理测试 | Auth service tests、Gateway auth/admin route tests、Gateway active API verifier、前端 route guard 和表单测试。 | 自助注册兼容、管理员管理范围、profile 自助编辑、必需改密、密码策略、会话刷新和 stale authorization 防回归。 |
| Knowledge runtime tests | Route allowlist、adapter/runtime contract、配置加载、service token 和 clean DB provisioning 单测。 | Knowledge RAGFlow runtime API/worker 或 adapter contract 变更；真实 provider、OCR 质量和部署资源需要具备环境后单独记录。 |
| Knowledge ingestion real deps smoke | `KNOWLEDGE_INGESTION_SMOKE=1` 显式启用；使用真实 Knowledge runtime、PostgreSQL、Redis、MinIO 和 Elasticsearch。 | 验证 Knowledge 上传 fixture、runtime 解析/切片/embedding/索引、状态更新和检索前置数据；不替代 retrieval/rerank/MCP/Gateway 总入口。 |
| Gateway -> Knowledge owner route smoke | `GATEWAY_KNOWLEDGE_OWNER_SMOKE=1` 显式启用；使用真实 Gateway/Auth/session cache、Knowledge、PostgreSQL、Redis 和 Knowledge runtime。 | 先验证无 Bearer token 的伪造 `X-User-*` 请求被 Gateway 拒绝，再通过 Gateway 创建/读取 KB 并断言 `createdBy` 是真实 session user；不替代完整 Gateway route matrix。 |
| Auth/Gateway/Redis full smoke | `AUTH_GATEWAY_REDIS_FULL_SMOKE=1` 显式启用；`scripts/run_issue_352_smoke.sh` 准备 PostgreSQL/Redis infra 并在宿主机启动 Auth/Gateway。 | 验证 Auth migration apply、Gateway 创建用户/登录/当前用户/登出、Redis session key/value/TTL 脱敏，以及 fake owner 捕获 Gateway 注入认证上下文 header。 |
| Cross-service smoke | 已有 `scripts/run_issue_125_smoke.sh` 汇总入口；真实依赖仍按 slice 显式启用。 | Auth -> Gateway -> Domain、Document -> File/AI Gateway、QA -> Knowledge/AI Gateway 等链路。 |
| Knowledge PDF E2E | 显式启用；使用真实 Knowledge adapter、RAGFlow runtime API/worker、PostgreSQL、MinIO、Elasticsearch/向量索引和 provider credential。 | 验证真实 PDF 上传、解析、切块、embedding、索引和检索结果；不替代完整 Gateway/MCP/QA 总入口。 |
| Issue #125 smoke slices | `bash scripts/run_issue_125_smoke.sh --list` / `--all` | 汇总 Auth/Gateway/Redis、File owner、QA RAG、Document REST 和 Document MCP slices；仍是显式 opt-in smoke，不等同于完整前端 E2E 或真实 provider 验收。 |

env-gated repository tests：

```bash
cd services/qa
QA_TEST_DATABASE_URL='postgres://qa_app:qa_app_dev@localhost:5433/qa_system?sslmode=disable' go test ./internal/repository

cd services/document
DOCUMENT_TEST_DATABASE_URL='postgres://document_app:document_app_dev@localhost:5435/document_system?sslmode=disable' go test ./internal/repository

cd services/knowledge
KNOWLEDGE_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./internal/repository -count=1
```

Knowledge RAGFlow runtime PDF E2E is a manual or env-gated verification path:
start the host-run Knowledge runtime API/worker, run the Knowledge adapter
with `VENDOR_RUNTIME_URL` pointing at the reachable runtime API, upload
`DL_T_673-1999.pdf`, wait for document status `ready`, assert non-zero chunks,
then run `knowledge-queries` and assert retrieval hits. If this check is skipped
or cannot run, PR verification must state the missing dependency and residual
risk.

Before running Gateway-level smoke, start infra from `deploy/` and start Auth,
File, Knowledge, QA, AI Gateway, Gateway, and the host-run Knowledge RAGFlow
runtime required by the scenario. The old standalone Parser service is not part
of the current startup path.

env-gated Auth/Gateway/Redis full smoke for #352:

```bash
bash scripts/run_issue_352_smoke.sh
```

The script prints an `AUTH_GATEWAY_REDIS_FULL_SMOKE_RESULT pass ...` line when
successful. It exits with a `blocked:` message when Docker is unavailable,
PostgreSQL/Redis cannot become healthy, Auth migration apply fails, Go is
missing, or host-run Auth/Gateway startup cannot complete.

CI support is optional, not required: `.github/workflows/auth-gateway-redis-smoke.yml`
is a manual `workflow_dispatch` job that runs the same script on demand.
Default PR CI still runs the smoke package in skip/compile mode when
`services/deploy/smoke/**` changes; the real
`AUTH_GATEWAY_REDIS_FULL_SMOKE=1` path is intentionally not a required PR check
because it starts Docker infrastructure and host-run service processes.

QA 快速契约和安全边界测试使用 fake repository / fake runner，不依赖 PostgreSQL 或真实模型 provider：

```bash
cd services/gateway
go test ./internal/http -run QA

cd services/qa
go test ./internal/service -run 'AskSSEPayloads|AskToolProgress|AskPersistsCitation|NormalizeCitation|PreservesGatewayValidation'
```

env-gated AI Gateway real provider smoke:

```bash
cd services/ai-gateway
AI_GATEWAY_REAL_PROVIDER_SMOKE=1 \
AI_GATEWAY_REAL_PROVIDER_BASE_URL='https://api.example.com/v1' \
AI_GATEWAY_REAL_PROVIDER_API_KEY="$PROVIDER_API_KEY" \
AI_GATEWAY_REAL_CHAT_MODEL='provider-chat-model' \
go test ./internal/http -run '^TestRealProviderSmoke_ExplicitEnvOnly$' -count=1 -v
```

Set `AI_GATEWAY_REAL_EMBEDDING_MODEL`,
`AI_GATEWAY_REAL_EMBEDDING_DIMENSIONS`, and/or `AI_GATEWAY_REAL_RERANK_MODEL`
only when the selected provider supports those operations. With the gate unset,
ordinary `go test ./...` must skip the external provider path. With the gate
enabled, failures must report request IDs and key names only, not secret values,
prompts, document text, embedding payloads, or provider raw bodies.

## 前端测试层级

| 层级 | 当前状态 | 说明 |
| --- | --- | --- |
| Type check | 已落地 | `bun run --cwd apps/web typecheck`。 |
| Lint | 已落地 | `bun run --cwd apps/web lint`。 |
| Format check | 已落地 | `bun run --cwd apps/web format:check`。 |
| Build | 已落地 | `bun run --cwd apps/web build`。 |
| API type generation | 已落地 | `bun run --cwd apps/web api:generate`，以 Gateway OpenAPI 为源。 |
| Component/unit tests | 已落地 | `bun run --cwd apps/web test:unit`，使用 Vitest + React Testing Library。 |
| Browser/E2E tests | 已落地 / smoke | `bun run --cwd apps/web test:e2e`，使用 Playwright 覆盖基础应用 smoke；完整业务 E2E 仍需随页面能力扩展。 |

前端不得直接调用服务内部地址。涉及 QA SSE、上传、报告任务进度、admin model/parser configuration、admin users、profile 或 required password-change 的改动，应同时检查 `docs/architecture/frontend-backend-contract.md` 和 Gateway OpenAPI。

## 契约和文档检查

| 检查 | 触发条件 |
| --- | --- |
| Gateway active API verifier | 改 `docs/services/gateway/api/public.openapi.yaml`、owner map、Gateway route 或前端 API 生成规则。 |
| 服务 implementation 文档 | 改服务能力、stub/501 状态、runtime dependency、migration、worker 或 provider adapter。 |
| 技术选型基线 | 引入新运行时依赖、镜像、CLI、SDK、队列、数据库或工具链。 |
| 本地联调手册 | 新增 Compose、env template、seed data、跨服务 smoke 或端口约定。 |
| Knowledge runtime 契约一致性 | 改 `services/knowledge-runtime/**`、Knowledge adapter runtime client、parser config 到 RAGFlow config 映射、host-run runtime API/worker。 |
| 测试策略 | 新增 CI workflow、测试框架、E2E smoke、路径过滤规则或 required check。 |

文档同步检查：

| 改动类型 | 必须同步考虑 |
| --- | --- |
| 服务能力、stub/501 状态、worker、provider adapter 或 migration 变化 | 对应服务 `docs/implementation.md`。 |
| OpenAPI / Gateway active path / 数据模型变化 | OpenAPI、owner map、README、service boundaries 或相关契约文档；契约语义变化需先交管理组决策。 |
| runtime dependency / Compose / CI 变化 | `technology-decisions.md`、runbook 或本文。 |
| Docker infra Compose、基础设施镜像、Docker daemon mirror、registry rewrite 变化 | `docs/runbooks/docker-image-pull-environment.md`、`deploy/README.md`、`deploy/.env.example`、`docs/architecture/technology-decisions.md`、`scripts/check_docker_policy.py`、`scripts/check_docker_environment.py` 和相关 Trellis spec；Compose 基础镜像覆盖变量必须保持 pinned 默认，不得把正常路径改成 `latest`。 |
| Knowledge RAGFlow runtime、Python packaging 或 HTTP tests 变化 | Knowledge README、`technology-decisions.md`、runbook 和本文。 |
| open PR 或未合入能力 | 只能写 pending、待合入或 follow-up，不得写成已实现。 |

## 跨服务 smoke 目标

当前已有 `scripts/run_issue_125_smoke.sh` 作为 #125 smoke slices 的汇总入口，但它仍按显式 opt-in 运行，并按 slice 报告跳过、阻断或通过状态。后续升级为完整一键前端到后端 E2E 前，至少还应覆盖：

1. Auth 创建会话，Gateway 写入 Redis session cache。
2. Gateway 使用认证上下文代理一个 Knowledge/QA/Document active path。
3. File 保存并读取一个基础 file object，业务服务响应不泄露 object key。
4. Knowledge ingestion 真实依赖 smoke 已验证一个 fixture 文档从 Knowledge adapter -> Knowledge runtime -> indexing；Gateway -> Knowledge owner route smoke 已验证 Auth/Gateway session 到 Knowledge owner route 的最小上下文注入；后续统一 E2E 应复用这些信号并补完整 Gateway/MCP/业务断言。
5. AI Gateway 创建 chat、embedding、rerank profile，并通过 fake provider 完成三类调用。
6. QA 创建 session/message，非流式和 SSE 路径都能保存 response run 和事件摘要。
7. Document 创建 report/job，worker 推进 attempt/event；真实生成落地后再验证 AI Gateway 和 File Service。
8. 前端 typed client 能在 Gateway OpenAPI 更新后重新生成并通过 check/build。

## 测试报告归档

测试证据是测试任务的必交付物，不是 PR body 中一句“已测试”的替代品。`T-*` 测试任务完成时应按测试类型分层记录：

- 纯单元测试、组件测试和静态检查默认并入自动化；在 issue/PR 中记录命令、环境、结果、失败证据、未运行原因和缺陷处理即可。
- 集成测试、E2E、权限/安全边界、文件/Knowledge runtime 边界、migration、环境验收、人工验收、回归测试或缺陷复现必须复制 `docs/testing/templates/test-report-template.md` 生成完整报告。
- 完整报告保存到 `docs/testing/reports/YYYY-MM-DD/`，日期使用实际执行日期。
- 在测试 issue 和 PR 中链接报告路径或轻量执行记录。

旧的 `docs/tests/` 目录不再新增报告；历史报告已迁移到 `docs/testing/reports/`。

## PR 记录要求

PR body 的检查部分要写具体命令和结果。示例：

```text
已运行：
- git diff --check
- cd services/ai-gateway && go test ./...
- python3 scripts/verify_gateway_active_api.py

未运行：
- DOCUMENT_TEST_DATABASE_URL integration tests；原因：本次未改 document SQL/repository，且本地未启动 document PostgreSQL。
```

如果只写“已测试”而没有命令，reviewer 无法判断覆盖范围。对于因为环境缺失而未运行的检查，应写明缺什么环境和残余风险。

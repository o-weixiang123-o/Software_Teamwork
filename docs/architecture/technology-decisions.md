# 技术选型基线

本文记录当前团队确认的工程技术选型、版本固定状态和落地约束。后续服务实现、接口文档、CI 和部署文档应以本文为基线；如需偏离，必须在对应服务 README 说明原因并同步更新本文。

## 总体原则

- 后端服务继续采用独立 Go module 的微服务形态。
- 前端继续采用 `apps/web` 下的 Bun + Vite + React + TypeScript 应用。
- 服务间通信以 RESTful HTTP API 为主，公开接口以 gateway OpenAPI 为权威。
- 技术选型优先选择团队可维护、容易在 CI 中验证、和当前代码形态一致的方案。
- 已经写入 `package.json`、`bun.lock`、`go.mod`、Compose 或 GitHub Actions 的版本视为当前仓库版本。
- 本地开发 Compose 中的共享基础设施镜像必须使用明确 tag；新增或升级镜像时必须同步本文和对应 Compose/README。

## 版本标注规则

| 标注 | 含义 |
| --- | --- |
| 已固定 | 版本已经在仓库配置或锁文件中固定，新代码默认沿用该版本或同一大版本。 |
| 已选型，待固定 | 技术路线已确认，但仓库尚未引入依赖、CLI 或镜像版本；首次落地时必须写入明确版本并更新本文。 |
| 标准库 / 协议 | 由 Go 标准库、Web 标准、HTTP/OpenAPI 协议或团队契约定义，不存在独立第三方依赖版本。 |
| Compose 镜像 tag | 本地 infra Compose 使用的镜像 tag，必须明确记录并与 Compose 文件一致。 |

## 服务文档使用方式

本文是项目技术选型的权威位置。`docs/services/<service>/README.md` 不应复制完整技术栈表；服务文档只记录：

- 本服务是否适用某项通用选型，例如是否拥有 PostgreSQL、Redis/asynq、MinIO、runtime doc engine 或模型调用。
- 本服务的服务内目录、迁移、任务、日志字段或测试重点。
- 与本文不同的明确偏离原因；偏离必须同时更新本文和对应服务文档。

如果只是说明 `pgx` + `sqlc`、`goose`、`net/http` / `ServeMux`、`slog`、`envconfig` 风格配置、统一测试工具或前端 OpenAPI 类型生成，应在服务文档中链接本文，不重复描述。

## 当前仓库状态

| 模块 | 当前状态 | 版本来源 |
| --- | --- | --- |
| `apps/web` | 已落地前端应用，使用 Bun workspace、Vite、React、TypeScript、Tailwind 和 ESLint Flat Config。 | 根 `package.json`、`apps/web/package.json`、`bun.lock` |
| `services/gateway` | 已落地 Go gateway、auth public routes、Redis session cache、active proxy route matrix 和边缘中间件；Knowledge active routes、QA session attachments、Auth profile/password-change/admin-users 和 admin parser configs 已转 owner proxy；admin overview/metrics 是 active contract，但当前 Gateway route 仍按稳定 `not_implemented` 占位。 | `services/gateway/go.mod`、`services/gateway/internal/http/routes.go`、`docs/services/gateway/docs/implementation.md` |
| `services/auth` | 已落地 Go auth 服务、PostgreSQL repository、用户/会话内部 API、argon2id、token hash 和 migration。 | `services/auth/go.mod`、`services/auth/migrations/`、`docs/services/auth/docs/implementation.md` |
| `services/file` | 已落地 Go file 服务、基础 `/internal/v1/files/**` API、memory/local/MinIO object store、file_objects migration、PostgreSQL metadata runtime 和 service-token 校验；`FILE_DATABASE_URL` 为空时仍保留 memory metadata 模式。 | `services/file/go.mod`、`services/file/migrations/`、`docs/services/file/docs/implementation.md` |
| `services/knowledge` | 已落地 Go knowledge adapter 服务，通过 `services/knowledge-runtime` 的 RAGFlow runtime API/worker 完成知识库 CRUD、文档上传、解析、切块、embedding、索引和检索；旧 `services/parser` 已退役。 | `services/knowledge/go.mod`、`services/knowledge-runtime/`、`services/knowledge/migrations/`、`docs/services/knowledge/docs/implementation.md` |
| `services/qa` | 已落地 Go QA 服务、PostgreSQL repository、会话/消息/SSE、session attachments、版本化 `systemPrompt` 配置契约、引用、工具/MCP/model client 基础、QA -> AI Gateway、Gateway -> Knowledge -> QA RAG 和 QA -> Document MCP env-gated smoke；完整 #125/前端端到端与真实 provider 证据仍待补齐。 | `services/qa/go.mod`、`services/qa/migrations/`、`docs/services/qa/docs/implementation.md` |
| `services/document` | 已落地 Go document 服务、PostgreSQL repository、模板/材料/报告/大纲/章节 API、report jobs/attempts/events、report files、statistics、settings、asynq worker 状态机、`summer_peak_inspection` 基础 AI 大纲/正文生成编排、服务内 Document MCP 工具适配层和 Streamable HTTP MCP server；更多报告类型生成策略、共享环境完整 MCP/Gateway smoke 和 Pandoc/LibreOffice 富 DOCX 工具链仍未落地。 | `services/document/go.mod`、`services/document/migrations/`、`docs/services/document/docs/implementation.md` |
| `services/ai-gateway` | 已落地 Go AI Gateway、PostgreSQL repository、model profile CRUD、credential encryption、service-token auth、OpenAI-compatible chat completions、embeddings、rerankings、provider invocation 记录和 usage aggregate；真实 provider/跨服务 smoke 仍待补齐。 | `services/ai-gateway/go.mod`、`services/ai-gateway/migrations/`、`docs/services/ai-gateway/docs/implementation.md` |
| CI | 已有 PR guard、commitlint、auto-label、前端 check/build/unit/E2E smoke workflow、Go service path-filtered build/test workflow、goose migration apply workflow、Docker/Compose config 检查、Gateway contract workflow 和 API type drift workflow。 | `.github/workflows/*.yml` |

## 当前事实与目标基线

本文同时记录当前仓库事实和目标技术基线。已经写入仓库配置或锁文件的版本先按当前事实记录；如果与目标基线不一致，应优先开实现 issue 让代码向目标基线对齐，而不是在实现 PR 中临时改契约或让文档长期迁就漂移。

当前已知需要对齐的差异：

| 领域 | 当前仓库事实 | 目标基线 / 后续动作 |
| --- | --- | --- |
| PostgreSQL client | Auth、Knowledge、QA、Document、File、AI Gateway 均已升级至 `pgx/v5@v5.9.2`（S-025 安全升级）。 | 新增 PostgreSQL 服务沿用 `pgx/v5@v5.9.2`，不得重新引入 `pgx/v4` 或第三种 major 版本。 |
| Redis client | `go-redis/v9@v9.21.0` 是直接 Redis client 固定基线。Document 当前被 `asynq v0.26.0` 间接带入 `go-redis/v9@v9.14.1`，这是实现与基线出入，不作为目标版本。Knowledge Go adapter 当前不依赖 go-redis；Redis 只在 RAGFlow runtime 内部使用。 | 新增直接 Redis 依赖沿用 `v9.21.0`；后续升级 asynq 时优先消除传递依赖版本出入，不能消除时必须在服务 implementation 文档登记原因。 |
| asynq | Document 已接入 `asynq v0.26.0`；Knowledge Go adapter 已切到 RAGFlow runtime，不再接入 asynq client/worker。 | 技术表和三选一记录统一标为已固定；新增异步服务按需复用该版本或显式决策升级。 |
| File object store | Runtime 已有 memory/local/MinIO object store；根目录本地 Compose 已固定 MinIO server/mc 镜像并初始化本地 bucket。 | File 仍是 MinIO 对象存储边界；MinIO server 与 `mc` 是不同镜像，分别记录固定 tag；SDK 和本地 server/client 镜像版本需保持同步记录。 |
| 前端 OpenAPI 类型生成 | `openapi-typescript@7.13.0` 已进入 `apps/web/package.json` 和 `bun.lock`。 | API type drift check 持续约束 generated diff；升级版本需同步本文。 |

## 已确认选型总览

| 领域 | 选型 | 当前版本 | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| Monorepo 包管理 | Bun workspace | `bun@1.3.12` | 已固定 | 根目录 `packageManager` 固定；`bun.lock` 为唯一前端锁文件。 |
| 前端框架 | React + React DOM | `19.2.7` | 已固定 | `apps/web` 使用 React 19。 |
| 前端语言 | TypeScript | `6.0.3` | 已固定 | 以 `bun.lock` 解析版本为准。 |
| 前端构建 | Vite | `8.1.0` | 已固定 | 使用 `@vitejs/plugin-react` 和 `@tailwindcss/vite`。 |
| 前端样式 | Tailwind CSS | `4.3.1` | 已固定 | 配套 `@tailwindcss/vite@4.3.1`。 |
| 前端路由 | TanStack Router | `@tanstack/react-router@1.170.16` | 已固定 | `apps/web/src/app/router.tsx` 负责路由组合。 |
| 前端服务状态 | TanStack Query | `@tanstack/react-query@5.101.1` | 已固定 | 用于服务端状态、缓存和 mutation。 |
| 前端客户端状态 | Zustand | `5.0.14` | 已固定 | 用于主题、认证、UI、聊天等本地状态。 |
| 前端运行时校验 | Zod | `4.4.3` | 已固定 | 用于 schema 与输入校验。 |
| 前端 UI 基础 | Base UI + Radix primitives | Base UI `1.6.0`；Radix 见前端明细 | 已固定 | `components.json` 使用 `base-nova`、Lucide icons、Tailwind CSS variables。 |
| 前端图标 | Lucide React | `1.21.0` | 已固定 | `components.json` 的 `iconLibrary` 为 `lucide`。 |
| 前端 Markdown | react-markdown | `10.1.0` | 已固定 | QA 聊天消息渲染使用。 |
| 前端 class 工具 | `clsx` + `class-variance-authority` | `clsx@2.1.1`，`cva@0.7.1` | 已固定 | UI variant 和 class 合并基础。 |
| 前端 API 类型 | `openapi-typescript` + typed fetch wrapper | `openapi-typescript@7.13.0` | 已固定 | `api:generate` 已进入 `apps/web/package.json`；生成目录为 `apps/web/src/api/generated/`。 |
| 前端 SSE | `fetch` stream wrapper | Web 标准 | 标准库 / 协议 | QA 消息创建使用 POST + `text/event-stream`，支持 `AbortController`。 |
| 前端测试 | Vitest + React Testing Library + Playwright | Vitest `4.1.9`；Testing Library 见前端明细；Playwright `1.61.1` | 已固定 | 已加入 `apps/web/package.json` 和 `.github/workflows/frontend.yml`。 |
| 前端代码质量 | ESLint Flat Config + Prettier | ESLint `9.39.4`，Prettier `3.9.0` | 已固定 | 插件版本见前端明细。 |
| Knowledge RAGFlow runtime | Python vendored runtime + worker | `services/knowledge-runtime` vendored snapshot | 已固定 | runtime API 通过宿主机 Python/uv 常驻启动，并使用 API-only dependency profile；worker 通过宿主机 Python/uv 或生产调度器运行，并使用 worker/full dependency profile。Knowledge adapter 在上传时调用 `/documents/parse` 入队；当 `KNOWLEDGE_RUNTIME_WORKER_START_COMMAND` 已配置且缺少 worker heartbeat 时，会先触发该受控启动入口，并等到 heartbeat 后再入队。本地 helper 会在队列空闲超时后关闭 worker；生产可用 KEDA 按 Redis Stream lag 从 0 扩容，或使用 systemd/supervisor 等入口。解析、切块、embedding、索引和检索支持均在该 runtime 内完成。 |
| 后端语言 | Go | `go 1.25` | 已固定 | 项目 Go 服务基线固定为 1.25；已落地服务 module 应保持一致。 |
| 后端 HTTP 路由 | Go `net/http` / `http.ServeMux` | Go `1.25` 标准库 | 已固定 | 不默认引入 `gin`/`chi`。 |
| 后端日志 | Go `log/slog` | Go `1.25` 标准库 | 已固定 | 生产默认 JSON 结构化日志。 |
| PostgreSQL 访问 | `pgx` + 手写 SQL 或 `sqlc` 形态 | `pgx/v5@v5.9.2`；sqlc CLI 推荐版本 `v1.31.1` | 已固定 | 已落地 PostgreSQL 服务统一使用 `pgx/v5@v5.9.2`。Auth、Document、File、AI Gateway 使用 sqlc；Knowledge 当前 parser-config repository 为手写 SQL；重生成任何服务的查询包时须使用 `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`。 |
| ORM | 不使用 ORM | N/A | 已固定 | 禁止默认引入 GORM/ent 等 ORM。 |
| 数据库迁移 | `goose` | `v3.27.1` | 已固定 | 使用 `pressly/goose` CLI 或库执行服务内 migration；该版本要求 Go 1.25+。 |
| 关系数据库 | PostgreSQL | `postgres:16-alpine` | 已固定 | 当前本地 Compose 固定在 16 Alpine。 |
| Redis 队列 | `asynq` over Redis | `asynq v0.26.0`；Redis `7-alpine` | 已固定 | Document 已接入 asynq client/worker；Knowledge Go adapter 不接入 asynq，RAGFlow runtime 内部使用 Redis/worker。后续异步 Go 服务按需复用该队列基线。 |
| Redis 缓存/会话 | `go-redis` | `go-redis/v9 v9.21.0` | 已固定 | Gateway 直接使用 `github.com/redis/go-redis/v9@v9.21.0`；Document 当前通过 asynq 间接带入的 `v9.14.1` 是实现出入，不作为目标版本。Knowledge Go adapter 当前不依赖 go-redis。 |
| 检索 / 索引后端 | Runtime doc engine / Elasticsearch | `docker.elastic.co/elasticsearch/elasticsearch:8.15.3` | 已固定 | 当前 Knowledge RAGFlow runtime 默认 doc engine 为 Elasticsearch/索引后端；根目录本地 Compose 默认启动 Elasticsearch 作为 active RAG infra。真实 runtime/query/provider smoke 仍需单独记录。 |
| 对象存储 | MinIO 边界；当前 memory/local/MinIO object store | `minio/minio:RELEASE.2025-09-07T16-13-09Z`；`minio/mc:RELEASE.2025-08-13T08-35-41Z` | 已固定 | File service runtime 已有 MinIO adapter；根目录本地 Compose 使用一个 MinIO server 和一个 `mc` bucket 初始化容器；`minio-init` 不是第二个 MinIO server。 |
| MinIO Go SDK | 官方 MinIO Go SDK | `github.com/minio/minio-go/v7@v7.2.1` | 已固定 | `services/file` 通过 `internal/platform/storage` 封装 SDK，不向 handler 或 owner service client 泄露 MinIO 细节。 |
| 认证 token | Opaque Bearer token | 协议契约 | 标准库 / 协议 | 不使用 JWT access token；服务端保存 token hash。 |
| 密码哈希 | `argon2id` | `argon2id-v1`，PHC `v=19` | 已固定 | 参数：`m=65536 KiB`、`t=3`、`p=2`、`salt=16 bytes`、`key=32 bytes`。 |
| Secret 管理 | 本地 env；生产 secret ref；第一阶段可加密列 | 加密实现待固定 | 已选型，待固定 | AI Gateway 不保存明文 provider API key。 |
| 模型调用 | AI Gateway 统一封装 OpenAI-compatible API | API 契约 `0.1.0`；provider/model 运行时配置 | 部分已落地 | chat completions、function calling 透传、embeddings 和 rerankings 已落地；真实 provider smoke 和下游接入仍待补齐。 |
| 本地 embedding | Knowledge runtime fallback / AI Gateway provider | 已落地 / 部分验证 | 已固定 | Knowledge RAGFlow runtime 可走本地 runtime fallback；AI Gateway embedding endpoint 已支持 OpenAI-compatible provider，真实 provider indexing smoke 仍需单独记录。 |
| 文档解析运行时 | RAGFlow runtime behind Knowledge adapter | 已落地 | 已固定 | `services/parser` 已退役；Knowledge Go 进程不承载 PaddleOCR/PaddlePaddle/OpenCV/CUDA 依赖，解析、切块、embedding、索引和检索支持由 `services/knowledge-runtime` 提供。 |
| OpenAPI | OpenAPI | `3.0.3` / `3.1.0`，以各契约文件 `openapi:` 头为准 | 已固定 | Gateway、Auth、File、AI Gateway 当前使用 3.0.3；Document、Knowledge、QA 的 internal/实现本地契约使用 3.1.0，服务级 public 设计面仍为 3.0.3。 |
| API 版本前缀 | `/api/v1` / `/internal/v1` | `v1` | 已固定 | 公开入口以 gateway OpenAPI 为准；内部服务使用服务级契约。 |
| 后端测试 | Go `testing` + `httptest` | Go `1.25` 标准库 | 已固定 | 默认不引入 BDD 测试框架。 |
| CI | GitHub Actions | `actions/github-script@v7`；runner `ubuntu-latest`；Bun `1.3.12`；Go `1.25.x` | 部分已固定 | 已有协作类 workflow、前端 check/build/unit/E2E smoke、Go service path-filtered build/test、goose migration apply、Docker/Compose config、Gateway contract 和 API type drift workflow。 |
| Docker 镜像与构建源策略 | 默认官方 pinned images；中国大陆显式 `--china` registry rewrite | `deploy/.env.example` 不默认启用第三方 registry；`./scripts/local/dev-up.sh --china` 在本次进程使用 DaoCloud rewrite，优先级为 `registry rewrite > daemon mirror > proxy`；Compose 基础镜像可用 `POSTGRES_IMAGE`、`REDIS_IMAGE`、`MINIO_IMAGE`、`MINIO_MC_IMAGE`、`KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE` 本地覆盖；`scripts/check_docker_policy.py` 做 CI 策略守门，`scripts/check_docker_environment.py` 做本机网络诊断 | 已固定 | 仓库 Docker 只跑基础设施；Knowledge RAGFlow runtime 走宿主机 Python/uv 启动；不得为提速默认关闭 Go checksum verification，不得把 Compose 镜像改成 `latest`，不得把业务服务放回 Docker 路径。 |
| 宿主机 Go 模块下载 | `GOPROXY` + `GOSUMDB` env | 默认 `GOPROXY=https://proxy.golang.org,direct`；`GOSUMDB=sum.golang.org`；`--china` 时临时使用 `https://goproxy.cn,direct` / `sum.golang.google.cn` | 已固定 | `deploy/.env.example` 默认写入官方 Go module proxy/checksum DB，用于 `dev-up.sh` 的 goose migration 以及 `run-backend.sh` 的 Go 服务 `go run`；中国大陆网络显式使用脚本 `--china`，这不是 Docker registry，也不是 Knowledge runtime uv 的 `UV_DEFAULT_INDEX`。 |
| 观测 | `slog` + Prometheus metrics；关键链路 OpenTelemetry tracing | `github.com/prometheus/client_golang@v1.23.2`；`go.opentelemetry.io/otel@v1.44.0`；`go.opentelemetry.io/otel/sdk@v1.44.0`；`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.44.0`；`go.opentelemetry.io/otel/exporters/prometheus@v0.66.0` | 已选型，待固定 | 第一阶段先保证结构化日志和低基数字段指标；首次落地 metrics/tracing 时必须写入对应服务 `go.mod` 并同步本文状态。 |
| DOCX 生成 | Document worker 当前使用内置 Go `SimpleDOCXGenerator`；Pandoc 作为富 DOCX 候选工具链；LibreOffice 暂不引入，保留后续候选 | 内置 Go 生成器：标准库；Pandoc CLI 版本后续在富 DOCX 任务中固定；LibreOffice：暂不引入 | 已固定（当前路径） | 当前路径为内置 Go `SimpleDOCXGenerator`；富 DOCX worker 的运行方式不属于当前本地 Docker 基线。 |
| MCP 集成 | 官方 MCP Go SDK；暂不拆独立 sidecar | `github.com/modelcontextprotocol/go-sdk@v1.6.1` | 已固定 | QA 负责工具白名单、权限、参数校验、超时和脱敏记录；Document 和 Knowledge 已有 Streamable HTTP MCP server/工具适配；SDK 升级或 sidecar 化另开兼容性任务。 |
| 本地基础设施 | Docker Compose | Compose 文件格式无 top-level version | 已固定 | 根 `deploy/docker-compose.yml` 只拉取 PostgreSQL、Redis、MinIO、`minio-init` 和 Elasticsearch；业务服务全部 host-run。 |

## 前端版本明细

| 类型 | 包 | 当前版本 |
| --- | --- | --- |
| Runtime | `react` | `19.2.7` |
| Runtime | `react-dom` | `19.2.7` |
| Runtime | `@tanstack/react-query` | `5.101.1` |
| Runtime | `@tanstack/react-router` | `1.170.16` |
| Runtime | `zustand` | `5.0.14` |
| Runtime | `zod` | `4.4.3` |
| Runtime | `react-markdown` | `10.1.0` |
| UI | `@base-ui/react` | `1.6.0` |
| UI | `@radix-ui/react-collapsible` | `1.1.14` |
| UI | `@radix-ui/react-dialog` | `1.1.17` |
| UI | `@radix-ui/react-hover-card` | `1.1.17` |
| UI | `@radix-ui/react-popover` | `1.1.17` |
| UI | `@radix-ui/react-scroll-area` | `1.2.12` |
| UI | `@radix-ui/react-slot` | `1.3.0` |
| UI | `lucide-react` | `1.21.0` |
| UI | `class-variance-authority` | `0.7.1` |
| UI | `clsx` | `2.1.1` |
| Style / Build | `tailwindcss` | `4.3.1` |
| Style / Build | `@tailwindcss/vite` | `4.3.1` |
| Build | `vite` | `8.1.0` |
| Build | `@vitejs/plugin-react` | `6.0.3` |
| Language | `typescript` | `6.0.3` |
| Types | `@types/node` | `26.0.1` |
| Types | `@types/react` | `19.2.17` |
| Types | `@types/react-dom` | `19.2.3` |
| Lint | `eslint` | `9.39.4` |
| Lint | `@eslint/js` | `9.39.4` |
| Lint | `typescript-eslint` | `8.62.0` |
| Lint | `eslint-config-prettier` | `10.1.8` |
| Lint | `eslint-plugin-jsx-a11y` | `6.10.2` |
| Lint | `eslint-plugin-react` | `7.37.5` |
| Lint | `eslint-plugin-react-hooks` | `7.1.1` |
| Lint | `eslint-plugin-simple-import-sort` | `13.0.0` |
| Format | `prettier` | `3.9.0` |

## 后端与基础设施版本明细

| 组件 | 当前版本 | 来源 | 备注 |
| --- | --- | --- | --- |
| Go toolchain | `1.25` | 技术选型基线 | Go 服务统一使用 1.25；`services/*/go.mod` 应保持一致。 |
| `github.com/jackc/pgx/v5` | `v5.9.2` | `services/auth/go.mod`、`services/knowledge/go.mod`、`services/qa/go.mod`、`services/document/go.mod`、`services/file/go.mod`、`services/ai-gateway/go.mod` | S-025 安全升级后全仓统一为 v5.9.2。 |
| `sqlc` CLI 推荐版本 | `v1.31.1` | `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` | 全仓统一推荐版本（S-033）。Auth/Document 已用 v1.31.1 生成；QA 存量生成包来自 v1.29.0，下次变更 QA SQL 时须用 v1.31.1 重新生成并提交。Knowledge 当前没有 `sqlc.yaml`，parser-config repository 使用手写 SQL。 |
| `github.com/pressly/goose/v3` | `v3.27.1` | 技术选型基线 | 迁移工具版本固定；可用 CLI 或库方式接入。 |
| `github.com/redis/go-redis/v9` | `v9.21.0` | `services/gateway/go.mod` | 直接 Redis client 固定基线；Document 当前由 asynq 间接带入的 `v9.14.1` 是实现出入，待后续 asynq/queue 依赖升级时消除。Knowledge Go adapter 当前不依赖 go-redis。 |
| PostgreSQL | `postgres:16-alpine` | `deploy/docker-compose.yml` | 本地开发数据库。 |
| Redis | `redis:7-alpine` | `deploy/docker-compose.yml` | 本地队列、缓存、短期协调依赖。 |
| Elasticsearch | `docker.elastic.co/elasticsearch/elasticsearch:8.15.3` | `deploy/docker-compose.yml` | Knowledge runtime active doc engine / 检索索引镜像。 |
| MinIO server | `minio/minio:RELEASE.2025-09-07T16-13-09Z` | `deploy/docker-compose.yml` | 根目录本地 Compose 对象存储服务端镜像。 |
| MinIO client (`mc`) | `minio/mc:RELEASE.2025-08-13T08-35-41Z` | `deploy/docker-compose.yml` | 根目录本地 Compose bucket 初始化镜像；`minio/mc:RELEASE.2025-09-07T16-13-09Z` 当前无 Docker Hub manifest，不能按 server tag 强行统一。 |
| Prometheus Go client | `github.com/prometheus/client_golang@v1.23.2` | 目标技术基线，尚未写入 `go.mod` | 新增 Go 服务暴露 Prometheus metrics 时默认沿用该版本，指标 label 不得包含用户输入、prompt、token、object key 或 API key 指纹。 |
| OpenTelemetry Go API | `go.opentelemetry.io/otel@v1.44.0` | 目标技术基线，尚未写入 `go.mod` | OpenTelemetry API/root module；新增 tracing 不应只引入该 module。 |
| OpenTelemetry Go SDK | `go.opentelemetry.io/otel/sdk@v1.44.0` | 目标技术基线，尚未写入 `go.mod` | 关键链路 tracing 使用 parent-based ratio sampler；dev/local 可配置为 100%，生产默认 1%。 |
| OpenTelemetry OTLP HTTP trace exporter | `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.44.0` | 目标技术基线，尚未写入 `go.mod` | 配置 exporter endpoint 时用于导出 traces；未配置 endpoint 时不导出 trace，避免无意外联。 |
| OpenTelemetry Prometheus exporter | `go.opentelemetry.io/otel/exporters/prometheus@v0.66.0` | 目标技术基线，尚未写入 `go.mod` | 用于 OTel metrics 与 Prometheus scrape 兼容；若服务只使用 Prometheus client，可暂不引入。 |
| MCP Go SDK | `github.com/modelcontextprotocol/go-sdk@v1.6.1` | `services/qa/go.mod`、`services/document/go.mod`、`services/knowledge/go.mod` | QA 当前直接作为 MCP Host/Client；Document 和 Knowledge 提供 Streamable HTTP MCP server/工具适配；暂不切换到独立 MCP sidecar。 |
| Document 内置 DOCX 生成器 | Go 标准库 `archive/zip` + XML | `services/document/internal/service/docx_generator.go` | 当前只覆盖基础 DOCX 包装和文本/表格扁平化导出，不等同于 Pandoc/LibreOffice 富文档转换。 |
| Pandoc CLI | 后续富 DOCX 任务固定 | N/A | 当前 Document 路径不依赖 Pandoc；如后续引入，必须作为 host-run 或独立工具链任务同步本文。 |
| LibreOffice headless | 暂不引入（C-011 决策） | N/A | Pandoc 满足首期富 DOCX 需求；LibreOffice 保留为后续候选，引入时须固定镜像 tag + digest，不能依赖运行环境自带 `soffice`。 |

## API 与契约版本

服务级机器可读契约在 `docs/services/<service>/api/` 下按调用面拆分：
`public.openapi.yaml` 描述该服务拥有的 public/Gateway-facing 设计面；
只有进入 `docs/services/gateway/api/public.openapi.yaml` active paths 的内容
才是前端稳定公开契约，未进入 Gateway active paths 的服务级 public
内容必须标为 candidate/draft。`internal.openapi.yaml` 描述服务间
`/internal/v1/**`、服务本地运行路径和健康检查契约，例如 Document report job
内部运行合同。历史 `openapi.yaml` 文件按服务后续文档整理任务逐步迁移；新建服务或新增调用面应先按
public/internal 命名落位。

| 契约 | OpenAPI 版本 | API 文档版本 | 说明 |
| --- | --- | --- | --- |
| Gateway public API | `3.0.3` | `0.1.0` | 前端公开契约权威来源。 |
| Gateway internal API | `3.0.3` | `0.1.0` | Gateway 内部操作契约。 |
| Auth service API | `3.0.3` | `0.1.0` | 服务级身份与会话契约。 |
| AI Gateway public API | `3.0.3` | `0.1.0` | 明确声明 AI Gateway 无直接前端公开路径；前端模型配置入口在 Gateway public API。 |
| AI Gateway internal API | `3.0.3` | `0.1.0` | 服务间模型配置和 OpenAI-compatible 调用契约。 |
| QA service public API draft | `3.0.3` | `0.1.0` | QA Agent Host 服务级 public 设计面；稳定前端入口仍以 Gateway OpenAPI 为准。 |
| QA service internal API / implementation copy | `3.1.0` | `1.0.0` | QA 服务本地运行契约和实现副本。 |
| Document service public API draft | `3.0.3` | `0.1.0` | 报告生成服务级 public 设计面；稳定前端入口仍以 Gateway OpenAPI 为准。 |
| Document service internal API / implementation copy | `3.1.0` | `0.1.0` | Document 服务内部运行和 report job 合同。 |
| Knowledge service internal API / implementation copy | `3.1.0` | `0.1.0` | 服务内 OpenAPI 覆盖内部知识库、文档、parser config、chunks/content 和查询能力；前端稳定入口仍以 gateway OpenAPI 为准。 |
| Knowledge public API draft | `3.0.3` | `0.1.0` | Knowledge 公开资源设计草案；稳定公开入口仍以 gateway OpenAPI 为准。 |
| File service internal API / implementation copy | `3.0.3` | `0.2.0` | File 服务是后端内部基础文件能力，不直接作为前端公开 API。 |
| File service public API draft | `3.0.3` | `0.1.0` | File 当前无前端公开 API；public 文件用于明确调用面边界。 |

## 服务级偏离

| 服务 | 偏离项 | 原因 |
| --- | --- | --- |
| `knowledge` | 无。 | Knowledge 现在与仓库 Go 1.25 baseline 一致；仍沿用标准库 `net/http` / `http.ServeMux` 路由形态。 |

## 三选一决策记录

| 领域 | 备选 1 | 备选 2 | 备选 3 | 当前决定 | 版本状态 |
| --- | --- | --- | --- | --- | --- |
| 数据库访问 | `pgx` + 手写 SQL | `pgx` + `sqlc` | GORM/ent ORM | `pgx`；按服务选择手写 SQL 或 `sqlc` | `pgx/v5@v5.9.2` 已固定（S-025 升级）；sqlc CLI 推荐版本 `v1.31.1` 已固定（S-033）；Auth/Document/File/AI Gateway 使用 sqlc，Knowledge 当前 parser-config repository 使用手写 SQL，QA 存量生成包来自 v1.29.0 |
| 数据库迁移 | `goose` | `golang-migrate` | Atlas | `goose` | `goose@v3.27.1` 已固定 |
| 日志 | `slog` | `zap` | `zerolog` | `slog` | Go `1.25` 标准库 |
| HTTP 路由 | 标准库 `ServeMux` | `chi` | `gin` | 标准库 `ServeMux` | Go `1.25` 标准库 |
| 配置 | 手写 `os.Getenv` | `envconfig` | `viper` | `envconfig` 风格结构化加载 | 依赖待固定；允许先手写等价实现 |
| 队列 | Redis Streams 手写 | `asynq` | PostgreSQL queue | `asynq` | `asynq v0.26.0` 已固定 |
| 前端 API client | 手写 fetch 类型 | `openapi-typescript` + wrapper | Orval | `openapi-typescript` + wrapper | `openapi-typescript@7.13.0` 已固定；wrapper 已有 |
| 认证 token | Opaque token | JWT access + refresh | HttpOnly cookie session | Opaque Bearer token | 协议契约 |
| DOCX 生成 | Go DOCX 库 | Pandoc/LibreOffice | 独立模板服务 | 当前阶段：Document worker + 内置 Go `SimpleDOCXGenerator`；富文档阶段：Document worker + `pandoc/core:3.10` worker image | 内置生成器已固定为标准库实现；Pandoc 镜像 tag 已固定为 `pandoc/core:3.10`（C-011）；LibreOffice 暂不引入，保留候选；详见 [rich-docx-worker.md](../services/document/docs/rich-docx-worker.md) |

## 后端落地约定

### PostgreSQL、sqlc 和 pgx

- 每个需要 PostgreSQL 的服务在服务目录维护 `sqlc.yaml`。
- SQL 查询文件放在服务本地目录，例如：

```text
services/<service>/
  internal/repository/queries/
  internal/repository/sqlc/
  migrations/
  sqlc.yaml
```

- `sqlc` 生成代码只能被服务本地 repository 适配层使用，不直接泄露到 HTTP handler。
- 事务由 service/use-case 层发起；repository 接收 `pgx.Tx` 或抽象后的 querier。
- 查询必须显式列名，不使用 `SELECT *`。
- 用户输入只能通过参数绑定传入 SQL。
- 当前仓库已落地 PostgreSQL 服务统一使用 `pgx/v5@v5.9.2`（S-025 安全升级）。新增服务默认沿用该版本；如需升级或偏离，必须同步更新服务文档和本文。
- 全仓 sqlc CLI 推荐版本统一为 `v1.31.1`（S-033）。新增或重生成任意服务的查询包时，须使用 `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`，不得使用裸 `sqlc generate`（无版本固定）。各服务 README 已更新为 pinned 命令。Knowledge 和 QA 的存量生成包来自 v1.29.0；下次变更这两个服务的 SQL 文件时，必须用 v1.31.1 重新生成并提交。

### goose 迁移

- 迁移文件继续放在 `services/<service>/migrations/`。
- 文件名使用有序前缀，例如 `0001_create_users.sql`。
- 首期允许 forward-only migration；如果写 down migration，必须能在本地和 CI 验证。
- CI 对有 SQL migration 的已落地 Go 服务执行迁移 apply 校验。
- `goose` 固定使用 `github.com/pressly/goose/v3@v3.27.1`；服务可按需要使用 CLI 或库方式接入，但 CI 和 README 必须引用同一版本。

### Redis 和 asynq

- 需要 Go 服务内异步队列时，默认使用 `asynq`；Knowledge Go adapter 当前不接入 asynq，解析/索引任务由 RAGFlow runtime 内部 Redis/worker 处理。
- 任务类型命名使用 `<service>:<resource>:<action>`，例如 `document:report:generate`。
- 任务 payload 必须是 JSON，并包含可追踪字段：

```json
{
  "requestId": "req_123",
  "jobId": "job_123",
  "userId": "usr_123"
}
```

- 业务状态、任务最终状态、失败摘要和重试次数以 PostgreSQL 为权威；asynq 只负责排队、调度和执行。
- handler 不直接执行长任务，只创建业务 job 记录并投递 asynq task。
- 当前 Redis 本地镜像版本为 `redis:7-alpine`；直接 Redis client 固定为 `go-redis/v9@v9.21.0`。Document 当前通过 `asynq v0.26.0` 间接带入的 `go-redis/v9@v9.14.1` 是实现与文档基线出入；新增直接 Redis 依赖沿用 `v9.21.0`。Knowledge runtime 内部 Redis 使用由 runtime 文档单独约束。

### 日志、指标和追踪

- 后端服务默认初始化 `slog.NewJSONHandler(os.Stdout, ...)`。
- 日志字段至少包含 `service`、`request_id`、`operation`、`status`；有用户上下文时可包含 `user_id`。
- 不记录密码、token、API key、数据库连接串、MinIO secret、prompt 全文、文档全文、object key 或 provider 原始响应体。
- 指标第一阶段采用 Prometheus 风格 endpoint；Go 服务新增指标默认使用 `github.com/prometheus/client_golang@v1.23.2`，指标 label 不得包含用户输入正文、prompt、token、object key 或 API key 指纹。
- 关键链路 tracing 默认使用 `go.opentelemetry.io/otel@v1.44.0`、`go.opentelemetry.io/otel/sdk@v1.44.0` 和 `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.44.0`。采样策略为 parent-based ratio sampler：dev/local 可配置为 100%，生产默认 1%；未配置 exporter endpoint 时不导出 trace，避免无意外联。
- 如需将 OTel metrics 暴露给 Prometheus scrape，使用 `go.opentelemetry.io/otel/exporters/prometheus@v0.66.0`；只需要普通业务指标的服务可先使用 Prometheus client。

### Elasticsearch、MinIO 和对象边界

- 当前 Knowledge Go adapter 不维护独立索引客户端；解析、embedding、索引和检索通过 RAGFlow runtime 完成，默认 doc engine 为 Elasticsearch/索引后端。
- Runtime index 只保存检索需要的最小 payload；展示正文、权限判断和状态判断必须经 Knowledge adapter/runtime 契约映射，不直接暴露索引 payload。
- File service 是 MinIO 对象存储边界；业务服务不得直接暴露 bucket、object key、内部 URL、access key 或 presigned URL。
- MinIO adapter 已在 File Service 落地，Go SDK 固定为 `github.com/minio/minio-go/v7@v7.2.1`；根目录本地 Compose 固定 MinIO server 和 `mc` client 镜像版本。`minio-init` 只用 `mc` 创建 bucket，不是第二个 MinIO server。后续生产部署应沿用明确 tag 并同步本文。

## 前端落地约定

- OpenAPI 类型生成统一使用 `openapi-typescript@7.13.0`；版本已写入 `apps/web/package.json` 和 `bun.lock`。
- 生成目录为 `apps/web/src/api/generated/`，生成文件不得手工修改。
- `apps/web/src/api/client.ts` 只负责 transport：base URL、认证头、request id、JSON/form/SSE 处理、envelope 和错误归一化。
- 前端不得继续依赖旧 `{ code, message, data }` envelope；gateway 项目自有 JSON 接口使用 `{ data, requestId }` 成功响应和 `{ error }` 错误响应。
- SSE 通过 `fetch` + stream reader 处理，必须支持 `AbortController` 取消。
- 当前 UI 生成配置来自 `components.json`：`base-nova`、Tailwind `neutral` base color、CSS variables、Lucide icons、`@` 指向 `apps/web/src`。
- 当前前端质量命令为 `bun run --cwd apps/web check` 和 `bun run --cwd apps/web build`。

## 安全落地约定

- `Authorization: Bearer <accessToken>` 中的 access token 是 opaque token。
- Auth 服务保存 token hash，不保存明文 access token。
- Gateway Redis 会话缓存 key 使用 token hash，不使用原始 token。
- 密码哈希使用 `argon2id-v1`；固定参数为 `m=65536 KiB`、`t=3`、`p=2`、`salt=16 bytes`、`key=32 bytes`，编码为 PHC string。
- AI Gateway provider API key 不进入日志、响应、指标 label 或前端缓存。
- AI Gateway provider API key 生产环境优先保存 secret manager 引用；第一阶段如果使用数据库加密列，必须保存加密密文、加密密钥版本和脱敏状态，不能保存明文。
- AI Gateway provider API key 指纹只用于审计和变更检测，不用于认证。数据库字段沿用 `fingerprint_sha256` 兼容命名，但写入值必须是用凭据加密密钥派生出的 HMAC-SHA-256 指纹，不能是明文 API key 的裸 SHA-256。
- QA、Knowledge、Document 等领域服务调用 AI Gateway 时只能使用受控内部地址。运行时配置不得包含 URL credentials、query、fragment、意外 path 或公网/内网 IP literal；本地开发仅允许 loopback/`localhost` 和显式 Compose 服务名 `ai-gateway`。
- QA 内置命令工具不得通过 shell 解释用户可控字符串。内置命令工具只能执行代码内批准的 path-free diagnostic command，例如 `echo`、`pwd`、带数值时长的 `sleep`；文件访问必须走 workspace-bounded file tools。MCP runtime 配置只能使用 Streamable HTTP；stdio 仅保留给包内 SDK lifecycle 测试，且必须映射到代码内精确 command spec allowlist，不能把配置中的 executable 或 argv 原样传给 `exec.Command`。
- Repository 层把分页、游标或 limit/offset 转为 `int32` 前必须做显式范围检查；不得依赖上层校验，也不得用 `MaxInt32` 静默截断调用方输入。

## CI 和部署落地约定

- 当前 GitHub Actions 已固定 `actions/github-script@v7`，用于 PR guard、commitlint 和 auto-label。
- 当前 runner 使用 `ubuntu-latest`，这是 GitHub 托管滚动版本；如需完全可复现 CI，后续应改为团队认可的固定 runner 镜像或自托管 runner。
- Frontend CI 对 `apps/web/**`、根前端依赖文件和自身 workflow 变更执行 `bun install --frozen-lockfile`、`bun run --cwd apps/web check`、`bun run --cwd apps/web build`、`bun run --cwd apps/web test:unit` 和 Playwright smoke。
- Go Service CI 按服务路径选择受影响服务，执行 `go test ./...` 和 `go build ./cmd/server`；QA 额外执行 `go build ./cmd/agent`。
- Goose migration CI 对有 SQL migration 的服务执行 `goose@v3.27.1` apply 校验。
- Docker / Deploy Checks 只验证 `deploy/docker-compose.yml` 的 infra-only config、Docker policy 和 Docker environment 诊断；不处理业务服务容器。
- 当前仓库不保留生产/准生产 Compose 基线；后续如要增加部署能力，需要独立任务重新设计。

## 后续需要同步的实现任务

- 为尚未完成数据库 runtime repository 的 Go 服务补充或迁移 `sqlc.yaml`、query 文件和 `pgx` repository；Knowledge parser-config repository 当前为手写 SQL，如要迁移到 sqlc 需单独任务。
- 为后续新增的数据库服务同步 `goose@v3.27.1` 迁移命令和 CI 校验。
- 为需要 Go 服务内异步任务的服务接入 `asynq` client/worker；Document 当前 asynq 传递依赖仍带入旧 `go-redis`，后续服务接入或升级前应优先对齐直接 Redis client 基线。Knowledge adapter 的解析/索引任务继续走 RAGFlow runtime。
- 前端接入 `openapi-typescript`，生成 gateway 类型，并固定生成器版本。
- 前端测试接入 Vitest、React Testing Library 和 Playwright，并固定版本。
- 本地 infra Compose 已固定 PostgreSQL、Redis、MinIO、MinIO mc、Elasticsearch 等依赖镜像 tag；后续升级必须继续使用明确 tag，不能以 `latest` 作为基线。
- 为 Prometheus metrics、OpenTelemetry tracing 和 MCP SDK/sidecar 固定版本；Document 富 DOCX worker 若后续引入，必须同步本文和对应服务文档。

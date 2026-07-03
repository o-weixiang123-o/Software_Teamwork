# 本地联调运行手册

默认联调路径只有一条：

```text
Docker infra -> host backend -> frontend
```

不要启动业务服务容器，不要使用 `--build`，不要手工 export 一长串变量。
`deploy/.env.example` 是默认配置来源；用户复制成 `deploy/.env` 后，脚本只读取它。

Issue #125 的 MCP 与跨服务 smoke 汇总入口见
[`Issue #125 MCP and Cross-Service Smoke`](./issue-125-smoke.md)。只有对应 slice
在当前环境实际通过后，才能在 PR 或验收记录中声明完整跨服务 smoke 通过。
Auth/Gateway/Redis 的完整本地 smoke 可直接执行：
`bash scripts/run_issue_352_smoke.sh`。

## 启动命令

先确认 Go 安装在实际运行脚本的环境中，并能下载 modules。WSL、Git Bash 和
Windows PowerShell 的 `go env` 互不等价；在哪个 shell 里运行脚本，就在那里检查：

```bash
go version
go env GOPROXY
```

默认使用官方源。中国大陆网络如果访问 GitHub、Docker Hub、PyPI、HuggingFace 或
Go modules 不稳定，启动脚本支持进程内大陆镜像，不改写 `deploy/.env`：

```bash
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china
```

`dev-up.sh --china` 会一并准备 Knowledge runtime 的 Python 依赖和 GitHub release/raw、
NLTK、HuggingFace、Tika、Chrome 等 artifact 下载；重复启动或只想拉起 infra 时可加
`--skip-knowledge-runtime-deps`。

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
cd apps/web && bun install && bun run dev
```

日常再次启动：

```bash
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
cd apps/web && bun run dev
```

停止：

```bash
./scripts/local/stop-backend.sh
```

重置本地数据：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v
```

## 启动后应该看到什么

- 前端：`http://localhost:5173`
- Gateway：`http://localhost:8080`
- 本机 OpenAI-compatible 模型服务默认地址：`http://localhost:11434/v1`
- 默认 demo 管理员：`admin` / `LocalDemoAdmin#12345`
- 默认 demo 超管：`superadmin` / `LocalDemoAdmin#12345`
- 后端日志：`.local/logs/*.log`
- 后端进程组 PID：`.local/run/*.pid`

快速确认：

```bash
curl --noproxy '*' -fsS http://localhost:8080/healthz
curl --noproxy '*' -fsS http://localhost:8080/readyz
```

`ai-gateway /readyz` 在 placeholder profile 下返回 `503 degraded` 是预期行为。
这表示真实 provider credential 尚未配置，不表示进程失败。

## Document MCP 注册与工具发现

`dev-up.sh` 在 QA 数据库插入 `mcp_servers.alias=document` 的非敏感元数据；
`deploy/.env` 提供 `MCP_SERVER_TOKEN`。`run-backend.sh` 在宿主机启动 Document
`http://localhost:8085/mcp` 和 QA，根级 Compose 不启动这两个业务服务容器。

确认 seed：

```bash
psql "$QA_DATABASE_URL" -c \
  "select alias, transport, endpoint_url, token_encrypted is null as token_from_env, enabled from mcp_servers where alias = 'document';"
```

fresh local volume 的预期值为 `document`、`streamable_http`、
`http://localhost:8085/mcp`、`token_from_env=true`、`enabled=true`。已有管理员配置不会被
seed 覆盖；同名记录被显式禁用时，环境 bootstrap 也不会把它重新启用。

运行现有 9 个工具的发现与调用 smoke：

```bash
cd services/qa
QA_DOCUMENT_MCP_SMOKE=1 \
go test ./internal/platform/mcpclient -run '^TestDocumentMCPReportToolsSmoke$' -count=1 -v
```

该 smoke 验证 `tools/list`、`document__` 前缀、默认白名单、生成 job accepted/status、
DOCX export/result 安全摘要及无权限结果。真实模型 provider 仍不可用时，可以验证 MCP
client 与 Document 工具，不代表完整 QA Agent + LLM 链路通过。Issue #510 的
`document__generate_report_from_content` 合并前不在当前 9 工具验收范围内。

常见失败：

| 现象 | 排查 |
| --- | --- |
| QA 日志 `connection failed` | 检查 Document 已启动、`MCP_SERVER_URL=http://localhost:8085/mcp`。 |
| Document 返回 `401` | 检查 `MCP_SERVER_TOKEN` 与 `DOCUMENT_MCP_SERVICE_TOKEN`/`INTERNAL_SERVICE_TOKEN` 一致，header 均为 `Authorization`。 |
| `tools/list` 成功但模型看不到工具 | 检查 active QA config 的 `enabledToolNames`；默认只放行 5 个常用 `document__...` 工具。 |
| job 一直 pending/running | 查 `.local/logs/document.log`、Redis/asynq worker、AI Gateway profile；不要在 MCP HTTP 调用中做无界等待。 |

精确工具参数、返回结构和 Agent 多轮流程见
[Document MCP 工具契约](../services/document/docs/mcp-tools.md)。

## 谁负责什么

- `dev-up.sh`：检查同一宿主机环境中的 Docker、Go、`psql`、`uv`（仅 `--china`
  runtime 准备需要）和必要的 `curl`，
  infra pull/up、等待 `postgres` / `redis` / `qdrant` / `minio` Compose
  health checks、单独运行一次性 `minio-init`、Go module 配置检查、migration、
  demo seed；传入 `--china` 时还会自动准备 Knowledge runtime 依赖和 artifact 下载，
  可用 `--skip-knowledge-runtime-deps` 或 `LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS=1` 跳过。
  如果 `QDRANT_URL` 明确配置，则额外执行 legacy/test-only Qdrant collection 初始化。
  当前 Knowledge 索引准备属于宿主机 RAGFlow runtime/doc engine 路径，不通过恢复 Go
  侧 Qdrant collection bootstrap 作为必需默认路径。
  `minio-init` 正常 `Exited (0)` 不应阻断后续步骤；非零失败时看
  `docker compose logs minio-init`。
- `run-backend.sh`：后端进程启动、日志和进程组 PID。Knowledge 使用 `cmd/adapter`
  调用宿主机 RAGFlow runtime API/worker。启动前会用当前 `deploy/.env` 对每个 Go
  服务执行 `go mod download` 预检；服务 fork 后默认观察 8 秒，若进程组很快退出，
  会直接汇总对应 `.local/logs/<service>.log` 尾部。
- Go module 下载默认来自 `deploy/.env` 里的 `GOPROXY` / `GOSUMDB`，覆盖
  `dev-up.sh` 里的 goose migration 和 `run-backend.sh` 里的 Go 服务 `go run`。
  官方默认值是 `https://proxy.golang.org,direct` / `sum.golang.org`；`--china`
  只在本次进程切到大陆镜像。这不是 Docker 镜像源，也不是 Knowledge runtime 的
  `UV_DEFAULT_INDEX`。
- `stop-backend.sh`：按 `.local/run/` 中记录的进程组停止后端，避免只杀掉
  `go run` / `uv run` wrapper 后留下真实服务占用端口。
- `deploy/.env`：本地配置。脚本不生成、不改写、不维护第二套默认值。
- 这三个本地入口脚本都必须在命令行输出彩色的开始、成功、警告和失败摘要。失败摘要应
  说明当前阶段、退出码和下一步排查入口，不能只靠用户自己翻日志猜状态。`NO_COLOR=1`
  可关闭颜色，`FORCE_COLOR=1` 可在非 TTY 中强制开色。

## 故障判断

Infra 拉取慢：

- 默认是 Compose 里的 Docker Hub pinned tags。
- 中国大陆网络先运行 `./scripts/local/dev-up.sh --china`，本次进程会使用 DaoCloud
  registry rewrite；`deploy/.env` 不会被脚本改写。
- 已配置 Docker daemon mirror 时，运行 `python3 scripts/check_docker_environment.py --profile all --clean-env`。
- 代理只作为最后选择；shell proxy、daemon proxy 和 registry rewrite 是三条不同路径。

RAGFlow runtime 启动慢：

- 默认 `UV_DEFAULT_INDEX=https://pypi.org/simple`，host-run `uv sync` 使用官方 PyPI。
- 中国大陆网络运行 `./scripts/local/dev-up.sh --china` 时会自动执行 runtime 依赖和
  artifact 下载。该步骤会用临时 overlay 将 Python 包、GitHub release/raw、NLTK、
  HuggingFace、Tika 和 Chrome 下载切到镜像，但提交的 `pyproject.toml` / `uv.lock`
  仍保持官方 URL。
- 如果此前用 `--skip-knowledge-runtime-deps` 跳过，可按
  `services/knowledge-runtime/README.md` 手工补跑 runtime 下载脚本。
- runtime API 和 worker 走宿主机启动，不通过根级 Docker Compose 构建或运行。
- 不要恢复 `services/parser`；PDF 解析、切块、embedding、索引和检索由 RAGFlow
  runtime worker 完成。

Go modules 下载慢或超时：

- `dev-up.sh` 会用 `go run github.com/pressly/goose/v3/cmd/goose@v3.27.1`
  执行 migration；`run-backend.sh` 会用 `go run ./cmd/server` 启动各 Go 服务。
- 默认保留 `deploy/.env.example` 里的官方
  `GOPROXY=https://proxy.golang.org,direct` 和 `GOSUMDB=sum.golang.org`；脚本读取
  `deploy/.env` 后会把它们传给 host-run Go 命令。中国大陆网络使用
  `./scripts/local/dev-up.sh --china` 或 `./scripts/local/run-backend.sh --china`
  临时切换到 `https://goproxy.cn,direct` 和 `sum.golang.google.cn`。
- `dev-up.sh` 会在 migration 前检查 Go module 配置；`run-backend.sh` 会在启动服务前
  预检 Go module 下载。旧 `deploy/.env` 如果仍保留镜像值，脚本会尊重本地覆盖并提示；
  若 proxy 或 checksum DB 不可达或下载超时，脚本会在终端直接失败并打印当前有效
  `GOPROXY` / `GOSUMDB`，而不是只把错误藏在 `.local/logs/*.log`。
- Go modules 下载不走 Docker registry rewrite，也不受 `UV_DEFAULT_INDEX` 影响。
- 如果 `.local/logs/auth.log`、`.local/logs/gateway.log` 等文件出现
  `proxy.golang.org`、`i/o timeout` 或 `go: downloading ...` 后退出，Gateway/Auth
  可能没有监听 `8080`/`8001`，前端登录会表现为 `502 Bad Gateway`。
- 如果 `.local/logs/auth.log`、`.local/logs/gateway.log` 或其他 Go 服务日志出现
  `Get "https://proxy.golang.org/...": i/o timeout`，中国大陆网络先改用对应脚本的
  `--china`；其他网络检查企业代理或本机 Go 配置。
- 已有旧 `deploy/.env` 的环境不会被脚本自动改写；想恢复官方默认值，重新复制
  `deploy/.env.example` 后再恢复本机私有配置。
- 如果需要把镜像配置持久写入当前 shell 使用的 Go 全局配置，在运行脚本的同一个环境中执行：

  ```bash
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=sum.golang.google.cn
  ```

- 之后重新运行 `./scripts/local/stop-backend.sh` 和 `./scripts/local/run-backend.sh`，
  再用 `curl --noproxy '*' -fsS http://localhost:8080/healthz` 验证 Gateway。

后端没起来：

- 先看 `run-backend.sh` 命令行失败摘要；它会说明 Go module 预检、服务启动或短窗口
  进程检查中哪一步失败。
- 先看 `.local/logs/<service>.log`。
- Knowledge ingestion 到 embedding/index 阶段失败时，先确认 `VENDOR_RUNTIME_URL`
  指向可访问的 runtime API，并检查宿主机 runtime worker 是否在处理任务。
- Auth、File、Knowledge、QA、Document、AI Gateway 优先查数据库和 migration。
- Gateway 优先查 Redis、Auth URL 和下游服务端口。
- File/Document/QA 内部 file 调用 `401` 时，检查 `INTERNAL_SERVICE_TOKEN` 是否一致，以及启用 File caller allowlist 后是否传递了 `X-Caller-Service`。出现 `403` 时，检查 `FILE_ALLOWED_CREATE_CALLERS`、`FILE_ALLOWED_READ_CALLERS`、`FILE_ALLOWED_DELETE_CALLERS` 是否包含实际调用方。Knowledge runtime 调用 `401` 时，检查 `VENDOR_RUNTIME_SERVICE_TOKEN` 与 `KNOWLEDGE_RUNTIME_SERVICE_TOKEN` 是否一致。

WSL 内存高：

- 先看 `docker stats`。
- 当前默认 Docker 只跑 infra；内存压力主要来自 PostgreSQL、Qdrant、MinIO、
  宿主机 RAGFlow runtime 或本机后端进程。
- 不需要保留环境时先停后端，再执行 `docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v`。

```bash
cd ../services/knowledge
GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 \
GATEWAY_BASE_URL='http://127.0.0.1:8080' \
KNOWLEDGE_SERVICE_BASE_URL='http://127.0.0.1:8083' \
VENDOR_RUNTIME_URL='http://127.0.0.1:9380' \
KNOWLEDGE_TEST_DATABASE_URL='postgres://knowledge_app:knowledge_app_dev@127.0.0.1:5432/knowledge_system?sslmode=disable' \
KNOWLEDGE_REDIS_ADDR='127.0.0.1:6379' \
GATEWAY_SMOKE_USERNAME='admin' \
GATEWAY_SMOKE_PASSWORD='LocalDemoAdmin#12345' \
go test ./internal/integration -run '^TestGatewayKnowledgeOwnerRouteSmoke$' -count=1 -v
```

该测试会：

- `GET /readyz` 检查 Gateway、Knowledge 和 RAGFlow runtime 可达性。
- 使用 `KNOWLEDGE_TEST_DATABASE_URL` ping Knowledge PostgreSQL。
- 使用 `KNOWLEDGE_REDIS_ADDR` 发送 Redis `PING`。
- 调用带伪造 `X-User-*` 但无 Bearer token 的 `GET /api/v1/knowledge-bases`，
  断言 Gateway 返回 `401 unauthorized`。
- `POST /api/v1/sessions` 创建真实 Gateway session。
- 带 Bearer token 调用 `GET /api/v1/knowledge-bases`，同时发送一个伪造的
  `X-User-Id`；Gateway 应忽略该伪造 header 并注入 Auth/session cache 中的上下文。
- 通过 Gateway 创建并读取一个 run-scoped knowledge base，断言 `createdBy` 等于真实
  session user，而不是伪造的 `X-User-Id`。

常见失败：

- `GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 requires ...`：缺必填 env；失败只列 key，不列值。
- `vendor ping failed` 或 Knowledge readiness 返回 `vendor_runtime_ok=false`：
  先确认 `VENDOR_RUNTIME_URL` 指向 `http://127.0.0.1:9380`，runtime API 已在宿主机启动。
- Gateway session 返回 `401`：确认 `./scripts/local/dev-up.sh` 已完成 seed SQL，并使用
  `.env.example` 中的 `admin` / `LocalDemoAdmin#12345` 或显式
  `GATEWAY_SMOKE_USERNAME` / `GATEWAY_SMOKE_PASSWORD`。
- Gateway Knowledge route 返回 `401`：Gateway session cache/Redis 可能不可用；查
  `.local/logs/gateway.log`、`.local/logs/auth.log`，并确认 Redis infra 已启动。
- Gateway Knowledge route 返回 `502`：Knowledge owner route 或 service token 配置异常；
  查 `.local/logs/gateway.log` 和 `.local/logs/knowledge.log` 并用相同 `X-Request-Id` 搜索。

### Gateway -> Knowledge -> QA RAG 端到端 smoke

该 smoke 是 Issue #304 的最小 RAG 验收样例。它通过 Gateway public
`/api/v1/**` 创建 session、创建知识库、上传文档、轮询 Knowledge ingestion、
调用 `knowledge-queries`，再配置 QA 的临时 LLM/retrieval config，创建 QA session/message，
并断言 answer 和 citation 摘要。它不替代 #125 的完整跨服务/MCP smoke。

样例固定在测试代码中：

| 项目 | 值 |
| --- | --- |
| 文档文件名 | `gateway-rag-e2e-smoke.md` |
| 样例问题 | `What calibration marker must be checked for the RAG E2E smoke?` |
| 预期命中 | `calibrate relay RAG-E2E-304` |
| 预期 citation 字段 | `knowledgeBaseId`、`documentId`、`chunkId` 非空且匹配本轮资源；`contentPreview` 或 `text` 包含预期命中。 |

前置要求：

- 已按本手册“启动命令”使用 `./scripts/local/dev-up.sh` 启动 infra、migration 和
  seed，并用 `./scripts/local/run-backend.sh` 在宿主机启动后端服务。
- `QA_SETTINGS_OPEN=true` 只建议在本地 smoke 环境启用，用于允许测试通过 Gateway
  创建本轮 QA/LLM config versions；需要在启动后端前写入 `deploy/.env`。
  也可改用具备 `qa:settings:write` 权限或 `QA_ADMIN_USER_IDS` 的账号。
- `QA_SMOKE_CHAT_PROFILE_ID` 和 `QA_SMOKE_CHAT_MODEL` 必须指向 AI Gateway 中可实际
  调用的 chat profile/model。`.env.example` 的 `default-chat` /
  `local-placeholder-chat` 只在 `localhost:11434/v1` 后面有可用
  OpenAI-compatible provider 时可用；真实 provider 或受控 stub provider 仍需显式配置。
- 默认 Knowledge adapter 不自动触发 runtime ingestion。需要证明真实上传、解析、
  embedding、索引和检索时，在启动 runtime 前准备可访问的 Elasticsearch/doc engine
  和 embedding provider，并在本地 `deploy/.env` 中显式设置
  `KNOWLEDGE_AUTO_START_INGESTION=true` 及对应 `KNOWLEDGE_RUNTIME_EMBEDDING_*`
  变量。

启动本地栈：

```bash
cp deploy/.env.example deploy/.env
# 如需运行本 smoke，可先在 deploy/.env 中设置 QA_SETTINGS_OPEN=true。
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

运行 smoke：

```bash
cd services/knowledge
GATEWAY_RAG_E2E_SMOKE=1 \
GATEWAY_BASE_URL='http://127.0.0.1:8080' \
VENDOR_RUNTIME_URL='http://127.0.0.1:9380' \
KNOWLEDGE_SERVICE_BASE_URL='http://127.0.0.1:8083' \
QA_SERVICE_BASE_URL='http://127.0.0.1:8084' \
AI_GATEWAY_BASE_URL='http://127.0.0.1:8086' \
KNOWLEDGE_TEST_DATABASE_URL='postgres://knowledge_app:knowledge_app_dev@127.0.0.1:5432/knowledge_system?sslmode=disable' \
KNOWLEDGE_REDIS_ADDR='127.0.0.1:6379' \
GATEWAY_SMOKE_USERNAME='admin' \
GATEWAY_SMOKE_PASSWORD='LocalDemoAdmin#12345' \
QA_SMOKE_CHAT_PROFILE_ID='default-chat' \
QA_SMOKE_CHAT_MODEL='local-placeholder-chat' \
go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v
```

预期结果：

```text
=== RUN   TestGatewayRAGE2ESmoke
--- PASS: TestGatewayRAGE2ESmoke (...s)
PASS
```

测试会创建 run-scoped knowledge base 和文档，并在清理阶段先调用 Gateway
`DELETE /api/v1/documents/{documentId}` 触发 RAGFlow runtime 文档删除，再按
chunks、jobs、documents、knowledge base 的顺序删除本轮 Knowledge PostgreSQL 行。
测试开始前会读取当前 active QA config 和 LLM config，结束时通过 Gateway settings API
重新创建并激活一份等价恢复版本。本轮 smoke 创建的 QA config versions 仍会作为本地运行
记录留存，因为它们是 QA settings 的真实业务资源；只在临时本地栈运行该 smoke。

常见失败和定位：

| 阶段 | 典型失败 | 排查 |
| --- | --- | --- |
| File | `File stage: gateway document upload returned HTTP ...` | 查 `.local/logs/gateway.log` 和 `.local/logs/file.log`，确认 File ready、`INTERNAL_SERVICE_TOKEN` 一致、上传大小未超限。不要打印 multipart body 或 object key。 |
| Knowledge runtime | `Parser stage: ready document did not record parserBackend` 或文档状态 `failed` | 查 `.local/logs/knowledge.log` 和 runtime API/worker 终端日志，确认 `VENDOR_RUNTIME_URL`、`VENDOR_RUNTIME_SERVICE_TOKEN`、`KNOWLEDGE_RUNTIME_SERVICE_TOKEN` 一致；Markdown fixture 不需要真实 OCR 模型下载。 |
| Knowledge ingestion | `document ... did not become ready` 或 `chunkCount = 0` | 查 `.local/logs/knowledge.log` 和 runtime worker 日志，并确认 PostgreSQL、Redis、MinIO、Elasticsearch 处于 healthy；检查 runtime task 状态和对象存储 bucket。 |
| Knowledge retrieval | `Knowledge retrieval stage: ...`、无 expected hit 或 rerank trace 异常 | 查 `.local/logs/gateway.log`、`.local/logs/knowledge.log` 和 `.local/logs/ai-gateway.log`；确认 `knowledge-queries` 返回 ready 文档 chunk，`rerank=true` 在无 `RERANK_MODEL` 时只证明 no-op fallback trace。 |
| AI Gateway | QA message 返回 `502`、model error 或 provider unavailable | 查 `.local/logs/qa.log` 和 `.local/logs/ai-gateway.log`；确认 chat profile enabled、model exact-match、credential/provider 可用。不要粘贴 provider 原始错误 body 或 API key。 |
| QA | QA config POST `403/400`、answer 未完成、无 citation | 确认 `QA_SETTINGS_OPEN=true` 或账号具备 `qa:settings:write`；确认模型支持 OpenAI-compatible tool/function calling，并且 QA config 只启用 `search_knowledge`。 |

如果只需要验证 Knowledge ingestion 和 retrieval，不要运行本 RAG smoke；先使用上面的
`KNOWLEDGE_INGESTION_SMOKE` 或 `GATEWAY_KNOWLEDGE_OWNER_SMOKE` 缩小范围。

### QA + Auth + Gateway 宿主机联调检查

```bash
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

该路径适合验证 Auth、QA、Gateway 的基础 ready 状态和 QA 非 provider 依赖路径：

```bash
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8084/readyz
curl -fsS http://localhost:8080/readyz
```

日志查看 `.local/logs/auth.log`、`.local/logs/qa.log`、`.local/logs/gateway.log`。
触发真实 LLM 调用、LLM connection test 或 Agent Run 时，确保宿主机 `ai-gateway`
已由 `run-backend.sh` 启动，并且 `deploy/.env` 中的 profile/provider 配置可用。

### Document 宿主机联调检查

```bash
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

该路径适合验证 Document PostgreSQL、Redis、migration、job enqueue 和 worker 状态机：

```bash
curl -fsS http://localhost:8085/readyz
```

注意：模板、材料和报告文件 bytes 需要 File Service；真实大纲/正文生成需要 AI Gateway。
当前基础 DOCX 导出使用 Document 内置 `SimpleDOCXGenerator`，不需要 Pandoc/LibreOffice；
Pandoc/LibreOffice 仅是后续富 DOCX worker 工具链。日志查看
`.local/logs/document.log`、`.local/logs/file.log` 和 `.local/logs/ai-gateway.log`。
Document worker 会执行 `report_file_creation` 的基础 DOCX 导出；其他大纲/正文生成类
job 依赖可用的 AI Gateway chat profile/provider。

### AI Gateway host-run

PR #487 之后 AI Gateway 作为本机进程运行（不再有 AI Gateway Docker 业务服务）。
`dev-up.sh` 会自动执行 ai-gateway migration 并写入本地 placeholder profile；`run-backend.sh`
会随其他服务一起启动 ai-gateway。

AI Gateway 服务 token 运行时只接受 hash，如需手动验证：

```bash
TOKEN=dev-internal-service-token-change-me
printf '%s' "$TOKEN" | shasum -a 256 | awk '{print "sha256:" $1}'
```

环境变量通过 `deploy/.env` 统一加载（`AI_GATEWAY_DATABASE_URL`、
`AI_GATEWAY_SERVICE_TOKEN_HASHES`、`AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY` 等均在
`.env` 中定义）。若要单独调试，可手动 source 后运行：

```bash
set -a && source deploy/.env && set +a

cd services/ai-gateway
go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 \
  -dir migrations postgres "$AI_GATEWAY_DATABASE_URL" up
go run ./cmd/server
```

创建可调用 profile 后，`/internal/v1/chat/completions`、`/internal/v1/embeddings` 和 `/internal/v1/rerankings` 都会按 profile 解析 provider、model、base URL 和 API key。当前 fake-provider 单元测试覆盖了协议形态；真实 provider smoke 仍需单独执行并记录。

### 报告大纲/正文 AI 生成配置

报告大纲生成（`outline_generation`）和正文生成（`content_generation`）调用 AI Gateway 的
`default-chat` profile。**本地 seed 写入的是 Ollama 占位 URL，不带真实模型，默认不可用。**
拉取最新 develop 后如果点击"生成大纲"一直失败，根本原因就是 AI Gateway 没有指向可用的
LLM provider。

按以下步骤配置后重试即可。

#### 配置 OpenAI 兼容 provider（以 DeepSeek 为例）

将 `default-chat` profile 指向 DeepSeek，然后在管理端「模型配置」页面的 Credentials
表单里填入 API key（**不要把 key 写进任何文件或命令行历史**）。

先用 seed 写入的 admin 账号登录并提取 session token（token 是动态签发的，不要硬编码）：

```bash
ADMIN_TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"LocalDemoAdmin#12345"}' \
  | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "token: $ADMIN_TOKEN"
```

再更新 profile：

```bash
curl -X PATCH http://localhost:8080/api/v1/admin/model-profiles/default-chat \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"baseUrl":"https://api.deepseek.com/v1","model":"deepseek-chat"}'
```

API key 通过管理端 UI 配置（Admin → 模型配置 → default-chat → 更新凭证），
不经过命令行，避免泄漏到 shell 历史。

> **代理注意**：ai-gateway 以本机进程运行（PR #487 后不再使用 Docker），直接继承
> 宿主机代理环境变量。如果所在网络需要代理才能访问外部 provider，在 `deploy/.env`
> 中添加：
>
> ```bash
> HTTP_PROXY=http://127.0.0.1:7897
> HTTPS_PROXY=http://127.0.0.1:7897
> ```
>
> 然后重启 ai-gateway 进程（先执行 `stop-backend-windows.ps1`，再执行
> `start-backend-windows.ps1`）。端口号改成本机实际代理端口。

#### 验证

配置完成后确认 profile 可用：

```bash
# AI Gateway 健康检查
curl -fs http://localhost:8086/readyz && echo "ok"

# 快速冒烟：直接调 chat completions（model 填你实际配置的模型名）
INTERNAL_TOKEN="local-dev-internal-service-token-change-me"
curl -X POST http://localhost:8086/internal/v1/chat/completions \
  -H "X-Service-Token: $INTERNAL_TOKEN" \
  -H "X-Caller-Service: document" \
  -H "Content-Type: application/json" \
  -d '{"profile_id":"default-chat","model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}'
```

返回包含 `choices` 字段则 provider 正常，之后在前端选择「迎峰度夏检查报告」
（`summer_peak_inspection`）或「煤库存审计报告」（`coal_inventory_audit`）重新提交，
即可触发大纲生成。正文生成可继续通过报告页面的「正文生成」任务或单章重新生成验证。

## 手动联调顺序

完整链路尚未一键化时，建议按下面顺序缩小问题范围：

1. 单服务测试和 build 先通过：`go test ./...`、`go build ./cmd/server`。
2. 对有 migration 的服务执行 goose apply smoke。
3. 启动 Auth、Gateway、目标领域服务和该领域服务的数据库。
4. 需要模型调用时再启动 AI Gateway，并创建对应 `purpose=chat|embedding|rerank` 的 enabled/default profile。
5. 需要文件 bytes 时再启动 File Service；不要让领域服务直接暴露 object key、bucket 或内部 URL。
6. 通过 Gateway public `/api/v1/**` 验证前端可见能力；只在服务间 smoke 中直连 `/internal/v1/**`。

## 冒烟检查清单

| 场景 | 检查 | 当前预期 |
| --- | --- | --- |
| Auth/Gateway/QA 宿主机联调检查 | 各服务 `GET /readyz` + 目标 Gateway API smoke | Gateway `/readyz` 只证明 Redis/Auth 和 owner URL 配置；QA `GET /readyz` 证明 QA 进程与自身依赖 ready；真实 AI 调用仍可能因 AI Gateway profile/provider 未配置失败。 |
| Document 宿主机联调检查 | 创建 report job 后查询 job/attempt/events | 非文件生成类任务会入队并由 worker 推进为 succeeded；不会生成真实 AI 大纲/正文。若额外提供 File Service，`report_file_creation` 可生成基础 DOCX 并通过 content endpoint 读取成功文件。 |
| AI Gateway profile | 创建 chat/embedding/rerank profile，调用对应内部 endpoint | fake provider 和兼容 provider 应返回 OpenAI-style body；真实 provider 需手工验证。 |
| Gateway contract | `python3 scripts/verify_gateway_active_api.py` | active path、owner、security 和 owner map 不漂移。 |
| File PostgreSQL + MinIO | `FILE_MINIO_POSTGRES_SMOKE=1 ... go test ./internal/integration -run TestFileMinIOPostgresSmoke -count=1 -v` | 只在真实 PostgreSQL/MinIO 可用时运行；验证 upload、metadata、content read、delete 和清理状态。File service 默认本地配置已使用 PostgreSQL + MinIO；生产化验证还应显式设置非本地 `FILE_ENV`、caller allowlist 和 `FILE_ALLOWED_CONTENT_TYPES`，确认 memory backend guard、`401`/`403` 和 MIME 拒绝路径符合预期。 |
| Knowledge runtime route/config | `cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio --with filelock --with ruamel-yaml python -m pytest test/routes/test_config_utils.py test/routes/test_route_registry.py test/routes/test_gateway_auth.py test/routes/test_runtime_dependency_check.py -q` | 验证 runtime 配置脱敏、路由 allowlist、service token、clean DB provisioning 纯逻辑和 host-run dependency guard。 |
| Knowledge ingestion real deps | `KNOWLEDGE_INGESTION_SMOKE=1 ... go test ./internal/integration -run '^TestKnowledgeIngestionRealDepsSmoke$' -count=1 -v` | 只在 Knowledge adapter 以 `KNOWLEDGE_AUTO_START_INGESTION=true` 重启后，且 Knowledge runtime API、runtime worker、Redis/MinIO/Elasticsearch/PostgreSQL 和 embedding provider 均可用时运行；验证 Markdown fixture 上传、解析、切片、embedding、索引写入、ready 状态、chunk 回看和检索命中。当前 Knowledge 主路径不依赖 File Service。 |
| Gateway -> Knowledge owner route | `GATEWAY_KNOWLEDGE_OWNER_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayKnowledgeOwnerRouteSmoke$' -count=1 -v` | 只在 Gateway/Auth/Redis/Knowledge/PostgreSQL 和 Knowledge runtime 可用时运行；验证伪造 `X-User-*` 未认证请求被拒绝，并用 KB `createdBy` 断言 Gateway 注入真实 session user。 |
| Gateway -> Knowledge -> QA RAG | `GATEWAY_RAG_E2E_SMOKE=1 ... go test ./internal/integration -run '^TestGatewayRAGE2ESmoke$' -count=1 -v` | 只在 Gateway/Auth/Redis/Knowledge/Knowledge runtime/QA/AI Gateway 和可用 chat profile/provider 可用时运行；验证上传、ingestion ready、`knowledge-queries` 命中、QA answer 和 citation 摘要。 |
| 前端 Gateway 类型 | `bun run --cwd apps/web api:generate` 后检查 diff | 生成类型应与 Gateway OpenAPI 保持同步。 |

## 已知缺口

| 缺口 | 影响 | 跟踪 |
| --- | --- | --- |
| 根级跨服务 smoke 缺失 | 即使使用 `deploy/docker-compose.yml` 启动本地/演示基线，也不能自动证明 Auth/Gateway/File/Knowledge/QA/Document/AI Gateway 链路可用。 | #125 |
| 跨服务契约测试和 E2E smoke 缺失 | 不能自动证明前端 -> Gateway -> 多服务链路可用。 | #125 |
| Gateway `/readyz` 非完整依赖诊断 | `/readyz` 不请求所有 owner service `/readyz`，也不执行业务级 smoke；它通过不等于 Knowledge/QA/Document/AI Gateway 全链路可用。 | #353、#125、#352 |
| Knowledge runtime 真实解析/检索 smoke 不在普通 CI 中运行 | CI 覆盖 route/config/auth/provisioning 和语法门禁；真实 PDF 解析、OCR 质量、embedding、Elasticsearch/MinIO/Redis 组合仍需在具备依赖的本地或部署环境手动记录。 | #125 |
| Knowledge/QA RAG smoke 仍为显式 opt-in | File 自身 PostgreSQL + MinIO smoke 已有；Knowledge ingestion 真实依赖 smoke 覆盖 Knowledge runtime/PostgreSQL/Redis/MinIO/Elasticsearch 写入和状态更新，当前 Knowledge 主路径不依赖 File Service；Gateway -> Knowledge -> QA RAG smoke 已提供最小验收样例，但依赖可用 AI Gateway chat profile/provider，且不覆盖 MCP、前端或 #125 完整一键 E2E。 | #125、#152、#154、#304 |
| 生产部署基线缺失 | 当前 `deploy/docker-compose.yml` 是本地/演示基线，不能直接当生产部署。 | #150 |
| Document 真实 AI 生成和富 DOCX 工具链未落地 | 报告 job 状态机和基础 DOCX 导出可用；真实大纲/正文生成、Pandoc/LibreOffice 富 DOCX 转换和跨服务内容读取 smoke 仍需补齐。 | #160、#223 |
| Document 跨服务 smoke 仍缺失 | settings/statistics/logs 已在服务端落地，但管理端、Gateway、File Service、Document worker 串联 smoke 仍未一键化。 | #159、#221 |
| QA Agent Run MVP 和权限一致性仍在推进 | QA 会话/消息基础可用，完整 Agent 编排和 403 一致性仍需收口。 | #157、#217 |
| 前端业务 E2E 覆盖不足 | 已有 Playwright 基础 smoke；Knowledge、QA、Document 等完整业务流程仍需随页面能力扩展。 | #117、#163 |

## PR 前判断

- 只改文档：至少执行 `git diff --check`，并检查新增链接、相对路径和实现事实。
- 改后端服务：执行对应服务 `go test ./...` 和 `go build ./cmd/server`；QA 还要 `go build ./cmd/agent`。
- 改 migration：执行 goose apply；如果服务有 env-gated repository integration tests，尽量使用本地 PostgreSQL 跑一遍。
- 改 Knowledge runtime 契约或运行时：检查 `services/knowledge-runtime/README.md`、Knowledge adapter 文档和本 runbook 是否一致；运行 runtime targeted pytest、`python -m compileall api common rag deploy`、Knowledge adapter `go test ./...` / `go build ./cmd/adapter`。如触碰真实解析、embedding、Elasticsearch、Redis 或 MinIO 组合，尽量追加真实 PDF E2E，并在 PR 记录中区分 unit/contract 检查与真实依赖 smoke。
- 改 Gateway OpenAPI：执行 `python3 scripts/verify_gateway_active_api.py`，前端类型相关改动还要执行 `bun run --cwd apps/web api:generate` 并检查生成 diff。
- 改前端：执行 `bun install --frozen-lockfile`、`bun run --cwd apps/web check`、`bun run --cwd apps/web build`、`bun run --cwd apps/web test:unit`；关键页面改动再跑 `bun run --cwd apps/web test:e2e`。

## Appendix: AI Gateway real-provider cross-service acceptance

Use this checklist only in a protected local or shared environment that can hold
real provider credentials. Do not commit `.env` values, provider keys, service
tokens, full prompts, document text, embedding payloads, or provider raw error
bodies.

1. Start the local stack (infra + all backend services):
   `./scripts/local/dev-up.sh && ./scripts/local/run-backend.sh`.
2. Check AI Gateway readiness:
   `curl -s http://127.0.0.1:8086/readyz`.
   `missing` means the profile or active credential is absent; `placeholder`
   means the seeded local fake credential is still present; `ok` means a
   non-placeholder credential is configured, not that the external provider has
   accepted it.
3. Create or patch chat, embedding, and rerank profiles through
   `/internal/v1/model-profiles` with `X-Service-Token`,
   `X-Caller-Service`, and `X-Request-Id`. The `model` in smoke requests must
   exactly match the selected profile.
4. Run direct AI Gateway smoke with
   `AI_GATEWAY_REAL_PROVIDER_SMOKE=1`. Chat is the minimum; embedding and rerank
   run only when the provider and env vars support them.
5. Run QA validation by creating a Gateway session, creating a QA session, and
   sending a message through Gateway public `/api/v1/**` or the documented
   `QA_AI_GATEWAY_SMOKE=1` service-client test. Use the same request id to
   search `.local/logs/gateway.log`, `.local/logs/qa.log`, and
   `.local/logs/ai-gateway.log`.
6. Run Document validation for `summer_peak_inspection` and
   `coal_inventory_audit`: create a report, create an `outline_generation`,
   `section_regeneration`, or `content_generation` job, poll jobs, events, and
   sections until terminal state, then search
   `.local/logs/gateway.log`, `.local/logs/document.log`, and
   `.local/logs/ai-gateway.log` by request id. Rich DOCX
   Pandoc/LibreOffice worker validation is not part of this checklist.
7. Run Knowledge validation in the intended mode. The default local path uses
   local hashing embeddings and no-op/empty rerank configuration; do not count
   it as real AI Gateway embedding/rerank. For real provider validation set
   `EMBEDDING_PROVIDER=ai_gateway`,
   `KNOWLEDGE_AI_GATEWAY_BASE_URL=http://127.0.0.1:8086`, and the embedding or
   rerank profile/model env vars, then inspect Knowledge and AI Gateway logs by
   request id.

Acceptance record template:

| Area | Command or request | Expected proof | Logs |
| --- | --- | --- | --- |
| AI Gateway | `/readyz` + real provider smoke | profile status is not `placeholder`; smoke succeeds for configured operations | `.local/logs/ai-gateway.log` |
| QA | session/message or `QA_AI_GATEWAY_SMOKE=1` | answer path reaches AI Gateway and returns normalized response/error | `.local/logs/gateway.log`, `.local/logs/qa.log`, `.local/logs/ai-gateway.log` |
| Knowledge | local hashing or AI Gateway embedding/rerank path | selected path is explicitly named; real provider path has profile/model env | `.local/logs/gateway.log`, `.local/logs/knowledge.log`, `.local/logs/ai-gateway.log` |
| Document | `summer_peak_inspection` and `coal_inventory_audit` report job flow | job/events/sections reach expected terminal state | `.local/logs/gateway.log`, `.local/logs/document.log`, `.local/logs/ai-gateway.log` |

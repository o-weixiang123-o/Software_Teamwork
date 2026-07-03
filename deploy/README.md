# 本地启动手册

本地完整启动路径分三层：

```text
Docker: postgres + redis + minio + minio-init + elasticsearch
Host:   knowledge-runtime API, plus worker only for ingestion
Host:   knowledge-adapter
Host:   auth + file + ai-gateway + qa + document + gateway + frontend
```

`services/parser` 已由 Knowledge 的 RAGFlow runtime 方案替代，不再作为本地后端服务启动。

## 直接启动

先安装 Docker、Go `1.25.x`、uv、Bun、`psql` 客户端和 `curl`。
Go 必须安装在实际运行这些脚本的宿主机环境中；如果使用 WSL 启动脚本，Windows
里的 Go 不等于 WSL 里的 Go。

源选择采用新策略：仓库默认配置保持官方源，国内网络通过显式 `--china` 切换镜像。
旧的大陆优先默认镜像契约已废弃；默认文件不再提交 active 第三方镜像值。

默认使用官方源。`deploy/.env.example` 里的 Docker、uv 和 Go 默认值分别指向
Docker Hub pinned images、PyPI、`proxy.golang.org` 和 `sum.golang.org`。中国大陆网络
如果下载慢，给本地脚本加 `--china`，只在本次进程使用大陆镜像，不改写
`deploy/.env`：

```bash
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china
```

`dev-up.sh --china` 会一并准备 Knowledge runtime 的 Python 依赖和 GitHub release/raw、
NLTK、HuggingFace、Tika、Chrome 等 artifact 下载。重复启动或只想拉起 infra 时可加
`--skip-knowledge-runtime-deps`，之后需要时再手工补跑 runtime 下载脚本。

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh

cd apps/web
bun install
bun run dev
```

中国大陆网络按同一顺序启动，但给会下载 Docker/Go/uv/runtime artifact 的脚本加
`--china`：

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china

cd apps/web
bun install
bun run dev
```

日常再次启动时，已经执行过 `bun install` 可以直接：

```bash
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
cd apps/web && bun run dev
```

中国大陆网络日常启动同样使用：

```bash
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china
cd apps/web && bun run dev
```

如果要运行真实 Knowledge 解析、embedding、索引和检索链路，先在本地
`deploy/.env` 中填写 provider。Elasticsearch 是默认本地 infra，由
`./scripts/local/dev-up.sh` 启动：

```text
DOC_ENGINE=elasticsearch
KNOWLEDGE_RUNTIME_ES_URL=http://127.0.0.1:9200
KNOWLEDGE_AUTO_START_INGESTION=true
```

然后启动 host-run runtime/adapter：

```bash
./scripts/local/run-knowledge-parse-stack.sh
```

如果只验证已构建知识库的查询链路，启动 API-only runtime 即可：

```bash
./scripts/local/run-knowledge-runtime-api.sh
```

该命令使用 `services/knowledge-runtime` 的 base dependency profile，不启动
`knowledge-runtime-worker`。真实上传解析链路可以继续使用
`./scripts/local/run-knowledge-parse-stack.sh` 一次性启动 full parse stack；如果
只运行 API-only runtime + Go 后端，本地 `KNOWLEDGE_RUNTIME_WORKER_START_COMMAND`
会在首次上传需要解析时调用 `./scripts/local/start-knowledge-runtime-worker.sh`
按需启动 worker。

中国大陆网络下，对会下载 runtime model artifact 的 runtime 启动命令也显式加
`--china`：

```bash
./scripts/local/run-knowledge-parse-stack.sh --china
```

停止：

```bash
# 在运行 bun run dev 的终端按 Ctrl-C 停前端。
./scripts/local/stop-backend.sh
```

`stop-backend.sh` 只管理 `.local/run/` 记录的后端、Knowledge runtime 和
Knowledge adapter 进程组；前端 Vite dev server 是前台命令，需要在对应终端按
`Ctrl-C` 单独停止。如果忘记前端在哪个终端运行，先查默认端口再杀对应进程：

```bash
lsof -nP -iTCP:5173 -sTCP:LISTEN
kill <pid>
```

清空本地 infra 数据：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v
```

彻底冷启动时，如果还要删除本项目本地容器、卷和镜像，先确认没有其他项目依赖这些镜像：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v --remove-orphans

set -a
source deploy/.env
set +a
docker rmi \
  "${POSTGRES_IMAGE:-postgres:16-alpine}" \
  "${REDIS_IMAGE:-redis:7-alpine}" \
  "${MINIO_IMAGE:-minio/minio:RELEASE.2025-09-07T16-13-09Z}" \
  "${MINIO_MC_IMAGE:-minio/mc:RELEASE.2025-08-13T08-35-41Z}" \
  "${KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE:-docker.elastic.co/elasticsearch/elasticsearch:8.15.3}" \
  2>/dev/null || true
```

## 配置来源

`deploy/.env.example` 是唯一默认配置来源。用户只复制一次：

```bash
cp deploy/.env.example deploy/.env
```

脚本不会生成、改写或维护另一套默认变量。它们只读取 `deploy/.env`，让宿主机
Go 进程拿到同一份本地配置。默认使用官方源。Go modules 下载默认读取
`deploy/.env` 里的 `GOPROXY` / `GOSUMDB`；`dev-up.sh` 执行 goose migration、
`run-backend.sh` 启动 Go 后端时都会把这两个变量传给 host-run Go 命令。官方默认值是
`GOPROXY=https://proxy.golang.org,direct` 和 `GOSUMDB=sum.golang.org`。

中国大陆网络使用大陆镜像时，不要把仓库默认文件改成第三方代理；运行
`./scripts/local/dev-up.sh --china` 和 `./scripts/local/run-backend.sh --china` 即可。
如果已有旧 `deploy/.env` 仍保留 TUNA、DaoCloud、`goproxy.cn` 或
`sum.golang.google.cn`，脚本会继续尊重这些本地值并提示你这是本地覆盖。想回到官方
默认值，重新复制 `deploy/.env.example` 后再恢复私有配置，或手动改回官方地址。

默认 demo 账号：

```text
admin / LocalDemoAdmin#12345
superadmin / LocalDemoAdmin#12345
```

Docker 镜像默认使用 Compose 里的 Docker Hub pinned tags。中国大陆网络可用
`./scripts/local/dev-up.sh --china` 在本次进程切换到 DaoCloud registry rewrite；企业
镜像仓库可在本机 `deploy/.env` 设置 `POSTGRES_IMAGE`、`REDIS_IMAGE`、
`MINIO_IMAGE`、`MINIO_MC_IMAGE` 和 `KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE`，
但不要提交为默认值。

`UV_DEFAULT_INDEX` 控制宿主机 uv 在解析或重锁依赖时使用的 Python 包索引，默认使用
官方 PyPI。`services/parser` 已退役，默认路径不再准备 standalone Parser；解析、切块、
embedding、索引和检索通过 `services/knowledge-runtime` 完成。中国大陆网络运行
`./scripts/local/dev-up.sh --china` 时会自动准备 runtime 依赖并下载
NLTK/HuggingFace/GitHub release/raw artifacts。如果此前用
`--skip-knowledge-runtime-deps` 跳过，或只想手工补跑 runtime 下载脚本，使用：

```bash
cd services/knowledge-runtime
uv run --no-project \
  --with "nltk>=3.9.4" \
  --with "huggingface-hub>=1.3.1" \
  ragflow_deps/download_deps.py --china
```

该命令会用临时 overlay 把 runtime 依赖同步到 `.venv`，并把 GitHub release、PyPI、
NLTK、HuggingFace、Tika 和 Chrome 下载切到镜像；提交的 `pyproject.toml` / `uv.lock`
仍保持官方 URL。需要企业源时，显式设置本机环境变量或本机 `deploy/.env`。

`HF_ENDPOINT` 控制 Knowledge runtime worker 首次加载 deepdoc OCR/vision 模型时访问
HuggingFace 的地址。提交的默认配置不启用第三方 mirror；如果在中国大陆网络运行真实
解析栈，使用 `./scripts/local/run-knowledge-parse-stack.sh --china`，脚本会在本次进程
内为未设置 `HF_ENDPOINT` 的环境使用 `https://hf-mirror.com`。企业环境也可以只在本机
`deploy/.env` 中设置内部 HuggingFace mirror，不要提交真实或第三方默认值。

`GOPROXY` 和 `GOSUMDB` 控制宿主机 Go 工具链下载模块和校验数据库的路径，默认使用
`https://proxy.golang.org,direct` 和 `sum.golang.org`，用于 `dev-up.sh` 中的
goose migration 以及 `run-backend.sh` 中的 Go 服务 `go run`。如果在中国大陆网络看到
`Get "https://proxy.golang.org/...": i/o timeout`，重新运行对应脚本并加 `--china`。
无法访问公开镜像的企业环境可以在本机 `deploy/.env` 中改为企业 Go proxy / checksum DB。

## 脚本职责

`./scripts/local/dev-up.sh`：

- 校验 `deploy/docker-compose.yml`。
- 先检查同一宿主机环境中的 Docker、Go、`psql`、`uv`（仅 `--china` runtime 准备需要），缺失时直接
  在命令行报错，避免跑到 migration/seed 中途才失败。
- 传入 `--china` 时自动执行 Knowledge runtime 依赖和 artifact 下载；可用
  `--skip-knowledge-runtime-deps` 或 `LOCAL_SKIP_KNOWLEDGE_RUNTIME_DEPS=1` 跳过。
- 拉取 infra 镜像，默认启动并等待 `postgres`、`redis`、`minio`、`elasticsearch`
  Compose health checks 通过。
- 单独运行一次性 `minio-init` 创建/校验 `software-teamwork-local` 和
  `software-teamwork-knowledge` bucket；`minio-init` 正常退出不会阻断后续
  migration/seed，非零失败时会提示查看 `docker compose logs minio-init`。
- 在宿主机执行各服务 goose migration。
- migration 前检查有效 Go module 配置；默认使用官方 `GOPROXY` / `GOSUMDB`，
  传入 `--china` 时本次进程改用大陆镜像。旧 `deploy/.env` 仍含镜像值时，脚本会尊重
  本地覆盖并提示。
- 用 `psql` 依次应用本地 demo 数据、AI Gateway profile 和 QA Document MCP
  注册 seed。Document MCP seed 只保存 endpoint/alias 等非敏感元数据；token 来自
  `deploy/.env` 的 `MCP_SERVER_TOKEN`。
- 命令行会输出彩色的开始、成功、警告和失败摘要；失败摘要包含当前阶段和后续排查入口。
  `NO_COLOR=1` 可关闭颜色，`FORCE_COLOR=1` 可在非 TTY 中强制开色。

`./scripts/local/run-backend.sh`：

- 启动任何服务前，先在每个 Go 服务目录执行 `go mod download` 预检模块下载。优先用
  当前 `deploy/.env` 的 `GOPROXY` / `GOSUMDB`；默认是官方源，`--china` 则只在本次
  进程使用大陆镜像。下载失败或超时时直接在终端打印错误和修复提示，不再继续伪装成
  后端已启动。
- 按顺序启动 `auth`、`file`、`knowledge`、`ai-gateway`、`qa`、`document`、`gateway`。
- Knowledge 运行 `cmd/adapter`，并通过 `VENDOR_RUNTIME_URL` 调用 RAGFlow runtime。
  如果已经先用 `run-knowledge-parse-stack.sh` 启动了 `knowledge-adapter`，本脚本会复用
  现有 adapter，不再重复绑定 `8083`。
- Go 服务启动使用宿主机 `go run`；Go 模块下载走 `deploy/.env` 里的
  `GOPROXY` / `GOSUMDB`，不是 Docker registry，也不是 `UV_DEFAULT_INDEX`。
- 服务 fork 后默认观察 8 秒；若某个进程组很快退出，脚本会汇总对应
  `.local/logs/<service>.log` 尾部并以非零状态退出。可用
  `LOCAL_BACKEND_STARTUP_CHECK_SECONDS` 调整观察窗口。
- 日志写入 `.local/logs/`，进程组 leader PID 写入 `.local/run/`，供
  `stop-backend.sh` 停掉 `go run` 及其子进程。
- Linux 有 `setsid` 时脚本直接用 `setsid` 管理进程组；macOS 等没有 `setsid`
  的环境会自动使用 `python3` 的 `os.setsid()` fallback，所以同一套命令可在本机
  zsh 环境运行。
- 命令行会输出彩色的开始、成功、警告和失败摘要；失败时优先按终端提示处理，再查看
  服务日志。`NO_COLOR=1` 可关闭颜色，`FORCE_COLOR=1` 可在非 TTY 中强制开色。

`./scripts/local/stop-backend.sh`：

- 按 `.local/run/*.pid` 停止 host-run 后端进程组并清理 pid 文件。
- 即使没有 `.local/run/` 或没有 pid 文件，也会明确输出“nothing to stop”并成功退出。
- 命令行会输出彩色的开始、成功、警告和失败摘要；失败时检查 `.local/run/*.pid` 和残留进程。

## Knowledge / RAGFlow

Knowledge 文档上传、解析、切块、embedding、索引和检索通过 RAGFlow runtime 完成。
默认本地栈只启动 Knowledge adapter；真实 ingestion/retrieval 需要显式启动 runtime
及其外部依赖。本地 Knowledge adapter 读取：

```text
VENDOR_RUNTIME_URL=http://127.0.0.1:9380
VENDOR_RUNTIME_SERVICE_TOKEN=local-dev-runtime-service-token-change-me
KNOWLEDGE_RUNTIME_SERVICE_TOKEN=local-dev-runtime-service-token-change-me
KNOWLEDGE_RUNTIME_READINESS_MODE=query
KNOWLEDGE_AUTO_START_INGESTION=true
```

runtime API 在宿主机启动；runtime worker 默认不随后端启动，Knowledge adapter
只会在 `KNOWLEDGE_RUNTIME_WORKER_START_COMMAND` 已配置且缺少 worker heartbeat
时通过该受控入口按需触发 worker，并等到 heartbeat 后再调用
`/documents/parse` 入队；本地默认命令为
`${SOFTWARE_TEAMWORK_ROOT}/scripts/local/start-knowledge-runtime-worker.sh`，
该 helper 会在队列空闲 `KNOWLEDGE_RUNTIME_WORKER_IDLE_SHUTDOWN_SECONDS` 后停止
worker。默认值是 300 秒；设置为 `0` 可禁用本地 idle shutdown。
生产环境应替换为 systemd、K8s、supervisor 或同类受控入口管理 worker。
Kubernetes/KEDA 示例见
[`deploy/k8s/knowledge-runtime-worker-keda.example.yaml`](./k8s/knowledge-runtime-worker-keda.example.yaml)。
本地默认 adapter 使用 `http://127.0.0.1:9380`。tenant-scoped runtime API 需要
`X-Service-Token`，由 Knowledge adapter 使用 `VENDOR_RUNTIME_SERVICE_TOKEN`
自动转发。不要再启动 `services/parser`，也不要把 runtime 放回根级 Compose。

启用真实 ingestion 前，需要先准备：

- 从默认配置复制本地环境文件：

  ```bash
  cp deploy/.env.example deploy/.env
  ```

- 在 `deploy/.env` 中配置 AI Gateway embedding/rerank profile。外部 provider
  API key 只放入 AI Gateway local seed 或管理 API，不放入 Knowledge runtime。
  推荐配置：

  ```text
  KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN=local-dev-internal-service-token-change-me
  KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=AI_GATEWAY
  KNOWLEDGE_RUNTIME_EMBEDDING_MODEL=BAAI/bge-m3
  KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL=http://127.0.0.1:8086/internal/v1
  KNOWLEDGE_RUNTIME_RERANK_FACTORY=AI_GATEWAY
  KNOWLEDGE_RUNTIME_RERANK_MODEL=BAAI/bge-reranker-v2-m3
  KNOWLEDGE_RUNTIME_RERANK_BASE_URL=http://127.0.0.1:8086/internal/v1
  KNOWLEDGE_VENDOR_EMBEDDING_ID=BAAI/bge-m3@default@AI_GATEWAY
  KNOWLEDGE_VENDOR_RERANK_ID=BAAI/bge-reranker-v2-m3@default@AI_GATEWAY
  KNOWLEDGE_AUTO_START_INGESTION=true
  DOC_ENGINE=elasticsearch
  KNOWLEDGE_RUNTIME_ES_URL=http://127.0.0.1:9200
  ```

  direct provider（例如 `SILICONFLOW`）仍可通过显式配置
  `KNOWLEDGE_RUNTIME_MODEL_API_KEY` 使用，但这会绕过 AI Gateway invocation
  审计和 usage 聚合，只应作为本地/应急路径。

- 如果希望 AI Gateway 的本地默认 profile 也自动指向同一组 provider，在
  `deploy/.env` 中额外填写一组本地 seed 变量，并显式设置
  `AI_GATEWAY_LOCAL_SEED_ENABLED=true`。只有启用该开关时，
  `./scripts/local/dev-up.sh` 才会在 SQL demo seed 后运行
  `scripts/local/render_ai_gateway_local_seed.go` 并把生成的 SQL 写入 PostgreSQL。
  overlay 会使用 `AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY` 加密 API key，并更新
  `default-chat`、`default-embedding`、`default-rerank`。chat、embedding、rerank
  三类本地模型变量都需要填写；脚本还会新增或激活 QA `llm_config_versions` 中匹配
  `default-chat` 的版本，避免 QA persisted runtime config 继续请求 placeholder model。

  ```text
  AI_GATEWAY_LOCAL_SEED_ENABLED=true
  AI_GATEWAY_LOCAL_PROVIDER=siliconflow
  AI_GATEWAY_LOCAL_PROVIDER_BASE_URL=https://api.siliconflow.cn/v1
  AI_GATEWAY_LOCAL_PROVIDER_API_KEY=<your provider key>
  AI_GATEWAY_LOCAL_CHAT_MODEL=deepseek-ai/DeepSeek-V3
  AI_GATEWAY_LOCAL_EMBEDDING_MODEL=BAAI/bge-m3
  AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS=1024
  AI_GATEWAY_LOCAL_RERANK_MODEL=BAAI/bge-reranker-v2-m3
  AI_GATEWAY_LOCAL_RERANK_TOP_N=5
  ```

  如果 `DOCUMENT_AI_GATEWAY_MODEL` 仍是 `local-placeholder-chat`，
  `./scripts/local/run-backend.sh` 会在本次 host-run 启动里自动改用
  `AI_GATEWAY_LOCAL_CHAT_MODEL`，避免 Document 请求模型名和 `default-chat`
  profile 不一致。QA 的 persisted LLM 配置由同一个本地 env seed overlay 同步。

- `./scripts/local/run-knowledge-runtime-api.sh` 只启动 host-run runtime API，使用
  API-only dependency profile，不启动 worker 或 Knowledge adapter，适合
  `KNOWLEDGE_RUNTIME_READINESS_MODE=query` 的查询链路验证。
- `./scripts/local/run-knowledge-parse-stack.sh` 启动 host-run runtime API、worker 和
  Knowledge adapter，并生成 `.local/knowledge-runtime/service_conf.yaml` 供 runtime API
  和 worker 使用。它不再直接执行 `docker build` 或 `docker run`；本地
  Elasticsearch 由前一步 `./scripts/local/dev-up.sh` 作为默认根 Compose 基础设施管理。
  如果你要使用已有宿主机或外部 Elasticsearch，把 `KNOWLEDGE_RUNTIME_ES_URL`
  改成实际地址。
- runtime worker 第一次启动会按需下载 `InfiniFlow/deepdoc` 等 deepdoc 模型。中国大陆
  网络用 `./scripts/local/run-knowledge-parse-stack.sh --china` 显式启用
  `HF_ENDPOINT=https://hf-mirror.com` 的本次进程覆盖；按需启动时
  `start-knowledge-runtime-worker.sh` 会在 runtime API 可达时等待 worker heartbeat，
  并在空闲超时后关闭 worker。
  full parse-stack 脚本默认最多等待 300 秒，可用
  `KNOWLEDGE_ADAPTER_READY_TIMEOUT_SECONDS` 调整。

只有可信本地 provider 才可显式设置 `KNOWLEDGE_RUNTIME_ALLOW_EMPTY_MODEL_API_KEY=1`。

## 快速确认

```bash
curl --noproxy '*' -fsS http://localhost:8080/healthz
curl --noproxy '*' -fsS http://localhost:8080/readyz
curl --noproxy '*' -fsS http://localhost:8083/healthz
```

`http://localhost:8083/readyz` 在 runtime 未启动或 runtime 外部依赖未配置时返回
`503 degraded` 是预期行为；真实 ingestion/retrieval 配好后再用它确认 Knowledge
adapter 到 runtime 的链路。

`http://localhost:8086/readyz` 在 placeholder profile 下返回 `503 degraded` 是预期行为，
表示还没配置真实模型 provider credential，不代表 AI Gateway 进程失败。
默认本地模型 profile 的 OpenAI-compatible 地址是 `http://localhost:11434/v1`。如果
`deploy/.env` 设置 `AI_GATEWAY_LOCAL_SEED_ENABLED=true`，`dev-up.sh` 会用
`AI_GATEWAY_LOCAL_*` 覆盖 `default-chat`、`default-embedding`、`default-rerank`
三条默认 profile，并同步激活 QA 的 LLM 配置到同一个 `default-chat` model。
Document MCP 的默认 endpoint 是 `http://localhost:8085/mcp`，QA 将其工具暴露为
`document__<tool>`；完整工具参数和 Agent 工作流见
[Document MCP 工具契约](../docs/services/document/docs/mcp-tools.md)。

## Seed Data

`dev-up.sh` 按顺序应用 `deploy/seeds/001-local-demo-seed.sql`、
`deploy/seeds/002-ai-gateway-model-profiles.sql`、
`deploy/seeds/003-qa-document-mcp.sql` 和
`deploy/seeds/004-qa-default-knowledge-base.sql`。最后一个 seed 会清理
legacy `kb_local_demo` 默认绑定，让 QA 的 `defaultKnowledgeBaseIds` 保持空列表；
空列表表示通过 Knowledge MCP / Knowledge query 搜索所有已索引知识库。如果
`deploy/.env` 填了
`AI_GATEWAY_LOCAL_SEED_ENABLED=true`、`AI_GATEWAY_LOCAL_PROVIDER`、
`AI_GATEWAY_LOCAL_PROVIDER_BASE_URL`、`AI_GATEWAY_LOCAL_PROVIDER_API_KEY`、
`AI_GATEWAY_LOCAL_CHAT_MODEL`、`AI_GATEWAY_LOCAL_EMBEDDING_MODEL`、
`AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS`、`AI_GATEWAY_LOCAL_RERANK_MODEL` 和
`AI_GATEWAY_LOCAL_RERANK_TOP_N`，随后才会通过
`scripts/local/render_ai_gateway_local_seed.go` 生成 SQL 并加密写入本地 AI Gateway
provider credential；未启用该开关时不会应用这个 overlay。它也会激活匹配的 QA
`llm_config_versions` 版本。

Seeded local resources:

| Area | Deterministic resource |
| --- | --- |
| Auth | user `usr_local_admin`, username `admin`, password `LocalDemoAdmin#12345`, role `admin` |
| Auth permissions | `admin:model-profile:write`, `admin:parser-config:write`, `qa:settings:read`, and `qa:settings:write` |
| Knowledge | knowledge base `kb_local_demo`, document `doc_local_demo_seed`, chunk `chunk_local_demo_seed_001` |
| Document | material `22222222-2222-4222-8222-222222222201`, report `22222222-2222-4222-8222-222222222301`, outline `22222222-2222-4222-8222-222222222401` |
| AI Gateway | optional placeholder profiles `default-chat`, `default-embedding`, and `default-rerank`; `.env` local seed can replace their provider `base_url` and encrypted `api_key` |
| QA | conversation `33333333-3333-4333-8333-333333333301`, user message `33333333-3333-4333-8333-333333333401`, assistant message `33333333-3333-4333-8333-333333333402`; default KB list intentionally empty for global Knowledge MCP search |

AI Gateway 的静态 seed 不保存真实 provider key，只保存可识别的本地 placeholder。
需要本机真实 provider 时，在 `deploy/.env` 中设置：

```bash
AI_GATEWAY_LOCAL_SEED_ENABLED=true
AI_GATEWAY_LOCAL_PROVIDER=siliconflow
AI_GATEWAY_LOCAL_PROVIDER_BASE_URL=https://api.siliconflow.cn/v1
AI_GATEWAY_LOCAL_PROVIDER_API_KEY=<local-provider-api-key>
AI_GATEWAY_LOCAL_CHAT_MODEL=deepseek-ai/DeepSeek-V3
AI_GATEWAY_LOCAL_EMBEDDING_MODEL=BAAI/bge-m3
AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS=1024
AI_GATEWAY_LOCAL_RERANK_MODEL=BAAI/bge-reranker-v2-m3
AI_GATEWAY_LOCAL_RERANK_TOP_N=5
```

再次运行 `./scripts/local/dev-up.sh` 后，脚本会用
`AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY` 加密 provider key，更新 AI Gateway 默认
profiles，并新增/激活一条 QA `llm_config_versions` 记录，使 QA 请求的 model 与
`default-chat` profile 一致。生成的 SQL 不应提交，真实 key 只留在本机 `.env`。

`minio-init` 会创建两个本地 bucket：`software-teamwork-local` 供 File service
使用，`software-teamwork-knowledge` 供 RAGFlow Knowledge runtime 使用。

`001-local-demo-seed.sql` 里的本地管理员密码 hash 是
`LOCAL_ADMIN_PASSWORD=LocalDemoAdmin#12345` 对应的 `argon2id` PHC 字符串，
参数为 `m=65536`、`t=3`、`p=2`、16-byte salt、32-byte key。轮换本地密码时，
需要一起更新 `deploy/.env.example`、`001-local-demo-seed.sql` 和本文档，然后重新
运行 `./scripts/local/dev-up.sh` 让 host-run seed SQL 生效。不要把 demo 密码或 hash 用在共享环境或长期环境。

## 排障入口

- Docker 拉取慢、registry rewrite、daemon mirror、proxy 和 WSL 内存：
  [docs/runbooks/docker-image-pull-environment.md](../docs/runbooks/docker-image-pull-environment.md)
- 本地联调顺序、端口和故障判断：
  [docs/runbooks/local-integration.md](../docs/runbooks/local-integration.md)

后端启动后，可以通过 Gateway 确认 seed 管理员和权限：

```bash
curl --noproxy '*' -fsS http://localhost:8080/api/v1/sessions \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"LocalDemoAdmin#12345"}'
```

响应应包含 `admin` 角色，以及 `admin:model-profile:write`、
`admin:parser-config:write`、`qa:settings:read` 等本地演示权限。拿到 token 后，
可以请求 `/api/v1/admin/parser-configs` 或 `/api/v1/admin/model-profiles` 确认
Gateway 管理路由鉴权正常。

只清理本地 demo seed 数据：

```bash
set -a
source deploy/.env
set +a
psql "$POSTGRES_ADMIN_URL" -v ON_ERROR_STOP=1 -f deploy/seeds/099-local-demo-cleanup.sql
```

完整重置本地 infra 数据：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v
./scripts/local/dev-up.sh
```

AI Gateway 的本地 placeholder profile 只用于 readiness 检查，里面不是可用的真实
provider key。真正调用模型前，需要通过管理端配置真实 provider credential，或在
`deploy/.env` 中启用 `AI_GATEWAY_LOCAL_SEED_ENABLED=true` 的本地 overlay。默认
OpenAI-compatible 地址是 `http://localhost:11434/v1`。

## Request ID 排障

所有服务都会返回或透传 `X-Request-Id`。

```bash
rid=req_local_debug_001
curl --noproxy '*' -fsS http://localhost:8080/readyz -H "X-Request-Id: ${rid}"
rg "${rid}" .local/logs
```

前端问题先记录响应里的 `requestId` 或 `X-Request-Id`，再查 Gateway 日志。如果
Gateway 报依赖错误，用同一个 id 继续查 owner service 日志。

## Knowledge 集成确认

Knowledge 主路径通过 Gateway 暴露：

```bash
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-bases \
  -H "Authorization: Bearer ${token}" \
  -H "X-Request-Id: req_knowledge_local_001"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-bases/kb_local_demo/documents \
  -H "Authorization: Bearer ${token}"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/documents/<documentId>/chunks \
  -H "Authorization: Bearer ${token}"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-queries \
  -H "Authorization: Bearer ${token}" \
  -H 'Content-Type: application/json' \
  -d '{"query":"local demo","topK":3}'
```

Knowledge 路由依赖 `VENDOR_RUNTIME_URL` 指向 RAGFlow runtime。runtime 负责 PDF
解析、切块、embedding、索引和检索；不要再启动 `services/parser`。

宿主机启动 runtime API；查询-only 用 API helper，需要真实 ingestion 时启动完整
parse stack：

```bash
cp deploy/.env.example deploy/.env
# Edit deploy/.env with AI_GATEWAY runtime embedding/rerank variables,
# optional explicit direct-provider fallback variables,
# optional AI_GATEWAY_LOCAL_PROVIDER_* seed variables,
# KNOWLEDGE_AUTO_START_INGESTION=true, and DOC_ENGINE=elasticsearch.
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-runtime-api.sh
./scripts/local/run-knowledge-parse-stack.sh
```

更多 runtime 说明见 [services/knowledge-runtime/README.md](../services/knowledge-runtime/README.md)。

## Common Dependency Failures

| Symptom | Likely cause | Check |
| --- | --- | --- |
| `gateway /readyz` returns `503 dependency_error` | Redis, auth, or required owner service base URL configuration is not ready | `docker compose ps`, service logs under `.local/logs/` |
| `auth /readyz` returns `postgres unavailable` | Auth migration or PostgreSQL failed | `docker compose logs postgres`; check `.local/logs/auth.log` |
| Knowledge upload/query returns `502 dependency_error` | RAGFlow runtime unreachable or ES/MinIO not ready | Check `VENDOR_RUNTIME_URL`, runtime API, worker, Elasticsearch, and MinIO |
| Knowledge adapter `/readyz` says `task executor heartbeat is missing` | Runtime worker exited or is still downloading deepdoc models | Check `.local/logs/knowledge-runtime-worker.log`; for mainland China rerun `./scripts/local/run-knowledge-parse-stack.sh --china` or set a local/internal `HF_ENDPOINT` |
| QA message call fails on model invocation | AI Gateway profile is not running, fake local credential is still in use, or host provider is not listening on `host.docker.internal:11434` | Check `.local/logs/ai-gateway.log` and `.local/logs/qa.log` |
| MinIO bucket missing | `minio-init` did not complete or one of `software-teamwork-local` / `software-teamwork-knowledge` is absent | `docker compose logs minio minio-init` |
| Host port conflict | Another local process or old host-run/tmux session uses a default port | `lsof -nP -iTCP:<port> -sTCP:LISTEN`; stop the old process or change the matching `*_PORT` in `deploy/.env` |

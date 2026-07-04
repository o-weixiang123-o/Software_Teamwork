# 配置管理层

`config/` 是仓库默认配置的唯一来源，也是配置权威（configuration authority）。
它只管理可提交的默认值、环境 profile 覆盖和 secret 引用；真实密钥不进入 Git。

服务本身仍读取环境变量。启动脚本会先用 `config/ctl` 合并 profile 和本地
secret，再把结果渲染到 `.local/config/`，供 Docker Compose 和宿主机进程共同使用。

## 文件职责

```text
config/
├── schema.yaml          # profile 契约和 secret 策略说明
├── base.yaml            # 所有环境共享的非敏感默认值和 secret 引用
├── dev.yaml             # 本地开发覆盖
├── staging.yaml         # 预发覆盖，只提交非敏感值和 secret 引用
├── production.yaml      # 生产覆盖，只提交非敏感值和 secret 引用
└── ctl/                 # 校验和渲染工具
```

根目录的 `.env.example` 是本地 secret 模板。开发者复制成未跟踪的
`.env.local` 后填写本机密钥、provider key、PaddleOCR token 或私有镜像覆盖。

## 解析优先级

最终运行时环境按以下顺序合并，后者覆盖前者：

```text
代码内默认值
< config/base.yaml
< config/{profile}.yaml
< .env.local 或其他未跟踪 dotenv secret 文件
< CI/CD、容器平台或 shell 注入的环境变量
< 脚本显式参数，例如 --china
```

常规本地 profile 是 `dev`。渲染产物在：

```text
.local/config/dev.env      # Docker Compose --env-file 使用
.local/config/dev.env.sh   # host-run 脚本 source 使用
```

这些 `.local/config/` 文件是运行时产物，不提交。

## 本地使用

第一次启动：

```bash
cp .env.example .env.local
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
cd apps/web && bun install && bun run dev
```

中国大陆网络使用显式镜像模式：

```bash
./scripts/local/dev-up.sh --china
./scripts/local/run-backend.sh --china
```

`--china` 只影响本次进程：Docker registry rewrite、Go proxy/checksum DB、
uv/Python 包索引、Knowledge runtime artifact 下载等都会临时切到大陆镜像。
脚本不会改写 `config/` 或 `.env.local`。

手动渲染当前本地配置：

```bash
CONFIG_SECRET_FILE=.env.local ./scripts/config/load-profile.sh --print-compose-env
```

使用其他 profile：

```bash
CONFIG_PROFILE=staging CONFIG_SECRET_FILE=.env.staging \
  ./scripts/config/load-profile.sh --print-compose-env
```

## Secret 规则

可以提交：

- 非敏感默认值，例如端口、服务 URL、profile id、官方镜像 tag；
- `fromEnv` secret 引用名；
- 本地 demo 占位值，例如 `local-dev-...-change-me`；
- 说明性注释和占位符。

不能提交：

- 真实 API key、bearer token、password、service token；
- 带账号密码的 DSN；
- 私钥、证书或 PEM；
- 生产 provider credential；
- 个人代理、私有 endpoint 或企业内部 secret。

生产和预发环境应由 CI/CD masked variables、Kubernetes Secret、Vault、
SOPS、External Secrets 或云厂商 secret manager 注入真实值。

## 模型和 OCR 本地配置

AI Gateway 是模型配置权威。本地如果要使用真实 SiliconFlow/OpenAI-compatible
provider，在 `.env.local` 中启用 seed overlay：

```bash
AI_GATEWAY_LOCAL_SEED_ENABLED=true
AI_GATEWAY_LOCAL_PROVIDER=siliconflow
AI_GATEWAY_LOCAL_PROVIDER_BASE_URL=https://api.siliconflow.cn/v1
AI_GATEWAY_LOCAL_PROVIDER_API_KEY=<local-provider-api-key>
AI_GATEWAY_LOCAL_CHAT_MODEL=deepseek-ai/DeepSeek-V4-Flash
AI_GATEWAY_LOCAL_EMBEDDING_MODEL=BAAI/bge-m3
AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS=1024
AI_GATEWAY_LOCAL_RERANK_MODEL=BAAI/bge-reranker-v2-m3
AI_GATEWAY_LOCAL_RERANK_TOP_N=5
AI_GATEWAY_TIMEOUT=120s
```

重新运行 `./scripts/local/dev-up.sh` 后，脚本会把 provider key 加密写入
AI Gateway 本地数据库，并更新 `default-chat`、`default-embedding`、
`default-rerank` profiles。QA 和 Document 可以保持使用 `default-chat`
profile；显式设置 `MODEL_ID` 或 `DOCUMENT_AI_GATEWAY_MODEL` 时必须与 profile
里的模型名完全一致。

Knowledge runtime 的 embedding/rerank 默认通过 AI Gateway：

```bash
KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=AI_GATEWAY
KNOWLEDGE_RUNTIME_EMBEDDING_MODEL=BAAI/bge-m3
KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL=http://127.0.0.1:8086/internal/v1
KNOWLEDGE_RUNTIME_RERANK_FACTORY=AI_GATEWAY
KNOWLEDGE_RUNTIME_RERANK_MODEL=BAAI/bge-reranker-v2-m3
KNOWLEDGE_RUNTIME_RERANK_BASE_URL=http://127.0.0.1:8086/internal/v1
```

PaddleOCR cloud parser 也通过 `.env.local` 配置：

```bash
PADDLEOCR_BASE_URL=https://paddleocr.aistudio-app.com
PADDLEOCR_ACCESS_TOKEN=<local-paddleocr-token>
PADDLEOCR_ALGORITHM=PaddleOCR-VL
PADDLEOCR_AUTH_SCHEME=token
PADDLEOCR_REQUEST_TIMEOUT=900
```

`PADDLEOCR_ACCESS_TOKEN` 是 secret，只能保存在 `.env.local` 或外部 secret
系统中。

## 校验命令

配置工具单元测试：

```bash
cd config/ctl && go test ./...
```

用模板值渲染 dev profile：

```bash
CONFIG_SECRET_FILE=.env.example ./scripts/config/load-profile.sh --print-compose-env
```

校验本地启动脚本和文档契约：

```bash
uv run --no-project --with pytest --with pyyaml python -m pytest scripts/tests -q
python3 scripts/check_docker_policy.py
docker compose -f deploy/docker-compose.yml --env-file .local/config/dev.env config --quiet
```

## 常见反模式

- 不要把真实 key 写进 `config/*.yaml`、`.env.example`、README 或 PR 描述。
- 不要手工维护 `.local/config/*.env`；它们由脚本生成。
- 不要为了网络慢把官方默认源改成大陆镜像；使用 `--china` 或本机未提交覆盖。
- 不要恢复业务服务容器；根 Compose 只负责 PostgreSQL、Redis、MinIO、
  `minio-init` 和 Elasticsearch。

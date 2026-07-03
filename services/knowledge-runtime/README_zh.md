# Knowledge Runtime（裁剪版）

本目录是上游 [RAGFlow](https://github.com/infiniflow/ragflow) 的隔离快照，作为 Knowledge 的 **vendor 运行时** 部署。Go 契约适配器在 `services/knowledge/cmd/adapter`，通过 `VENDOR_RUNTIME_URL` 调用本目录 Python API（`:9380`）。

完整上游信息与 refresh 步骤见 [`UPSTREAM.md`](UPSTREAM.md)。

## 进程

| 进程 | 端口 | 入口 | 职责 |
| --- | --- | --- | --- |
| runtime API | `127.0.0.1:9380` | `api/ragflow_server.py` | 数据集/文档/检索 HTTP API |
| runtime worker | n/a | `rag/svr/task_executor.py` | deepdoc 解析、分块、嵌入（Redis 队列） |

共用 PostgreSQL（`knowledge_system`）、MinIO（`software-teamwork-knowledge`）、受支持的 doc engine（如 Elasticsearch）和 Redis。
上游 RAGFlow MCP server/client 产品面不属于本运行时；项目自有 Knowledge MCP 桥接在 `services/knowledge`。

## 已裁剪的产品面

上游 Web UI、Agent、Admin、Chat、用户注册/登录、Go HTTP 运行时、容器内 nginx、vendor 自带 docker-compose 等已移除。运行时仅在 `X-Service-Token` 匹配 `KNOWLEDGE_RUNTIME_SERVICE_TOKEN` 后才信任 Gateway 注入的 `X-Tenant-Id` / `X-User-Id`。

## 主要目录

| 路径 | 说明 |
|------|------|
| `api/` | Python REST API 与 DB 服务（adapter 调用面） |
| `deepdoc/` | 文档解析器与视觉模型 |
| `rag/` | 分块、嵌入、检索、GraphRAG、任务执行 |
| `conf/` | 宿主机运行时配置（`service_conf.yaml`） |
| `docs/` | parser/RAG 参考文档 |

## 模型配置

默认 embedding/rerank 模型可通过环境变量注入，不要把真实密钥写入仓库：

| 变量 | 说明 |
|------|------|
| `KNOWLEDGE_RUNTIME_SERVICE_TOKEN` | runtime 受保护路由校验的内部 token，需要与 adapter 的 `VENDOR_RUNTIME_SERVICE_TOKEN` 一致 |
| `KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS` | 是否在 Gateway 租户首次访问时自动创建 runtime user/tenant，默认 `true`；设为 `false` 时缺失租户直接返回认证/租户错误，不写入合成数据 |
| `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT` | metadata pushdown 失败后允许内存 fallback 的最大候选文档数，默认 `10000`；超过上限直接失败，避免全量内存过滤 |
| `KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN` | `AI_GATEWAY` factory 调用 AI Gateway internal API 的 service token；也可复用 `AI_GATEWAY_SERVICE_TOKEN` 或 `INTERNAL_SERVICE_TOKEN` |
| `KNOWLEDGE_RUNTIME_AI_GATEWAY_EMBEDDING_PROFILE_ID` | AI Gateway embedding profile，默认 `default-embedding` |
| `KNOWLEDGE_RUNTIME_AI_GATEWAY_RERANK_PROFILE_ID` | AI Gateway rerank profile，默认 `default-rerank` |
| `KNOWLEDGE_RUNTIME_MODEL_API_KEY` | 仅 direct provider factory 使用的 provider API key；`AI_GATEWAY` factory 不需要 runtime 持有外部 provider key |
| `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY` | embedding provider factory，推荐 `AI_GATEWAY`；直连 provider 如 `SILICONFLOW` 只作显式 legacy/local 选择 |
| `KNOWLEDGE_RUNTIME_EMBEDDING_MODEL` | embedding model id |
| `KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL` | embedding provider base URL；`AI_GATEWAY` 时指向 `http://127.0.0.1:8086/internal/v1` |
| `KNOWLEDGE_RUNTIME_RERANK_FACTORY` | rerank provider factory，推荐 `AI_GATEWAY` |
| `KNOWLEDGE_RUNTIME_RERANK_MODEL` | rerank model id |
| `KNOWLEDGE_RUNTIME_RERANK_BASE_URL` | rerank provider base URL；`AI_GATEWAY` 时指向 `http://127.0.0.1:8086/internal/v1` |
| `KNOWLEDGE_RUNTIME_ES_URL` | Elasticsearch 地址，默认 `http://127.0.0.1:9200` |
| `HF_ENDPOINT` | deepdoc 首次加载 OCR/vision 模型时使用的 HuggingFace endpoint；默认不设置第三方 mirror，中国大陆网络用 `run-knowledge-parse-stack.sh --china` 或本地覆盖 |

`deploy/api/run-local.sh` 和 `deploy/worker/run-local.sh` 会在启动前检查 doc
engine 与 embedding provider。推荐用仓库根目录的 `./scripts/local/dev-up.sh`
先通过默认根级 Compose 基础设施启动本地 Elasticsearch（active RAG doc engine），再用
`./scripts/local/run-knowledge-parse-stack.sh` 启动 host-run runtime API、worker 和
Knowledge adapter；该脚本会生成 `.local/knowledge-runtime/service_conf.yaml` 指向
`KNOWLEDGE_RUNTIME_ES_URL`，但不直接执行 Docker build/run。如果使用已有
Elasticsearch，修改 `KNOWLEDGE_RUNTIME_ES_URL`。

runtime worker 第一次导入 deepdoc OCR/vision 模块时会按需下载
`InfiniFlow/deepdoc` 等模型。提交的默认配置不启用第三方 mirror；中国大陆网络使用
`./scripts/local/run-knowledge-parse-stack.sh --china`，脚本会在本次进程内为未设置
`HF_ENDPOINT` 的环境使用 `https://hf-mirror.com`。如果 worker 日志出现
`LocalEntryNotFoundError`、`ConnectTimeout` 或 `InfiniFlow/deepdoc` 下载失败，使用
`--china` 或把本机 `deploy/.env` 改成可达的内部 HuggingFace 镜像。

第一次启用真实解析前，先复制默认环境文件并在 `deploy/.env` 填写 AI Gateway
profile 与 ingestion 配置。外部 provider API key 应写入 AI Gateway profile
seed 或管理 API，不应写入 Knowledge runtime：

```bash
cp deploy/.env.example deploy/.env
```

推荐 AI Gateway 示例：

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

如果显式选择 direct provider（例如 `SILICONFLOW`），需要设置
`KNOWLEDGE_RUNTIME_MODEL_API_KEY` 和对应 provider base URL；这条路径会绕过
AI Gateway 的 invocation 审计和 usage 聚合，只应作为本地/应急选择。

默认依赖和 artifact 下载使用官方 URL。中国大陆网络显式使用镜像模式：

```bash
cd services/knowledge-runtime
uv run --no-project \
  --with "nltk>=3.9.4" \
  --with "huggingface-hub>=1.3.1" \
  ragflow_deps/download_deps.py --china
```

该命令会用临时 uv overlay 将 PyPI 和 GitHub release 下载切到镜像，包括
`en-core-web-sm`，并镜像 NLTK raw GitHub 数据、HuggingFace、Tika、Chrome 和 uv
release artifact。它会写入正常 `.venv` 和 artifact 目录，但不会改写提交中的
`pyproject.toml` 或 `uv.lock`。

## 本地验证

```bash
PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio --with filelock --with ruamel-yaml python -m pytest test/routes/test_config_utils.py test/routes/test_route_registry.py test/routes/test_gateway_auth.py test/routes/test_runtime_dependency_check.py -q
```

## 许可证

Apache License 2.0，详见 [`LICENSE`](LICENSE)。

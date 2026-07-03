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
| `KNOWLEDGE_RUNTIME_MODEL_API_KEY` | embedding/rerank provider API key |
| `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY` | embedding provider factory，例如 `SILICONFLOW` |
| `KNOWLEDGE_RUNTIME_EMBEDDING_MODEL` | embedding model id |
| `KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL` | embedding provider OpenAI-compatible base URL |
| `KNOWLEDGE_RUNTIME_RERANK_FACTORY` | rerank provider factory |
| `KNOWLEDGE_RUNTIME_RERANK_MODEL` | rerank model id |
| `KNOWLEDGE_RUNTIME_RERANK_BASE_URL` | rerank provider base URL |

`deploy/api/run-local.sh` 和 `deploy/worker/run-local.sh` 会在启动前检查 doc
engine 与 embedding provider。根级 Compose 不启动 Elasticsearch，也不启动本地
embedding 服务；启用真实 ingestion 前需要先配置宿主机或外部依赖。

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

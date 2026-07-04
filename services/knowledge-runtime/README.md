# Knowledge Runtime

Host-run runtime for Knowledge document ingestion and retrieval.

This directory owns the Python runtime boundary behind `services/knowledge`:
document parsing, chunking, embedding, indexing, retrieval, and rerank support.
Gateway, Auth, QA, and public Knowledge business APIs live outside this
directory.

## Processes

| Process | Port | Entry | Role |
| --- | --- | --- | --- |
| runtime API | `127.0.0.1:9380` | `api/ragflow_server.py` | Dataset, document, chunk, and retrieval HTTP API |
| runtime worker | n/a | `rag/svr/task_executor.py` | Parse, chunk, embed, and index jobs from Redis |

Both processes use PostgreSQL, Redis, MinIO, and the configured document index
engine. Elasticsearch is the local development default and is started by the
root infrastructure helper.

## 本地开发

正常联调从仓库根目录启动，配置来源是 `config/` 和根 `.env.local`：

```bash
cp .env.example .env.local
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-runtime-api.sh
./scripts/local/run-knowledge-parse-stack.sh
```

embedding 和 rerank 推荐通过 AI Gateway。默认 profile 已在 `config/base.yaml`
中声明；需要真实 provider 时，在 `.env.local` 中配置 `AI_GATEWAY_LOCAL_*`，
再重新运行 `./scripts/local/dev-up.sh` 写入本地 AI Gateway seed。

Knowledge runtime 侧的兼容标签如下：

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

`KNOWLEDGE_RUNTIME_EMBEDDING_MODEL` 和 `KNOWLEDGE_RUNTIME_RERANK_MODEL` 是
RAGFlow runtime 兼容标签。它们应与 AI Gateway profile 中的模型名保持一致；
provider、base URL、credential 和调用审计的权威仍是 AI Gateway profile。

`SILICONFLOW` 等直接 provider factory 只作为显式本地/应急选择保留。它们需要
`KNOWLEDGE_RUNTIME_MODEL_API_KEY`，且绕过 AI Gateway 的调用审计和用量聚合。

### PaddleOCR Cloud 解析

Knowledge runtime 可以通过 PaddleOCR cloud async Job API 解析 PDF，避免加载本地
PaddlePaddle/OCR 模型。默认配置从 `.env.local` 经 `config/ctl` 渲染后进入 runtime
进程：

```text
PADDLEOCR_BASE_URL=https://paddleocr.aistudio-app.com
PADDLEOCR_ACCESS_TOKEN=<local-secret>
PADDLEOCR_ALGORITHM=PaddleOCR-VL
PADDLEOCR_AUTH_SCHEME=token
PADDLEOCR_REQUEST_TIMEOUT=900
```

`PADDLEOCR_ACCESS_TOKEN` 是 secret，不要提交。API 创建数据集时，也可以通过顶层
`parser_config_credentials.paddleocr_cloud` 传入凭据。runtime 会把这些凭据写入
OCR model record，并在 `parser_config` 中只保留类似
`PaddleOCR-VL@PaddleOCR-VL@PaddleOCR` 的模型引用。

The parser implementation is intentionally split into:

- `deepdoc/parser/paddleocr_client.py` for submit/poll/result download.
- `deepdoc/parser/paddleocr_adapter.py` for converting PaddleOCR response
  variants into ordered page/block records.
- `deepdoc/parser/paddleocr_normalizer.py` for cleaning markdown/layout blocks,
  converting HTML tables to pipe tables, preserving formulas, and producing
  semantic sections with page/bbox metadata.
- `deepdoc/parser/paddleocr_parser.py` for wiring the cloud client, adapter,
  normalizer, and legacy RAGFlow section tuple output.
- `rag/llm/ocr_model.py` for runtime model/env configuration.

The data flow is:

```text
PaddleOCR Cloud raw result
  -> PaddleOCR result adapter
  -> Markdown/layout normalizer
  -> semantic sections with metadata
  -> chunker
  -> embedding/index
```

The chunker still receives the existing tuple shape, such as `(text, tag)` or
`(text, block_type, tag)`, so PaddleOCR-specific response fields do not leak
past the parser boundary.

## Dependency Preparation

The worker lazily downloads OCR and vision model artifacts when those modules
are imported. Committed defaults use official artifact sources. On mainland
China networks, run the helper with `--china` or prepare runtime artifacts
manually:

```bash
cd services/knowledge-runtime
uv run --no-project \
  --with "nltk>=3.9.4" \
  --with "huggingface-hub>=1.3.1" \
  ragflow_deps/download_deps.py --china
```

直接调试 runtime 进程时，先从根配置渲染并 source 当前 profile：

```bash
cd services/knowledge-runtime
uv sync --python 3.13 --frozen
export PYTHONPATH=.
cd ../..
CONFIG_SECRET_FILE=.env.local ./scripts/config/load-profile.sh --print-compose-env
set -a && . .local/config/dev.env.sh && set +a
cd services/knowledge-runtime
./deploy/api/run-local.sh
./deploy/worker/run-local.sh
```

## Configuration

- Runtime auth: protected routes require `X-Service-Token` matching
  `KNOWLEDGE_RUNTIME_SERVICE_TOKEN`; the Go adapter sends
  `VENDOR_RUNTIME_SERVICE_TOKEN`.
- Runtime scope: all datasets share `KNOWLEDGE_RUNTIME_SCOPE_ID` and
  `KNOWLEDGE_RUNTIME_INDEX_ID`.
- Object storage: root `minio-init` creates `software-teamwork-knowledge` for
  this runtime.
- Metadata filtering:
  `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT` defaults to `10000`; push-down
  failures above this cap fail clearly.
- Model credentials: the `AI_GATEWAY` provider uses
  `KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN`, `AI_GATEWAY_SERVICE_TOKEN`, or
  `INTERNAL_SERVICE_TOKEN`; it does not use an external provider key.

## Validation

Use targeted Python checks for runtime changes:

```bash
cd services/knowledge-runtime
PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio \
  python -m pytest test/routes test/unit_test/rag/llm -q
```

For code-only syntax checks:

```bash
rg --files services/knowledge-runtime --glob '*.py' \
  --glob '!**/__pycache__/**' --glob '!services/knowledge-runtime/.venv/**' |
  xargs -r python3 -m py_compile
```

## License

This runtime still contains Apache-licensed source files with retained headers.
Keep `LICENSE` while those files remain in the tree.

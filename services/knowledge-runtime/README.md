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

## Local Development

Use the root helpers for normal integration work:

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-runtime-api.sh
./scripts/local/run-knowledge-parse-stack.sh
```

Preferred embedding and rerank calls go through AI Gateway:

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

Direct provider factories such as `SILICONFLOW` remain available only by
explicit local or emergency choice. They require
`KNOWLEDGE_RUNTIME_MODEL_API_KEY` and bypass AI Gateway invocation audit and
usage aggregation.

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

Manual process startup for direct runtime debugging:

```bash
cd services/knowledge-runtime
uv sync --python 3.13 --frozen
export PYTHONPATH=.
set -a && . ../../deploy/.env && set +a
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

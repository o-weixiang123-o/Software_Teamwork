# Knowledge Runtime

First-class RAG/deepdoc runtime (formerly `services/knowledge/vendor/ragflow-runtime`).
The Knowledge **contract adapter** lives separately in `services/knowledge/cmd/adapter`.

## Processes (Tier 2 split)

| Process | Port | Entry | Role |
| --- | --- | --- | --- |
| runtime API | `127.0.0.1:9380` | `api/ragflow_server.py` | Dataset/document/search HTTP API |
| runtime worker | n/a | `rag/svr/task_executor.py` | deepdoc parse, chunk, embed (Redis queue) |

Both share PostgreSQL (`knowledge_system`), MinIO (`software-teamwork-knowledge`),
a supported doc engine such as Elasticsearch, and Redis.
The upstream RAGFlow MCP server/client product surface is intentionally not part
of this runtime; the project-owned Knowledge MCP bridge lives in
`services/knowledge`.

## Local development

The root helpers are the preferred local path. Elasticsearch is the default
local doc-engine infrastructure for the active RAG chain; the runtime helper
starts only host-run processes and connects to the configured Elasticsearch URL.

```bash
cp deploy/.env.example deploy/.env
# Edit deploy/.env with the provider and ingestion variables below.
./scripts/local/dev-up.sh
./scripts/local/run-knowledge-runtime-api.sh
./scripts/local/run-knowledge-parse-stack.sh
```

`run-knowledge-runtime-api.sh` is the API-only/query-ready path. It installs the
base runtime dependencies with `uv sync --python 3.13 --frozen
--no-default-groups` and starts only `api/ragflow_server.py`. It does not start
the parser worker, so it is suitable for query-only validation after a
knowledge base has already been built.

`run-knowledge-parse-stack.sh` is the full ingestion path. It installs the
worker profile with `uv sync --python 3.13 --frozen --group worker`, starts the
runtime API, starts `rag/svr/task_executor.py`, and starts the Knowledge adapter.

For SiliconFlow local parsing, set these values in `deploy/.env` before running
the helper:

```text
KNOWLEDGE_RUNTIME_MODEL_API_KEY=<your SiliconFlow key>
KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=SILICONFLOW
KNOWLEDGE_RUNTIME_EMBEDDING_MODEL=BAAI/bge-m3
KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL=https://api.siliconflow.cn/v1
KNOWLEDGE_RUNTIME_RERANK_FACTORY=SILICONFLOW
KNOWLEDGE_RUNTIME_RERANK_MODEL=BAAI/bge-reranker-v2-m3
KNOWLEDGE_RUNTIME_RERANK_BASE_URL=https://api.siliconflow.cn/v1
KNOWLEDGE_VENDOR_EMBEDDING_ID=BAAI/bge-m3@default@SILICONFLOW
KNOWLEDGE_VENDOR_RERANK_ID=BAAI/bge-reranker-v2-m3@default@SILICONFLOW
KNOWLEDGE_AUTO_START_INGESTION=true
DOC_ENGINE=elasticsearch
KNOWLEDGE_RUNTIME_ES_URL=http://127.0.0.1:9200
```

Run `./scripts/local/dev-up.sh` to start the default root Compose infrastructure,
including the pinned local `elasticsearch` service. The runtime helper writes a
config overlay to `.local/knowledge-runtime/service_conf.yaml` so the runtime API
and worker use the configured Elasticsearch URL. To use an existing Elasticsearch
instead, point `KNOWLEDGE_RUNTIME_ES_URL` at that instance.

The runtime worker lazily downloads deepdoc OCR/vision model artifacts from
HuggingFace the first time those modules are imported. Committed defaults use
official HuggingFace behavior. On mainland China networks, run
`./scripts/local/run-knowledge-parse-stack.sh --china`; if `HF_ENDPOINT` is
unset, that command uses `https://hf-mirror.com` for the current process only.
You can also set an internal HuggingFace mirror in local `deploy/.env`.

Manual process startup is still supported when debugging the runtime directly:

```bash
cd services/knowledge-runtime
uv sync --python 3.13 --frozen --no-default-groups
export PYTHONPATH=.
set -a && . ../../deploy/.env && set +a
./deploy/api/run-local.sh
```

Worker/full ingestion requires the worker dependency group:

```bash
cd services/knowledge-runtime
uv sync --python 3.13 --frozen --group worker
export PYTHONPATH=.
set -a && . ../../deploy/.env && set +a
./deploy/worker/run-local.sh
```

Official package and artifact URLs are the committed default. For mainland
China networks, prepare dependencies and runtime artifacts with explicit mirror
mode:

```bash
cd services/knowledge-runtime
uv run --no-project \
  --with "nltk>=3.9.4" \
  --with "huggingface-hub>=1.3.1" \
  ragflow_deps/download_deps.py --china
```

That command uses a temporary uv project overlay for mirrored PyPI and GitHub
release downloads, including `en-core-web-sm`, and mirrors NLTK raw GitHub data,
HuggingFace, Tika, Chrome, and uv release artifacts. Deepdoc model repositories
are written into `rag/res/deepdoc`, which is the worker's runtime load path. If
the HuggingFace client cannot use mirror HEAD metadata, the script falls back to
direct file downloads from the selected endpoint. It
writes into the normal `.venv` and artifact directories but does not rewrite committed
`pyproject.toml` or `uv.lock`.

Adapter (separate module):

```bash
cd services/knowledge
set -a && . ../../deploy/.env && set +a
go run ./cmd/adapter
```

## Configuration

- Local dev: edit `conf/service_conf.yaml` and point hosts at localhost
- Runtime auth: tenant-scoped API routes require `X-Service-Token` matching
  `KNOWLEDGE_RUNTIME_SERVICE_TOKEN`; the Go adapter sends
  `VENDOR_RUNTIME_SERVICE_TOKEN`.
- Gateway tenant bridge: `KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS` defaults to
  `true` for local compatibility. Set it to `false` to reject missing runtime
  tenants instead of creating Gateway-derived user/tenant rows during auth.
- Metadata filtering: `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT` defaults to
  `10000`; push-down failures above this cap fail clearly instead of loading an
  unbounded metadata set into memory.
- Object storage: root `minio-init` creates both `software-teamwork-local`
  (File service) and `software-teamwork-knowledge` (Knowledge runtime).
- HuggingFace model downloads: `HF_ENDPOINT` is not set by committed defaults.
  If the worker exits with `InfiniFlow/deepdoc`, `LocalEntryNotFoundError`, or
  `ConnectTimeout`, rerun `./scripts/local/start-knowledge-runtime-worker.sh --china`
  or `./scripts/local/run-knowledge-parse-stack.sh --china` on mainland China
  networks, or point `HF_ENDPOINT` at a reachable internal mirror in local
  `deploy/.env`. If you prefetch artifacts with `ragflow_deps/download_deps.py
  --china`, verify `rag/res/deepdoc` contains the model files before starting the
  worker.
- NLTK tokenizer data: worker startup exports `NLTK_DATA` to
  `ragflow_deps/nltk_data` and fails fast when `punkt_tab` or `wordnet` is
  missing. Run `ragflow_deps/download_deps.py --china` or set `NLTK_DATA` to a
  provisioned directory before ingestion.
- Model credentials: set `KNOWLEDGE_RUNTIME_MODEL_API_KEY` in your local shell or
  untracked env file. Use `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY`,
  `KNOWLEDGE_RUNTIME_EMBEDDING_MODEL`, `KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL`,
  `KNOWLEDGE_RUNTIME_RERANK_FACTORY`, `KNOWLEDGE_RUNTIME_RERANK_MODEL`, and
  `KNOWLEDGE_RUNTIME_RERANK_BASE_URL` to select external embedding/rerank
  providers without editing committed config. The startup scripts fail fast if
  the selected doc engine or embedding provider is not configured.

## Upstream

See `UPSTREAM.md` for import provenance and refresh instructions.

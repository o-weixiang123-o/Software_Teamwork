# Design

## Overview

Local Elasticsearch becomes an optional infrastructure service in the root Compose file. The default Compose path stays unchanged for users who simply copy `deploy/.env.example` to `deploy/.env`: PostgreSQL, Redis, Qdrant, MinIO, and `minio-init` remain the only default services.

Elasticsearch is enabled explicitly from local env. The preferred user-facing toggle is `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true`, which local scripts read. Direct Compose users can also use the service profile through `COMPOSE_PROFILES=knowledge-runtime`.

## Compose Boundary

`deploy/docker-compose.yml` adds:

- service: `elasticsearch`
- profile: `knowledge-runtime`
- image: `${KNOWLEDGE_RUNTIME_ELASTICSEARCH_IMAGE:-docker.elastic.co/elasticsearch/elasticsearch:8.15.3}`
- port: `${KNOWLEDGE_RUNTIME_ELASTICSEARCH_PORT:-9200}:9200`
- volume: `elasticsearch_data`
- security disabled for local development only
- healthcheck against `/_cluster/health`

This keeps Docker activity inside Compose while preserving the repository's
pull-only root Compose policy.

## Script Behavior

### `dev-up.sh`

- Loads `deploy/.env` as before.
- Builds its Compose service list from the default infrastructure services.
- If `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true`, appends `elasticsearch` and enables the `knowledge-runtime` profile for that Compose invocation.
- If `--china` is passed, continues to use the existing registry rewrite/mirror path for the current process only.
- Does not rewrite `deploy/.env`.

### `run-knowledge-parse-stack.sh`

- Removes direct `docker build` / `docker run` / container marker behavior.
- Adds `--china` handling.
- If `--china` is passed and `HF_ENDPOINT` is unset, exports `HF_ENDPOINT=https://hf-mirror.com`.
- If `DOC_ENGINE=elasticsearch`, waits for `KNOWLEDGE_RUNTIME_ES_URL`.
- If Elasticsearch is unavailable, fails with a message pointing to either:
  - `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true` before `dev-up.sh`, or
  - an externally managed Elasticsearch at `KNOWLEDGE_RUNTIME_ES_URL`.

### `stop-backend.sh`

- Stops host-run processes only.
- Does not own Compose infrastructure containers. Elasticsearch stops with the normal Compose cleanup path:
  `docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v`.

## Docker Policy Updates

The checker keeps these restrictions:

- root Compose default service set remains unchanged.
- business services are never allowed as default or profile services.
- service-level Dockerfiles and service-level Compose files stay forbidden.
- active third-party mirror defaults stay forbidden in `deploy/.env.example`.
- unpinned/default `latest` image paths stay forbidden.

The checker gains one explicit exception:

- root Compose may define profile service `elasticsearch` under profile `knowledge-runtime`.
- no Compose service may use `build:`.

## Source Policy

Default sources remain official:

- Docker Hub / official vendor registries for image defaults.
- PyPI official default.
- `proxy.golang.org` and `sum.golang.org`.
- HuggingFace official behavior unless the user sets `HF_ENDPOINT` or passes `--china`.

China mainland mirrors remain explicit:

- `dev-up.sh --china`
- `run-backend.sh --china`
- `run-knowledge-parse-stack.sh --china`
- local uncommitted `.env` overrides

## Compatibility

- Existing users without Elasticsearch enabled see no new Compose service in the default `dev-up.sh` path.
- Users who already run an external Elasticsearch can keep `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=false` and set `KNOWLEDGE_RUNTIME_ES_URL`.
- Users who want local Elasticsearch set `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true` before `dev-up.sh`.

## Risks

- Compose profile parsing in the policy checker may be too shallow if YAML structure changes. Mitigation: add focused unit tests for allowed and disallowed profile cases.
- Elasticsearch healthcheck may depend on tools available inside the upstream image. Mitigation: use a simple HTTP healthcheck and verify with Compose config; if local runtime verification is unavailable, document that risk.
- `COMPOSE_PROFILES` behavior with `--env-file` can vary by Compose implementation. Mitigation: scripts explicitly target the `elasticsearch` service when `KNOWLEDGE_RUNTIME_START_ELASTICSEARCH=true`, so profile env is not the only path.

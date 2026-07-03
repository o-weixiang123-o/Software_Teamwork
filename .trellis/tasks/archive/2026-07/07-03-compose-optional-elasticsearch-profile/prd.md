# Compose optional Elasticsearch profile

## Goal

Make local Elasticsearch a root `deploy/docker-compose.yml` optional infrastructure profile controlled by local environment settings, while preserving the repository source policy: official sources are default, and China mainland mirrors are explicit through `--china` or uncommitted local overrides.

## Background

- PR #536 currently adds a Knowledge runtime local Elasticsearch path, but the implementation starts it from `scripts/local/run-knowledge-parse-stack.sh` via direct `docker build` and `docker run`.
- Review blockers require removing default active third-party HuggingFace mirror behavior and avoiding Docker work outside the root Compose policy boundary.
- The desired direction is to include Elasticsearch in the root Compose file, controlled by `.env`, instead of using an out-of-band Docker command path.
- Existing Docker policy currently allows only default root Compose services `postgres`, `redis`, `qdrant`, `minio`, and `minio-init`, and rejects any profile service or `build:` entry.

## Requirements

1. Documentation must be updated before code changes.
2. Root Compose must include an optional Elasticsearch service for Knowledge runtime doc engine support.
3. Elasticsearch must not start in the default Compose path after `cp deploy/.env.example deploy/.env`.
4. Local users must be able to enable Elasticsearch through `deploy/.env`.
5. The optional Elasticsearch path must use root Compose, not direct `docker build` / `docker run` from scripts.
6. Docker policy/spec/checks must explicitly allow only this optional Elasticsearch infrastructure profile while continuing to reject business service containers, service-level Compose files, unpinned images, and all `build:` entries in root Compose.
7. `HF_ENDPOINT=https://hf-mirror.com` must not be active by default in `deploy/.env.example` or forced by `run-knowledge-parse-stack.sh` when `--china` is not passed.
8. China mainland support must remain explicit:
   - `dev-up.sh --china` for Docker/Go/uv/runtime artifact mirror preparation.
   - `run-backend.sh --china` for Go module mirrors.
   - `run-knowledge-parse-stack.sh --china` for runtime worker HuggingFace endpoint mirror behavior.
9. Existing local startup flow remains host-run for business services and Knowledge runtime API/worker; only Elasticsearch moves into optional Compose infrastructure.

## Acceptance Criteria

- [ ] `deploy/README.md`, `docs/runbooks/local-integration.md`, architecture/testing docs, and Trellis Docker policy specs describe the optional Elasticsearch Compose profile and official-default source policy.
- [ ] `deploy/docker-compose.yml` defines an `elasticsearch` service under a non-default profile, with a pinned image variable, healthcheck, volume, and port variables.
- [ ] `deploy/.env.example` includes safe official-default local variables for the optional Elasticsearch profile, with the profile disabled by default.
- [ ] `scripts/local/dev-up.sh` starts Elasticsearch through root Compose only when the local env setting enables it.
- [ ] `scripts/local/run-knowledge-parse-stack.sh` no longer performs direct `docker build` or `docker run`; it only verifies the configured Elasticsearch URL and starts host-run runtime processes.
- [ ] `scripts/local/run-knowledge-parse-stack.sh --china` is the only scripted path that sets `HF_ENDPOINT=https://hf-mirror.com` when the user has not set `HF_ENDPOINT`.
- [ ] `scripts/check_docker_policy.py` and tests permit only the optional Elasticsearch profile in root Compose and reject all root Compose `build:` entries.
- [ ] Local seed contract verification is updated to reflect the Compose-profile Elasticsearch path and no active third-party HF default.
- [ ] CI-equivalent checks pass locally, especially `python3 scripts/check_docker_policy.py`, Docker policy unit tests, local seed contract tests, shell syntax checks, and `git diff --check`.

## Out of Scope

- Running Knowledge runtime API/worker inside Docker.
- Adding Elasticsearch to the default root Compose service set.
- Adding production deployment support for Elasticsearch.
- Changing Knowledge runtime indexing behavior beyond local startup wiring.

## Open Questions

None. The user has chosen root Compose inclusion controlled by `.env`.

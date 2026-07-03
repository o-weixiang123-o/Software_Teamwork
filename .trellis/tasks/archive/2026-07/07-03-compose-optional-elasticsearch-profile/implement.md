# Implementation Plan

## Order

1. Documentation and spec first:
   - Update `deploy/README.md`.
   - Update `docs/runbooks/local-integration.md`.
   - Update `services/knowledge-runtime/README.md` and `README_zh.md`.
   - Update `services/knowledge/README.md` if it mentions direct Docker Elasticsearch startup.
   - Update `docs/architecture/technology-decisions.md`.
   - Update `docs/testing/strategy.md`.
   - Update `.trellis/spec/cicd.md` and `.trellis/spec/backend/quality-guidelines.md`.
2. Compose and env:
   - Add optional root Compose `elasticsearch` profile service.
   - Keep default service set unchanged.
   - Add/update `.env.example` variables with Elasticsearch disabled by default.
   - Remove active default `HF_ENDPOINT=https://hf-mirror.com`; keep mirror as documented local override or `--china` behavior only.
3. Script updates:
   - Update `dev-up.sh` to include Elasticsearch service only when env enables it.
   - Update `run-knowledge-parse-stack.sh` to remove direct Docker build/run and add `--china` HF mirror behavior.
   - Update `stop-backend.sh` if needed to remove direct container marker assumptions.
4. Policy and contract tests:
   - Update `scripts/check_docker_policy.py`.
   - Update `scripts/tests/test_check_docker_policy.py`.
   - Update `scripts/verify_local_seed_contract.py`.
   - Update `scripts/tests/test_local_seed_contract.py` and any local startup script tests.
   - Ensure root Compose `build:` is rejected, including for the optional
     Elasticsearch profile.
5. Validation:
   - `bash -n scripts/local/dev-up.sh scripts/local/run-backend.sh scripts/local/run-knowledge-parse-stack.sh scripts/local/stop-backend.sh`
   - `python3 scripts/check_docker_policy.py`
   - `uv run --no-project --python 3.12 python -m unittest scripts.tests.test_check_docker_policy scripts.tests.test_check_docker_environment scripts.tests.test_local_dev_up_script scripts.tests.test_local_seed_contract`
   - `python3 scripts/verify_local_seed_contract.py`
   - `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`
   - `git diff --check`
6. Commit and push to PR #536 branch.

## Review Gates

- After documentation edits, verify docs no longer say the default Docker layer includes Elasticsearch.
- After code edits, verify no script contains direct `docker build` / `docker run` for Elasticsearch.
- Before commit, verify policy-check equivalent passes locally.

## Rollback Points

- If Compose profile support creates policy or startup instability, revert code changes and keep only documentation stating Elasticsearch is an external dependency.
- If `--china` behavior conflicts with runtime dependency download logic, keep HF mirror as user-local `.env` override only and do not set it in scripts.

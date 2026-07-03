# Implementation Plan

## Steps

1. Add committed profile files:
   - `config/README.md`
   - `config/schema.yaml`
   - `config/base.yaml`
   - `config/dev.yaml`
   - `config/staging.yaml`
   - `config/production.yaml`

2. Add canonical local secret template:
   - root `.env.example`
   - verify `.env.local` stays ignored by `.gitignore`
   - stop documenting `deploy/.env.example` as the default source of truth

3. Implement `config/ctl`:
   - new Go module under `config/ctl`
   - `verify` command for profile existence, YAML parsing, profile name, and
     secret policy checks
   - `render` command for profile merge, dotenv parsing, process-env override,
     required-value validation, dotenv output, and shell output
   - unit tests for success and failure paths

4. Add shared shell loader:
   - `scripts/config/load-profile.sh`
   - render files under `.local/config/`
   - export `CONFIG_PROFILE`, `CONFIG_SECRET_FILE`, `CONFIG_COMPOSE_ENV_FILE`,
     and source shell-safe env output

5. Update local scripts to use profile loader:
   - `scripts/local/dev-up.sh`
   - `scripts/local/run-backend.sh`
   - Knowledge runtime local helpers that currently source `deploy/.env`
   - smoke/run scripts that use `deploy/.env` as their default env source

6. Update verifiers/tests:
   - add `scripts/verify_config_profiles.py` or equivalent wrapper if needed
   - integrate config profile verification into
     `scripts/verify_local_seed_contract.py`
   - update `scripts/tests/test_local_seed_contract.py`
   - add tests for config profile verifier/loader

7. Update docs/specs:
   - `deploy/README.md`
   - `docs/runbooks/local-integration.md`
   - `.trellis/spec/backend/quality-guidelines.md`
   - `.trellis/spec/backend/api-contracts.md` only if local env contract text is
     present and needs alignment
   - `AGENTS.md` / local instructions only if they directly contradict the new
     config architecture

8. Run formatting and checks, then fix drift:
  - Go formatting/tests for `config/ctl`
   - shell syntax checks
   - Python verifier tests
   - Docker policy and Compose config using rendered profile env
   - repository grep for stale `deploy/.env.example is the single default`
     wording

## Validation Commands

```bash
cd config/ctl && go test ./...
cd config/ctl && go run . verify
cd config/ctl && go run . render --profile dev --secret-file ../../.env.example --format dotenv --out ../../.local/config/dev.env
cd config/ctl && go run . render --profile dev --secret-file ../../.env.example --format shell --out ../../.local/config/dev.env.sh

python3 scripts/verify_config_profiles.py
python3 scripts/verify_local_seed_contract.py
python3 scripts/tests/test_local_seed_contract.py
python3 scripts/tests/test_local_dev_up_script.py
python3 scripts/check_docker_policy.py

bash -n scripts/config/load-profile.sh \
  scripts/local/dev-up.sh \
  scripts/local/run-backend.sh \
  scripts/local/run-knowledge-runtime-api.sh \
  scripts/local/start-knowledge-runtime-worker.sh \
  scripts/local/watch-knowledge-runtime-worker-idle.sh \
  scripts/local/run-knowledge-parse-stack.sh

CONFIG_PROFILE=dev CONFIG_SECRET_FILE=.env.example ./scripts/config/load-profile.sh --print-compose-env
docker compose -f deploy/docker-compose.yml --env-file .local/config/dev.env config --quiet

git diff --check
```

Service-local checks if touched:

```bash
cd services/knowledge && env -u GOROOT go test ./...
cd services/ai-gateway && env -u GOROOT go test ./...
```

## Risky Files

- `scripts/local/dev-up.sh`
- `scripts/local/run-backend.sh`
- `scripts/local/run-knowledge-runtime-api.sh`
- `scripts/local/run-knowledge-parse-stack.sh`
- `deploy/.env.example`
- `deploy/README.md`
- `scripts/verify_local_seed_contract.py`
- `.trellis/spec/backend/quality-guidelines.md`

## Rollback Points

- After adding `config/` and `config/ctl`, run loader tests before script
  changes.
- After updating `dev-up.sh`, verify rendered Compose config before touching
  backend startup.
- After updating run scripts, use `bash -n` and verifier tests before running
  any long-lived service command.

## Review Gate

Before `task.py start`, review:

- profile schema and examples are acceptable;
- loader precedence matches the user's intended final architecture;
- breaking local workflow change from `deploy/.env` to `.env.local` is accepted.

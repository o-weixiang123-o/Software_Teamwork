# Knowledge runtime API worker dependency split implementation plan

## Checklist

1. Inspect import boundaries for `api/ragflow_server.py`, active REST routes,
   and `rag/svr/task_executor.py`.
2. Classify dependencies in `services/knowledge-runtime/pyproject.toml` into:
   API-required, worker/full, and test-only.
3. Move worker-only packages into a `worker` dependency group.
4. Regenerate `services/knowledge-runtime/uv.lock` with `uv lock` after the
   dependency split.
5. Add or update local startup helpers:
   - API-only sync/start command;
   - full parse-stack sync/start command.
6. Add script-level contract checks that assert:
   - API-only sync command does not request the worker group;
   - full parse-stack helper does request worker/full dependencies;
   - no helper accidentally starts the worker in API-only mode.
7. Update `services/knowledge-runtime/README.md`, `services/knowledge/README.md`,
   `deploy/README.md`, and `docs/runbooks/local-integration.md`.
8. Run validation commands and fix failures.

## Validation

Minimum checks:

```bash
python3 scripts/tests/test_local_seed_contract.py
python3 scripts/tests/test_local_dev_up_script.py
bash -n scripts/local/run-backend.sh
bash -n scripts/local/run-knowledge-parse-stack.sh
cd services/knowledge-runtime && uv lock --check
cd services/knowledge-runtime && PYTHONPATH=. uv run --no-project --with pytest --with pytest-asyncio --with filelock --with ruamel-yaml python -m pytest test/routes/test_config_utils.py test/routes/test_route_registry.py test/routes/test_gateway_auth.py test/routes/test_runtime_dependency_check.py -q
cd services/knowledge && env -u GOROOT go test ./...
cd services/knowledge && env -u GOROOT go build ./cmd/adapter
git diff --check
```

If local package downloads are slow, record the blocker explicitly and still run
static/script contract checks.

## Risks

- Some API route imports may currently import worker-only modules at module load
  time. Fix with small lazy imports where needed.
- `uv lock` may rewrite many transitive entries. Review the diff for accidental
  version drift unrelated to the split.
- Security constraints in `[tool.uv]` must remain in force for both API and
  worker profiles.

## Rollback Points

- Before `uv lock`, inspect and stage `pyproject.toml` separately.
- After `uv lock`, review `uv.lock` diff before changing scripts.
- If API startup requires too many worker imports, stop and document the import
  boundary blocker instead of moving dependencies blindly.

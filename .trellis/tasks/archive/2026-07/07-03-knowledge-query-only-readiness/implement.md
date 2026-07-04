# Implementation Plan

## Steps

1. Remove `RuntimeWorker*` config fields, env parsing, and config tests.
2. Delete adapter worker-start implementation and remove `ensureRuntimeWorkerReady`
   from upload handling.
3. Adjust adapter upload tests so auto ingestion calls `/documents/parse`
   without a task executor heartbeat, and disabled ingestion still skips parse.
4. Remove local worker-start script wiring from `run-backend.sh` and seed
   contract expectations.
5. Update docs/spec/env examples for query-first readiness and deployment-owned
   worker lifecycle.
6. Add a Kubernetes KEDA Redis Streams example manifest for production worker
   scale-from-zero.

## Validation

- `gofmt` on changed Go files.
- `cd services/knowledge && env -u GOROOT go test ./...`
- `cd services/knowledge && env -u GOROOT go build ./cmd/adapter`
- `bash -n scripts/local/run-backend.sh`
- `PYTHONPATH=. python3 scripts/tests/test_local_seed_contract.py`
- `python3 scripts/tests/test_local_dev_up_script.py`
- `python3 scripts/check_docker_policy.py`
- `docker compose -f deploy/docker-compose.yml --env-file deploy/.env.example config --quiet`
- `git diff --check`

## Risky Files

- `services/knowledge/internal/adapter/handlers.go`
- `services/knowledge/internal/adapterconfig/config.go`
- `services/knowledge/internal/adapter/contract_test.go`
- `scripts/local/run-backend.sh`
- `deploy/.env.example`
- `deploy/k8s/knowledge-runtime-worker-keda.example.yaml`

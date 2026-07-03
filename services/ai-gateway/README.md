# AI Gateway Service

AI Gateway is an internal backend service for runtime model profiles, provider
credential metadata, and OpenAI-compatible model calls. Frontend clients
must call the public gateway, not this service directly.

Authoritative contract docs:

- `docs/services/ai-gateway/README.md`
- `docs/services/ai-gateway/docs/data-models.md`
- `docs/services/ai-gateway/docs/provider-adapters.md`
- `docs/services/ai-gateway/api/internal.openapi.yaml`

## Environment

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `AI_GATEWAY_HTTP_ADDR` | No | `:8086` | HTTP listen address. |
| `AI_GATEWAY_DATABASE_URL` | Yes | unset | PostgreSQL connection string. Never log this value. |
| `AI_GATEWAY_SERVICE_TOKEN_HASHES` | Yes | unset | Comma-separated allowed internal service token hashes. Format: `sha256:<hex>`. |
| `AI_GATEWAY_SECRET_MODE` | Yes | `encrypted_column` | S-02 supports `encrypted_column`. |
| `AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY_REF` | Yes | unset | Secret-safe key version/reference recorded with encrypted credentials. |
| `AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY` | Yes | unset | Local encryption secret for AES-GCM. Use a long random value; never commit it. |
| `AI_GATEWAY_DEFAULT_TIMEOUT_MS` | No | `60000` | Default provider timeout for new profiles. |
| `AI_GATEWAY_MAX_REQUEST_BYTES` | No | `1048576` | Maximum JSON request body size. |
| `AI_GATEWAY_METRICS_ADDR` | No | unset | Reserved metrics listen address. |
| `AI_GATEWAY_SHUTDOWN_TIMEOUT` | No | `10s` | Graceful shutdown timeout. |

`AI_GATEWAY_CREDENTIAL_ENCRYPTION_KEY` is hashed with SHA-256 into a 32-byte
AES key. It is not logged or returned. Production should replace this local
secret with a managed secret reference in a later slice.

## Local Checks

```bash
go test ./...
go build ./cmd/server
go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$AI_GATEWAY_DATABASE_URL" up
AI_GATEWAY_TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/ai_gateway_test?sslmode=disable" go test ./internal/repository -run '^TestPostgresRepositoryDBSmoke$' -count=1 -v
```

For local provider credentials, `./scripts/local/dev-up.sh` applies the local
env seed overlay after SQL demo seed only when `AI_GATEWAY_LOCAL_SEED_ENABLED=true`.
Set that flag plus `AI_GATEWAY_LOCAL_PROVIDER`,
`AI_GATEWAY_LOCAL_PROVIDER_BASE_URL`, `AI_GATEWAY_LOCAL_PROVIDER_API_KEY`,
`AI_GATEWAY_LOCAL_CHAT_MODEL`, `AI_GATEWAY_LOCAL_EMBEDDING_MODEL`,
`AI_GATEWAY_LOCAL_EMBEDDING_DIMENSIONS`, `AI_GATEWAY_LOCAL_RERANK_MODEL`, and
`AI_GATEWAY_LOCAL_RERANK_TOP_N` in `deploy/.env`; the overlay encrypts the API
key and updates the default local profiles without printing the key.

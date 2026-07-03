# File Service

`services/file` is the first runnable Go module for base file-object upload, metadata, deletion, and original content lookup. It is an internal foundation service for owner services to call; it does not own knowledge ingestion, knowledge document state, chunks, indexing, QA, report templates, report materials, report files, or report workflows.

Public frontend routes remain owned by gateway and are documented in `docs/services/gateway/api/public.openapi.yaml`. Frontend callers must not call this service directly. Stable file capability must be reached through gateway `/api/v1/**` resources owned by `knowledge` or `document`, while those owner services reuse this service's internal base file APIs.

The implemented internal contract is generic file-object shaped (`/internal/v1/files/**`). Legacy knowledge-document compatibility routes have been removed from this service boundary; owner services must model their own business resources and call `/internal/v1/files/**` for raw object storage.

## Current Scope

Implemented now:

- `GET /healthz`
- `GET /readyz`
- `POST /internal/v1/files`
- `GET /internal/v1/files/{fileId}`
- `DELETE /internal/v1/files/{fileId}`
- `GET /internal/v1/files/{fileId}/content`
- Memory, local, and MinIO object-store adapters behind `service.ObjectStore`
- Streaming service-layer upload path with SHA-256 calculation during object-store write
- Optional MIME sniffing allowlist and per-operation caller allowlists
- Non-local configuration guards that reject memory metadata/object storage
- Env-gated PostgreSQL metadata + MinIO object-store smoke test


Out of scope for this MVP:

- Knowledge ingestion handoff and knowledge document state
- Report template, report material, and generated report file business state
- Public knowledge-owned document list/detail/chunks/content contracts

## Local Run

完整本地应用从仓库根目录启动：

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
```

只调 File 服务代码时，在 `services/file` 内跑服务级检查：

```bash
go test ./...
go build ./cmd/server
go run ./cmd/server
```

By default, metadata uses the in-memory repository for tests and local development.
To run with durable PostgreSQL metadata, apply the migration, set `FILE_DATABASE_URL`,
and configure a service token:

```powershell
cd services/file
$env:FILE_DATABASE_URL = "postgres://file:file@localhost:5432/file?sslmode=disable"
$env:FILE_INTERNAL_SERVICE_TOKEN = "local-file-service-token"
go run ./cmd/server
```

Internal file endpoints require trusted caller context headers for local testing:

```text
X-Request-Id: req_local
X-Caller-Service: knowledge
X-Service-Token: local-file-service-token
```

Missing trusted caller context returns `401 unauthorized`.

When `FILE_INTERNAL_SERVICE_TOKEN` or `INTERNAL_SERVICE_TOKEN` is configured,
base file routes under `/internal/v1/files/**` also require `X-Service-Token`.
Invalid or missing service tokens return `401 unauthorized`.

Production-like runs should set `FILE_ENV` to a non-local value such as
`production`. In that mode the service fails startup when object storage is
`memory` or `FILE_DATABASE_URL` is empty. Keep `FILE_ENV=local`, `test`, or
`development` only for tests and developer-only runs.

Caller and MIME policies are optional for local compatibility. When enabled,
`FILE_ALLOWED_CREATE_CALLERS`, `FILE_ALLOWED_READ_CALLERS`, and
`FILE_ALLOWED_DELETE_CALLERS` require `X-Caller-Service` to match the operation
allowlist; missing caller returns `401`, and a caller outside the configured
operation list returns `403`. `FILE_ALLOWED_CONTENT_TYPES` compares against the
sniffed/effective MIME type, not just the multipart header.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `FILE_ENV` | `ENV` or `local` | Runtime environment label. Non-local values reject memory storage and empty metadata DB. Local-like values: `local`, `test`, `development`. |
| `FILE_HTTP_ADDR` | `:8082` | HTTP listen address. |
| `FILE_MAX_UPLOAD_BYTES` | `33554432` | Multipart upload limit in bytes. |
| `FILE_STORAGE_BACKEND` | `memory` | Supported values: `memory`, `local`, `minio`. |
| `FILE_LOCAL_STORAGE_DIR` | `.file-storage` | Local object-store root when `FILE_STORAGE_BACKEND=local`. |
| `FILE_MINIO_ENDPOINT` | empty | Required when `FILE_STORAGE_BACKEND=minio`; host and port without scheme. |
| `FILE_MINIO_ACCESS_KEY` | empty | Required when `FILE_STORAGE_BACKEND=minio`; never returned in responses. |
| `FILE_MINIO_SECRET_KEY` | empty | Required when `FILE_STORAGE_BACKEND=minio`; never returned in responses or logs. |
| `FILE_MINIO_BUCKET` | empty | Required when `FILE_STORAGE_BACKEND=minio`; internal storage detail. |
| `FILE_MINIO_USE_SSL` | `false` | Whether the MinIO endpoint uses TLS. |
| `FILE_MINIO_REGION` | empty | Optional MinIO/S3 region. |
| `FILE_MINIO_TIMEOUT` | `10s` | Per-request MinIO client timeout. |
| `FILE_DATABASE_URL` | empty | Enables PostgreSQL metadata repository when set; empty keeps memory metadata mode. |
| `FILE_INTERNAL_SERVICE_TOKEN` | empty | Required when `FILE_DATABASE_URL` is set. Validates `X-Service-Token` for `/internal/v1/files/**`. |
| `INTERNAL_SERVICE_TOKEN` | empty | Shared fallback token when `FILE_INTERNAL_SERVICE_TOKEN` is empty. |
| `FILE_ALLOWED_CONTENT_TYPES` | empty | Comma-separated effective MIME allowlist, for example `application/pdf,text/plain,application/octet-stream`. Empty allows all current behavior. |
| `FILE_ALLOWED_CREATE_CALLERS` | empty | Comma-separated `X-Caller-Service` values allowed to create files. Empty preserves current caller-context behavior. |
| `FILE_ALLOWED_READ_CALLERS` | empty | Comma-separated callers allowed to read metadata and content. Empty preserves current caller-context behavior. |
| `FILE_ALLOWED_DELETE_CALLERS` | empty | Comma-separated callers allowed to delete files. Empty preserves current caller-context behavior. |
| `FILE_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout. |

## Storage Port

Object storage is behind `service.ObjectStore`. The service layer streams upload
content into that port while calculating SHA-256 and counting bytes; it no
longer reads the entire upload into service memory before calling the object
store. The `memory` adapter still buffers data internally and exists only for
tests and local development. The `local` adapter stores objects under
`FILE_LOCAL_STORAGE_DIR` for local durable smoke tests. The `minio` adapter uses
the official `github.com/minio/minio-go/v7@v7.2.1` SDK and expects an existing
MinIO or S3-compatible endpoint.

Storage adapters do not expose object keys, bucket names, storage paths, internal URLs, access keys, or secret keys through API responses. MinIO SDK usage stays inside `internal/platform/storage` and `cmd/server` wiring; `internal/http` handlers and service use cases continue to depend on the `service.ObjectStore` port.

## Metadata Port

File metadata is behind the service repository port. The runtime uses the memory repository when `FILE_DATABASE_URL` is empty and switches to the PostgreSQL repository when `FILE_DATABASE_URL` is configured. Non-local `FILE_ENV` values reject the memory metadata fallback. PostgreSQL stores only base file metadata such as file id, display filename, content type, size, checksum, storage reference, created timestamp, delete request timestamp, purge timestamp, and safe failure summary. Knowledge-base IDs, report IDs, template IDs, material IDs, business tags, processing status, and ACLs belong to their owner services.


## Migrations

The contract migration under `migrations/` is applied with the project-pinned `goose@v3.27.1` command:

```powershell
cd services/file
$env:FILE_DATABASE_URL = "postgres://file:file@localhost:5432/file?sslmode=disable"
go run github.com/pressly/goose/v3/cmd/goose@v3.27.1 -dir migrations postgres "$env:FILE_DATABASE_URL" up
```

Repository smoke tests are env-gated and use an isolated schema:

```powershell
cd services/file
$env:FILE_TEST_DATABASE_URL = "postgres://file:file@localhost:5432/file?sslmode=disable"
go test ./internal/repository
```

Regenerate the service-local sqlc query package after changing `internal/repository/queries/*.sql`:

```powershell
cd services/file
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```

PostgreSQL + MinIO smoke tests are also env-gated. They use the public File
Service HTTP handler, create an isolated PostgreSQL schema, write the object to
MinIO, read it back through `/internal/v1/files/{fileId}/content`, then delete
through the File API so test data is cleaned up.

Start the root infra baseline from the repository root:

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
cd services/file
```

The root Compose MinIO initializer creates the local bucket
`software-teamwork-local` with these placeholder credentials:

```text
endpoint: localhost:9000
access key: minio_local_demo
secret key: minio-local-demo-password
region: empty
```

Run the combined smoke from `services/file`:

```bash
FILE_MINIO_POSTGRES_SMOKE=1 \
FILE_TEST_DATABASE_URL='postgres://file_app:file_app_dev@localhost:5432/file_system?sslmode=disable' \
FILE_MINIO_ENDPOINT='localhost:9000' \
FILE_MINIO_ACCESS_KEY='minio_local_demo' \
FILE_MINIO_SECRET_KEY='minio-local-demo-password' \
FILE_MINIO_BUCKET='software-teamwork-local' \
go test ./internal/integration -run TestFileMinIOPostgresSmoke -count=1 -v
```

If the smoke is not enabled, `go test ./...` skips it. If it is enabled but
PostgreSQL, MinIO, or the bucket is missing, the test fails with the missing
environment variable or dependency error. Check `docker compose logs postgres
minio minio-init` from the root infra Compose first:

```bash
docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs postgres minio minio-init
```

The smoke normally deletes its test file;
if it is interrupted, remove the leftover object from the MinIO console or `mc`
when the file ID is known from the partial run.

## Multipart Upload Shape

Upload uses `multipart/form-data`:

- `file`: required binary part
- `checksumSha256`: optional SHA-256 checksum for `/internal/v1/files`; when omitted, the service computes it

The service reads a bounded prefix for MIME sniffing, then streams the complete
content through the object-store port. When `FILE_ALLOWED_CONTENT_TYPES` is set,
the effective content type must be allowed. Header-only type claims are not
trusted when the sniffed bytes indicate a different unsafe type. Unknown binary
content falls back to `application/octet-stream`, which must be allowed when an
allowlist is configured.

## Response Shape

JSON success responses use:

```json
{
  "data": {},
  "requestId": "req_123"
}
```

JSON errors use:

```json
{
  "error": {
    "code": "validation_error",
    "message": "request validation failed",
    "requestId": "req_123"
  }
}
```

Internal metadata responses include base file fields such as `contentType`, `sizeBytes`, and checksum for owner-service integration. They never expose bucket names, object keys, internal storage URLs, or storage credentials.

Content reads return raw bytes on success and the same JSON error envelope on failure.

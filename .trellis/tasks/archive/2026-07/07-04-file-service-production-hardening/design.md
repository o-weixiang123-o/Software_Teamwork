# Design

## Boundaries

`services/file` remains an internal base file-object service. It owns raw bytes, minimum object metadata, object-storage coordination, and internal read/delete semantics. Owner services continue to own business resources, permissions, and public response shape.

No frontend route, owner resource ID, business ACL, bucket, object key, internal URL, or presigned URL should be introduced by this task.

## Streaming Upload

The service should replace the current `io.ReadAll` upload path with a streaming pipeline:

1. Handler parses multipart with the existing max upload limit.
2. Service receives the multipart stream and size metadata.
3. Service reads a bounded sniff prefix, determines effective content type, then streams the full content through `io.MultiWriter` or equivalent hash calculation into `ObjectStore.Put`.
4. `ObjectStore.Put` receives the stream, effective content type, and declared/known size.
5. After object write succeeds, service persists metadata with checksum and effective content type.
6. If metadata write fails, service best-effort deletes the object as today.

This keeps the same external contract while removing full-content buffering from service code.

## Content Type Validation

Introduce File runtime config for allowed content types. Recommended shape:

- `FILE_ALLOWED_CONTENT_TYPES`: comma-separated MIME list.
- Empty value means allow all current behavior for local compatibility.
- Exact matches are enough for this slice; wildcard matching can be a later enhancement if needed.

The effective content type should prefer a sniffed type when the request header is missing, generic, or disagrees in an unsafe way. Unknown content can use `application/octet-stream` only when the allowlist permits it or no allowlist is configured.

## Backend Guards

Add an environment label, using `FILE_ENV` with fallback to `ENV` and then `local`. Treat `local`, `test`, and `development` as local-like. When the environment is not local-like:

- Reject `FILE_STORAGE_BACKEND=memory`.
- Reject empty `FILE_DATABASE_URL`.

For this first slice, reject only memory object storage in non-local environments. `local` storage remains selectable for controlled single-node demos and durable local smoke tests, but docs must state that production should use MinIO or an equivalent persistent object storage adapter.

## Caller Policy

Add caller operation policy in config:

- `FILE_ALLOWED_CREATE_CALLERS`
- `FILE_ALLOWED_READ_CALLERS`
- `FILE_ALLOWED_DELETE_CALLERS`

Each is comma-separated. Empty values preserve current behavior for local compatibility. When a list is configured, `X-Caller-Service` must be present and included for that operation. Missing caller remains `401`; present but unauthorized caller returns `403`.

The initial default examples should include `document` and `qa`, matching current owner-service usage. Gateway and frontend must not call File directly.

## Compatibility

Existing APIs remain unchanged:

- `POST /internal/v1/files`
- `GET /internal/v1/files/{fileId}`
- `DELETE /internal/v1/files/{fileId}`
- `GET /internal/v1/files/{fileId}/content`

Existing response schemas remain unchanged except effective `contentType` may now be sniffed and normalized.

## Rollback

The config additions should be optional and backward-compatible in local mode. Rollback is limited to reverting service/config changes and docs. No database migration is required for this slice.

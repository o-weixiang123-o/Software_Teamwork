# Implementation Plan

## Steps

1. Read backend specs and File service docs.
2. Inspect current config, handler, service, object-store adapters, and tests.
3. Add File config fields:
   - environment
   - allowed content types
   - allowed create/read/delete callers
   - production guard validation
4. Add content-type policy helpers and tests.
5. Refactor service upload path to stream content while hashing.
6. Adjust object-store adapters and tests only as needed to preserve streaming semantics.
7. Enforce caller allowlist per route operation.
8. Update service/handler/config tests for new behavior.
9. Update File docs and local integration runbook.
10. Run validation.

## Validation

- `cd services/file && go test ./...`
- If docs or runbooks include Docker/File smoke policy changes, run targeted script tests where applicable.
- Do not run real PostgreSQL + MinIO smoke unless local dependencies are available and explicitly configured.

## Risk Points

- Multipart handling can accidentally re-buffer content; verify service layer no longer calls `io.ReadAll` on upload content.
- Content type sniffing consumes bytes; ensure the object store still receives the full file content.
- Existing owner clients may omit `X-Caller-Service`; keep policy optional by default and document how to enable it.
- Non-local backend guards must not break tests that intentionally use memory backends.

## Rollback Points

- Config guard changes can be reverted independently if local/dev workflows break.
- MIME allowlist can remain empty to restore previous permissive behavior.
- Caller allowlists can remain empty to restore previous trust behavior.

# Harden File service production baseline

## Goal

Make `services/file` safer for production use as the internal base file-object service while preserving its current service boundary: File stores raw object bytes and minimum metadata, while `document`, `qa`, and other owner services keep business state and authorization.

## Background

Confirmed facts from the repository:

- File is an internal service only. It must not expose frontend-owned `/api/v1/files/**` routes or own knowledge/document/QA business state.
- Current File routes are generic `/internal/v1/files/**` plus `healthz` and `readyz`.
- Current runtime supports memory/local/MinIO object storage and memory/PostgreSQL metadata.
- Current upload service reads the full multipart content into memory before writing to object storage.
- Current production dependency checks are documented but weakly enforced: memory metadata/object storage can still be selected through configuration.
- Current caller trust is mostly `X-Service-Token` plus caller context; service-specific allowlist and per-operation scopes are not modeled.
- Current validation covers size, empty file, filename normalization, and SHA-256 checksum, but not content sniffing or configured MIME allowlists.
- PostgreSQL + MinIO smoke exists but is env-gated and not part of the default test path.

## Requirements

1. Preserve File service ownership boundaries.
   - File must continue to store only base file metadata and object-storage references.
   - File must not add knowledge-base IDs, report IDs, QA session IDs, business ACLs, or frontend public file routes.

2. Stream upload processing.
   - File upload must avoid holding the whole file in memory in the service layer.
   - The implementation must compute SHA-256 while streaming and still validate caller-provided checksums.
   - The `ObjectStore` port should accept streaming input without forcing service-level buffering.
   - Existing memory/local/MinIO adapters and tests must keep working.

3. Production backend guardrails.
   - Non-local runtime configuration must reject memory metadata or memory object storage.
   - Local/test defaults may remain convenient for unit tests and developer-only runs.
   - The failure must be clear during config load or server startup.

4. Caller allowlist and operation scopes.
   - Internal file routes must support a configurable caller policy.
   - The policy must distinguish at least create, read, and delete operations.
   - Existing trusted callers used by `document` and `qa` must be supported without exposing `file_ref`, object keys, buckets, or internal URLs.
   - Missing/unauthorized caller context must return stable `401` or `403` errors without leaking storage internals.

5. MIME sniffing and allowlist.
   - File must sniff content type from the initial bytes and not blindly trust multipart headers.
   - Configuration must support a content type allowlist.
   - Empty or unknown content should fall back safely to `application/octet-stream` only when allowed by policy.
   - Mismatched extension/header/sniffed type must be handled predictably and covered by tests.

6. Smoke/runbook coverage.
   - Update File docs/runbooks to describe production backend guards, caller policy, MIME allowlist, and streaming behavior.
   - Preserve or extend existing PostgreSQL + MinIO smoke instructions.
   - Add focused tests for new config, service, and handler behavior.

## Acceptance Criteria

- [ ] `cd services/file && go test ./...` passes.
- [ ] File upload path no longer reads the entire file into service memory before calling the object store.
- [ ] Checksum validation still rejects invalid SHA-256 and mismatch cases.
- [ ] Non-local configuration rejects memory metadata/object storage with clear errors.
- [ ] Caller policy tests cover allowed create/read/delete callers and forbidden callers.
- [ ] MIME validation tests cover sniffed content type, allowlist accept/reject, and safe fallback behavior.
- [ ] Docs mention the new runtime config and operational expectations.
- [ ] No public frontend file routes or business owner fields are added.

## Out Of Scope

- Virus/malware scanning and quarantine state.
- Range requests, resumable upload, multipart upload sessions, CDN integration, or presigned frontend URLs.
- A durable file reference table or owner-service reference lifecycle.
- Async purge worker, deletion task table, or global audit query UI.
- Knowledge runtime storage migration.

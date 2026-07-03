# Logging Guidelines

> Structured logging rules for Go backend services.

---

## Overview

Backend services must use structured logs with Go standard-library `log/slog`.
Production output defaults to JSON unless a service documents a reason to
diverge in `docs/architecture/technology-decisions.md` and the service README.

Logs must help operators answer:

- Which service handled the request?
- Which request or job failed?
- Which dependency was involved?
- What action should be investigated?

Logs must not leak secrets or sensitive business data.

---

## Log Levels

| Level | Use For |
|-------|---------|
| `debug` | Local troubleshooting, branch decisions, non-production detail |
| `info` | Service startup/shutdown, successful important workflows |
| `warn` | Recoverable problems, retries, degraded dependency behavior |
| `error` | Failed requests, failed jobs, unrecoverable dependency failures |

Rules:

- Do not use `error` for expected validation failures unless they indicate abuse or system issues.
- Do not log every successful request at high detail unless access logging is intentionally enabled.
- Do not use `debug` logs as a substitute for tests.

---

## Required Fields

Include these fields when available:

| Field | Meaning |
|-------|---------|
| `service` | Service name, for example `gateway` |
| `request_id` | Request correlation ID |
| `user_id` | Authenticated user ID, when safe and relevant |
| `operation` | High-level operation name |
| `dependency` | Downstream service or infrastructure dependency |
| `status` | Outcome such as `success`, `failed`, `retrying` |
| `duration_ms` | Operation duration in milliseconds |

Example:

```go
logger.InfoContext(ctx, "file uploaded",
    "service", "file",
    "request_id", requestID,
    "operation", "upload_file",
    "file_id", fileID,
    "duration_ms", duration.Milliseconds(),
)
```

---

## Request Logging

HTTP middleware should attach or generate a request ID. Boundary logs should
include:

- method,
- path template, not raw unbounded URL when it contains sensitive query data,
- status code,
- duration,
- request ID,
- authenticated user ID when available.

Do not log request bodies by default.

---

## Dependency Logging

Log dependency failures with enough detail to identify the failing dependency:

- downstream service name,
- HTTP status code when applicable,
- infrastructure component name such as `postgres`, `redis`, `elasticsearch`, or `minio`,
- retry count when retrying,
- request ID or job ID.

Do not log database DSNs, access keys, object storage credentials, tokens, or full SQL statements with parameter values.

---

## What To Log

- Service startup and validated runtime mode.
- Graceful shutdown start and completion.
- Failed background jobs.
- Cross-service dependency failures.
- File ingestion and document generation state transitions.
- Security-relevant events such as repeated authentication failures, without logging credentials.

---

## What Not To Log

Never log:

- passwords,
- tokens,
- API keys,
- MinIO access keys or secret keys,
- database URLs with credentials,
- raw uploaded document contents,
- full generated documents,
- personally sensitive data unless explicitly approved,
- vector payloads when they contain source document text.

---

## Common Mistakes

- Logging the same error in repository, service, and handler layers.
- Logging raw request or response bodies for convenience.
- Emitting unstructured strings that cannot be searched by `service`, `request_id`, or `operation`.
- Including internal object keys in user-facing logs without checking sensitivity.

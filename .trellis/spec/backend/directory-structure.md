# Directory Structure

> How Go backend services are organized in this project.

---

## Overview

Backend services live under `services/<service>/`. Each service is an
independent Go module with its own `go.mod`, HTTP server entrypoint,
configuration, internal packages, tests, and host-run startup docs.

Do not rely on a repository-root `go.mod` for backend service builds.

---

## Monorepo Layout

```text
services/
├── gateway/
│   ├── go.mod
│   ├── cmd/server/
│   ├── internal/
│   ├── migrations/
│   └── api/
├── auth/
├── file/
├── qa/
├── knowledge/
├── document/
├── ai-gateway/
└── knowledge-runtime/
deploy/
└── docker-compose.yml
```

Use the same service-local layout for every Go service unless a service has a
clear reason to omit a directory. `services/knowledge-runtime/` is the explicit
non-Go runtime exception: it is the vendored RAGFlow runtime boundary behind the
Go Knowledge adapter and should not be forced into the Go module layout.

---

## Service-Local Layout

```text
services/<service>/
├── go.mod
├── go.sum
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   ├── http/
│   ├── service/
│   ├── repository/
│   └── platform/
├── api/
│   └── openapi.yaml
├── migrations/
└── README.md
```

Directory responsibilities:

| Directory | Responsibility |
|-----------|----------------|
| `cmd/server/` | Process entrypoint, dependency wiring, graceful shutdown |
| `internal/config/` | Environment parsing and validated runtime configuration |
| `internal/http/` | HTTP handlers, middleware, request/response DTOs |
| `internal/service/` | Business use cases and orchestration |
| `internal/repository/` | PostgreSQL persistence and transaction boundaries |
| `internal/platform/` | Clients for Redis, MinIO, runtime doc engines, or other infrastructure |
| `api/` | Public or internal HTTP contract documentation |
| `migrations/` | Service-owned PostgreSQL migrations |

---

## Module Organization

- Keep business rules in `internal/service/`; handlers should translate HTTP to service calls.
- Keep database-specific code in `internal/repository/`; service code should not build SQL strings.
- Keep infrastructure clients behind small service-owned interfaces.
- Keep request and response shapes close to the HTTP handlers that own them.
- A service must not import another service's `internal/` packages.
- Cross-service calls must go through HTTP clients or a documented API contract.

---

## Naming Conventions

- Service directories use short lowercase names: `gateway`, `auth`, `file`, `qa`, `knowledge`, `document`, `ai-gateway`.
- Go packages use lowercase names with no underscores.
- File names use lowercase words separated by underscores only when readability requires it, for example `user_repository.go`.
- HTTP handler files should be named by resource or workflow, for example `knowledge_handler.go`.
- Tests live next to production code and use `_test.go`.
- Migration files use monotonically increasing prefixes, for example `0001_create_users.sql`.

---

## Adding A New Service

When adding a service:

1. Create `services/<service>/go.mod`.
2. Add `cmd/server/main.go`.
3. Add `internal/config` before reading environment variables elsewhere.
4. Add service-local tests.
5. Add host-run README instructions and required environment variables.
6. Add CI path filters for `services/<service>/**`.
7. Update README and this spec if the service changes architecture.

When adding or changing a non-Go runtime such as `services/knowledge-runtime`,
document the runtime boundary, package manager, lockfile, host-run command, and
service API under `docs/services/<service>/` or the owning service docs plus
`docs/architecture/technology-decisions.md`. Do not introduce Go wrappers around
runtime-specific libraries solely to match the Go service template.

### Baseline Slice For Service Migration

When replacing a prototype service with the standard Go microservice shape, the
first implementation slice should establish a runnable baseline before moving
business workflows:

- service-local `go.mod` and `cmd/server/main.go`,
- `internal/config` with validated env keys and documented local defaults,
- `GET /healthz` and `GET /readyz` using the standard JSON envelope,
- service-local `api/openapi.yaml` for implemented internal or operational
  routes,
- handler tests for response envelopes and request ID propagation,
- host-run command and local infra dependencies when the service participates in local integration,
- README notes that identify any retained prototype code as migration reference
  rather than stable runtime behavior.

This keeps the ownership boundary testable while avoiding a risky one-shot
rewrite of ingestion, retrieval, storage, and gateway proxy behavior.

---

## Common Mistakes

- Placing shared business logic in `services/gateway/` because it is the entrypoint.
- Importing another service's internal packages instead of calling its HTTP API.
- Creating a root-level Go module that makes all services build together.
- Storing deployment-only configuration inside service source directories.
- Allowing handlers to contain SQL, runtime doc-engine queries, or MinIO object logic directly.

# Backend Development Guidelines

> Engineering rules for Go backend services in the power-industry knowledge management system.

---

## Overview

Backend code is organized as independent Go microservices under `services/`.
Each service owns its own `go.mod`, HTTP API, configuration, tests, and CI job.
Services communicate through HTTP/REST in the initial
architecture.

Backend services:

| Service | Path | Responsibility |
|---------|------|----------------|
| Gateway | `services/gateway/` | Public backend entrypoint, routing, request coordination |
| Auth | `services/auth/` | Authentication, authorization, identities, sessions/tokens |
| File | `services/file/` | Uploads, file metadata, MinIO object orchestration |
| QA | `services/qa/` | AI question answering over retrieved knowledge |
| Knowledge | `services/knowledge/` | Knowledge ingestion, indexing, retrieval coordination |
| Document | `services/document/` | Report and document generation workflows |
| AI Gateway | `services/ai-gateway/` | Internal model profiles, provider credentials, OpenAI-compatible chat/embedding/rerank APIs |

Infrastructure dependencies:

- PostgreSQL for relational application data.
- Redis for cache, sessions, lightweight queues, or short-lived coordination.
- Elasticsearch is the active local Knowledge runtime doc engine. Retired
  vector-store compatibility fields should not be restored.
- MinIO for object storage.
- `services/knowledge-runtime/` for Knowledge-owned RAGFlow parsing, chunking,
  embedding, indexing, and retrieval support.

The old standalone `services/parser/` runtime is retired. Do not restore it for
document ingestion work; route Knowledge document parsing through
`services/knowledge` and its `services/knowledge-runtime` runtime API/worker
boundary.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Service layout and module boundaries | Active |
| [Database Guidelines](./database-guidelines.md) | PostgreSQL, migrations, transactions, Redis, MinIO, runtime-owned index stores | Active |
| [API Contracts](./api-contracts.md) | Gateway OpenAPI, response envelopes, route ownership, context headers | Active |
| [Error Handling](./error-handling.md) | Go error propagation and HTTP error responses | Active |
| [Quality Guidelines](./quality-guidelines.md) | Build, test, lint, review expectations | Active |
| [Logging Guidelines](./logging-guidelines.md) | Structured logging and sensitive-data rules | Active |
| [MCP Agent Runtime](./mcp-agent-runtime.md) | QA Agent Loop + Knowledge MCP Server contracts | Active |

---

## Pre-Development Checklist

Before changing backend code:

- [ ] Identify the affected service under `services/<service>/`.
- [ ] Read `docs/architecture/service-boundaries.md` before changing a service boundary, gateway route, model invocation flow, or data owner.
- [ ] Read `docs/architecture/technology-decisions.md` before selecting or changing backend infrastructure, libraries, queueing, auth, logging, or testing tools.
- [ ] Read [Directory Structure](./directory-structure.md) for service layout rules.
- [ ] Read [Database Guidelines](./database-guidelines.md) if data, cache, vector search, or object storage is touched.
- [ ] Read [API Contracts](./api-contracts.md) if gateway routes, frontend-facing DTOs, or cross-service HTTP contracts are touched.
- [ ] Read [Error Handling](./error-handling.md) if handlers, service calls, or client responses are touched.
- [ ] Read [Logging Guidelines](./logging-guidelines.md) before adding logs or changing error logging.
- [ ] Read [Quality Guidelines](./quality-guidelines.md) before declaring implementation complete.
- [ ] Run service-local checks from the service directory, at minimum `go test ./...`.

---

## Cross-Service Principles

- Keep each service independently buildable and testable.
- Prefer explicit HTTP contracts between services over importing another service's internal Go packages.
- Do not create shared Go libraries casually. Introduce shared packages only when a repeated pattern appears across at least three services and the ownership boundary is clear.
- Keep service-specific configuration in that service; keep deployment wiring in `deploy/`.
- Validate incoming requests at service boundaries and return stable JSON error responses.

---

**Language**: Backend spec documents are written in English.

# Gateway Service

`gateway` is the public backend entrypoint for frontend, admin, backend module,
and tool callers. It provides operational health checks, request edge
middleware, auth session cache handling, and active OpenAPI route proxy coverage.
Gateway does not own a business database or implement downstream domain
workflows.

Authoritative contracts and service boundaries:

- `docs/services/gateway/README.md`
- `docs/services/gateway/api/public.openapi.yaml`
- `docs/services/gateway/docs/data-models.md`
- `docs/architecture/service-boundaries.md`
- `docs/architecture/technology-decisions.md`

## Local Run

```bash
cd services/gateway
go run ./cmd/server
```

Default address:

```text
:8080
```

Health checks:

```bash
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

Both endpoints return the project success envelope and include `X-Request-Id`:

```json
{
  "data": {
    "status": "ok",
    "service": "gateway",
    "version": "0.1.0",
    "environment": "local"
  },
  "requestId": "req_123"
}
```

`/healthz` is process liveness. `/readyz` is the lightweight Gateway readiness
gate: it checks Redis session-cache readiness, Auth `/readyz`, and configured
owner service base URLs. It does not call Knowledge, QA, Document, or AI
Gateway readiness endpoints, and it does not prove upload, retrieval, QA
answers, report generation, model profiles, or real provider calls are working.
Use the local integration runbook smoke checks for those workflows.

All non-empty service base URL variables must be absolute `http` or `https`
URLs with a host and without credentials, query, or fragment. Blank owner
service URLs remain allowed at config load time and make `/readyz` report the
owner as not configured. Owner service hosts must be the documented service DNS
name (`auth`, `knowledge`, `qa`, `document`, or `ai-gateway`) or a local
development loopback host such as `localhost`, `127.0.0.1`, or `[::1]`.

## Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `GATEWAY_HTTP_ADDR` | `:8080` | HTTP listen address. |
| `GATEWAY_METRICS_ADDR` | `:9091` | Internal Prometheus metrics listen address. Not exposed via the public gateway port. |
| `GATEWAY_SERVICE_VERSION` | `0.1.0` | Version reported by health checks and startup logs. |
| `GATEWAY_ENV` | `local` | Runtime environment label. |
| `GATEWAY_MAX_BODY_BYTES` | `10485760` | Maximum request body size enforced at the gateway edge. |
| `GATEWAY_MAX_IN_FLIGHT` | `128` | Maximum concurrent public requests per gateway process. `0` disables this process-local guard. |
| `GATEWAY_AUTH_REFRESH_MAX_IN_FLIGHT` | `32` | Maximum concurrent protected-route Auth authority refresh calls per gateway process. `0` disables this specialized guard. |
| `GATEWAY_REQUEST_TIMEOUT` | `30s` | Per-request context timeout. |
| `GATEWAY_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown timeout. |
| `GATEWAY_DOWNSTREAM_TIMEOUT` | `10s` | Timeout for auth and owner-service HTTP calls. |
| `GATEWAY_CORS_ALLOWED_ORIGINS` | `*` | Comma-separated allowed CORS origins. |
| `GATEWAY_CORS_ALLOWED_METHODS` | `GET,POST,PATCH,DELETE,OPTIONS` | Comma-separated allowed CORS methods. |
| `GATEWAY_CORS_ALLOWED_HEADERS` | `Authorization,Content-Type,X-Request-Id` | Comma-separated allowed CORS headers. |
| `GATEWAY_CORS_ALLOW_CREDENTIALS` | `false` | Whether CORS credentialed requests are allowed. |
| `GATEWAY_REDIS_ADDR` | `localhost:6379` | Redis endpoint for `gateway:session:<accessTokenHash>` cache entries. |
| `GATEWAY_REDIS_PASSWORD` | unset | Redis password. Never log this value. |
| `GATEWAY_REDIS_DB` | `0` | Redis DB index. |
| `GATEWAY_TOKEN_HASH_SECRET` | local dev default | HMAC secret used to derive opaque-token cache keys. Override outside local development. |
| `GATEWAY_TOKEN_HASH_KEY_VERSION` | `v1` | Version segment in `hmac-sha256:<version>:<hex>`. |
| `GATEWAY_INTERNAL_SERVICE_TOKEN` | unset | Internal service credential forwarded as `X-Service-Token` when configured. |
| `GATEWAY_AUTH_ADMIN_SERVICE_TOKEN` | required | Gateway-only credential forwarded as `X-Service-Token` for Auth admin user-management routes and current-user profile/password write routes; must be non-empty and differ from `GATEWAY_INTERNAL_SERVICE_TOKEN`. |
| `GATEWAY_GITHUB_TOKEN` | unset | Optional backend-only GitHub token for app-version freshness checks. Never expose this value to the frontend. |
| `GATEWAY_APP_VERSION_CURRENT_SHA` | unset | Full 40-character frontend build SHA trusted to trigger the GitHub compare check. `scripts/local/run-backend.sh` auto-fills the repository `HEAD` for the current process when this is unset; production deployments should set it to the served frontend artifact commit. |
| `GATEWAY_APP_VERSION_ALLOWED_SHAS` | unset | Optional comma-separated additional full 40-character frontend build SHAs that may trigger the GitHub compare check. When neither this list nor `GATEWAY_APP_VERSION_CURRENT_SHA` trusts the requested SHA, Gateway returns `unknown` without calling GitHub. |
| `GATEWAY_AUTH_BASE_URL` | `http://localhost:8001` | Auth service base URL for user/session public routes. Must be an absolute `http` or `https` URL without credentials, query, or fragment. |
| `GATEWAY_KNOWLEDGE_BASE_URL` | unset | Knowledge service base URL for knowledge-owned active routes. Must follow the service base URL rule above when non-empty. |
| `GATEWAY_QA_BASE_URL` | unset | QA service base URL for QA-owned active routes. Must follow the service base URL rule above when non-empty. |
| `GATEWAY_DOCUMENT_BASE_URL` | unset | Document service base URL for document-owned active routes. Must follow the service base URL rule above when non-empty. |
| `GATEWAY_AI_GATEWAY_BASE_URL` | unset | AI Gateway base URL for admin model-profile routes. Must follow the service base URL rule above when non-empty. |

## Metrics

The gateway exposes Prometheus metrics on a separate internal port (default `:9091`)
so that `/metrics` is never reachable through the public gateway port.

Metrics exposed:

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `gateway_http_requests_total` | Counter | `method`, `path`, `status` | Total HTTP requests handled by the gateway. `path` is the matched route pattern (e.g. `GET /api/v1/sessions`), never the raw URL. |
| `gateway_http_request_duration_seconds` | Histogram | `method`, `path`, `status` | Request duration in seconds. |

Verify locally after starting the server:

```bash
curl -s http://localhost:9091/metrics | grep gateway_http
```

## Tests

Run service-local checks from this directory:

```bash
go test ./...
go build ./cmd/server
```

## Runtime Behavior

- `POST /api/v1/users` and `POST /api/v1/sessions` call auth internal
  `/internal/v1/**` resources, then cache the returned session identity in
  Redis.
- Protected routes require `Authorization: Bearer <accessToken>` and resolve
  identity from Redis before proxying.
- Gateway injects `X-Request-Id`, `X-User-Id`, `X-User-Roles`,
  `X-User-Permissions`, `X-Forwarded-For`, and `X-Forwarded-Proto` into
  downstream requests.
- Active routes from `docs/services/gateway/docs/active-api-owner-map.md` are
  registered. Missing `admin-overview` and `admin-metrics` placeholders are not
  implemented.
- Binary content and `text/event-stream` responses are streamed from downstream
  without wrapping them in a JSON envelope.
- When `GATEWAY_MAX_IN_FLIGHT` or `GATEWAY_AUTH_REFRESH_MAX_IN_FLIGHT` is
  saturated, Gateway returns the standard error envelope with
  `429 rate_limited`. Gateway does not set `Retry-After` for process-local
  saturation because it does not know a stable remaining wait time.

## Scope Guardrails

- Gateway initializes Redis only for short-lived session cache entries.
- Gateway does not persist auth, file, knowledge, QA, document, or AI Gateway
  business state.
- Public JSON responses use `{ "data": ..., "requestId": ... }` for success and
  `{ "error": { "code": ..., "message": ..., "requestId": ... } }` for errors.
- Gateway must not call SQL, MinIO, runtime doc engines, or model providers directly.

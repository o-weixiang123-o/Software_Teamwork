# Design

## Boundaries

This task changes two backend surfaces:

- `services/knowledge`: the Go contract adapter that maps Gateway-owned
  internal routes to the vendored runtime API.
- `services/knowledge-runtime`: the Python RAGFlow runtime snapshot trimmed for
  Knowledge-owned parsing, indexing, and retrieval.

The Gateway/public API envelope remains stable. Behavior changes should be
limited to making previously silent fallbacks explicit, bounded, or rejected.

## Adapter Design

### Document Dataset Context

Vendor document operations need a dataset/knowledge-base ID. The current
adapter scans every dataset to infer that context. Replace this with explicit
context:

- Accept `knowledgeBaseId` query parameter for document routes that are already
  shaped as `/internal/v1/documents/{documentId}[/**]`.
- Return `400 validation_error` when it is required and missing.
- Use direct `GetDatasetDocument(userID, kbID, documentID)` once the context is
  present.

This preserves existing route paths while removing the unbounded scan.

### Request Validation

`chunkStrategy` is a project-facing JSON blob. Invalid JSON must be rejected
before reaching vendor mapping. The builder functions should return a validation
error when `json.Unmarshal` fails instead of silently omitting
`parser_config`.

### Retrieval Trace

The existing trace fields are shaped for older Qdrant-owned Knowledge behavior.
Vendor mode cannot truthfully provide all fields. Keep the trace object but
avoid false facts:

- Use configured `KNOWLEDGE_VENDOR_EMBEDDING_ID` and
  `KNOWLEDGE_VENDOR_RERANK_ID` when available.
- Use `runtimeEngine: vendor` / `docEngine: runtime` style fields only if the
  DTO can be safely extended.
- For legacy fields that must remain, prefer explicit empty or
  `runtime-managed` values over hard-coded `vendor-default` / `elasticsearch`.

### Vendor Error Classification

Teach `vendorclient.APIError` to carry HTTP status when available. Map 404 from
HTTP status or stable vendor code, not by matching free-form message strings.
Tests should verify that arbitrary message text does not decide 404 behavior.

## Runtime Design

### Auth Types

Runtime auth must remain Gateway-service-token based. Legacy constants can stay
for imports, but `_load_user(auth_types)` must reject routes whose declared auth
types exclude `GATEWAY`.

### Tenant Provisioning

Runtime tenant auto-provisioning exists to bridge Gateway identities into
RAGFlow's user/tenant schema. Make it explicit:

- Add env-controlled `KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS` defaulting to
  `true` for current local compatibility.
- When disabled and tenant is missing, return unauthorized/provisioning error
  without writing synthetic user/tenant rows.
- Keep provisioning implementation behind a clearly named helper.

### RAPTOR Dataset Scope

The runtime currently uses a fake document ID for dataset-level RAPTOR tasks.
Full schema migration is high-risk inside this task, so the first safe step is:

- Replace ambiguous `fake_doc_id` parameter naming with explicit
  `task_scope_doc_id` / `dataset_task_doc_id` constants.
- Validate dataset-level fake IDs cannot collide with real document IDs.
- Add tests around task payload shape and cleanup behavior.

If the existing schema requires `Task.doc_id`, retain a contained compatibility
value with explicit naming and comments.

### Empty Embedding Inputs

Literal `"None"` embedding should not be indexed as content. Introduce a helper
that marks empty inputs as skipped/non-indexable for chunk embedding. Query
embedding can still reject empty query with validation when possible; if runtime
internals cannot change the route shape safely, return a clear error rather
than embedding a placeholder.

### Metadata Fallback

Pushdown is the preferred metadata filter path. In-memory fallback is acceptable
only below a bounded cap:

- Add a config default such as `METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT=10000`.
- When pushdown fails and the metadata candidate count exceeds the cap, return a
  clear error/degraded result instead of loading everything into memory.
- Keep small-result fallback for compatibility.

## Compatibility And Rollback

- Most changes should be guarded by config defaults that preserve current local
  behavior where necessary, but should make unsafe paths visible.
- Adapter validation changes are intentionally stricter and may expose client
  bugs earlier.
- Rollback is file-scoped: adapter changes are isolated under
  `services/knowledge/internal`, runtime changes under targeted Python modules.

## Documentation

Update `services/knowledge/README.md`, `services/knowledge-runtime/README.md`,
or backend specs only where a runtime env key or behavior changes.

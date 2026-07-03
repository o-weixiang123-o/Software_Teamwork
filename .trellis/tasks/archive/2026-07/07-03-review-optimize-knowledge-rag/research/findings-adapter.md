# R4 Findings — Go Adapter Integration Surface
Status: COMPLETE — merged from two R4 agents (original incremental-writer covered map.go/vendorclient; time-boxed successor covered handlers/auth/server/response/config) + orchestrator verification. Combined counts: P0=0, P1=5, P2=7.

## [P1] topK not forwarded as `size` — result count silently pinned to runtime default 30
Anchor: services/knowledge/internal/adapter/map.go:594-632 (buildRetrievalBody) + services/knowledge-runtime/api/apps/services/dataset_api_service.py:1265 (size = req.get("size", 30)) + rag/nlp/search.py:701-703 (end = begin + page_size).
What: adapter sends `top_k` (candidate pool) but sends `size` (page size = returned-result count) ONLY when rerank && rerankTopN set. Runtime defaults size=30. So knowledge-queries with topK=5 return up to 30 results (more than requested), and topK=100 return at most 30 (silently truncated). Trace.SearchTopK reports the client's topK, contradicting the actual result count.
Trigger: any POST /internal/v1/knowledge-queries with topK != 30 and no rerankTopN.
Fix: always set payload["size"] = topK (and keep top_k >= size); keep rerank clamp.

## [P1] DownloadDocument returns runtime JSON error envelope as file bytes
Anchor: services/knowledge/internal/vendorclient/client.go:330-353 + services/knowledge-runtime/api/apps/restful_apis/document_api.py:1916-1975 (download route errors via get_data_error_result/construct_json_result = HTTP 200 JSON).
What: runtime download errors ("Document not found!", "This file is empty.") are HTTP 200 + JSON envelope. DownloadDocument only rejects status >= 400, so the JSON error body is passed through and served by handleGetDocumentContent (handlers.go:348-377) as document content with HTTP 200 and Content-Type application/json.
Trigger: storage object missing/empty for an existing document row (upload raced/GC'd), or any 200-enveloped denial; adapter pre-check GetDatasetDocument does not cover storage state.
Fix: in DownloadDocument, if Content-Type is application/json, attempt envelope decode; code != 0 -> APIError.

## [P1] PATCH document (tags) response omits the tags just written
Anchor: services/knowledge/internal/adapter/handlers.go:242-280 + map.go:563-567 + services/knowledge-runtime/api/apps/restful_apis/document_api.py:236-247 (returns map_doc_keys(doc) from Document model).
What: runtime update_document persists meta_fields via DocMetadataService, but its response re-reads the peewee Document model, which has NO meta_fields column (removed; doc_metadata_service.py:20). map_doc_keys therefore emits no meta_fields; tagsFromVendor -> nil; adapter's 200 response has "tags" absent even though the write succeeded. Subsequent GET (list path merges metadata at document_service.py:106-108) shows them.
Trigger: every PATCH /internal/v1/documents/{id} with tags.
Fix (adapter-side, minimal): after UpdateDocument success, re-fetch via GetDatasetDocument and map that; or merge requested tags into response.

## [P2] documentChunkFromVendor builds metadata map, never returns it (dead code; Metadata always absent)
Anchor: services/knowledge/internal/adapter/map.go:273-297.
What: metadata map (all non-excluded vendor keys) is computed but the returned documentChunkSummary never sets Metadata — dead code. Consequence: no internal-field leak (the feared q_*_vec/tenant passthrough cannot happen today), but the DTO's `metadata` field is always absent. Also TokenCount/SectionPath/ChunkType always zero/absent and CreatedAt is zero time ("0001-01-01T00:00:00Z") because the runtime list_chunks response (chunk_api.py:405-489) carries none of token_count/create_time. If metadata is ever wired up, the exclusion list is WRONG for this route: runtime emits `document_id`/`dataset_id`/`docnm_kwd`/`tag_feas`/`positions` (not doc_id/kb_id), so IDs and internal ranking features would leak.
Fix: delete the dead map or assign it with an allowlist (important_keywords, questions, positions, available, image_id).

## [P2] mapRetrievalChunk fabricates chunkIndex=0 and byte-truncates UTF-8
Anchor: services/knowledge/internal/adapter/map.go:336-359.
What: (a) retrieval chunks (search.py:721-739) carry neither chunk_index nor page_num_int; intField returns 0 and `idx >= 0` always true, so every result gets chunkIndex=0 instead of omitting the optional field. (b) content[:240] is a byte slice; Chinese content (3-byte runes) splits a rune at the boundary ~2/3 of the time; json.Marshal replaces the invalid tail byte with U+FFFD — client sees a mangled trailing char in contentPreview. No panic/marshal error.
Fix: only set chunkIndex when the key exists; truncate on rune boundary (e.g. strings.ToValidUTF8 after cut or iterate runes).

---

# Part 2 — handlers/auth/server/response/config (time-boxed second agent)

## [P1] knowledge-statistics unbounded N+1 vendor fan-out
Anchor: services/knowledge/internal/adapter/handlers.go:440-478.
What: every GET /internal/v1/knowledge-statistics pages through ALL datasets (100/page loop) then calls ListDocuments once per dataset sequentially: no cache, no concurrency bound, no dataset-count cap — N knowledge bases = ceil(N/100) + N vendor round-trips. Dashboard polling amplifies RAGFlow load and latency.
Fix: short-TTL cache; or aggregate `doc_num` already present in the dataset list response (map.go:216 reads it) instead of per-dataset ListDocuments; or bounded concurrency.

## [P1] JSON endpoints lack request-body size limit
Anchor: handlers.go:498-511 (decodeJSONBody); only the upload path has http.MaxBytesReader (handlers.go:513-514).
What: /knowledge-queries, KB create/update, document update, parser-configs all decode unbounded bodies into memory. Defense-in-depth gap (callers hold service token), hence P1 not P0.
Fix: add MaxBytesReader (e.g. 1 MiB) inside decodeJSONBody and map MaxBytesError to a validation error.

## [P2] statistics endpoint returns 200 empty payload when X-User-Id missing
Anchor: handlers.go:418-423. Inconsistent with 401 semantics of all other /internal/v1/ routes; masks gateway misconfiguration.

## [P2] redundant GetDatasetDocument pre-checks
Anchor: handlers.go:264,331,362. Update/list-chunks/download do an existence probe before the vendor call that already validates (userID, kbID, docID) — doubled round-trips + TOCTOU window.

## [P2] panic recovery may double-write response header
Anchor: server.go:72-84. If handler panics after writing headers, writeAppError calls WriteHeader again; panic value/stack not logged. Fix: guard on recorder.status == 0, log panic value.

## [P2] document download fully buffers file in memory
Anchor: handlers.go:367-377. DownloadDocument returns []byte; bounded today by the upload cap, streaming would remove the cap.

## [P2] secureTokenEqual length short-circuit leaks token length
Anchor: auth.go:108-115. Info-grade; HMAC comparison removes the length oracle.

## [P2] statusRecorder drops http.Flusher/io.ReaderFrom
Anchor: handlers.go:537-545. Harmless now; silently breaks if download switches to streaming.

## [P2] newRequestID falls back to constant "req_fallback"
Anchor: handlers.go:547-553. RNG failure gives every request the same ID; timestamp fallback is better.

## Clean areas (verified)
- Service-token gate fail-closed: empty configured token rejects (auth.go:22-24); config.Load() hard-fails on missing tokens (adapterconfig/config.go:67-72); constant-time compare.
- readScope/mutationScope consistent across handlers; all /internal/v1/ routes pass gatewayContext except the statistics quirk above.
- JSON decode: DisallowUnknownFields + trailing-content rejection; upload path size-limited, empty-file rejected, auto-parse failure rolls back with cleanup logging.
- Error envelope (response.go): consistent status mapping, requestId propagated, unknown errors collapse to non-leaking 500.

## Orchestrator verification notes (map.go items from Part 1)
- Metadata dead-code (Part 1 P2) CONFIRMED by orchestrator read: documentChunkFromVendor computes `metadata` map (map.go:279-287) but the return (map.go:288-297) never assigns .Metadata — no leak possible today; runtime list_chunks whitelist (chunk_api.py:443-455) additionally contains no q_*_vec/tenant fields.
- topK/size P1 (Part 1) consistent with orchestrator Stage A read of buildRetrievalBody (map.go:594-632): "size" only set under Rerank && RerankTopN.
- Not deep-reviewed anywhere: handlers_parser.go, internal/mcp/*, vendorclient/client.go beyond download/error-envelope paths (error-mapping theme covered from runtime side by R3).

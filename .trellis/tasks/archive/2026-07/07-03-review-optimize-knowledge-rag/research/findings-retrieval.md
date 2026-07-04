# R3 Findings — Retrieval + Runtime API (correctness/robustness)

Product retrieval path: Go adapter RetrievalSearch -> POST /api/v1/datasets/search ->
dataset_api.py:search_datasets -> dataset_api_service.search_datasets. The /retrieval route
(chunk_api.py:retrieval_test) and /datasets/<id>/search are NOT on the adapter path (SDK-only),
reported at reduced severity.

Adapter error contract (client.go:391 doJSON + map.go:157 mapVendorError): adapter treats any
runtime response with envelope code != 0 OR HTTP >= 400 as APIError, classifies ONLY by HTTP
401/403/404, everything else -> 502 dependency_error. Runtime returns almost all errors as HTTP
200 + non-zero code, so they map to 502.

## [P1] Empty/unindexed dataset retrieval returns 502 instead of empty results
Anchor: dataset_api_service.py:1352-1366 (search_datasets retrieval call, no try/except) + route
dataset_api.py:497-504 (no try/except). Source: es_conn.py:296-302 (ES raises index_not_found).
What: valid owned dataset with no chunk index yet -> retrieval raises index_not_found_exception ->
uncaught -> global handler api_utils.py:145-146 -> code=EXCEPTION_ERROR(100) HTTP 200 -> adapter
-> 502. Frontend sees dependency failure for normal "no results yet".
Trigger: query dataset whose docs are unparsed / still UNSTART. On primary product path.
Conflict: api-contracts.md says zero hits -> empty results; only real dependency failure -> 502.
retrieval_test (chunk_api.py:307-308) already returns empty for empty question; search_datasets
has no equivalent guard.
Fix: wrap retrieval; on index_not_found/not_found return True,{"total":0,"chunks":[],"labels":labels,"doc_aggs":{}}.

## [P1] All search_datasets business-validation failures collapse to 502 (unstable codes)
Anchor: dataset_api.py:497-504 (route maps every (False,msg) -> get_error_data_result code=102
HTTP 200). Service returns at dataset_api_service.py:1284,1288,1293,1296,1319,1328.
What: access denied, datasets not found, different embedding models, doc_ids not list, metadata
fallback too large, owner mismatch -> all become HTTP200 code=102 -> adapter 502 (never 400/403/404).
PR #536 stable-code hardening defeated because runtime emits one opaque 102 for validation/authz/
capacity alike. Mixed-embedding dataset set or oversized metadata filter -> 502 instead of 400.
Note: doc_ids-not-list unreachable from adapter; per-kb access already enforced adapter-side. Harm
is wrong status class, not a hole. P1 because stable-code contract is the explicit target.
Fix: raise typed exceptions mapped to distinct RetCode/HTTP, or at minimum ARGUMENT_ERROR for
input-shape + embedding-mismatch; surface MetadataFilterFallbackTooLarge as distinct code not 502.

## [P1] search_datasets/search tenant_ids resolution drops cross-tenant (team-shared) datasets
Anchor: dataset_api_service.py:1321-1328 (search_datasets) and :981-988 (search).
What: after per-kb access verified, code appends only the FIRST joined-tenant owning ANY one kb
then break. When kb_ids span multiple tenants (team-permission dataset in tenant A + own dataset
in tenant B, both accessible), tenant_ids has a single tenant. retrieval builds idx_names from
tenant_ids (search.py:619) so the other tenant's index is never searched -> silent partial results,
no error. Embedding-model uniqueness check (1291-1293) does NOT prevent this (two tenants can
share a model).
Trigger: multi-dataset query resolving to different tenant_ids (owner vs team-shared).
Fix: tenant_ids = list({kb.tenant_id for kb in kbs} & {t.tenant_id for t in UserTenantService.query(user_id=tenant_id)});
error if empty. kbs already loaded at line 1286.

## [P1] retrieval_test swallows retrieval errors -> 502; tenant list here IS correct
Anchor: chunk_api.py:399-402 (except: "not_found" -> DATA_ERROR else server_error_response).
What: on SDK-only /retrieval, genuine dependency error and empty index both -> HTTP200 non-zero
code -> 502 if adapter-facing. Unlike search_datasets, retrieval_test computes tenant_ids =
list(set([kb.tenant_id for kb in kbs])) (line 347) correctly across all datasets, so the
multi-tenant bug does NOT affect this route. Not on adapter path, bounded severity; flagged so the
fix is applied consistently.
Fix: same empty-result guard; keep correct tenant_ids derivation.

## [P1] similarity_threshold / top_k unbounded on raw /retrieval route
Anchor: chunk_api.py:331-335.
What: top=int(req.get("top_k",1024)), only top<=0 rejected; NO upper bound (service path clamps
max(1,min(top_k,2048)) at dataset_api_service.py:1270, this route does not). similarity_threshold
not clamped to [0,1]. int()/float() raise ValueError on non-numeric before try -> 500-class.
page_size IS bounded via validate_rest_api_page_size (305).
Trigger: SDK caller sends top_k=10_000_000 or malformed threshold to /retrieval.
Note: adapter path uses SearchDatasetsReq/SearchDatasetReq pydantic (validation_utils.py:908-946)
which bound top_k>=1, threshold 0..1, page/size>=1 -> adapter path safe. Raw-route-only gap.
Fix: clamp top max(1,min(top_k,2048)); validate 0<=threshold<=1; wrap casts -> ARGUMENT_ERROR.

## [P2] list_chunks pagination: page<1 unbounded lower end
Anchor: chunk_api.py:420-467.
What: page=int(req.get("page",1)) no >=1 guard; negative page -> negative offset downstream.
int() on non-numeric raises before handling. res["total"]=sres.total is engine total (correct);
empty-index branch returns total:0,chunks:[] (434,459) not error (good). Adapter path (ListChunks).
Fix: page=max(1,int(...)) guarded numeric parse, mirroring adapter normalizePage.

## [P2] list_datasets pagination + total key — verified clean
Anchor: dataset_api_service.py:345-391; api_utils.py:267-287 (total_datasets key).
ListDatasetReq.validate_page_size (validation_utils.py:967-970) already caps page_size; service
re-read is bounded. total emitted as total_datasets, adapter reads envelope.total_datasets
(client.go:65) — matched. Statistics aggregation loop int64(page*pageSize)>=total consistent. Clean.

## [P2] Metadata manual filter semantics — verified consistent, one caveat
Anchor: map.go:635-668 (vendorMetaDataFilter); metadata_utils.py:77-206 (meta_filter);
metadata_es_filter.py + doc_metadata_service.py:892-971.
Adapter emits {method:manual,logic:and,manual:[{key,op:"=",value},{key:tags,op:contains,value}]}.
= and contains both in SUPPORTED_OPERATORS and multivalue-safe (MULTIVALUE_UNSAFE only ≠,not in);
push-down and in-memory agree; and logic honored both paths; tag->contains on field tags; no-match
manual -> doc_ids=["-999"] sentinel guarantees empty (327-328). Caveat: ES total>limit raises
MetadataFilterFallbackTooLarge (960-968) -> (False,msg) -> 502 (covered by P1#2); fails closed
(matches "over-cap must fail clearly"). Bounded fallback 10000 enforced both engines
(metadata_utils.py:264-273 + doc_metadata_service.py:947-968) + fail-closed. PR#440 complete. Clean.

## [P2] doc_ids cross-dataset ownership in retrieval_test — coarse but not a leak
Anchor: chunk_api.py:315-319. list_documents_by_ids(kb_ids) returns docs for all access-checked
kb_ids (296-297); doc accepted iff in caller-owned union — no cross-tenant leak. Does not bind doc
to specific dataset, acceptable for union retrieval. Tenant isolation preserved. Clean.

## [P2] Gateway auth timing-safe; provisioning race idempotent — verified clean
Anchor: gateway_auth.py:30-39; gateway_tenant_provisioning.py:42-95; gateway_tenant_service.py;
__init__.py:113-163.
- hmac.compare_digest (gateway_auth.py:39) timing-safe; empty token -> False (fail closed).
- route_allows_gateway_auth rejects legacy-only JWT/API/BETA routes lacking GATEWAY (__init__.py:116-117).
- Race: ensure_gateway_tenant_with_store wraps in store.atomic() (line 48); every create swallows
  peewee.IntegrityError and re-reads (gateway_tenant_service.py:48-96); deterministic normalized id
  -> concurrent first-requests converge. Idempotent.
- AUTO_PROVISION_TENANTS=false: provision_gateway_tenant_if_enabled returns None w/o provisioner
  (36-39); _load_user sets clear disabled error, returns None -> 401, NO writes (__init__.py:150-157).
Minor (not defect): _load_user catches broad Exception -> 401 "Tenant not found" (159-161); a
transient DB failure during provisioning reports as 401 not 502. Fails closed, no leak, but can
mislead operators. PR#440/#536 auth guardrails complete. Clean.

## [P2] _load_user sets g.user before token validation — no leak, ordering fragile
Anchor: __init__.py:122-133. g is per-request (no cross-request bleed); g.user set to real user
only after valid token AND resolved/provisioned tenant; missing/invalid token -> None -> 401 via
login_required (184-186). No bypass. Clean.

## Clean areas explicitly checked
timing-safe token; legacy-only routes rejected; provisioning idempotency; false->no writes;
metadata =/contains/and agree adapter/ES/in-memory; bounded fallback both engines + fail-closed;
retrieval_test doc_ids scoped to owned union; empty-index chunk listing returns empty page;
per-kb accessible enforced on every route before doc-store access (tenant isolation holds on
executed path); llm_service.encode rejects empty/whitespace/None (58-77); rerank_id resolved only
when present, failure handled by retrieval except; rerank size vs top_k: adapter min(RerankTopN,topK)
(map.go:624-630), runtime clamps top 2048 + size through rerank window — consistent.

## Counts
P0: 0
P1: 5 (empty-index->502; validation->502 unstable codes; cross-tenant tenant_ids drop;
retrieval_test error-swallow; raw /retrieval unbounded top_k/threshold)
P2: 7 (list_chunks page<1; list_datasets clean; metadata semantics clean; doc_ids coarse-safe;
auth clean; provisioning-race clean; g.user ordering)

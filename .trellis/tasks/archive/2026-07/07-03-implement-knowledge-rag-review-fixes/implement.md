# Implementation Plan

## Ordered Checklist

1. Inspect current runtime and adapter code/tests around the report anchors.
2. Implement runtime fixes:
   - `TaskService.get_task` `doc_ids` join substitution;
   - zero-task scheduling failure path;
   - parser/OCR failure progress preservation for empty-chunk fallbacks;
   - parser data-integrity fixes;
   - empty-index retrieval guard;
   - P1-C2 structured search business errors and HTTP status mapping.
3. Implement Go adapter fixes:
   - `topK -> size` mapping;
   - JSON error-envelope detection in downloads;
   - PATCH tags response preservation;
   - statistics aggregation without unbounded N+1 calls;
   - bounded JSON request decoding;
   - runtime body-code to adapter error-code mapping.
4. Add focused tests using existing runtime/adapter test patterns.
5. Sync Knowledge internal/OpenAPI and Gateway query error docs for the updated mapping.
6. Run validation:
   - runtime focused pytest/compile checks where available;
   - `cd services/knowledge && go test ./...`;
   - any narrower package tests required by failures.
7. Review diff for accidental unrelated changes, staged Trellis archive preservation, and generated artifacts.

## Validation Commands

Preferred commands, adjusted after inspecting test harness:

```bash
cd services/knowledge && go test ./...
cd services/knowledge-runtime && uv run pytest <focused tests>
cd services/knowledge-runtime && uv run python -m compileall api rag deepdoc
```

## Risk Points

- Runtime vendored files should get the smallest possible diffs.
- Python runtime tests may require local services or environment variables; if so, run focused unit-level tests and report any blocked integration checks.
- The working tree already contains staged Trellis archive files from the previous review task; preserve them and avoid mixing them into code reasoning.

## P1-C2 Mapping

- Request shape / invalid cross-dataset combination / metadata fallback too large: runtime `400 ARGUMENT_ERROR`, adapter `validation_error`.
- Missing or hidden dataset: runtime `404 NOT_FOUND`, adapter `not_found`.
- Empty index: success with empty results.
- Unexpected runtime dependency or infrastructure failures: adapter `dependency_error`.

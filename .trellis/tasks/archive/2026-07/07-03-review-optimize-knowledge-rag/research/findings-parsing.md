# R2 Findings — Document Parsing Path (correctness/robustness)

Reviewer: R2 agent, 2026-07-03. Branch `L1nggTeam/feat/ragflow-runtime-vendor`.

Scope: rag/app chunkers (naive, table, qa, paper, book, presentation, manual, laws, one, picture), deepdoc/parser (pdf, docx, excel, txt, markdown, html, json, ppt, figure, utils), deepdoc/server/adapters (dla, tsr), queue_tasks excel row-split contract.

STATUS: COMPLETE (see totals at end of file).

---

## [P0] Corrupt/unreadable PDF (and 0-row table file) leaves document stuck in RUNNING forever — queue_tasks creates zero tasks

**Anchor:** `services/knowledge-runtime/api/db/services/task_service.py:400-430, 467` + `deepdoc/parser/pdf_parser.py:1517-1525` + `api/db/services/document_service.py:917-924`

**What:** `PdfParser.total_page_number()` swallows all exceptions and returns `None` (pdf_parser.py:1524-1525). `queue_tasks` maps `None → pages = 0` (task_service.py:403-404), so the page loop `range(s, min(e-1, 0), page_size)` produces **zero tasks**. Same for `parser_id=="table"` when `row_number` returns `0` (empty/only-empty-sheets workbook). With `parse_task_array == []`, the function still runs `bulk_insert_into_db(Task, [], True)` and `DocumentService.begin2parse()` (task_service.py:467), which sets `run=RUNNING, progress≈0.005`. No task is ever queued, so nothing will ever update the document. The periodic sweeper selects the doc (`get_unfinished_docs`: `0 < progress < 1`) but `_sync_progress` immediately skips it because `TaskService.query(doc_id=...)` is empty (`if not tsks: continue`, document_service.py:923-924). Result: document shows "parsing…" at ~0% **forever**, no error message; re-parse is blocked by the "currently being processed" guard (chunk_api.py:190-200) until the user manually cancels.

**Trigger:** Upload a corrupt/truncated/password-protected PDF (pdfplumber.open raises), a 0-byte PDF, or an Excel file whose sheets are all empty with `docType=table` — then trigger parse. All are ordinary user inputs.

**Fix:** In `queue_tasks`, after building `parse_task_array`, if it is empty either (a) raise a clean error *before* `begin2parse` so the API returns a meaningful failure and the doc goes to FAIL, or (b) mark the document FAILED with a message ("cannot read PDF page count / no rows found"). Additionally `total_page_number` should re-raise or return a sentinel distinguishable from "0 pages".

## [P1] queue_tasks raises TypeError (500) for `table` parser on non-xls/csv/txt filenames, after doc already marked RUNNING

**Anchor:** `services/knowledge-runtime/deepdoc/parser/excel_parser.py:299-317` + `api/db/services/task_service.py:423-424` + `api/apps/restful_apis/chunk_api.py:189-216`

**What:** `RAGFlowExcelParser.row_number()` returns `None` implicitly for any filename whose extension is neither `*xls*` nor `csv`/`txt` (no final return). `queue_tasks` then executes `range(0, None, 3000)` → `TypeError: 'NoneType' object cannot be interpreted as an integer`. The parse API has already set the document to `run=RUNNING` (chunk_api.py:189-199) before `queue_tasks` is called, so the caller gets an opaque 500 (global `server_error_response`) and the document is left RUNNING with zero tasks — same stuck state as the P0 above (recoverable only via manual cancel).

**Trigger:** Any document whose stored name lacks an xls/csv/txt extension (e.g. `.tsv`, `.ods`, `.docx`, or extensionless) with `chunk_method=table` — the adapter passes `docType` through as a free string (reachability map: no enum gate), so this is one misconfigured API call away.

**Fix:** `row_number` should raise a descriptive ValueError ("table parser supports xlsx/csv/txt") or return 0 plus the empty-array guard from the P0 fix; validate parser_id/file-extension compatibility before flipping the doc to RUNNING.

## [P1] Excel embedded-image descriptions corrupted to one-character-per-line ("\n".join over str)

**Anchor:** `services/knowledge-runtime/rag/app/table.py:62-63` + `deepdoc/parser/figure_parser.py:274-277`

**What:** `VisionFigureParser.__call__` mutates `self.descriptions[i]` from a **list** to a **str** when the vision model returns text (figure_parser.py:277: `self.descriptions[figure_num] = txt + "\n".join(...)`). `Excel.__call__` in table.py then does `images[i]["image_description"] = "\n".join(bf[0][1])` — joining a *string* character by character. A description like "This chart shows…" becomes `T\nh\ni\ns\n …`. The corrupted text is written into the row cell (single-cell images) or emitted as an image chunk via `tokenize_table` (flow images), destroying tokenization/retrieval for those chunks.

**Trigger:** `chunk_method=table` + xlsx containing embedded images + tenant has an IMAGE2TEXT model (the enhancement only activates then) + vision call succeeds (the buggy branch is the *success* path; on failure descriptions stay lists and join correctly).

**Fix:** In table.py guard: `desc = bf[0][1]; images[i]["image_description"] = desc if isinstance(desc, str) else "\n".join(desc)` — or make `VisionFigureParser` keep descriptions as lists consistently.

## [P1] Markdown parse fails entirely (TypeError) when vision model returns empty for any section image — deterministic with <11px images

**Anchor:** `services/knowledge-runtime/rag/app/naive.py:1051-1053` + `rag/app/picture.py:163-186` + `deepdoc/parser/figure_parser.py:276-277`

**What:** On the naive markdown path, `"\n\n".join([fig[0][1] for fig in boosted_figures])` assumes each description is a str. Descriptions are only converted to str when the vision call returns non-empty text; `vision_llm_chunk` returns `""` for images with any side < 11px (picture.py:166-169) and for *any* vision-model exception (picture.py:183-186, which also fires `callback(-1)`). In those cases `fig[0][1]` stays a **list** → `TypeError: sequence item 0: expected str instance, list found` → the whole `chunk()` raises → task FAILED. Unlike the docx/pdf/excel wrappers (figure_parser.py:62-63, 89-90, 130-131), this call site has no try/except, so one bad/tiny image kills the whole document parse instead of degrading.

**Trigger:** Naive chunker + `.md` file whose section images include a tiny image (1px tracker, small icon) or any transient vision-model error, with an IMAGE2TEXT model configured. Deterministic for tiny images.

**Fix:** Wrap the VisionFigureParser call in try/except like the other wrappers, and normalize `fig[0][1]` to str (`desc if isinstance(desc,str) else "\n".join(desc)`).

## [P1] Layout-engine failure ends as document DONE with 0 chunks — "No chunk built" (prog=1.0) overwrites the -1 error state

**Anchor:** `services/knowledge-runtime/rag/svr/task_executor.py:1360-1362` + `rag/app/naive.py:294-301, 181-183, 961-962` + `api/db/services/task_service.py:342-347`

**What:** Three interacting behaviors: (1) `by_paddleocr` returns `(None, None, None)` **without any callback** when the OCR model can't be resolved or `parse_pdf` throws — the `callback(-1, "PaddleOCR not found.")` at naive.py:299-301 is unreachable in worker context because it requires `tenant_id` to be falsy (tasks always carry tenant_id). `by_mineru`/`by_docling`/`by_tcadp` at least call `callback(-1, "... not found.")` before returning None. (2) `chunk()` maps `sections=None, tables=None` to `return []` (naive.py:961-962). (3) `do_handle_task` sees empty chunks and calls `progress_callback(1.0, "No chunk built …")` — and `TaskService.update_progress`'s rule `(prog >= 1)` explicitly bypasses the `-1` latch (task_service.py:342-347), then the failed-doc re-sync clause in `get_unfinished_docs` flips the document from FAIL back to DONE. Net result: OCR backend outage/misconfig → every PDF "parses successfully" with **zero chunks**; the error (if any) is buried in the progress log. Same mechanism converts the `build_chunks` size-limit rejection (task_executor.py:257-260: `set_progress(-1, "File size exceeds…")` then `return []`) into a DONE/0-chunk document. Per the reachability map, PaddleOCR is the remote-compatible **default** layout engine for this deployment, so this is the primary failure mode when the OCR service is down.

**Trigger:** Parse any PDF while the PaddleOCR/MinerU model is unregistered or the OCR endpoint errors; or upload a file larger than DOC_MAXIMUM_SIZE.

**Fix:** In `do_handle_task`, treat "chunker returned empty AND task progress already -1 (or a parser-failure sentinel)" as failure — don't emit prog=1.0. In `by_paddleocr`, move the `callback(-1, …)` so the model-missing/parse-error path in worker context actually reports, and/or re-raise instead of returning None triples.

## [P2] Debug `print(sections)` on the txt path dumps entire document content to stdout

**Anchor:** `services/knowledge-runtime/rag/app/naive.py:1009-1011`

**What:** The default (naive) chunker's txt/code branch executes `print("\n","-"*150,"\n"); print(sections); print(...)` on every parse of `.txt/.py/.js/.sql/...` files — the full parsed content of the document goes to worker stdout. Upstream RAGFlow has no such print; this is a debug leftover. Harm: unbounded log volume for large text files (an N-MB txt emits >N MB of stdout per parse task) and leaks full document contents into container logs.

**Trigger:** Any text/code file parsed with the default chunker.

**Fix:** Delete the three print lines.

## [P2] PlainParser silently truncates the document after a page-level extraction error; corrupt-PDF errors masked as "No chunk built"

**Anchor:** `services/knowledge-runtime/deepdoc/parser/pdf_parser.py:2005-2016`

**What:** The whole page loop sits in one `try/except Exception: logging.exception("Outlines exception")`. If `pypdf` raises on one malformed page mid-document (or open fails), the pages already collected are returned and **all later pages are silently dropped** — no callback, no error; the doc indexes partial content. If open fails entirely (e.g. encrypted PDF in "Plain Text" mode), it returns `([], [])` → feeds the "No chunk built"/DONE path (see P1 above). Vendor-inherited, but Plain Text is a product-selectable layout option.

**Trigger:** Partially corrupt or encrypted PDF with `layout_recognize="Plain Text"`.

**Fix:** Catch per-page, continue on page errors; report open failures via `callback(-1, …)`/raise instead of swallowing.

## [P2] RAGFlowPdfParser.__images__ swallows open errors then crashes with misleading AttributeError

**Anchor:** `services/knowledge-runtime/deepdoc/parser/pdf_parser.py:1537-1588`

**What:** If `pdfplumber.open`/page render raises (partially corrupt PDF that passed `total_page_number`), the exception is logged and swallowed, then execution continues to `self.page_chars` / `len(self.page_images)` (lines 1584-1588) which raises `AttributeError: ... has no attribute 'page_images'`. The task does fail cleanly (handle_task catches), but the user-facing message is "[Exception]: 'Pdf' object has no attribute 'page_images'" instead of the real parse error. Diagnosability defect only.

**Trigger:** PDF that opens for `total_page_number` but fails on re-open/render (page-level corruption).

**Fix:** Re-raise from the except block (or initialize the attrs and raise a descriptive ParserError).

## [P2] Multi-GPU workers: OCR results appended out of page order → page/box pairing scrambled

**Anchor:** `services/knowledge-runtime/deepdoc/parser/pdf_parser.py:786-794 (self.boxes.append), 1622-1649` + `common/settings.py:322`

**What:** With `PARALLEL_DEVICES = torch.cuda.device_count() > 1` (set automatically, not an explicit config), per-page OCR runs concurrently on different device semaphores and each `__ocr` call ends with `self.boxes.append(bxs)` — append order is completion order, not page order. `_layouts_rec` then pairs `page_images[i]` with `boxes[i]` positionally (`assert len(...) ==`, pdf_parser.py:797-798), so layout classification/positions are computed against the wrong page images → garbled sections/positions for the whole task. Single-GPU/CPU deployments are unaffected (sequential path).

**Trigger:** DeepDOC layout engine on a worker host with ≥2 CUDA GPUs.

**Fix:** Pre-size `self.boxes = [None]*n` and assign by index (`self.boxes[pagenum-1] = bxs`), or gather results and append in page order.

## [P1] book chunker drops ALL tables/figures for PDFs — `tables` never merged into `tbls`

**Anchor:** `services/knowledge-runtime/rag/app/book.py:73, 107, 176`

**What:** `chunk()` initializes `sections, tbls = [], []` (line 73). The PDF branch assigns the parser result to a *different* variable: `sections, tables, pdf_parser = parser(...)` (line 107). `tables` is only used for the empty-check (line 121) and is never copied into `tbls`; line 176 indexes `tokenize_table(tbls, …)` — still `[]`. Every table and figure extracted from a book-method PDF is silently discarded (docx branch fills `tbls` correctly). Upstream assigns `sections, tbls = pdf_parser(...)` directly; the divergence came with the PARSERS indirection refactor, so this is a project-introduced regression, not vendor behavior.

**Trigger:** Any PDF parsed with `chunk_method=book` (all layout engines go through line 107).

**Fix:** `tbls = tables` after line 107 (and keep the empty-check).

## [P2] paper chunker crashes with "'NoneType' object is not iterable" when layout engine returns None

**Anchor:** `services/knowledge-runtime/rag/app/paper.py:178-198, 232-235`

**What:** For non-DeepDOC engines, `by_mineru/by_paddleocr/...` return `(None, None, None)` on failure (see P1 "Layout-engine failure" above). paper.py stuffs `sections=None` into `paper["sections"]` without the `if not sections` guard naive/book have, then `bullets_category([txt for txt, _ in sorted_sections])` raises TypeError. Task fails "cleanly" but with a meaningless message, and the real cause (OCR backend down / model unregistered) is invisible.

**Trigger:** `chunk_method=paper` + layout_recognize=PaddleOCR/MinerU when the model is missing or errors.

**Fix:** Guard `if not sections and not tables: raise ValueError("layout engine returned nothing: <engine>")` (an explicit failure is preferable to naive's silent `[]`).

## [P2] Spurious final row-split task crashes on multi-column TAB .txt table files (np.array([]) → DataFrame shape error)

**Anchor:** `services/knowledge-runtime/rag/app/table.py:394-408` + `deepdoc/parser/excel_parser.py:314-317` + `api/db/services/task_service.py:421-428`

**What:** For csv/txt, `row_number` counts `len(txt.split("\n"))` — including the header line and the empty element from a trailing newline — while the chunker enumerates only `lines[1:]`. The count over-shoots by ~2, so when data-row count ≡ 2998/2999 (mod 3000) the final `[from,to)` task contains no parsable rows → `rows == []` → `pd.DataFrame(np.array([]), columns=headers)` raises `ValueError` for ≥2-column headers → that sub-task FAILS, marking the whole document failed even though the file is valid (the CSV branch is safe: `pd.DataFrame([], columns=…)` is legal). Excel over-count (headers included in `_get_actual_row_count`) only yields a benign empty task (`if len(data)==0: continue`).

**Trigger:** `chunk_method=table` + TAB-delimited `.txt` with ≥2 columns and an unlucky row count ≥ ~3000.

**Fix:** In the txt branch: `dfs = [pd.DataFrame(rows, columns=headers)] if rows else []` (list, not np.array), or make `row_number` return the data-row count (`len(lines) - 1`, ignoring trailing empty line).

## [P2] Excel single-cell image descriptions can land in the wrong row; images re-emitted by every row-split task

**Anchor:** `services/knowledge-runtime/rag/app/table.py:54-68, 98-114`

**What:** (a) `df_row_idx = excel_row - header_rows` assumes `df` contains every data row from index 0 — but `data` skips empty rows (line 91-92) / failed rows (89) and, for from_page>0 tasks, starts at row `from_page`. Any of these shifts the mapping, so a vision-generated description is written into the wrong row's cell (or silently demoted to a flow image). (b) `_extract_images_from_worksheet` + the vision wrapper run for *all* sheet images in *every* row-split task (>3000-row workbooks), so flow-image chunks are duplicated once per task and vision calls are repeated per task.

**Trigger:** `chunk_method=table` + xlsx with embedded images + IMAGE2TEXT model; (a) needs an empty row above the image or a multi-task workbook, (b) needs >3000 rows.

**Fix:** Map images by absolute sheet row → position in `data` (track kept row indices); extract/emit images only in the task whose window contains the image's anchor row (e.g. only when `from_page == 0`).

## [P2] Malformed JSON file silently indexes zero chunks

**Anchor:** `services/knowledge-runtime/deepdoc/parser/json_parser.py:130-138`

**What:** `_parse_json` swallows `json.JSONDecodeError` with `pass` (no log), returning `[]`. Combined with the "No chunk built" success path (see P1 above), an invalid `.json` upload (trailing comma, truncation) becomes a DONE document with 0 chunks and no user-visible error. `_parse_jsonl` likewise drops every bad line silently — acceptable for JSONL, but for whole-file JSON the user gets no signal.

**Trigger:** Upload syntactically invalid .json with the naive chunker.

**Fix:** Raise or `callback(-1, "invalid JSON: …")` when whole-file parse fails and no sections were produced.

## [P2] HTML without <body> parses to zero sections (html.parser adds no implied body)

**Anchor:** `services/knowledge-runtime/deepdoc/parser/html_parser.py:73`

**What:** `parser_txt` walks `soup.body` only. BeautifulSoup's `html.parser` builder does **not** synthesize `<html>/<body>` wrappers, so fragment HTML (`<p>…</p>` exports, snippet files with no explicit body tag) yields `soup.body is None` → `read_text_recursively(None, …)` returns immediately → zero sections → silent "No chunk built"/DONE document.

**Trigger:** Upload an .html file lacking an explicit `<body>` element.

**Fix:** `cls.read_text_recursively(soup.body or soup, …)`.

## [P2] qa chunker: progress callback fires on (nearly) every line — `len(res) % 999 == 0` is true while res is empty

**Anchor:** `services/knowledge-runtime/rag/app/qa.py:65-70, 360-362, 393-395`

**What:** The "every 999 pairs" progress guard `if len(res) % 999 == 0` is satisfied whenever `len(res)` is 0 (and stays satisfied at 999/1998… across consecutive non-pair lines). Each callback is a `set_progress` → `TaskService.update_progress` → task row read + progress_msg append under the shared `DB.lock("update_progress")`. Parsing a large txt/xlsx with few or no recognizable Q&A pairs issues one DB round-trip **per line/row** (100k-line file → 100k sequential locked updates), stalling the task for hours and contending the global progress lock used by all executors. Vendor-inherited, but the harm scales with file size on a reachable path.

**Trigger:** `chunk_method=qa` + a large text/excel file whose leading lines aren't Q&A pairs (common misconfiguration).

**Fix:** `if res and len(res) % 999 == 0` (and track last-notified count).

## [P2] qa CSV path pairs csv.reader rows with raw line indices — content corruption on quoted multi-line fields

**Anchor:** `services/knowledge-runtime/rag/app/qa.py:381-386`

**What:** `reader = csv.reader(lines, …)` iterates *records*, but the mismatch branch appends `lines[i]` where `i` is the record index. Once any quoted field spans multiple lines (or rows were skipped), record index ≠ line index, so unrelated raw lines get appended into answers (chunk content corruption), potentially duplicating/misplacing content for the rest of the file. Also line 398 calls `len(list(reader))` on the already-exhausted reader → final pair always gets `top_int=[0]`.

**Trigger:** `chunk_method=qa` + .csv containing quoted multi-line answers.

**Fix:** Append `",".join(row)` (the parsed record) instead of `lines[i]`; track the row counter for top_int before exhaustion.

## [P2] one.py: UnboundLocalError when tika returns empty content for .doc

**Anchor:** `services/knowledge-runtime/rag/app/one.py:145-166`

**What:** Unlike book.py (which initializes `sections = []` at the top) and naive/laws (which `return []`), one.py's `.doc` branch only assigns `sections` inside `if doc_parsed.get("content") is not None:` — for an empty/corrupt .doc, execution reaches `"\n".join(sections)` at line 166 with `sections` unbound → `UnboundLocalError` → task fails with a meaningless message.

**Trigger:** `chunk_method=one` + .doc file from which tika extracts no content.

**Fix:** Initialize `sections = []` at function start or `return []` in the else path.

## [P2] manual.py: IndexError sorting sections when an alt layout engine emits a position-less table

**Anchor:** `services/knowledge-runtime/rag/app/manual.py:234-237, 247` + `deepdoc/parser/docling_parser.py:299,311`

**What:** Docling (and possibly other alt engines) emit tables as `((img, html), positions if positions else "")`. manual.py converts `poss=""` → `[]` (list-comp over empty string) and appends `(rows, -1, [])`, then sorts with key `x[-1][0][0]` → `IndexError: list index out of range` → whole task fails. one.py's equivalent loop (one.py:121-125) is safe because it never indexes poss.

**Trigger:** `chunk_method=manual` + layout_recognize=Docling (config-gated engine) + a table without extractable positions.

**Fix:** Skip or default position when `poss` is falsy: `poss = poss or [(0,0,0,0,0)]` before appending.

## [P2] naive Markdown: html <img> src regex character-class is mangled — URLs truncated at the first letter "s"

**Anchor:** `services/knowledge-runtime/rag/app/naive.py:675`

**What:** `html_img_re = re.compile(r'src=["\\\']([^"\\\'>\\s]+)', ...)` — inside a raw string, `\\s` in a character class is an escaped backslash **plus literal `s`**, not whitespace. So the capture stops at the first `s` character: `src="https://…"` captures `http`. The line-based extractor therefore records garbage URLs (attempted as local paths, each producing a warning). The BeautifulSoup cross-line fallback (lines 690-712) re-extracts the correct src, so images still load — the defect currently costs only wasted lookups/log noise, but the primary extractor is effectively broken and correctness rests on the fallback alone.

**Trigger:** Any markdown file containing HTML `<img src=…>` tags.

**Fix:** Use a normal (non-doubled) escape: `r'src=["\']([^"\'>\s]+)'`.

---

# Observations (not filed as defects)

- **`children_delimiter`/txt delimiter `unicode_escape→latin1` round-trip** (naive.py:853, txt_parser.py:34): crashes with UnicodeEncodeError if a user writes delimiters as literal `\uXXXX` escapes decoding to non-latin1 chars (e.g. `。`). Vendor-inherited idiom; low likelihood (users typically paste actual CJK chars, which round-trip fine).
- **naive PDF early-return drops embedded-file/URL chunks** (naive.py:961-962): when a PDF yields no sections/tables, already-parsed email-attachment chunks (`embed_res`) are discarded by `return []`.
- **presentation.py:89 uses `print()` for position-parse errors** instead of logging (minor).
- **qa.py txt/csv `%999` guard also mis-fires between pairs** — covered in the qa flood finding.
- **`__images__` zoomin×3 retry on empty boxes** (pdf_parser.py:1670-1671): up to 648-DPI re-render of the window (~GB-scale RAM for 12 blank scanned pages) — vendor-intentional retry, flagging for awareness only.
- **table.py `column_data_type`** (line 330-334): bare `int(str(a))` after regex on `%%`-stripped copy can raise on values like `"123%%"`; extremely narrow.
- **find_codec discards chardet result for non-ascii detections** (rag/nlp/__init__.py:54-72) — vendor-inherited; decode uses errors="ignore" everywhere on this path so worst case is mojibake, not crashes.

# Clean areas (explicitly reviewed, no defects filed)

- **deepdoc/parser/docx_parser.py** — corrupt docx fails cleanly via python-docx exceptions; table composer handles ragged/empty tables; image extraction has layered fallbacks.
- **deepdoc/server/adapters/dla_adapter.py, tsr_adapter.py** — coordinate clamping, unknown-label skips, explicit not-loaded errors; wire format consistent between the two.
- **rag/nlp naive_merge / naive_merge_with_images** — overlap math is guarded (recount after prefix), empty-section and custom-delimiter paths safe; leading "" chunk filtered by tokenize_chunks.
- **Excel row-split contract (xlsx path)** — chunker's cross-sheet data-row counter is consistent between tasks; queue-side header over-count only yields benign empty trailing tasks (`if len(data)==0: continue`); no row loss or duplication. CSV split likewise safe (`pd.DataFrame([], columns=…)`).
- **queue_tasks PDF page-range arithmetic for the default range** — `[(1, MAXIMUM_PAGE_NUMBER)]` covers [0, pages) exactly with no gaps/overlaps between tasks (the `e-1` off-by-one only affects the not-product-exposed explicit "pages" config).
- **markdown_parser.py element extractor** — no infinite loops (all branches advance), unclosed fences handled, line-number coordinates consistent with image-ref extraction.
- **txt_parser / get_text / html_parser encoding** — find_codec + errors="ignore" throughout; no undecodable-input crash path found.
- **qa.py PDF path** — unrecognizable Q&A structure raises a clear ValueError (clean task failure).
- **handle_task exception propagation** (task_executor.py:1541-1553) — chunker exceptions are caught and fail the task with `[Exception]: <msg>`; crash-type findings above were calibrated against this.

---
STATUS: COMPLETE — 2026-07-03. Totals: 1 P0, 5 P1, 14 P2.

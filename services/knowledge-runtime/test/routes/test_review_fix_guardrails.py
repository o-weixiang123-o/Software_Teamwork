from pathlib import Path


def _runtime_root() -> Path:
    return Path(__file__).parents[2]


def _repo_root() -> Path:
    return _runtime_root().parents[1]


def test_task_service_uses_real_source_doc_join_for_dataset_scope_tasks():
    source = _runtime_root() / "api" / "db" / "services" / "task_service.py"
    content = source.read_text(encoding="utf-8")

    assert "doc_join = cls.model.doc_id == Document.id" in content
    assert "if doc_ids:" in content
    assert "doc_join = Document.id == doc_ids[0]" in content
    assert ".join(Document, on=doc_join)" in content


def test_task_service_marks_zero_parse_tasks_failed():
    source = _runtime_root() / "api" / "db" / "services" / "task_service.py"
    content = source.read_text(encoding="utf-8")

    assert "def _mark_parse_scheduling_failed" in content
    assert '"run": TaskStatus.FAIL.value' in content
    assert "No parse tasks were created" in content
    assert "if not parse_task_array:" in content


def test_task_handler_preserves_parser_failure_when_no_chunks_are_built():
    source = _runtime_root() / "rag" / "svr" / "task_executor_refactor" / "task_handler.py"
    content = source.read_text(encoding="utf-8")

    assert "TaskService.get_or_none(id=ctx.id)" in content
    assert "task.progress == -1" in content
    assert "preserving failure state" in content
    assert "ctx.progress_cb(1., msg=f\"No chunk built from {ctx.name}\")" in content


def test_paddleocr_worker_failures_report_negative_progress():
    source = _runtime_root() / "rag" / "app" / "naive.py"
    content = source.read_text(encoding="utf-8")

    assert "callback(-1, f\"PaddleOCR parse failed: {e}\")" in content
    assert "callback(-1, f\"PaddleOCR parse failed: {e}\")\n                return None, None, None" in content
    assert "callback(-1, \"PaddleOCR not found.\")" in content


def test_dataset_search_returns_empty_payload_for_missing_index():
    source = _runtime_root() / "api" / "apps" / "services" / "dataset_api_service.py"
    content = source.read_text(encoding="utf-8")

    assert "def _is_missing_search_index_error" in content
    assert "index_not_found_exception" in content
    assert "return True, _empty_search_result(labels)" in content


def test_dataset_search_business_errors_have_stable_status_codes():
    service_source = _runtime_root() / "api" / "apps" / "services" / "dataset_api_service.py"
    service_content = service_source.read_text(encoding="utf-8")

    assert "class SearchBusinessError" in service_content
    assert "RetCode.ARGUMENT_ERROR, 400" in service_content
    assert "RetCode.NOT_FOUND, 404" in service_content
    assert 'return False, _search_validation_error("Datasets use different embedding models.")' in service_content
    assert 'return False, _search_validation_error("`doc_ids` should be a list")' in service_content
    assert "return False, _search_not_found_error()" in service_content

    route_source = _runtime_root() / "api" / "apps" / "restful_apis" / "dataset_api.py"
    route_content = route_source.read_text(encoding="utf-8")

    assert "def _get_search_business_error_result" in route_content
    assert "return _with_status(get_result(code=result.code, message=result.message), result.http_status)" in route_content
    assert "return _with_status(get_error_argument_result(message), 400)" in route_content
    assert "return _with_status(get_error_data_result(message=message, code=RetCode.EXCEPTION_ERROR), 502)" in route_content
    assert "return _get_search_business_error_result(result)" in route_content


def test_parser_data_integrity_fixes_remain_in_place():
    runtime_root = _runtime_root()

    book = (runtime_root / "rag" / "app" / "book.py").read_text(encoding="utf-8")
    assert "tbls = tables" in book

    figure_parser = (runtime_root / "deepdoc" / "parser" / "figure_parser.py").read_text(encoding="utf-8")
    assert "self.descriptions[figure_num] = [txt] + list(desc)" in figure_parser

    naive = (runtime_root / "rag" / "app" / "naive.py").read_text(encoding="utf-8")
    assert "Markdown vision enhancement failed" in naive
    assert "figure_descriptions.append(desc)" in naive


def test_text_parser_debug_prints_are_removed():
    source = _runtime_root() / "rag" / "app" / "naive.py"
    content = source.read_text(encoding="utf-8")

    assert "print(sections)" not in content
    assert 'print("\\n", "-"*150, "\\n")' not in content


def test_runtime_worker_timeouts_are_enabled_in_local_defaults():
    repo_root = _repo_root()

    env_example = (repo_root / "deploy" / ".env.example").read_text(encoding="utf-8")
    assert "ENABLE_TIMEOUT_ASSERTION=1" in env_example

    runbook = (repo_root / "docs" / "runbooks" / "local-integration.md").read_text(encoding="utf-8")
    assert "ENABLE_TIMEOUT_ASSERTION=1" in runbook

from pathlib import Path

from api.route_registry import (
    RUNTIME_RESTFUL_API_ALLOWLIST,
    collect_runtime_page_paths,
    filter_runtime_restful_api_paths,
    is_runtime_restful_api_allowed,
)


def test_runtime_route_allowlist_keeps_knowledge_core_routes():
    assert RUNTIME_RESTFUL_API_ALLOWLIST == {
        "chunk_api.py",
        "dataset_api.py",
        "document_api.py",
        "system_api.py",
    }


def test_runtime_route_allowlist_excludes_unrelated_ragflow_routes():
    excluded = {
        "chunk_feedback_api.py",
        "file_api.py",
        "file2document_api.py",
        "mcp_api.py",
    }

    for filename in excluded:
        assert not is_runtime_restful_api_allowed(Path(filename))


def test_filter_runtime_restful_api_paths_preserves_input_order():
    paths = [
        Path("file_api.py"),
        Path("dataset_api.py"),
        Path("mcp_api.py"),
        Path("document_api.py"),
        Path("chunk_feedback_api.py"),
        Path("system_api.py"),
    ]

    assert filter_runtime_restful_api_paths(paths) == [
        Path("dataset_api.py"),
        Path("document_api.py"),
        Path("system_api.py"),
    ]


def test_collect_runtime_page_paths_filters_deleted_restful_routes(tmp_path):
    app_route = tmp_path / "api" / "apps" / "knowledge_app.py"
    allowed_route = tmp_path / "api" / "apps" / "restful_apis" / "chunk_api.py"
    deleted_route = tmp_path / "api" / "apps" / "restful_apis" / "mcp_api.py"
    hidden_route = tmp_path / "api" / "apps" / "restful_apis" / ".hidden_api.py"

    for path in [app_route, allowed_route, deleted_route, hidden_route]:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("# route fixture\n")

    result = collect_runtime_page_paths(tmp_path / "api" / "apps")

    assert result == [
        app_route,
        allowed_route,
    ]

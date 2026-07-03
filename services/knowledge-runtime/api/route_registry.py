from pathlib import Path


RUNTIME_RESTFUL_API_ALLOWLIST = frozenset(
    {
        "chunk_api.py",
        "dataset_api.py",
        "document_api.py",
        "system_api.py",
    }
)


def is_runtime_restful_api_allowed(path: Path) -> bool:
    return path.name in RUNTIME_RESTFUL_API_ALLOWLIST


def filter_runtime_restful_api_paths(paths):
    return [path for path in paths if is_runtime_restful_api_allowed(path)]


def collect_runtime_page_paths(page_path: Path):
    app_paths = [path for path in page_path.glob("*_app.py") if not path.name.startswith(".")]
    restful_paths = [path for path in page_path.glob("*restful_apis/*.py") if not path.name.startswith(".")]
    app_paths.extend(filter_runtime_restful_api_paths(restful_paths))
    return app_paths

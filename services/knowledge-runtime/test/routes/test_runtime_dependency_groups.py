import tomllib
from pathlib import Path


HEAVY_WORKER_DEPENDENCIES = {
    "crawl4ai",
    "en-core-web-sm",
    "graspologic",
    "onnxruntime",
    "onnxruntime-gpu",
    "opencv-python",
    "opencv-python-headless",
    "openpyxl",
    "pyclipper",
    "selenium-wire",
    "spacy",
    "webdriver-manager",
    "xgboost",
}

API_STARTUP_DEPENDENCIES = {
    "networkx",
    "xxhash",
}


def _dependency_name(requirement: str) -> str:
    name = requirement.split("@", 1)[0].split(";", 1)[0].split("[", 1)[0]
    for operator in ("==", ">=", "<=", "~=", "!=", ">", "<"):
        name = name.split(operator, 1)[0]
    return name.strip().lower().replace("_", "-")


def test_api_base_dependency_profile_excludes_worker_heavy_packages():
    pyproject = Path(__file__).resolve().parents[2] / "pyproject.toml"
    data = tomllib.loads(pyproject.read_text(encoding="utf-8"))

    base_dependencies = {
        _dependency_name(requirement)
        for requirement in data["project"]["dependencies"]
    }
    worker_dependencies = {
        _dependency_name(requirement)
        for requirement in data["dependency-groups"]["worker"]
    }

    assert HEAVY_WORKER_DEPENDENCIES.isdisjoint(base_dependencies)
    assert HEAVY_WORKER_DEPENDENCIES.issubset(worker_dependencies)
    assert API_STARTUP_DEPENDENCIES.issubset(base_dependencies)


def test_api_import_boundaries_do_not_top_level_import_worker_modules():
    runtime_root = Path(__file__).resolve().parents[2]
    guarded_imports = {
        "api/utils/web_utils.py": [
            "from selenium ",
            "from webdriver_manager",
        ],
        "api/db/services/task_service.py": [
            "from deepdoc.parser ",
            "from deepdoc.parser.excel_parser ",
        ],
        "rag/llm/ocr_model.py": [
            "from deepdoc.parser.mineru_parser ",
            "from deepdoc.parser.opendataloader_parser ",
            "from deepdoc.parser.paddleocr_parser ",
        ],
    }

    for relative, forbidden_top_level_imports in guarded_imports.items():
        source = (runtime_root / relative).read_text(encoding="utf-8")
        top_level_imports = [
            line
            for line in source.splitlines()
            if line.startswith("from ") or line.startswith("import ")
        ]
        for forbidden in forbidden_top_level_imports:
            assert not any(line.startswith(forbidden) for line in top_level_imports)

#!/usr/bin/env python3

# PEP 723 metadata
# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "nltk",
#   "huggingface-hub"
# ]
# ///

# This script downloads runtime artifacts into `ragflow_deps/` for host-run
# Knowledge runtime development. Run it from anywhere: the `__main__` block
# chdir's into this file's own directory, so all outputs land under
# `ragflow_deps/` regardless of the caller's CWD.
#
# Typical workflow:
#
#   uv run ragflow_deps/download_deps.py
#   uv run --no-project ragflow_deps/download_deps.py --china

import argparse
import os
import shutil
import subprocess
import tempfile
import urllib.request
from urllib.parse import quote
from pathlib import Path
from typing import Union

from nltk.downloader import Downloader
from huggingface_hub import HfApi, snapshot_download

GITHUB_PROXY_PREFIX = "https://gh-proxy.com/"
HF_MIRROR_ENDPOINT = "https://hf-mirror.com"
PYPI_MIRROR_INDEX = "https://pypi.tuna.tsinghua.edu.cn/simple"
PYPI_INDEX = "https://pypi.org/simple"
PYPI_FILE_BASE_URL = "https://files.pythonhosted.org/packages/"
PYPI_MIRROR_FILE_BASE_URL = "https://pypi.tuna.tsinghua.edu.cn/packages/"
NLTK_DATA_INDEX_URL = "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/index.xml"
NLTK_DATA_MIRROR_INDEX_URL = f"{GITHUB_PROXY_PREFIX}{NLTK_DATA_INDEX_URL}"
NLTK_DATA_PACKAGE_PREFIX = "https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/"
NLTK_DATA_MIRROR_PACKAGE_PREFIX = f"{GITHUB_PROXY_PREFIX}{NLTK_DATA_PACKAGE_PREFIX}"
EN_CORE_WEB_SM_URL = "https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl"
EN_CORE_WEB_SM_MIRROR_URL = f"{GITHUB_PROXY_PREFIX}{EN_CORE_WEB_SM_URL}"


def get_urls(use_china_mirrors=False) -> list[Union[str, list[str]]]:
    if use_china_mirrors:
        return [
            "http://mirrors.tuna.tsinghua.edu.cn/ubuntu/pool/main/o/openssl/libssl1.1_1.1.1f-1ubuntu2_amd64.deb",
            "http://mirrors.tuna.tsinghua.edu.cn/ubuntu-ports/pool/main/o/openssl/libssl1.1_1.1.1f-1ubuntu2_arm64.deb",
            "https://repo.huaweicloud.com/repository/maven/org/apache/tika/tika-server-standard/3.3.0/tika-server-standard-3.3.0.jar",
            "https://repo.huaweicloud.com/repository/maven/org/apache/tika/tika-server-standard/3.3.0/tika-server-standard-3.3.0.jar.md5",
            "https://openaipublic.blob.core.windows.net/encodings/cl100k_base.tiktoken",
            ["https://registry.npmmirror.com/-/binary/chrome-for-testing/121.0.6167.85/linux64/chrome-linux64.zip", "chrome-linux64-121-0-6167-85"],
            ["https://registry.npmmirror.com/-/binary/chrome-for-testing/121.0.6167.85/linux64/chromedriver-linux64.zip", "chromedriver-linux64-121-0-6167-85"],
            f"{GITHUB_PROXY_PREFIX}https://github.com/astral-sh/uv/releases/download/0.9.16/uv-x86_64-unknown-linux-gnu.tar.gz",
            f"{GITHUB_PROXY_PREFIX}https://github.com/astral-sh/uv/releases/download/0.9.16/uv-aarch64-unknown-linux-gnu.tar.gz",
        ]
    else:
        return [
            "http://archive.ubuntu.com/ubuntu/pool/main/o/openssl/libssl1.1_1.1.1f-1ubuntu2_amd64.deb",
            "http://ports.ubuntu.com/pool/main/o/openssl/libssl1.1_1.1.1f-1ubuntu2_arm64.deb",
            "https://repo1.maven.org/maven2/org/apache/tika/tika-server-standard/3.3.0/tika-server-standard-3.3.0.jar",
            "https://repo1.maven.org/maven2/org/apache/tika/tika-server-standard/3.3.0/tika-server-standard-3.3.0.jar.md5",
            "https://openaipublic.blob.core.windows.net/encodings/cl100k_base.tiktoken",
            ["https://storage.googleapis.com/chrome-for-testing-public/121.0.6167.85/linux64/chrome-linux64.zip", "chrome-linux64-121-0-6167-85"],
            ["https://storage.googleapis.com/chrome-for-testing-public/121.0.6167.85/linux64/chromedriver-linux64.zip", "chromedriver-linux64-121-0-6167-85"],
            "https://github.com/astral-sh/uv/releases/download/0.9.16/uv-x86_64-unknown-linux-gnu.tar.gz",
            "https://github.com/astral-sh/uv/releases/download/0.9.16/uv-aarch64-unknown-linux-gnu.tar.gz",
        ]


repos = [
    "InfiniFlow/text_concat_xgb_v1.0",
    "InfiniFlow/deepdoc",
]


def knowledge_runtime_root() -> Path:
    return Path(__file__).resolve().parents[1]


def model_local_directory(repository_id: str) -> Path:
    root = knowledge_runtime_root()
    if repository_id in {"InfiniFlow/deepdoc", "InfiniFlow/text_concat_xgb_v1.0"}:
        return root / "rag" / "res" / "deepdoc"
    return root / "ragflow_deps" / "huggingface.co" / repository_id


def download_model(repository_id):
    local_directory = model_local_directory(repository_id)
    os.makedirs(local_directory, exist_ok=True)
    endpoint = os.environ.get("HF_ENDPOINT")
    if not endpoint and os.environ.get("RAGFLOW_USE_CHINA_MIRRORS") == "1":
        endpoint = HF_MIRROR_ENDPOINT
    try:
        snapshot_download(repo_id=repository_id, local_dir=str(local_directory), endpoint=endpoint)
    except Exception as exc:
        print(f"snapshot_download failed for {repository_id}: {exc}")
        print("Falling back to direct file downloads...")
        download_model_files(repository_id, local_directory, endpoint)


def download_model_files(repository_id: str, local_directory: Path, endpoint: str | None):
    endpoint = (endpoint or "https://huggingface.co").rstrip("/")
    api = HfApi(endpoint=endpoint)
    info = api.model_info(repository_id, files_metadata=True)
    for sibling in info.siblings:
        filename = sibling.rfilename
        if not filename or filename.endswith("/"):
            continue
        target = local_directory / filename
        expected_size = getattr(sibling, "size", None)
        if target.exists() and (not expected_size or target.stat().st_size == expected_size):
            print(f"Using cached {target}")
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        encoded = quote(filename, safe="/")
        url = f"{endpoint}/{repository_id}/resolve/{info.sha}/{encoded}"
        print(f"Downloading {filename} from {url}...")
        temp_target = target.with_name(target.name + ".tmp")
        try:
            urllib.request.urlretrieve(url, temp_target)
            os.replace(temp_target, target)
        finally:
            if temp_target.exists():
                temp_target.unlink()


def rewrite_text_for_china_mirrors(text: str) -> str:
    return (
        text.replace(EN_CORE_WEB_SM_URL, EN_CORE_WEB_SM_MIRROR_URL)
        .replace(PYPI_FILE_BASE_URL, PYPI_MIRROR_FILE_BASE_URL)
        .replace(PYPI_INDEX, PYPI_MIRROR_INDEX)
    )


def sync_runtime_dependencies_with_china_mirrors():
    uv = shutil.which("uv")
    if not uv:
        raise RuntimeError("uv is required to sync knowledge-runtime dependencies")

    root = knowledge_runtime_root()
    with tempfile.TemporaryDirectory(prefix="knowledge-runtime-china-") as temp_dir:
        temp_root = Path(temp_dir)
        for filename in ("pyproject.toml", "uv.lock"):
            source = root / filename
            target = temp_root / filename
            target.write_text(rewrite_text_for_china_mirrors(source.read_text(encoding="utf-8")), encoding="utf-8")

        env = os.environ.copy()
        env.setdefault("UV_DEFAULT_INDEX", PYPI_MIRROR_INDEX)
        env["UV_PROJECT_ENVIRONMENT"] = str(root / ".venv")

        print("Syncing knowledge-runtime Python dependencies with mainland China mirrors...")
        subprocess.run(
            [
                uv,
                "sync",
                "--project",
                str(temp_root),
                "--python",
                "3.13",
                "--frozen",
                "--no-install-project",
            ],
            env=env,
            check=True,
        )


def build_nltk_downloader(use_china_mirrors=False):
    index_url = os.environ.get("NLTK_DOWNLOAD_INDEX_URL")
    if not index_url:
        index_url = NLTK_DATA_MIRROR_INDEX_URL if use_china_mirrors else NLTK_DATA_INDEX_URL

    downloader = Downloader(server_index_url=index_url)
    if use_china_mirrors or index_url.startswith(GITHUB_PROXY_PREFIX):
        package_prefix = os.environ.get("NLTK_DOWNLOAD_PACKAGE_PREFIX", NLTK_DATA_MIRROR_PACKAGE_PREFIX)
        # NLTK stores package URLs inside index.xml. Rewrite those URLs after
        # loading the index so the actual zip downloads also use the mirror.
        downloader._update_index()
        for package in downloader._packages.values():
            if package.url.startswith(NLTK_DATA_PACKAGE_PREFIX):
                package.url = package_prefix + package.url[len(NLTK_DATA_PACKAGE_PREFIX):]
    return downloader


if __name__ == "__main__":
    # Anchor CWD to this file's directory so all relative outputs
    # (huggingface.co/, nltk_data/, *.deb, *.jar, *.tar.gz, etc.) land
    # at the top of ragflow_deps/ regardless of where the user invokes
    # the script from.
    os.chdir(os.path.dirname(os.path.abspath(__file__)))

    parser = argparse.ArgumentParser(description="Download dependencies with optional China mirror support")
    parser.add_argument(
        "--china",
        "--china-mirrors",
        dest="china_mirrors",
        action="store_true",
        help="Use mainland China mirrors for PyPI, NLTK, HuggingFace, GitHub release/raw, Tika, Chrome, and uv release downloads",
    )
    parser.add_argument(
        "--skip-uv-sync",
        action="store_true",
        help="With --china, skip the temporary mirrored uv sync step and only download runtime artifacts",
    )
    args = parser.parse_args()

    urls = get_urls(args.china_mirrors)
    if args.china_mirrors:
        os.environ.setdefault("RAGFLOW_USE_CHINA_MIRRORS", "1")
        os.environ.setdefault("UV_DEFAULT_INDEX", PYPI_MIRROR_INDEX)
        os.environ.setdefault("HF_ENDPOINT", HF_MIRROR_ENDPOINT)
        os.environ.setdefault("NLTK_DOWNLOAD_INDEX_URL", NLTK_DATA_MIRROR_INDEX_URL)
        os.environ.setdefault("NLTK_DOWNLOAD_PACKAGE_PREFIX", NLTK_DATA_MIRROR_PACKAGE_PREFIX)
        if not args.skip_uv_sync:
            sync_runtime_dependencies_with_china_mirrors()

    # Some mirrors (e.g. archive.ubuntu.com) reject the default urllib
    # User-Agent with HTTP 403, so install an opener with a browser-like UA.
    opener = urllib.request.build_opener()
    opener.addheaders = [("User-Agent", "Mozilla/5.0")]
    urllib.request.install_opener(opener)

    for url in urls:
        download_url = url[0] if isinstance(url, list) else url
        filename = url[1] if isinstance(url, list) else url.split("/")[-1]
        print(f"Downloading {filename} from {download_url}...")
        if not os.path.exists(filename):
            urllib.request.urlretrieve(download_url, filename)

    local_dir = os.path.abspath("nltk_data")
    nltk_downloader = build_nltk_downloader(args.china_mirrors)
    for data in ["wordnet", "punkt", "punkt_tab"]:
        print(f"Downloading nltk {data}...")
        nltk_downloader.download(data, download_dir=local_dir)

    for repo_id in repos:
        print(f"Downloading huggingface repo {repo_id}...")
        download_model(repo_id)

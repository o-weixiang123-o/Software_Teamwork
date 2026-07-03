#!/usr/bin/env python3
"""Fail fast when host-run runtime dependencies are not configured."""

from __future__ import annotations

import os
import socket
import sys
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping
from pathlib import Path


LOCAL_KEYLESS_FACTORIES = {"Builtin", "LocalAI", "Ollama", "VLLM"}


def read_simple_yaml(path: Path) -> dict:
    root: dict = {}
    stack: list[tuple[int, dict]] = [(-1, root)]
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.split("#", 1)[0].rstrip()
        if not line.strip() or ":" not in line:
            continue
        indent = len(line) - len(line.lstrip(" "))
        key, raw_value = line.strip().split(":", 1)
        key = key.strip()
        value = raw_value.strip().strip("'\"")

        while stack and indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if value == "":
            child: dict = {}
            parent[key] = child
            stack.append((indent, child))
        else:
            parent[key] = value
    return root


def nested(config: Mapping, *keys: str) -> str:
    value = config
    for key in keys:
        if not isinstance(value, Mapping):
            return ""
        value = value.get(key, "")
    return str(value or "").strip()


def check_http_url(url: str, timeout: float = 3.0) -> None:
    req = urllib.request.Request(url, method="GET")
    with urllib.request.urlopen(req, timeout=timeout) as response:
        response.read(64)


def check_tcp_url(url: str, timeout: float = 3.0) -> None:
    parsed = urllib.parse.urlparse(url)
    host = parsed.hostname
    port = parsed.port
    if not host or not port:
        raise OSError(f"missing host or port in {url!r}")
    with socket.create_connection((host, port), timeout=timeout):
        return


def check_nltk_data(env: Mapping[str, str]) -> list[str]:
    if env.get("KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA") != "1":
        return []
    roots = [Path(item) for item in env.get("NLTK_DATA", "").split(os.pathsep) if item.strip()]
    if not roots:
        return ["Worker startup requires NLTK_DATA to point at provisioned nltk_data."]

    missing: list[str] = []
    for name, relative in {
        "punkt_tab": Path("tokenizers") / "punkt_tab",
        "wordnet": Path("corpora") / "wordnet",
    }.items():
        if not any((root / relative).exists() or (root / relative.parent / f"{relative.name}.zip").exists() for root in roots):
            missing.append(name)
    if missing:
        joined_roots = ", ".join(str(root) for root in roots)
        return [
            "Worker startup requires NLTK data "
            f"{', '.join(missing)} under NLTK_DATA ({joined_roots}). Run "
            "services/knowledge-runtime/ragflow_deps/download_deps.py --china or set NLTK_DATA to a provisioned directory."
        ]
    return []


def validate(
    config_path: Path,
    environ: Mapping[str, str] | None = None,
    http_checker: Callable[[str], None] | None = None,
    tcp_checker: Callable[[str], None] | None = None,
) -> list[str]:
    env = environ or os.environ
    http_checker = http_checker or check_http_url
    tcp_checker = tcp_checker or check_tcp_url
    config = read_simple_yaml(config_path)
    issues: list[str] = []
    issues.extend(check_nltk_data(env))

    doc_engine = env.get("DOC_ENGINE", "elasticsearch").strip().lower()
    if doc_engine == "elasticsearch":
        raw_hosts = env.get("KNOWLEDGE_RUNTIME_ES_URL") or nested(config, "es", "hosts")
        es_url = raw_hosts.split(",", 1)[0].strip().rstrip("/")
        if not es_url:
            issues.append("DOC_ENGINE=elasticsearch requires es.hosts in RAGFLOW_CONF or KNOWLEDGE_RUNTIME_ES_URL.")
        else:
            try:
                http_checker(f"{es_url}/_cluster/health")
            except Exception as exc:
                issues.append(
                    "DOC_ENGINE=elasticsearch requires a reachable Elasticsearch endpoint before starting "
                    f"knowledge-runtime; checked {es_url} ({exc}). Root deploy/docker-compose.yml is infra-only "
                    "and does not start Elasticsearch, so start a host/external ES instance or set DOC_ENGINE "
                    "to another supported reachable engine."
                )
    elif doc_engine not in {"opensearch", "infinity", "oceanbase", "seekdb"}:
        issues.append(f"DOC_ENGINE={doc_engine!r} is not supported by the vendored RAGFlow runtime.")

    env_embedding_model = env.get("KNOWLEDGE_RUNTIME_EMBEDDING_MODEL", "").strip()
    env_embedding_factory = env.get("KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY", "").strip()
    if env_embedding_model and env_embedding_factory:
        if (
            env_embedding_factory not in LOCAL_KEYLESS_FACTORIES
            and not env.get("KNOWLEDGE_RUNTIME_MODEL_API_KEY", "").strip()
            and env.get("KNOWLEDGE_RUNTIME_ALLOW_EMPTY_MODEL_API_KEY") != "1"
        ):
            issues.append(
                "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY/MODEL are set, but KNOWLEDGE_RUNTIME_MODEL_API_KEY is empty. "
                "Set a real provider key or KNOWLEDGE_RUNTIME_ALLOW_EMPTY_MODEL_API_KEY=1 for a trusted local provider."
            )
        return issues

    embedding = config.get("user_default_llm", {}).get("default_models", {}).get("embedding_model", {})
    cfg_model = str(embedding.get("name", "")).strip()
    cfg_factory = str(embedding.get("factory", "")).strip()
    cfg_base_url = str(embedding.get("base_url", "")).strip()
    if cfg_factory == "Builtin":
        compose_profiles = env.get("COMPOSE_PROFILES", "")
        if "tei-" in compose_profiles and cfg_model == env.get("TEI_MODEL", "") and cfg_base_url:
            try:
                tcp_checker(cfg_base_url)
            except Exception as exc:
                issues.append(f"Builtin embedding endpoint is not reachable at {cfg_base_url}: {exc}")
        else:
            issues.append(
                "Default Builtin embedding is not usable in the current host-run setup. "
                "Root Compose no longer starts a TEI/Builtin embedding service; set "
                "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY, KNOWLEDGE_RUNTIME_EMBEDDING_MODEL, "
                "KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL, and KNOWLEDGE_RUNTIME_MODEL_API_KEY before enabling ingestion."
            )
    elif not cfg_model or not cfg_factory:
        issues.append(
            "No default embedding model is configured. Set KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY and "
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL before enabling ingestion."
        )

    return issues


def main() -> int:
    root = Path(__file__).resolve().parents[1]
    config_path = Path(os.environ.get("RAGFLOW_CONF", root / "conf" / "service_conf.yaml"))
    if not config_path.is_file():
        print(f"Missing RAGFLOW_CONF: {config_path}", file=sys.stderr)
        return 2
    issues = validate(config_path)
    if issues:
        print("Knowledge runtime dependency check failed:", file=sys.stderr)
        for issue in issues:
            print(f"- {issue}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

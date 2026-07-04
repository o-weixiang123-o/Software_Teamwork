#!/usr/bin/env python3
"""Run a real Knowledge PDF upload -> parse -> chunks -> retrieval smoke."""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import pathlib
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from dataclasses import dataclass
from typing import Any


ROOT_DIR = pathlib.Path(__file__).resolve().parents[2]
DEFAULT_ENV_FILE = ROOT_DIR / "deploy" / ".env"
DEFAULT_PDF = ROOT_DIR / "DL_T_673-1999.pdf"
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


@dataclass
class Config:
    base_url: str
    service_token: str
    user_id: str
    permissions: str
    timeout_seconds: int
    poll_interval_seconds: int
    query: str
    keep_resources: bool


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Upload a PDF through the Knowledge adapter and verify runtime parsing, chunks, and retrieval."
    )
    parser.add_argument(
        "pdf",
        nargs="?",
        default=str(DEFAULT_PDF),
        help="PDF fixture path. Defaults to ./DL_T_673-1999.pdf.",
    )
    parser.add_argument(
        "--env-file",
        default=str(DEFAULT_ENV_FILE),
        help="Local env file to read when process env is missing values.",
    )
    return parser.parse_args()


def load_env_file(path: pathlib.Path) -> None:
    if not path.is_file():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in os.environ:
            os.environ[key] = value


def first_env(*keys: str) -> str:
    for key in keys:
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return ""


def load_config() -> Config:
    base_url = first_env("KNOWLEDGE_SERVICE_BASE_URL", "KNOWLEDGE_ADAPTER_BASE_URL")
    if not base_url:
        base_url = url_from_addr(
            first_env("KNOWLEDGE_PARSE_ADAPTER_ADDR", "KNOWLEDGE_HTTP_ADDR"),
            os.environ.get("KNOWLEDGE_HTTP_PORT", "8083").strip() or "8083",
        )
    service_token = first_env("KNOWLEDGE_SERVICE_TOKEN", "INTERNAL_SERVICE_TOKEN")
    if not service_token:
        raise SystemExit("KNOWLEDGE_SERVICE_TOKEN or INTERNAL_SERVICE_TOKEN is required")

    timeout_seconds = positive_int_env("KNOWLEDGE_PDF_E2E_TIMEOUT_SECONDS", 600)
    poll_interval_seconds = positive_int_env("KNOWLEDGE_PDF_E2E_POLL_INTERVAL_SECONDS", 3)
    query = os.environ.get(
        "KNOWLEDGE_PDF_E2E_QUERY",
        "DL/T 673-1999 火力发电厂水处理用001×7强酸性阳离子交换树脂报废标准适用对象是什么？",
    ).strip()
    if not query:
        raise SystemExit("KNOWLEDGE_PDF_E2E_QUERY must not be empty")

    return Config(
        base_url=base_url.rstrip("/"),
        service_token=service_token,
        user_id=os.environ.get("KNOWLEDGE_PDF_E2E_USER_ID", "dlt673-e2e-user").strip(),
        permissions=os.environ.get("KNOWLEDGE_PDF_E2E_PERMISSIONS", "knowledge:read,knowledge:write").strip(),
        timeout_seconds=timeout_seconds,
        poll_interval_seconds=poll_interval_seconds,
        query=query,
        keep_resources=os.environ.get("KNOWLEDGE_PDF_E2E_KEEP_RESOURCES", "") == "1",
    )


def url_from_addr(addr: str, fallback_port: str) -> str:
    addr = (addr or "").strip()
    if not addr:
        return f"http://127.0.0.1:{fallback_port}"
    if addr.startswith(("http://", "https://")):
        return addr.rstrip("/")
    if addr.startswith(":"):
        return f"http://127.0.0.1{addr}"
    if addr.startswith("0.0.0.0:"):
        return f"http://127.0.0.1:{addr.rsplit(':', 1)[1]}"
    if addr.startswith("[::]:"):
        return f"http://127.0.0.1:{addr.rsplit(':', 1)[1]}"
    return f"http://{addr}"


def positive_int_env(key: str, fallback: int) -> int:
    raw = os.environ.get(key, "").strip()
    if not raw:
        return fallback
    try:
        value = int(raw)
    except ValueError as exc:
        raise SystemExit(f"{key} must be a positive integer") from exc
    if value <= 0:
        raise SystemExit(f"{key} must be a positive integer")
    return value


def request(
    cfg: Config,
    method: str,
    path: str,
    body: bytes | dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    timeout: int = 120,
) -> tuple[int, Any, dict[str, str]]:
    merged_headers = {
        "X-Service-Token": cfg.service_token,
        "X-User-Id": cfg.user_id,
        "X-User-Permissions": cfg.permissions,
        "X-Caller-Service": "knowledge-pdf-e2e",
    }
    if headers:
        merged_headers.update(headers)

    data: bytes | None
    if isinstance(body, dict):
        data = json.dumps(body).encode("utf-8")
        merged_headers["Content-Type"] = "application/json"
    else:
        data = body

    req = urllib.request.Request(cfg.base_url + path, data=data, headers=merged_headers, method=method)
    try:
        with OPENER.open(req, timeout=timeout) as response:
            raw = response.read()
            decoded = decode_response_body(raw, response.headers.get("Content-Type", ""))
            return response.status, decoded, dict(response.headers.items())
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        decoded = decode_response_body(raw, exc.headers.get("Content-Type", ""))
        raise RuntimeError(f"{method} {path} -> HTTP {exc.code}: {decoded}") from None
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {path} failed: {exc}") from None


def decode_response_body(raw: bytes, content_type: str) -> Any:
    if not raw:
        return None
    if "application/json" in content_type.lower():
        return json.loads(raw.decode("utf-8"))
    return raw.decode("utf-8", "replace")


def multipart_file(field: str, path: pathlib.Path, content_type: str) -> tuple[str, bytes]:
    boundary = "----codex-dlt673-" + uuid.uuid4().hex
    body = b"".join(
        [
            f"--{boundary}\r\n".encode(),
            f'Content-Disposition: form-data; name="{field}"; filename="{path.name}"\r\n'.encode(),
            f"Content-Type: {content_type}\r\n\r\n".encode(),
            path.read_bytes(),
            b"\r\n",
            f"--{boundary}--\r\n".encode(),
        ]
    )
    return boundary, body


def wait_for_adapter(cfg: Config) -> dict[str, Any]:
    deadline = time.monotonic() + min(cfg.timeout_seconds, 120)
    last: Any = None
    while time.monotonic() < deadline:
        try:
            status, payload, _ = request(
                cfg,
                "GET",
                "/readyz",
                headers={"X-Request-Id": "req_dlt673_ready"},
                timeout=10,
            )
            if status == 200:
                return payload
            last = payload
        except Exception as exc:  # noqa: BLE001 - surface the last readiness problem.
            last = str(exc)
        time.sleep(2)
    raise RuntimeError(f"Knowledge adapter did not become ready: {last}")


def compact_preview(value: Any, limit: int = 220) -> str:
    text = " ".join(str(value or "").split())
    return text[:limit]


def create_knowledge_base(cfg: Config, run_id: str) -> str:
    description = "Codex real PDF parsing smoke for DL_T_673-1999.pdf"
    status, payload, _ = request(
        cfg,
        "POST",
        "/internal/v1/knowledge-bases",
        {
            "name": f"DLT673 PDF E2E {run_id}",
            "description": description,
            "docType": "naive",
            "chunkStrategy": {
                "layout_recognize": "DeepDOC",
                "chunk_token_num": 512,
                "delimiter": "\n",
                "auto_keywords": 0,
                "auto_questions": 0,
            },
        },
        headers={"X-Request-Id": "req_dlt673_create_kb"},
    )
    if status != 201:
        raise RuntimeError(f"create knowledge base returned HTTP {status}: {payload}")
    kb_id = str(payload.get("data", {}).get("id", "")).strip()
    if not kb_id:
        raise RuntimeError(f"create knowledge base returned no id: {payload}")
    return kb_id


def upload_pdf(cfg: Config, kb_id: str, pdf: pathlib.Path) -> dict[str, Any]:
    content_type = mimetypes.guess_type(pdf.name)[0] or "application/pdf"
    boundary, body = multipart_file("file", pdf, content_type)
    status, payload, _ = request(
        cfg,
        "POST",
        f"/internal/v1/knowledge-bases/{urllib.parse.quote(kb_id)}/documents",
        body,
        headers={
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            "X-Request-Id": "req_dlt673_upload_pdf",
        },
        timeout=180,
    )
    if status != 201:
        raise RuntimeError(f"upload document returned HTTP {status}: {payload}")
    doc = payload.get("data", {})
    if not doc.get("id"):
        raise RuntimeError(f"upload document returned no id: {payload}")
    if doc.get("status") == "uploaded":
        raise RuntimeError(
            "document stayed in uploaded status; start the adapter with KNOWLEDGE_AUTO_START_INGESTION=true"
        )
    return doc


def wait_for_document_ready(cfg: Config, kb_id: str, doc_id: str) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    deadline = time.monotonic() + cfg.timeout_seconds
    observations: list[dict[str, Any]] = []
    last_doc: dict[str, Any] = {}
    path = f"/internal/v1/documents/{urllib.parse.quote(doc_id)}?knowledgeBaseId={urllib.parse.quote(kb_id)}"
    while time.monotonic() < deadline:
        _, payload, _ = request(
            cfg,
            "GET",
            path,
            headers={"X-Request-Id": "req_dlt673_get_doc"},
            timeout=30,
        )
        doc = payload.get("data", {})
        last_doc = doc
        observations.append(
            {
                "status": doc.get("status"),
                "chunkCount": doc.get("chunkCount"),
                "parserBackend": doc.get("parserBackend"),
            }
        )
        if doc.get("status") == "ready":
            if int(doc.get("chunkCount") or 0) <= 0:
                raise RuntimeError(f"ready document has zero chunks: {doc}")
            return doc, observations
        if doc.get("status") == "failed":
            raise RuntimeError(f"document parsing failed: {doc}")
        time.sleep(cfg.poll_interval_seconds)
    raise RuntimeError(f"document did not become ready before timeout; last={last_doc}")


def list_chunks(cfg: Config, kb_id: str, doc_id: str) -> list[dict[str, Any]]:
    path = (
        f"/internal/v1/documents/{urllib.parse.quote(doc_id)}/chunks"
        f"?knowledgeBaseId={urllib.parse.quote(kb_id)}&page=1&pageSize=5"
    )
    status, payload, _ = request(
        cfg,
        "GET",
        path,
        headers={"X-Request-Id": "req_dlt673_chunks"},
        timeout=90,
    )
    if status != 200:
        raise RuntimeError(f"list chunks returned HTTP {status}: {payload}")
    chunks = payload.get("data", [])
    if not chunks:
        raise RuntimeError(f"list chunks returned no chunks: {payload}")
    return chunks


def create_query(cfg: Config, kb_id: str) -> dict[str, Any]:
    status, payload, _ = request(
        cfg,
        "POST",
        "/internal/v1/knowledge-queries",
        {
            "query": cfg.query,
            "knowledgeBaseIds": [kb_id],
            "topK": 5,
            "scoreThreshold": 0,
            "rerank": False,
        },
        headers={"X-Request-Id": "req_dlt673_query"},
        timeout=120,
    )
    if status != 201:
        raise RuntimeError(f"knowledge query returned HTTP {status}: {payload}")
    data = payload.get("data", {})
    results = data.get("results", [])
    hit_count = int(data.get("trace", {}).get("hitCount") or len(results))
    if hit_count <= 0 or not results:
        raise RuntimeError(f"retrieval returned no hits: {data}")
    return data


def cleanup(cfg: Config, kb_id: str | None, doc_id: str | None) -> dict[str, str]:
    result: dict[str, str] = {}
    if cfg.keep_resources:
        return {"mode": "kept"}
    if doc_id and kb_id:
        path = f"/internal/v1/documents/{urllib.parse.quote(doc_id)}?knowledgeBaseId={urllib.parse.quote(kb_id)}"
        try:
            request(
                cfg,
                "DELETE",
                path,
                headers={"X-Request-Id": "req_dlt673_cleanup_doc"},
                timeout=60,
            )
            result["document"] = "deleted"
        except Exception as exc:  # noqa: BLE001 - cleanup best effort is reported.
            result["document"] = f"failed: {exc}"
    if kb_id:
        try:
            request(
                cfg,
                "DELETE",
                f"/internal/v1/knowledge-bases/{urllib.parse.quote(kb_id)}",
                headers={"X-Request-Id": "req_dlt673_cleanup_kb"},
                timeout=60,
            )
            result["knowledgeBase"] = "deleted"
        except Exception as exc:  # noqa: BLE001
            result["knowledgeBase"] = f"failed: {exc}"
    return result


def run() -> int:
    args = parse_args()
    load_env_file(pathlib.Path(args.env_file))
    cfg = load_config()
    pdf = pathlib.Path(args.pdf).resolve()
    if not pdf.is_file():
        raise SystemExit(f"PDF fixture not found: {pdf}")

    run_id = time.strftime("%Y%m%d%H%M%S") + "-" + uuid.uuid4().hex[:8]
    kb_id: str | None = None
    doc_id: str | None = None
    summary: dict[str, Any] = {}
    exit_code = 1
    try:
        ready = wait_for_adapter(cfg)
        kb_id = create_knowledge_base(cfg, run_id)
        uploaded = upload_pdf(cfg, kb_id, pdf)
        doc_id = str(uploaded["id"])
        ready_doc, observations = wait_for_document_ready(cfg, kb_id, doc_id)
        chunks = list_chunks(cfg, kb_id, doc_id)
        query = create_query(cfg, kb_id)
        results = query.get("results", [])

        summary = {
            "ok": True,
            "adapterReady": ready.get("data", {}).get("vendor_runtime_ok"),
            "pdf": str(pdf),
            "knowledgeBaseId": kb_id,
            "documentId": doc_id,
            "documentStatus": ready_doc.get("status"),
            "documentName": ready_doc.get("name"),
            "parserBackend": ready_doc.get("parserBackend"),
            "chunkCount": ready_doc.get("chunkCount"),
            "firstChunksReturned": len(chunks),
            "firstChunkPreview": compact_preview(chunks[0].get("content")),
            "query": cfg.query,
            "queryId": query.get("id"),
            "queryHitCount": query.get("trace", {}).get("hitCount"),
            "queryResultsReturned": len(results),
            "topResultScore": results[0].get("score") if results else None,
            "topResultPreview": compact_preview(results[0].get("contentPreview") if results else ""),
            "statusObservations": observations[-10:],
        }
        exit_code = 0
    except Exception as exc:  # noqa: BLE001 - CLI prints a concise JSON failure summary.
        summary = {
            "ok": False,
            "error": str(exc),
            "pdf": str(pdf),
            "knowledgeBaseId": kb_id,
            "documentId": doc_id,
        }
    finally:
        if kb_id or doc_id:
            summary["cleanup"] = cleanup(cfg, kb_id, doc_id)
        if summary:
            print(json.dumps(summary, ensure_ascii=False, indent=2))
    return exit_code


if __name__ == "__main__":
    try:
        raise SystemExit(run())
    except Exception as exc:  # noqa: BLE001 - top-level CLI should print concise failure.
        print(f"knowledge PDF E2E failed: {exc}", file=sys.stderr)
        raise SystemExit(1)

from __future__ import annotations

import json
import os
import tempfile
import time
from dataclasses import dataclass
from typing import Any, Callable, Literal, Optional

import requests


AuthScheme = Literal["token", "bearer"]


@dataclass(frozen=True)
class PaddleOCRCloudRequestConfig:
    base_url: str
    access_token: str
    algorithm: str
    request_timeout: int = 600
    auth_scheme: AuthScheme = "token"


class PaddleOCRCloudError(RuntimeError):
    pass


class PaddleOCRCloudClient:
    """Thin client for PaddleOCR cloud async job API."""

    def __init__(self, session: Any | None = None):
        self.session = session or requests.Session()

    def parse_pdf(
        self,
        data: bytes,
        config: PaddleOCRCloudRequestConfig,
        optional_payload: dict[str, Any],
        callback: Optional[Callable[[float, str], None]] = None,
    ) -> dict[str, Any]:
        deadline = time.monotonic() + config.request_timeout
        jobs_url = f"{config.base_url.rstrip('/')}/api/v2/ocr/jobs"

        def remaining() -> float:
            value = deadline - time.monotonic()
            if value <= 0:
                raise PaddleOCRCloudError(f"[PaddleOCR] timed out after {config.request_timeout}s")
            return value

        if callback:
            callback(0.1, "[PaddleOCR] submitting request")

        job_id = self._submit_job(
            jobs_url=jobs_url,
            data=data,
            config=config,
            optional_payload=optional_payload,
            timeout=remaining(),
        )
        if callback:
            callback(0.2, f"[PaddleOCR] job submitted: {job_id}")

        poll_data = self._poll_job(
            poll_url=f"{jobs_url}/{job_id}",
            config=config,
            timeout_supplier=remaining,
        )
        if callback:
            callback(0.7, "[PaddleOCR] job done, fetching result")

        result_url = self._result_json_url(poll_data)
        raw_result = self._fetch_result_jsonl(result_url, timeout=remaining())
        if callback:
            callback(0.8, "[PaddleOCR] result received")

        return self._combine_result(raw_result)

    def _headers(self, config: PaddleOCRCloudRequestConfig) -> dict[str, str]:
        headers = {"Client-Platform": "ragflow"}
        token = (config.access_token or "").strip()
        if not token:
            return headers
        if config.auth_scheme == "bearer":
            headers["Authorization"] = f"Bearer {token}"
        else:
            headers["Authorization"] = f"token {token}"
        return headers

    def _submit_job(
        self,
        *,
        jobs_url: str,
        data: bytes,
        config: PaddleOCRCloudRequestConfig,
        optional_payload: dict[str, Any],
        timeout: float,
    ) -> str:
        tmp_file = None
        try:
            tmp_file = tempfile.NamedTemporaryFile(delete=False, suffix=".pdf")
            tmp_file.write(data)
            tmp_file.close()

            form_data = {
                "model": config.algorithm,
                "optionalPayload": json.dumps(optional_payload, ensure_ascii=False),
            }
            with open(tmp_file.name, "rb") as file_obj:
                response = self.session.post(
                    jobs_url,
                    data=form_data,
                    files={"file": ("document.pdf", file_obj, "application/pdf")},
                    headers=self._headers(config),
                    timeout=timeout,
                )
        except Exception as exc:
            raise PaddleOCRCloudError(f"[PaddleOCR] submit failed: {exc}") from exc
        finally:
            if tmp_file and os.path.exists(tmp_file.name):
                os.unlink(tmp_file.name)

        if response.status_code != 200:
            raise PaddleOCRCloudError(f"[PaddleOCR] submit failed: HTTP {response.status_code} {self._safe_response_excerpt(response)}")

        submit_data = self._decode_json(response, "submit")
        job_id = self._lookup(submit_data, "data.jobId") or submit_data.get("jobId")
        if not job_id:
            raise PaddleOCRCloudError(f"[PaddleOCR] job ID not found in submit response")
        return str(job_id)

    def _poll_job(self, *, poll_url: str, config: PaddleOCRCloudRequestConfig, timeout_supplier: Callable[[], float]) -> dict[str, Any]:
        interval = 3.0
        multiplier = 1.5
        max_interval = 15.0

        while True:
            try:
                response = self.session.get(poll_url, headers=self._headers(config), timeout=timeout_supplier())
            except Exception as exc:
                raise PaddleOCRCloudError(f"[PaddleOCR] poll failed: {exc}") from exc

            if response.status_code != 200:
                raise PaddleOCRCloudError(f"[PaddleOCR] poll failed: HTTP {response.status_code} {self._safe_response_excerpt(response)}")

            poll_data = self._decode_json(response, "poll")
            state = self._lookup(poll_data, "data.state") or poll_data.get("state")
            if state == "done":
                return poll_data
            if state == "failed":
                message = self._lookup(poll_data, "data.errorMsg") or self._lookup(poll_data, "data.error") or "Unknown error"
                raise PaddleOCRCloudError(f"[PaddleOCR] job failed: {message}")

            time.sleep(min(interval, timeout_supplier()))
            interval = min(interval * multiplier, max_interval)

    def _fetch_result_jsonl(self, result_json_url: str, timeout: float) -> list[dict[str, Any]]:
        try:
            response = self.session.get(result_json_url, timeout=timeout)
            response.raise_for_status()
        except Exception as exc:
            raise PaddleOCRCloudError(f"[PaddleOCR] failed to fetch result: {exc}") from exc

        rows: list[dict[str, Any]] = []
        for line in response.text.strip().splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except ValueError as exc:
                raise PaddleOCRCloudError(f"[PaddleOCR] result JSONL parse error: {exc}") from exc
        return rows

    def _combine_result(self, rows: list[dict[str, Any]]) -> dict[str, Any]:
        combined_result: dict[str, Any] = {"layoutParsingResults": [], "ocrResults": []}
        for row in rows:
            result = row.get("result", row)
            layout_results = result.get("layoutParsingResults", [])
            if isinstance(layout_results, list):
                combined_result["layoutParsingResults"].extend(layout_results)
            ocr_results = result.get("ocrResults", [])
            if isinstance(ocr_results, list):
                combined_result["ocrResults"].extend(ocr_results)
        return combined_result

    @staticmethod
    def _lookup(data: dict[str, Any], dotted_path: str) -> Any:
        current: Any = data
        for part in dotted_path.split("."):
            if not isinstance(current, dict):
                return None
            current = current.get(part)
        return current

    @staticmethod
    def _result_json_url(poll_data: dict[str, Any]) -> str:
        result_data = poll_data.get("data", {})
        result_url = result_data.get("resultUrl") or {}
        result_json_url = result_data.get("resultJsonUrl") or result_url.get("jsonUrl")
        if not result_json_url:
            raise PaddleOCRCloudError("[PaddleOCR] result URL not found")
        return str(result_json_url)

    @staticmethod
    def _decode_json(response: Any, phase: str) -> dict[str, Any]:
        try:
            decoded = response.json()
        except ValueError as exc:
            raise PaddleOCRCloudError(f"[PaddleOCR] {phase} response is not JSON: {exc}") from exc
        if not isinstance(decoded, dict):
            raise PaddleOCRCloudError(f"[PaddleOCR] {phase} response is not a JSON object")
        return decoded

    @staticmethod
    def _safe_response_excerpt(response: Any, limit: int = 200) -> str:
        text = response.text or ""
        for header_value in response.request.headers.values() if response.request is not None else []:
            if not header_value:
                continue
            parts = str(header_value).split()
            secret = parts[-1] if len(parts) > 1 else str(header_value)
            if secret:
                text = text.replace(secret, "[REDACTED]")
        return text[:limit]

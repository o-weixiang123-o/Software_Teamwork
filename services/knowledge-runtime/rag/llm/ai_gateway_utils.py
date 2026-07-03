#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

import os
import uuid
from urllib.parse import urljoin, urlparse

from common.exceptions import ModelException

DEFAULT_AI_GATEWAY_BASE_URL = "http://127.0.0.1:8086/internal/v1"
DEFAULT_AI_GATEWAY_CALLER_SERVICE = "knowledge"
DEFAULT_AI_GATEWAY_TIMEOUT_SECONDS = 30.0


def normalize_ai_gateway_endpoint(base_url: str | None, endpoint: str) -> str:
    base = (base_url or DEFAULT_AI_GATEWAY_BASE_URL).strip().rstrip("/")
    if not base:
        base = DEFAULT_AI_GATEWAY_BASE_URL
    if "://" not in base:
        base = f"http://{base}"

    suffix = endpoint.strip("/")
    parsed = urlparse(base)
    path = parsed.path.rstrip("/")
    if path.endswith(f"/{suffix}"):
        return base
    return urljoin(f"{base}/", suffix).rstrip("/")


def resolve_ai_gateway_service_token(configured_key: str | None = None) -> str:
    for value in (
        configured_key,
        os.getenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN"),
        os.getenv("AI_GATEWAY_SERVICE_TOKEN"),
        os.getenv("INTERNAL_SERVICE_TOKEN"),
    ):
        token = (value or "").strip()
        if token:
            return token
    raise ValueError(
        "AI Gateway provider requires KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN, "
        "AI_GATEWAY_SERVICE_TOKEN, or INTERNAL_SERVICE_TOKEN."
    )


def ai_gateway_caller_service() -> str:
    return (os.getenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_CALLER_SERVICE") or DEFAULT_AI_GATEWAY_CALLER_SERVICE).strip() or DEFAULT_AI_GATEWAY_CALLER_SERVICE


def ai_gateway_profile_id(env_name: str, default_value: str) -> str:
    return (os.getenv(env_name) or default_value).strip()


def ai_gateway_timeout_seconds() -> float:
    raw = (os.getenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_TIMEOUT_SECONDS") or "").strip()
    if not raw:
        return DEFAULT_AI_GATEWAY_TIMEOUT_SECONDS
    try:
        value = float(raw)
    except ValueError:
        return DEFAULT_AI_GATEWAY_TIMEOUT_SECONDS
    return value if value > 0 else DEFAULT_AI_GATEWAY_TIMEOUT_SECONDS


def current_runtime_request_id() -> str:
    try:
        from quart import request

        return (request.headers.get("X-Request-Id") or "").strip()
    except Exception:
        return ""


def ai_gateway_request_id() -> str:
    configured = (
        current_runtime_request_id()
        or os.getenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_REQUEST_ID")
        or os.getenv("X_REQUEST_ID")
        or os.getenv("REQUEST_ID")
        or ""
    ).strip()
    if configured:
        return configured
    return f"stw-kgw-{uuid.uuid4().hex}"


def ai_gateway_headers(service_token: str, caller_service: str, request_id: str | None = None) -> dict[str, str]:
    return {
        "accept": "application/json",
        "content-type": "application/json",
        "X-Service-Token": service_token,
        "X-Caller-Service": caller_service,
        "X-Request-Id": request_id or ai_gateway_request_id(),
    }


def raise_ai_gateway_model_exception_if_failed(resp):
    status_code = resp.status_code
    if status_code >= 400:
        retryable = status_code >= 500 or status_code in [408, 429]
        raise ModelException(f"status: {resp.status_code}, response: {resp.text}", retryable=retryable)

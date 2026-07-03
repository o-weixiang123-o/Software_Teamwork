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
import hmac
import os
from collections.abc import Iterable


SERVICE_TOKEN_HEADER = "X-Service-Token"
RUNTIME_SERVICE_TOKEN_ENV = "KNOWLEDGE_RUNTIME_SERVICE_TOKEN"
GATEWAY_AUTH_TYPE = "GATEWAY"


def configured_service_token() -> str:
    return os.getenv(RUNTIME_SERVICE_TOKEN_ENV, "").strip()


def service_token_is_valid(headers) -> bool:
    expected = configured_service_token()
    if not expected:
        return False

    provided = (headers.get(SERVICE_TOKEN_HEADER) or "").strip()
    if not provided:
        return False

    return hmac.compare_digest(provided, expected)


def normalize_route_auth_types(auth_types=None, default_auth_types=(GATEWAY_AUTH_TYPE,)) -> set[str]:
    if auth_types is None:
        return {str(auth_type).upper() for auth_type in default_auth_types}
    if isinstance(auth_types, str):
        return {auth_types.upper()}
    if isinstance(auth_types, Iterable):
        return {str(auth_type).upper() for auth_type in auth_types}
    return {str(auth_types).upper()}


def route_allows_gateway_auth(auth_types=None, default_auth_types=(GATEWAY_AUTH_TYPE,)) -> bool:
    return GATEWAY_AUTH_TYPE in normalize_route_auth_types(auth_types, default_auth_types)

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
import hashlib
import os
from types import SimpleNamespace


DEFAULT_RUNTIME_SCOPE_ID = "knowledge_runtime"
DEFAULT_RUNTIME_INDEX_ID = "runtime"
MAX_RUNTIME_SCOPE_ID_LENGTH = 32
RUNTIME_SCOPE_ID_ENV = "KNOWLEDGE_RUNTIME_SCOPE_ID"
RUNTIME_INDEX_ID_ENV = "KNOWLEDGE_RUNTIME_INDEX_ID"


def normalize_runtime_scope_id(value) -> str:
    raw = str(value or "").strip()
    if not raw:
        raw = DEFAULT_RUNTIME_SCOPE_ID
    if len(raw) <= MAX_RUNTIME_SCOPE_ID_LENGTH:
        return raw
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()
    return f"rt_{digest[:MAX_RUNTIME_SCOPE_ID_LENGTH - 3]}"


def runtime_scope_id(environ=None) -> str:
    environ = environ if environ is not None else os.environ
    raw = environ.get(RUNTIME_SCOPE_ID_ENV) or DEFAULT_RUNTIME_SCOPE_ID
    return normalize_runtime_scope_id(raw)


def runtime_index_id(environ=None) -> str:
    environ = environ if environ is not None else os.environ
    raw = str(environ.get(RUNTIME_INDEX_ID_ENV) or DEFAULT_RUNTIME_INDEX_ID).strip()
    return raw or DEFAULT_RUNTIME_INDEX_ID


def runtime_subject(environ=None):
    scope_id = runtime_scope_id(environ)
    return SimpleNamespace(
        id=scope_id,
        nickname="Knowledge runtime",
        status="1",
        is_authenticated="1",
        is_active="1",
        is_anonymous="0",
    )

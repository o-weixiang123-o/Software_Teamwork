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
"""Regression tests for protected PaddleOCR parser credentials."""

import importlib.util
import sys
from pathlib import Path
from types import ModuleType, SimpleNamespace
from unittest.mock import MagicMock

import pytest


def _stub(monkeypatch, name, **attrs):
    mod = ModuleType(name)
    for key, value in attrs.items():
        setattr(mod, key, value)
    monkeypatch.setitem(sys.modules, name, mod)
    if "." in name:
        parent_name, _, child_name = name.rpartition(".")
        parent_mod = sys.modules.get(parent_name)
        if parent_mod is not None:
            monkeypatch.setattr(parent_mod, child_name, mod, raising=False)
    return mod


def _load_dataset_service_module(
    monkeypatch,
    *,
    ensure_paddleocr_from_config,
    knowledgebase_service=None,
    remap_dictionary_keys=None,
    verify_embedding_availability=None,
):
    _stub(
        monkeypatch,
        "api.db.joint_services.runtime_model_service",
        ensure_paddleocr_from_config=ensure_paddleocr_from_config,
        get_model_config_from_provider_instance=MagicMock(),
    )
    _stub(
        monkeypatch,
        "api.db.services.document_service",
        DocumentService=SimpleNamespace(),
        queue_raptor_o_graphrag_tasks=MagicMock(),
    )
    _stub(
        monkeypatch,
        "api.db.services.file2document_service",
        File2DocumentService=SimpleNamespace(),
    )
    _stub(
        monkeypatch,
        "api.db.services.file_service",
        FileService=SimpleNamespace(),
    )
    _stub(
        monkeypatch,
        "api.db.services.knowledgebase_service",
        KnowledgebaseService=knowledgebase_service or SimpleNamespace(),
    )
    _stub(
        monkeypatch,
        "api.db.services.task_service",
        TaskService=SimpleNamespace(),
        GRAPH_RAPTOR_FAKE_DOC_ID="fake-doc",
    )
    _stub(
        monkeypatch,
        "api.utils.api_utils",
        deep_merge=MagicMock(),
        get_parser_config=MagicMock(),
        remap_dictionary_keys=remap_dictionary_keys or MagicMock(),
        verify_embedding_availability=verify_embedding_availability or MagicMock(),
    )
    _stub(
        monkeypatch,
        "api.db.db_models",
        File=SimpleNamespace(source_type="source_type", id="id", type="type", name="name"),
    )
    _stub(
        monkeypatch,
        "common.constants",
        PAGERANK_FLD="pagerank",
        FileSource=SimpleNamespace(KNOWLEDGEBASE="knowledgebase"),
        StatusEnum=SimpleNamespace(VALID=SimpleNamespace(value="1")),
    )
    _stub(
        monkeypatch,
        "common.settings",
        docStoreConn=SimpleNamespace(),
    )

    repo_root = Path(__file__).resolve().parents[5]
    module_path = repo_root / "api" / "apps" / "services" / "dataset_api_service.py"
    spec = importlib.util.spec_from_file_location("test_paddleocr_dataset_service_module", module_path)
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, "test_paddleocr_dataset_service_module", module)
    spec.loader.exec_module(module)
    return module


def test_apply_parser_config_credentials_sets_layout_model_without_persisting_credentials(monkeypatch):
    calls = {}

    def fake_ensure(scope_id, config, model_name=None):
        calls.update(scope_id=scope_id, config=config, model_name=model_name)
        return "PaddleOCR-VL-1.6@PaddleOCR-VL-1.6@PaddleOCR"

    module = _load_dataset_service_module(monkeypatch, ensure_paddleocr_from_config=fake_ensure)
    req = {"parser_config": {"chunk_token_num": 512}}

    module._apply_parser_config_credentials(
        "scope-1",
        req,
        {
            "paddleocr_cloud": {
                "paddleocr_base_url": "https://paddleocr.example.com/api",
                "paddleocr_access_token": "sk-secret",
                "paddleocr_algorithm": "PaddleOCR-VL-1.6",
            }
        },
    )

    assert req["parser_config"]["chunk_token_num"] == 512
    assert req["parser_config"]["layout_recognize"] == "PaddleOCR-VL-1.6@PaddleOCR-VL-1.6@PaddleOCR"
    assert "parser_config_credentials" not in req
    assert calls["scope_id"] == "scope-1"
    assert calls["model_name"] == "PaddleOCR-VL-1.6"
    assert calls["config"]["paddleocr_base_url"] == "https://paddleocr.example.com/api"
    assert calls["config"]["paddleocr_access_token"] == "sk-secret"


def test_apply_parser_config_credentials_keeps_request_unchanged_without_model(monkeypatch):
    ensure = MagicMock(return_value=None)
    module = _load_dataset_service_module(monkeypatch, ensure_paddleocr_from_config=ensure)
    req = {}

    module._apply_parser_config_credentials(
        "scope-1",
        req,
        {
            "paddleocr_cloud": {
                "paddleocr_base_url": "https://paddleocr.example.com/api",
                "paddleocr_algorithm": "PaddleOCR-VL-1.6",
            }
        },
    )

    assert req == {}
    ensure.assert_called_once()


@pytest.mark.asyncio
async def test_create_dataset_ignores_ext_parser_config_credentials(monkeypatch):
    captured = {}
    created_kb = SimpleNamespace(to_dict=lambda: {"id": "kb-1", "name": "Manuals"})
    ensure_paddleocr = MagicMock(return_value="PaddleOCR-VL@PaddleOCR-VL@PaddleOCR")

    def fake_create_with_name(name, scope_id, parser_id, **kwargs):
        captured.update(name=name, scope_id=scope_id, parser_id=parser_id, kwargs=kwargs)
        return True, {"id": "kb-1", "embd_id": "BAAI/bge-m3@default@SILICONFLOW"}

    knowledgebase_service = SimpleNamespace(
        create_with_name=fake_create_with_name,
        save=lambda **_kwargs: True,
        get_by_id=lambda _kb_id: (True, created_kb),
    )
    module = _load_dataset_service_module(
        monkeypatch,
        ensure_paddleocr_from_config=ensure_paddleocr,
        knowledgebase_service=knowledgebase_service,
        remap_dictionary_keys=lambda data: data,
        verify_embedding_availability=lambda *_args, **_kwargs: (True, None),
    )

    ok, result = await module.create_dataset(
        "scope-1",
        {
            "name": "Manuals",
            "parser_config": {"chunk_token_num": 512},
            "ext": {
                "parser_config_credentials": {
                    "paddleocr_cloud": {
                        "paddleocr_base_url": "https://paddleocr.example.com/api",
                        "paddleocr_access_token": "sk-secret",
                    }
                }
            },
        },
    )

    assert ok is True
    assert result["id"] == "kb-1"
    assert "parser_config_credentials" not in captured["kwargs"]
    assert captured["kwargs"]["parser_config"]["chunk_token_num"] == 512
    assert "layout_recognize" not in captured["kwargs"]["parser_config"]
    ensure_paddleocr.assert_not_called()


@pytest.mark.asyncio
async def test_create_dataset_consumes_top_level_parser_config_credentials(monkeypatch):
    captured = {}
    created_kb = SimpleNamespace(to_dict=lambda: {"id": "kb-1", "name": "Manuals"})

    def fake_create_with_name(name, scope_id, parser_id, **kwargs):
        captured.update(name=name, scope_id=scope_id, parser_id=parser_id, kwargs=kwargs)
        return True, {"id": "kb-1", "embd_id": "BAAI/bge-m3@default@SILICONFLOW"}

    knowledgebase_service = SimpleNamespace(
        create_with_name=fake_create_with_name,
        save=lambda **_kwargs: True,
        get_by_id=lambda _kb_id: (True, created_kb),
    )
    module = _load_dataset_service_module(
        monkeypatch,
        ensure_paddleocr_from_config=lambda *_args, **_kwargs: "PaddleOCR-VL@PaddleOCR-VL@PaddleOCR",
        knowledgebase_service=knowledgebase_service,
        remap_dictionary_keys=lambda data: data,
        verify_embedding_availability=lambda *_args, **_kwargs: (True, None),
    )

    ok, result = await module.create_dataset(
        "scope-1",
        {
            "name": "Manuals",
            "parser_config": {"chunk_token_num": 512},
            "parser_config_credentials": {
                "paddleocr_cloud": {
                    "paddleocr_base_url": "https://paddleocr.example.com/api",
                    "paddleocr_access_token": "sk-secret",
                }
            },
        },
    )

    assert ok is True
    assert result["id"] == "kb-1"
    assert "parser_config_credentials" not in captured["kwargs"]
    assert captured["kwargs"]["parser_config"]["chunk_token_num"] == 512
    assert captured["kwargs"]["parser_config"]["layout_recognize"] == "PaddleOCR-VL@PaddleOCR-VL@PaddleOCR"

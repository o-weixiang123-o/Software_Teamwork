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

from types import SimpleNamespace

from common.constants import ActiveStatusEnum
from api.db.joint_services import runtime_model_service as module


def test_resolve_instance_for_model_falls_back_from_default_to_single_active_instance(monkeypatch):
    provider = SimpleNamespace(id="provider-1", provider_name="SILICONFLOW")
    resolved = SimpleNamespace(
        id="instance-1",
        instance_name="yy2",
        status=ActiveStatusEnum.ACTIVE.value,
    )

    monkeypatch.setattr(
        module.RuntimeModelInstanceService,
        "get_by_provider_id_and_instance_name",
        lambda provider_id, instance_name: None,
    )
    monkeypatch.setattr(
        module.RuntimeModelInstanceService,
        "get_all_by_provider_id",
        lambda provider_id: [resolved],
    )

    got = module._resolve_instance_for_model(
        provider,
        "default",
        "Qwen/Qwen3-8B@default@SILICONFLOW",
    )

    assert got is resolved


def test_ensure_paddleocr_from_config_normalizes_ui_credentials(monkeypatch):
    calls = {}

    def fake_ensure(scope_id, provider_name, model_name, config):
        calls.update(
            scope_id=scope_id,
            provider_name=provider_name,
            model_name=model_name,
            config=config,
        )
        return f"{model_name}@{model_name}@{provider_name}"

    monkeypatch.setattr(module, "_ensure_ocr_provider_from_env", fake_ensure)

    got = module.ensure_paddleocr_from_config(
        "scope-1",
        {
            "paddleocr_base_url": "https://paddleocr.example.com/api",
            "paddleocr_access_token": "sk-secret",
            "paddleocr_algorithm": "PaddleOCR-VL-1.6",
        },
    )

    assert got == "PaddleOCR-VL-1.6@PaddleOCR-VL-1.6@PaddleOCR"
    assert calls["scope_id"] == "scope-1"
    assert calls["provider_name"] == "PaddleOCR"
    assert calls["model_name"] == "PaddleOCR-VL-1.6"
    assert calls["config"]["PADDLEOCR_BASE_URL"] == "https://paddleocr.example.com/api"
    assert calls["config"]["PADDLEOCR_ACCESS_TOKEN"] == "sk-secret"
    assert calls["config"]["PADDLEOCR_ALGORITHM"] == "PaddleOCR-VL-1.6"


def test_ensure_paddleocr_from_config_requires_token(monkeypatch):
    called = False

    def fake_ensure(*_args, **_kwargs):
        nonlocal called
        called = True

    monkeypatch.setattr(module, "_ensure_ocr_provider_from_env", fake_ensure)

    got = module.ensure_paddleocr_from_config(
        "scope-1",
        {
            "paddleocr_base_url": "https://paddleocr.example.com/api",
            "paddleocr_algorithm": "PaddleOCR-VL-1.6",
        },
    )

    assert got is None
    assert called is False

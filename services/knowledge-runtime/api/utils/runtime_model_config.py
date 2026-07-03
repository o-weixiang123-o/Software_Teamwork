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
import enum
import os

from common import settings
from common.constants import LLMType


AI_GATEWAY_FACTORY = "AI_GATEWAY"
DEFAULT_AI_GATEWAY_BASE_URL = "http://127.0.0.1:8086/internal/v1"


def model_type_value(model_type: str | enum.Enum) -> str:
    return model_type if isinstance(model_type, str) else model_type.value


def split_model_reference(model_name: str):
    parts = str(model_name or "").strip().split("@")
    if len(parts) == 1:
        return parts[0], "", ""
    if len(parts) == 2:
        return parts[0], "default", parts[1]
    return "@".join(parts[:-2]), parts[-2], parts[-1]


def compose_model_reference(model_name: str, factory: str, instance_name: str = "default") -> str:
    model_name = str(model_name or "").strip()
    factory = str(factory or "").strip()
    instance_name = str(instance_name or "default").strip() or "default"
    if not model_name:
        return ""
    _, _, existing_factory = split_model_reference(model_name)
    if existing_factory:
        return model_name
    if not factory:
        return model_name
    return f"{model_name}@{instance_name}@{factory}"


def default_model_id(model_type: str | enum.Enum) -> str:
    model_type_val = model_type_value(model_type)
    model, factory, _base_url, settings_cfg = _env_and_settings_for_type(model_type_val)
    if model:
        return compose_model_reference(model, factory)

    settings_model = str(settings_cfg.get("model") or "").strip()
    settings_factory = str(settings_cfg.get("factory") or "").strip()
    return compose_model_reference(settings_model, settings_factory)


def default_model_base_url(model_type: str | enum.Enum, provider_name: str = "") -> str:
    model_type_val = model_type_value(model_type)
    _model, factory, base_url, settings_cfg = _env_and_settings_for_type(model_type_val)
    provider_name = provider_name or factory
    if base_url:
        return base_url
    settings_base_url = str(settings_cfg.get("base_url") or "").strip()
    if settings_base_url:
        return settings_base_url
    if provider_name == AI_GATEWAY_FACTORY:
        return DEFAULT_AI_GATEWAY_BASE_URL
    return ""


def runtime_model_config(model_type: str | enum.Enum, model_name: str | None = None) -> dict | None:
    model_type_val = model_type_value(model_type)
    model_ref = str(model_name or default_model_id(model_type_val) or "").strip()
    if not model_ref:
        return None

    pure_model_name, _instance_name, provider_name = split_model_reference(model_ref)
    if not pure_model_name:
        return None

    _env_model, env_factory, _env_base_url, settings_cfg = _env_and_settings_for_type(model_type_val)
    if not provider_name:
        provider_name = env_factory or str(settings_cfg.get("factory") or "").strip()
    if not provider_name:
        return None

    api_key = ""
    if provider_name != AI_GATEWAY_FACTORY:
        api_key = (
            os.getenv("KNOWLEDGE_RUNTIME_MODEL_API_KEY", "").strip()
            or str(settings_cfg.get("api_key") or "").strip()
        )

    llm_info = _factory_llm_info(provider_name, pure_model_name)
    return {
        "llm_factory": provider_name,
        "api_key": api_key,
        "llm_name": pure_model_name,
        "api_base": default_model_base_url(model_type_val, provider_name),
        "model_type": model_type_val,
        "is_tools": bool(llm_info.get("is_tools", False)) if llm_info else False,
        "max_tokens": int(llm_info.get("max_tokens", 8192)) if llm_info else 8192,
    }


def _env_and_settings_for_type(model_type_val: str):
    if model_type_val == LLMType.EMBEDDING.value:
        return (
            os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_MODEL", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL", "").strip(),
            settings.EMBEDDING_CFG,
        )
    if model_type_val == LLMType.RERANK.value:
        return (
            os.getenv("KNOWLEDGE_RUNTIME_RERANK_MODEL", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_RERANK_FACTORY", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_RERANK_BASE_URL", "").strip(),
            settings.RERANK_CFG,
        )
    if model_type_val == LLMType.CHAT.value:
        return (
            os.getenv("KNOWLEDGE_RUNTIME_CHAT_MODEL", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_CHAT_FACTORY", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_CHAT_BASE_URL", "").strip(),
            settings.CHAT_CFG,
        )
    if model_type_val == LLMType.IMAGE2TEXT.value:
        return (
            os.getenv("KNOWLEDGE_RUNTIME_IMAGE2TEXT_MODEL", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_IMAGE2TEXT_FACTORY", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_IMAGE2TEXT_BASE_URL", "").strip(),
            settings.IMAGE2TEXT_CFG,
        )
    if model_type_val == LLMType.SPEECH2TEXT.value:
        return (
            os.getenv("KNOWLEDGE_RUNTIME_ASR_MODEL", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_ASR_FACTORY", "").strip(),
            os.getenv("KNOWLEDGE_RUNTIME_ASR_BASE_URL", "").strip(),
            settings.ASR_CFG,
        )
    return "", "", "", {}


def _factory_llm_info(provider_name: str, model_name: str) -> dict | None:
    for factory in settings.FACTORY_LLM_INFOS:
        if factory.get("name") != provider_name:
            continue
        for llm in factory.get("llm", []):
            if llm.get("llm_name") == model_name:
                return llm
    return None

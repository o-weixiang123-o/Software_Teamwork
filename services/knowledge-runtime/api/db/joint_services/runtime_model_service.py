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
import logging
import os
import enum
import json
from common import settings
from common.constants import ActiveStatusEnum, LLMType, MINERU_DEFAULT_CONFIG, MINERU_ENV_KEYS, OPENDATALOADER_DEFAULT_CONFIG, OPENDATALOADER_ENV_KEYS, PADDLEOCR_DEFAULT_CONFIG, PADDLEOCR_ENV_KEYS
from api.db.services.runtime_model_provider_service import RuntimeModelProviderService
from api.db.services.runtime_model_instance_service import RuntimeModelInstanceService
from api.db.services.runtime_model_service import RuntimeModelService
from api.utils.runtime_model_config import (
    runtime_model_config,
    split_model_reference,
)

logger = logging.getLogger(__name__)


def _factory_model_types(llm: dict) -> list[str]:
    model_type = llm.get("model_type")
    if isinstance(model_type, list):
        return model_type
    return [model_type] if model_type else []
def _decode_api_key_config(raw_api_key: str) -> tuple[str, bool | None, str | None]:
    if not raw_api_key:
        return raw_api_key, None, None

    try:
        parsed = json.loads(raw_api_key)
    except Exception:
        return raw_api_key, None, None

    if not isinstance(parsed, dict):
        return raw_api_key, None, None

    is_tools = bool(parsed["is_tools"]) if "is_tools" in parsed else None
    if set(parsed.keys()) <= {"api_key", "is_tools"}:
        return parsed.get("api_key", ""), is_tools, None

    return parsed.get("api_key", raw_api_key), is_tools, raw_api_key


def get_first_provider_model_name(scope_id: str, provider_name: str, model_type: str | enum.Enum) -> str | None:
    model_type_val = model_type if isinstance(model_type, str) else model_type.value
    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        return None

    for instance_obj in RuntimeModelInstanceService.get_all_by_provider_id(provider_obj.id):
        if instance_obj.status != ActiveStatusEnum.ACTIVE.value:
            continue
        for model_obj in RuntimeModelService.get_models_by_instance_id(instance_obj.id):
            if model_obj.model_type == model_type_val and model_obj.status == ActiveStatusEnum.ACTIVE.value:
                return f"{model_obj.model_name}@{instance_obj.instance_name}@{provider_name}"
    return None


def _collect_env_config(env_keys: list[str], default_config: dict) -> dict | None:
    config = dict(default_config)
    found = False
    for key in env_keys:
        value = os.environ.get(key)
        if value:
            found = True
            config[key] = value
    return config if found else None


def _ensure_ocr_provider_from_env(scope_id: str, provider_name: str, model_name: str, config: dict | None) -> str | None:
    if not config:
        return None

    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        RuntimeModelProviderService.insert(scope_id=scope_id, provider_name=provider_name)
        provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)

    api_key = json.dumps(config)
    instance_obj = RuntimeModelInstanceService.get_by_provider_id_and_api_key(provider_obj.id, api_key)
    if not instance_obj:
        instance_obj = RuntimeModelInstanceService.create_instance(
            provider_id=provider_obj.id,
            instance_name=model_name,
            api_key=api_key,
            extra="{}",
        )

    model_obj = RuntimeModelService.get_by_provider_id_and_instance_id_and_model_type_and_model_name(
        provider_obj.id,
        instance_obj.id,
        LLMType.OCR.value,
        model_name,
    )
    if not model_obj:
        RuntimeModelService.insert(
            model_name=model_name,
            provider_id=provider_obj.id,
            instance_id=instance_obj.id,
            model_type=LLMType.OCR.value,
            extra=json.dumps({"max_tokens": 0}),
        )

    return f"{model_name}@{instance_obj.instance_name}@{provider_name}"


def ensure_mineru_from_env(scope_id: str) -> str | None:
    return _ensure_ocr_provider_from_env(
        scope_id,
        "MinerU",
        "mineru-from-env",
        _collect_env_config(MINERU_ENV_KEYS, MINERU_DEFAULT_CONFIG),
    )


def ensure_paddleocr_from_env(scope_id: str) -> str | None:
    return _ensure_ocr_provider_from_env(
        scope_id,
        "PaddleOCR",
        "paddleocr-from-env",
        _collect_env_config(PADDLEOCR_ENV_KEYS, PADDLEOCR_DEFAULT_CONFIG),
    )


def ensure_paddleocr_from_config(scope_id: str, config: dict | None, model_name: str | None = None) -> str | None:
    """Provision a scope PaddleOCR OCR model from protected parser credentials."""
    if not isinstance(config, dict):
        return None

    normalized = dict(PADDLEOCR_DEFAULT_CONFIG)
    key_map = {
        "paddleocr_base_url": "PADDLEOCR_BASE_URL",
        "paddleocr_api_url": "PADDLEOCR_API_URL",
        "paddleocr_access_token": "PADDLEOCR_ACCESS_TOKEN",
        "paddleocr_algorithm": "PADDLEOCR_ALGORITHM",
    }
    for source_key, target_key in key_map.items():
        value = config.get(source_key)
        if value not in (None, ""):
            normalized[target_key] = value
    for key in PADDLEOCR_ENV_KEYS:
        value = config.get(key)
        if value not in (None, ""):
            normalized[key] = value

    if not normalized.get("PADDLEOCR_ACCESS_TOKEN"):
        return None

    selected_model = str(model_name or normalized.get("PADDLEOCR_ALGORITHM") or PADDLEOCR_DEFAULT_CONFIG["PADDLEOCR_ALGORITHM"]).strip()
    if not selected_model:
        selected_model = PADDLEOCR_DEFAULT_CONFIG["PADDLEOCR_ALGORITHM"]
    normalized["PADDLEOCR_ALGORITHM"] = selected_model

    return _ensure_ocr_provider_from_env(scope_id, "PaddleOCR", selected_model, normalized)


def get_runtime_default_model_by_type(scope_id: str, model_type: str|enum.Enum):
    runtime_config = runtime_model_config(model_type)
    if runtime_config:
        return runtime_config

    raise LookupError(f"Default runtime model for {model_type} is not configured")


def split_model_name(model_name: str):
    # Parse model_name: {model_name} or {model_name}@{factory_name} or {model_name}@{instance_name}@{factory_name}
    return split_model_reference(model_name)


def _resolve_instance_for_model(provider_obj, instance_name: str, model_name: str):
    instance_obj = RuntimeModelInstanceService.get_by_provider_id_and_instance_name(provider_obj.id, instance_name)
    if instance_obj:
        return instance_obj
    if instance_name != "default":
        raise LookupError(f"Instance {instance_name} not found for model {model_name}.")

    active_instances = [
        inst for inst in RuntimeModelInstanceService.get_all_by_provider_id(provider_obj.id)
        if inst.status == ActiveStatusEnum.ACTIVE.value
    ]
    if len(active_instances) == 1:
        logger.warning(
            "Model instance fallback applied for legacy default instance name",
            extra={
                "provider_name": provider_obj.provider_name,
                "requested_instance_name": instance_name,
                "resolved_instance_name": active_instances[0].instance_name,
                "model_name": model_name,
            },
        )
        return active_instances[0]

    raise LookupError(f"Instance {instance_name} not found for model {model_name}.")


def get_model_config_from_provider_instance(scope_id, model_type: str|enum.Enum, model_name: str):
    runtime_config = runtime_model_config(model_type, model_name)
    if runtime_config:
        return runtime_config

    pure_model_name, instance_name, provider_name = split_model_name(model_name)
    model_type_val = model_type if isinstance(model_type, str) else model_type.value
    # Builtin embedding model
    compose_profiles = os.getenv("COMPOSE_PROFILES", "")
    is_tei_builtin_embedding = (
            model_type_val == LLMType.EMBEDDING.value
            and "tei-" in compose_profiles
            and pure_model_name == os.getenv("TEI_MODEL", "")
            and (provider_name == "Builtin" or not provider_name)
    )
    if is_tei_builtin_embedding:
        # configured local embedding model
        embedding_cfg = settings.EMBEDDING_CFG
        return {
            "llm_factory": "Builtin",
            "api_key": embedding_cfg["api_key"],
            "llm_name": pure_model_name,
            "api_base": embedding_cfg["base_url"],
            "model_type": LLMType.EMBEDDING.value,
        }

    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        raise LookupError(f"Provider {provider_name} not found for model {model_name}.")
    instance_obj = _resolve_instance_for_model(provider_obj, instance_name, model_name)
    model_obj = RuntimeModelService.get_by_provider_id_and_instance_id_and_model_type_and_model_name(provider_obj.id, instance_obj.id, model_type_val, pure_model_name)

    api_key, is_tool, api_key_payload = _decode_api_key_config(instance_obj.api_key)
    extra_fields = json.loads(instance_obj.extra) if instance_obj.extra else {}

    if model_obj:
        if model_obj.status == ActiveStatusEnum.INACTIVE.value:
            raise LookupError(f"Model {model_name} is disabled.")
        if model_obj.status == ActiveStatusEnum.UNSUPPORTED.value:
            raise LookupError(f"Model {model_name} cannot be used as {model_type_val} model.")

        model_extra = json.loads(model_obj.extra) if model_obj.extra else {}
        model_config = {
            "llm_factory": provider_obj.provider_name,
            "api_key": api_key,
            "llm_name": model_obj.model_name,
            "api_base": extra_fields.get("base_url", ""),
            "model_type": model_obj.model_type,
            "is_tools": model_extra.get("is_tools", is_tool),
            "max_tokens": model_extra.get("max_tokens", 8192),
        }
        if api_key_payload is not None:
            model_config["api_key_payload"] = api_key_payload

        return model_config
    else:
        region = extra_fields.get("region", "default")
        if region == "intl" and provider_name.lower() == "siliconflow":
            target_factory_name = "siliconflow_intl"
        else:
            target_factory_name = provider_name
        fac_list = [f for f in settings.FACTORY_LLM_INFOS if f["name"] == target_factory_name]
        if not fac_list:
            raise LookupError(f"Model provider config not found: {provider_name}")
        llm_list = [llm for llm in fac_list[0]["llm"] if llm["llm_name"] == pure_model_name]
        if not llm_list:
            raise LookupError(f"Instance {instance_name} not found for model {model_name}.")
        llm_info = llm_list[0]
        if model_type_val not in _factory_model_types(llm_info):
            raise LookupError(f"Model {model_name} is not a {model_type_val} model.")
        model_config = {
            "llm_factory": provider_obj.provider_name,
            "api_key": api_key,
            "llm_name": llm_info["llm_name"],
            "api_base": extra_fields.get("base_url", ""),
            "model_type": model_type_val,
            "is_tools": llm_info.get("is_tools", is_tool),
            "max_tokens": llm_info.get("max_tokens", 8192),
        }
        if api_key_payload is not None:
            model_config["api_key_payload"] = api_key_payload
        return model_config


def get_api_key(scope_id: str, model_name: str):
    _, instance_name, provider_name = split_model_name(model_name)
    runtime_config = runtime_model_config("", model_name)
    if runtime_config is not None:
        return runtime_config.get("api_key", "")

    if not provider_name:
        raise LookupError("Provider name is required.")
    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        raise LookupError(f"Provider {provider_name} not found.")
    instance_obj = _resolve_instance_for_model(provider_obj, instance_name, model_name)
    return instance_obj.api_key


def get_model_type_by_name(scope_id: str, model_name: str):
    pure_model_name, instance_name, provider_name = split_model_name(model_name)
    for model_type in (LLMType.EMBEDDING.value, LLMType.RERANK.value, LLMType.CHAT.value, LLMType.IMAGE2TEXT.value, LLMType.SPEECH2TEXT.value):
        config = runtime_model_config(model_type, model_name)
        if config is not None:
            return [model_type]

    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        raise LookupError(f"Provider {provider_name} not found for model {model_name}.")
    instance_obj = _resolve_instance_for_model(provider_obj, instance_name, model_name)
    model_objs = RuntimeModelService.get_by_provider_id_and_instance_id_and_model_name(provider_obj.id, instance_obj.id, pure_model_name)
    types_in_json = []
    if not model_objs:
        extra_fields = json.loads(instance_obj.extra) if instance_obj.extra else {}
        region = extra_fields.get("region", "default")
        if region == "intl" and provider_name.lower() == "siliconflow":
            target_factory_name = "siliconflow_intl"
        else:
            target_factory_name = provider_name
        fac_list = [f for f in settings.FACTORY_LLM_INFOS if f["name"] == target_factory_name]
        if not fac_list:
            raise LookupError(f"Model provider config not found: {provider_name}")
        llm_list = [llm for llm in fac_list[0]["llm"] if llm["llm_name"] == pure_model_name]
        if not llm_list:
            raise LookupError(f"Model {pure_model_name} not found for model {model_name}.")
        types_in_json = _factory_model_types(llm_list[0])
    return list(set(types_in_json + [model_obj.model_type for model_obj in model_objs if model_obj.status != ActiveStatusEnum.UNSUPPORTED.value]) - {model_obj.model_type for model_obj in model_objs if model_obj.status == ActiveStatusEnum.UNSUPPORTED.value})


def delete_models_by_instance_ids(instance_ids: list[str]):
    return RuntimeModelService.delete_by_instance_ids(instance_ids)


def delete_instances_by_provider_ids(provider_ids: list[str]):
    return RuntimeModelInstanceService.delete_by_provider_ids(provider_ids)


def ensure_opendataloader_from_env(scope_id: str) -> str | None:
    return _ensure_ocr_provider_from_env(
        scope_id,
        "OpenDataLoader",
        "opendataloader-from-env",
        _collect_env_config(OPENDATALOADER_ENV_KEYS, OPENDATALOADER_DEFAULT_CONFIG),
    )


def get_models_by_scope_and_provider_and_model_type(scope_id: str, provider_name: str, model_type: str):
    """
    Query RuntimeModel records by scope_id, provider_name and model_type.
    Returns all matching model records under all instances of the specified provider.
    """
    provider_obj = RuntimeModelProviderService.get_by_scope_id_and_provider_name(scope_id, provider_name)
    if not provider_obj:
        return []
    instances = RuntimeModelInstanceService.get_all_by_provider_id(provider_obj.id)
    if not instances:
        return []
    results = []
    for inst in instances:
        model = RuntimeModelService.get_by_provider_id_and_instance_id_and_model_type(provider_obj.id, inst.id, model_type)
        if model and model.status != ActiveStatusEnum.UNSUPPORTED.value:
            results.append(model)
    return results

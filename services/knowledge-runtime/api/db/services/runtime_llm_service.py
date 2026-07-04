#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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
import json
import logging
from peewee import IntegrityError
from common import settings
from common.constants import MINERU_DEFAULT_CONFIG, MINERU_ENV_KEYS, OPENDATALOADER_DEFAULT_CONFIG, OPENDATALOADER_ENV_KEYS, PADDLEOCR_DEFAULT_CONFIG, PADDLEOCR_ENV_KEYS, LLMType
from api.db.db_models import DB, LLMFactories, RuntimeLLM
from api.db.services.common_service import CommonService
from api.utils.runtime_model_config import runtime_model_config


class LLMFactoriesService(CommonService):
    model = LLMFactories


class RuntimeLLMService(CommonService):
    model = RuntimeLLM

    @staticmethod
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

    @staticmethod
    def _encode_api_key_config(raw_api_key: str, is_tools: bool | None) -> str:
        if is_tools is None:
            return raw_api_key

        try:
            parsed = json.loads(raw_api_key or "{}")
        except Exception:
            parsed = None

        if isinstance(parsed, dict):
            payload = dict(parsed)
            payload["is_tools"] = bool(is_tools)
            return json.dumps(payload)

        return json.dumps({"api_key": raw_api_key or "", "is_tools": bool(is_tools)})

    @classmethod
    @DB.connection_context()
    def get_api_key(cls, scope_id, model_name, model_type=None):
        mdlnm, fid = RuntimeLLMService.split_model_name_and_factory(model_name)
        model_type_val = model_type.value if hasattr(model_type, "value") else model_type
        query_kwargs = {"scope_id": scope_id, "llm_name": mdlnm}
        if model_type_val is not None:
            query_kwargs["model_type"] = model_type_val
        if not fid:
            objs = cls.query(**query_kwargs)
        else:
            objs = cls.query(**query_kwargs, llm_factory=fid)

        if (not objs) and fid:
            if fid == "LocalAI":
                mdlnm += "___LocalAI"
            elif fid == "HuggingFace":
                mdlnm += "___HuggingFace"
            elif fid == "OpenAI-API-Compatible":
                mdlnm += "___OpenAI-API"
            elif fid == "VLLM":
                mdlnm += "___VLLM"
            query_kwargs["llm_name"] = mdlnm
            objs = cls.query(**query_kwargs, llm_factory=fid)
        if not objs:
            return None
        return objs[0]

    @classmethod
    @DB.connection_context()
    def get_my_llms(cls, scope_id):
        fields = [cls.model.id, cls.model.llm_factory, LLMFactories.logo, LLMFactories.tags, cls.model.model_type, cls.model.llm_name, cls.model.used_tokens, cls.model.status]
        objs = cls.model.select(*fields).join(LLMFactories, on=(cls.model.llm_factory == LLMFactories.name)).where(cls.model.scope_id == scope_id, ~cls.model.api_key.is_null()).dicts()

        return list(objs)

    @staticmethod
    def split_model_name_and_factory(model_name):
        arr = model_name.split("@")
        if len(arr) < 2:
            return model_name, None
        if len(arr) > 2:
            return "@".join(arr[0:-1]), arr[-1]

        # model name must be xxx@yyy
        try:
            model_factories = settings.FACTORY_LLM_INFOS
            model_providers = set([f["name"] for f in model_factories])
            if arr[-1] not in model_providers:
                return model_name, None
            return arr[0], arr[-1]
        except Exception as e:
            logging.exception(f"RuntimeLLMService.split_model_name_and_factory got exception: {e}")
        return model_name, None

    @classmethod
    @DB.connection_context()
    def get_model_config(cls, scope_id, llm_type, llm_name=None):
        model_config = runtime_model_config(llm_type, llm_name)
        if model_config:
            return model_config

        raise LookupError(f"Runtime model for {llm_type} is not configured")

    @classmethod
    @DB.connection_context()
    def model_instance(cls, model_config: dict, lang="Chinese", **kwargs):
        if not model_config:
            raise LookupError("Model config is required")
        from rag.llm import ChatModel, CvModel, EmbeddingModel, OcrModel, RerankModel, Seq2txtModel, TTSModel

        kwargs.update({"provider": model_config["llm_factory"]})
        api_key = model_config.get("api_key_payload", model_config["api_key"])
        if model_config["model_type"] == LLMType.EMBEDDING.value:
            if model_config["llm_factory"] not in EmbeddingModel:
                logging.error("Factory not in embedding model. Supported factories: %s", list(EmbeddingModel.keys()))
                return None
            return EmbeddingModel[model_config["llm_factory"]](api_key, model_config["llm_name"], base_url=model_config["api_base"])

        elif model_config["model_type"] == LLMType.RERANK.value:
            if model_config["llm_factory"] not in RerankModel:
                logging.error("Factory not in rerank model. Supported factories: %s", list(RerankModel.keys()))
                return None
            return RerankModel[model_config["llm_factory"]](api_key, model_config["llm_name"], base_url=model_config["api_base"])

        elif model_config["model_type"] == LLMType.IMAGE2TEXT.value:
            if model_config["llm_factory"] not in CvModel:
                logging.error("Factory not in cv model. Supported factories: %s", list(CvModel.keys()))
                return None
            return CvModel[model_config["llm_factory"]](api_key, model_config["llm_name"], lang, base_url=model_config["api_base"], **kwargs)

        elif model_config["model_type"] == LLMType.CHAT.value:
            if model_config["llm_factory"] not in ChatModel:
                logging.error("Factory not in chat model. Supported factories: %s", list(ChatModel.keys()))
                return None
            return ChatModel[model_config["llm_factory"]](api_key, model_config["llm_name"], base_url=model_config["api_base"], **kwargs)

        elif model_config["model_type"] == LLMType.SPEECH2TEXT.value:
            if model_config["llm_factory"] not in Seq2txtModel:
                logging.error("Factory not in speech2text model. Supported factories: %s", list(Seq2txtModel.keys()))
                return None
            return Seq2txtModel[model_config["llm_factory"]](key=api_key, model_name=model_config["llm_name"], lang=lang, base_url=model_config["api_base"])
        elif model_config["model_type"] == LLMType.TTS.value:
            if model_config["llm_factory"] not in TTSModel:
                logging.error("Factory not in tts model. Supported factories: %s", list(TTSModel.keys()))
                return None
            return TTSModel[model_config["llm_factory"]](
                api_key,
                model_config["llm_name"],
                base_url=model_config["api_base"],
            )

        elif model_config["model_type"] == LLMType.OCR.value:
            if model_config["llm_factory"] not in OcrModel:
                logging.error("Factory not in ocr model. Supported factories: %s", list(OcrModel.keys()))
                return None
            return OcrModel[model_config["llm_factory"]](
                key=api_key,
                model_name=model_config["llm_name"],
                base_url=model_config.get("api_base", ""),
                **kwargs,
            )

        return None

    @classmethod
    @DB.connection_context()
    def increase_usage(cls, scope_id, llm_type, used_tokens, llm_name=None):
        logging.debug("Skip RuntimeLLM usage update; runtime model usage is audited by AI Gateway/provider logs")
        return 0

    @classmethod
    @DB.connection_context()
    def increase_usage_by_id(cls, runtime_model_id: int, used_tokens: int):
        try:
            update_cnt = cls.model.update(used_tokens=cls.model.used_tokens + used_tokens).where(cls.model.id == runtime_model_id).execute()
        except Exception as e:
            logging.exception(f"RuntimeLLMService.increase_usage got exception {e}, Failed to update used_tokens for runtime_model_id {runtime_model_id}")
            return 0
        return update_cnt

    @classmethod
    @DB.connection_context()
    def get_openai_models(cls):
        objs = cls.model.select().where((cls.model.llm_factory == "OpenAI"), ~(cls.model.llm_name == "text-embedding-3-small"), ~(cls.model.llm_name == "text-embedding-3-large")).dicts()
        return list(objs)

    @classmethod
    def _collect_mineru_env_config(cls) -> dict | None:
        cfg = MINERU_DEFAULT_CONFIG
        found = False
        for key in MINERU_ENV_KEYS:
            val = os.environ.get(key)
            if val:
                found = True
                cfg[key] = val
        return cfg if found else None

    @classmethod
    @DB.connection_context()
    def ensure_mineru_from_env(cls, scope_id: str) -> str | None:
        """
        Ensure a MinerU OCR model exists for the scope if env variables are present.
        Return the existing or newly created llm_name, or None if env not set.
        """
        cfg = cls._collect_mineru_env_config()
        if not cfg:
            return None

        saved_mineru_models = cls.query(scope_id=scope_id, llm_factory="MinerU", model_type=LLMType.OCR.value)

        def _parse_api_key(raw: str) -> dict:
            try:
                return json.loads(raw or "{}")
            except Exception:
                return {}

        for item in saved_mineru_models:
            api_cfg = _parse_api_key(item.api_key)
            normalized = {k: api_cfg.get(k, MINERU_DEFAULT_CONFIG.get(k)) for k in MINERU_ENV_KEYS}
            if normalized == cfg:
                return item.llm_name

        used_names = {item.llm_name for item in saved_mineru_models}
        idx = 1
        base_name = "mineru-from-env"
        while True:
            candidate = f"{base_name}-{idx}"
            if candidate in used_names:
                idx += 1
                continue

            try:
                cls.save(
                    scope_id=scope_id,
                    llm_factory="MinerU",
                    llm_name=candidate,
                    model_type=LLMType.OCR.value,
                    api_key=json.dumps(cfg),
                    api_base="",
                    max_tokens=0,
                )
                return candidate
            except IntegrityError:
                logging.warning("MinerU env model %s already exists for scope %s, retry with next name", candidate, scope_id)
                used_names.add(candidate)
                idx += 1
                continue

    @classmethod
    def _collect_paddleocr_env_config(cls) -> dict | None:
        cfg = PADDLEOCR_DEFAULT_CONFIG
        found = False
        for key in PADDLEOCR_ENV_KEYS:
            val = os.environ.get(key)
            if val:
                found = True
                cfg[key] = val
        return cfg if found else None

    @classmethod
    @DB.connection_context()
    def ensure_paddleocr_from_env(cls, scope_id: str) -> str | None:
        """
        Ensure a PaddleOCR model exists for the scope if env variables are present.
        Return the existing or newly created llm_name, or None if env not set.
        """
        cfg = cls._collect_paddleocr_env_config()
        if not cfg:
            return None

        saved_paddleocr_models = cls.query(scope_id=scope_id, llm_factory="PaddleOCR", model_type=LLMType.OCR.value)

        def _parse_api_key(raw: str) -> dict:
            try:
                return json.loads(raw or "{}")
            except Exception:
                return {}

        for item in saved_paddleocr_models:
            api_cfg = _parse_api_key(item.api_key)
            normalized = {k: api_cfg.get(k, PADDLEOCR_DEFAULT_CONFIG.get(k)) for k in PADDLEOCR_ENV_KEYS}
            if normalized == cfg:
                return item.llm_name

        used_names = {item.llm_name for item in saved_paddleocr_models}
        idx = 1
        base_name = "paddleocr-from-env"
        while True:
            candidate = f"{base_name}-{idx}"
            if candidate in used_names:
                idx += 1
                continue

            try:
                cls.save(
                    scope_id=scope_id,
                    llm_factory="PaddleOCR",
                    llm_name=candidate,
                    model_type=LLMType.OCR.value,
                    api_key=json.dumps(cfg),
                    api_base="",
                    max_tokens=0,
                )
                return candidate
            except IntegrityError:
                logging.warning("PaddleOCR env model %s already exists for scope %s, retry with next name", candidate, scope_id)
                used_names.add(candidate)
                idx += 1
                continue

    @classmethod
    def _collect_opendataloader_env_config(cls) -> dict | None:
        cfg = dict(OPENDATALOADER_DEFAULT_CONFIG)
        found = False
        for key in OPENDATALOADER_ENV_KEYS:
            val = os.environ.get(key)
            if val:
                found = True
                cfg[key] = val
        return cfg if found else None

    @classmethod
    @DB.connection_context()
    def ensure_opendataloader_from_env(cls, scope_id: str) -> str | None:
        """
        Ensure an OpenDataLoader OCR model exists for the scope if env variables are present.
        Return the existing or newly created llm_name, or None if env not set.
        """
        cfg = cls._collect_opendataloader_env_config()
        if not cfg:
            return None

        saved_models = cls.query(scope_id=scope_id, llm_factory="OpenDataLoader", model_type=LLMType.OCR.value)

        def _parse_api_key(raw: str) -> dict:
            try:
                return json.loads(raw or "{}")
            except Exception:
                return {}

        for item in saved_models:
            api_cfg = _parse_api_key(item.api_key)
            normalized = {k: api_cfg.get(k, OPENDATALOADER_DEFAULT_CONFIG.get(k)) for k in OPENDATALOADER_ENV_KEYS}
            if normalized == cfg:
                return item.llm_name

        used_names = {item.llm_name for item in saved_models}
        idx = 1
        base_name = "opendataloader-from-env"
        while True:
            candidate = f"{base_name}-{idx}"
            if candidate in used_names:
                idx += 1
                continue
            try:
                cls.save(
                    scope_id=scope_id,
                    llm_factory="OpenDataLoader",
                    llm_name=candidate,
                    model_type=LLMType.OCR.value,
                    api_key=json.dumps(cfg),
                    api_base="",
                    max_tokens=0,
                )
                return candidate
            except IntegrityError:
                logging.warning("OpenDataLoader env model %s already exists for scope %s, retry with next name", candidate, scope_id)
                used_names.add(candidate)
                idx += 1
                continue

    @classmethod
    @DB.connection_context()
    def delete_by_scope_id(cls, scope_id):
        return cls.model.delete().where(cls.model.scope_id == scope_id).execute()

    @staticmethod
    def llm_id2llm_type(llm_id: str) -> str | None:
        from api.db.services.llm_service import LLMService

        llm_id, *_ = RuntimeLLMService.split_model_name_and_factory(llm_id)
        llm_factories = settings.FACTORY_LLM_INFOS
        for llm_factory in llm_factories:
            for llm in llm_factory["llm"]:
                if llm_id == llm["llm_name"]:
                    return llm["model_type"].split(",")[-1]

        for llm in LLMService.query(llm_name=llm_id):
            return llm.model_type

        llm = RuntimeLLMService.get_or_none(llm_name=llm_id)
        if llm:
            return llm.model_type
        for llm in RuntimeLLMService.query(llm_name=llm_id):
            return llm.model_type
        return None


class RuntimeLLMBundle:
    def __init__(self, scope_id: str, model_config: dict, lang="Chinese", **kwargs):
        self.scope_id = scope_id
        self.llm_name = model_config["llm_name"]
        self.model_config = model_config
        self.mdl = RuntimeLLMService.model_instance(model_config, lang=lang, **kwargs)
        assert self.mdl, "Can't find model for {}/{}/{}".format(scope_id, model_config["model_type"], model_config["llm_name"])
        self.max_length = model_config.get("max_tokens", 8192)

        self.is_tools = model_config.get("is_tools", False)
        self.verbose_tool_use = kwargs.get("verbose_tool_use")

    def close(self):
        """Release resources held by this RuntimeLLMBundle instance.

        This method should be called when the instance is no longer needed
        to properly release resources such as:
        - Underlying model instance resources (HTTP sessions, etc.)
        """
        # Release underlying model instance if it has a close method
        if self.mdl and hasattr(self.mdl, 'close') and callable(getattr(self.mdl, 'close')):
            try:
                self.mdl.close()
            except Exception:
                # Ignore errors during cleanup
                pass

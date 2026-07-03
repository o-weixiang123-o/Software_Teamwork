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
import logging
import json
import os
import time

from api.db.db_models import init_database_tables
from api.db.services.tenant_model_instance_service import TenantModelInstanceService
from api.db.services.tenant_model_provider_service import TenantModelProviderService
from api.db.services.tenant_model_service import TenantModelService
from api.db.services.document_service import DocumentService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.system_settings_service import SystemSettingsService
from api.db.services.user_service import TenantService
from common.constants import LLMType
from common.file_utils import get_project_base_directory
from common.misc_utils import get_uuid


def update_document_number_in_init():
    doc_count = DocumentService.get_all_kb_doc_count()
    for kb_id in KnowledgebaseService.get_all_ids():
        KnowledgebaseService.update_document_number_in_init(kb_id=kb_id, doc_num=doc_count.get(kb_id, 0))


def init_runtime_data():
    start_time = time.time()

    init_table()

    init_env_default_models()

    update_document_number_in_init()

    logging.info("init runtime data success:{}".format(time.time() - start_time))


def init_env_default_models():
    _init_env_default_models_for_tenants(None)


def init_env_default_models_for_tenant(tenant_id):
    _init_env_default_models_for_tenants([tenant_id])


def _init_env_default_models_for_tenants(tenant_ids):
    embedding_model = os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_MODEL", "").strip()
    embedding_factory = os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY", "").strip()
    embedding_base_url = os.getenv("KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL", "").strip()
    _ensure_env_default_model(
        model_name=embedding_model,
        provider_name=embedding_factory,
        model_type=LLMType.EMBEDDING.value,
        api_key=_env_model_key_for_factory(embedding_factory),
        base_url=embedding_base_url,
        tenant_field="embd_id",
        tenant_ids=tenant_ids,
    )

    rerank_model = os.getenv("KNOWLEDGE_RUNTIME_RERANK_MODEL", "").strip()
    rerank_factory = os.getenv("KNOWLEDGE_RUNTIME_RERANK_FACTORY", "").strip()
    rerank_base_url = os.getenv("KNOWLEDGE_RUNTIME_RERANK_BASE_URL", "").strip()
    _ensure_env_default_model(
        model_name=rerank_model,
        provider_name=rerank_factory,
        model_type=LLMType.RERANK.value,
        api_key=_env_model_key_for_factory(rerank_factory),
        base_url=rerank_base_url,
        tenant_field="rerank_id",
        tenant_ids=tenant_ids,
    )


def _env_model_key_for_factory(provider_name):
    if provider_name == "AI_GATEWAY":
        for key in (
            "KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN",
            "AI_GATEWAY_SERVICE_TOKEN",
            "INTERNAL_SERVICE_TOKEN",
        ):
            value = os.getenv(key, "").strip()
            if value:
                return value
        return ""
    return os.getenv("KNOWLEDGE_RUNTIME_MODEL_API_KEY", "").strip()


def _ensure_env_default_model(model_name, provider_name, model_type, api_key, base_url, tenant_field, tenant_ids=None):
    if not model_name or not provider_name or provider_name == "Builtin":
        return

    tenants = TenantService.model.select()
    if tenant_ids:
        tenants = tenants.where(TenantService.model.id.in_(tenant_ids))

    for tenant in tenants:
        provider = TenantModelProviderService.get_by_tenant_id_and_provider_name(tenant.id, provider_name)
        if not provider:
            TenantModelProviderService.insert(tenant_id=tenant.id, provider_name=provider_name)
            provider = TenantModelProviderService.get_by_tenant_id_and_provider_name(tenant.id, provider_name)

        instance = TenantModelInstanceService.get_by_provider_id_and_instance_name(provider.id, "default")
        extra = json.dumps({"base_url": base_url})
        if not instance:
            TenantModelInstanceService.insert(
                id=get_uuid(),
                provider_id=provider.id,
                instance_name="default",
                api_key=api_key,
                extra=extra,
            )
            instance = TenantModelInstanceService.get_by_provider_id_and_instance_name(provider.id, "default")
        else:
            TenantModelInstanceService.update_by_id(instance.id, {"api_key": api_key, "extra": extra})

        model = TenantModelService.get_by_provider_id_and_instance_id_and_model_type_and_model_name(
            provider.id,
            instance.id,
            model_type,
            model_name,
        )
        if not model:
            TenantModelService.insert(
                model_name=model_name,
                provider_id=provider.id,
                instance_id=instance.id,
                model_type=model_type,
                extra="{}",
            )

        TenantService.update_by_id(tenant.id, {tenant_field: f"{model_name}@default@{provider_name}"})


def init_table():
    # init system_settings
    with open(os.path.join(get_project_base_directory(), "conf", "system_settings.json"), "r") as f:
        records_from_file = json.load(f)["system_settings"]

    record_index = {}
    records_from_db = SystemSettingsService.get_all()
    for index, record in enumerate(records_from_db):
        record_index[record.name] = index

    to_save = []
    for record in records_from_file:
        setting_name = record["name"]
        if setting_name not in record_index:
            to_save.append(record)

    len_to_save = len(to_save)
    if len_to_save > 0:
        # not initialized
        try:
            SystemSettingsService.insert_many(to_save, len_to_save)
        except Exception as e:
            logging.exception("System settings init error: {}".format(e))
            raise e


if __name__ == '__main__':
    init_database_tables()
    init_runtime_data()

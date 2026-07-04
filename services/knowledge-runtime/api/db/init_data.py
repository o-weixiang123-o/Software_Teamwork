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
from api.db.services.document_service import DocumentService
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.system_settings_service import SystemSettingsService
from common.file_utils import get_project_base_directory


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
    # Runtime model configuration is read from KNOWLEDGE_RUNTIME_* environment
    # variables at call time. No per-runtime-scope model records are provisioned.
    return None


def init_env_default_models_for_scope(scope_id):
    return init_env_default_models()


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

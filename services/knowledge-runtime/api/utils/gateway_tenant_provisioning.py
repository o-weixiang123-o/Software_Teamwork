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

from api.utils.gateway_identity import normalize_gateway_principal_id


VALID_STATUS = "1"
OWNER_ROLE = "owner"
AUTO_PROVISION_TENANTS_ENV = "KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS"


def gateway_tenant_auto_provision_enabled(environ=None) -> bool:
    environ = environ if environ is not None else os.environ
    raw = str(environ.get(AUTO_PROVISION_TENANTS_ENV, "true")).strip().lower()
    if raw in {"0", "false", "no", "off"}:
        return False
    if raw in {"1", "true", "yes", "on", ""}:
        return True
    return True


def provision_gateway_tenant_if_enabled(external_id, provisioner):
    if not gateway_tenant_auto_provision_enabled():
        return None
    return provisioner(external_id)


def ensure_gateway_tenant_with_store(external_id, store, defaults, id_factory, model_initializer=None):
    runtime_id = normalize_gateway_principal_id(external_id)
    if not runtime_id:
        raise ValueError("gateway tenant id is required")

    changed = False
    with store.atomic():
        user = store.get_user(runtime_id)
        if user is None:
            user = store.create_user(runtime_id, _user_payload(runtime_id))
            changed = True
        elif getattr(user, "status", None) != VALID_STATUS:
            store.update_user(runtime_id, {"status": VALID_STATUS})
            changed = True

        tenant = store.get_tenant(runtime_id)
        if tenant is None:
            store.create_tenant(runtime_id, _tenant_payload(runtime_id, defaults))
            changed = True
        else:
            tenant_updates = _missing_tenant_defaults(tenant, defaults)
            if tenant_updates:
                store.update_tenant(runtime_id, tenant_updates)
                changed = True

        user_tenant = store.get_user_tenant(runtime_id, runtime_id)
        if user_tenant is None:
            store.create_user_tenant(
                runtime_id,
                runtime_id,
                {
                    "id": id_factory(),
                    "user_id": runtime_id,
                    "tenant_id": runtime_id,
                    "role": OWNER_ROLE,
                    "invited_by": runtime_id,
                    "status": VALID_STATUS,
                },
            )
            changed = True
        else:
            link_updates = {}
            if getattr(user_tenant, "status", None) != VALID_STATUS:
                link_updates["status"] = VALID_STATUS
            if getattr(user_tenant, "role", None) != OWNER_ROLE:
                link_updates["role"] = OWNER_ROLE
            if link_updates:
                store.update_user_tenant(user_tenant.id, link_updates)
                changed = True

    if changed and model_initializer is not None:
        model_initializer(runtime_id)

    return store.get_user(runtime_id)


def _user_payload(runtime_id):
    return {
        "id": runtime_id,
        "nickname": f"Gateway user {runtime_id}",
        "email": f"gateway-{runtime_id}@software-teamwork.local",
        "status": VALID_STATUS,
        "is_authenticated": "1",
        "is_active": "1",
        "is_anonymous": "0",
    }


def _tenant_payload(runtime_id, defaults):
    return {
        "id": runtime_id,
        "name": f"Gateway tenant {runtime_id}",
        "llm_id": _setting_value(defaults.get("chat")),
        "embd_id": _setting_value(defaults.get("embedding")),
        "asr_id": _setting_value(defaults.get("asr")),
        "img2txt_id": _setting_value(defaults.get("image2text")),
        "rerank_id": _setting_value(defaults.get("rerank")),
        "parser_ids": _setting_value(defaults.get("parsers")),
        "status": VALID_STATUS,
    }


def _missing_tenant_defaults(tenant, defaults):
    defaults = _tenant_payload(tenant.id, defaults)
    updates = {}
    for field in ("llm_id", "embd_id", "asr_id", "img2txt_id", "rerank_id", "parser_ids", "status"):
        default_value = defaults[field]
        if field == "status" and getattr(tenant, field, None) != default_value:
            updates[field] = default_value
        elif default_value and not getattr(tenant, field, None):
            updates[field] = defaults[field]
    return updates


def _setting_value(value) -> str:
    return str(value or "")

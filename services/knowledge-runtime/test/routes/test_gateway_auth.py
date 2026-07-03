from contextlib import nullcontext
from pathlib import Path
from types import SimpleNamespace

from api.utils.gateway_auth import SERVICE_TOKEN_HEADER, route_allows_gateway_auth, service_token_is_valid
from api.utils.gateway_identity import normalize_gateway_principal_id
from api.utils.gateway_tenant_provisioning import gateway_tenant_auto_provision_enabled, provision_gateway_tenant_if_enabled, ensure_gateway_tenant_with_store


def test_service_token_validation_fails_closed_when_env_unset(monkeypatch):
    monkeypatch.delenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", raising=False)

    assert not service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-token"})


def test_service_token_validation_uses_constant_runtime_token(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", "runtime-token")

    assert service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-token"})
    assert not service_token_is_valid({SERVICE_TOKEN_HEADER: "wrong-token"})
    assert not service_token_is_valid({})


def test_gateway_principal_id_normalization_preserves_legacy_short_ids():
    assert normalize_gateway_principal_id("usr_local_admin") == "usr_local_admin"


def test_gateway_principal_id_normalization_hashes_long_gateway_ids():
    gateway_id = "usr_" + "a" * 32
    normalized = normalize_gateway_principal_id(gateway_id)

    assert len(normalized) == 32
    assert normalized.startswith("gw_")
    assert normalize_gateway_principal_id(gateway_id) == normalized
    assert normalized != gateway_id


def test_runtime_route_auth_types_must_include_gateway():
    assert route_allows_gateway_auth(None)
    assert route_allows_gateway_auth(["JWT", "GATEWAY"])
    assert not route_allows_gateway_auth(["JWT", "API", "BETA"])
    assert not route_allows_gateway_auth([])


def test_legacy_document_route_auth_declaration_rejected_even_with_valid_service_token(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", "runtime-secret")
    assert service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-secret"})

    route_source = Path(__file__).parents[2] / "api" / "apps" / "restful_apis" / "document_api.py"
    assert "@login_required(auth_types=[AUTH_JWT, AUTH_API, AUTH_BETA])" in route_source.read_text(encoding="utf-8")
    assert not route_allows_gateway_auth(["JWT", "API", "BETA"])


def test_gateway_tenant_auto_provision_defaults_enabled(monkeypatch):
    monkeypatch.delenv("KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS", raising=False)

    assert gateway_tenant_auto_provision_enabled()


def test_gateway_tenant_auto_provision_disabled_skips_provisioner(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS", "false")
    called = []

    result = provision_gateway_tenant_if_enabled("tenant-1", called.append)

    assert result is None
    assert called == []


def test_gateway_tenant_auto_provision_enabled_calls_provisioner(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AUTO_PROVISION_TENANTS", "true")

    result = provision_gateway_tenant_if_enabled("tenant-1", lambda external_id: f"user:{external_id}")

    assert result == "user:tenant-1"


def test_gateway_tenant_provisioning_is_idempotent_for_clean_runtime():
    store = FakeGatewayTenantStore()
    initialized = []
    gateway_id = "usr_" + "b" * 32
    runtime_id = normalize_gateway_principal_id(gateway_id)

    defaults = {
        "chat": "chat-model@Builtin",
        "embedding": "embedding-model@Builtin",
        "rerank": "rerank-model@Builtin",
        "asr": "",
        "image2text": "",
        "parsers": "naive:General",
    }

    first = ensure_gateway_tenant_with_store(
        gateway_id,
        store,
        defaults=defaults,
        id_factory=lambda: "relation_1",
        model_initializer=initialized.append,
    )
    second = ensure_gateway_tenant_with_store(
        gateway_id,
        store,
        defaults=defaults,
        id_factory=lambda: "relation_2",
        model_initializer=initialized.append,
    )

    assert first.id == runtime_id
    assert second.id == runtime_id
    assert store.create_user_count == 1
    assert store.create_tenant_count == 1
    assert store.create_user_tenant_count == 1
    assert initialized == [runtime_id]
    assert store.tenants[runtime_id]["embd_id"] == "embedding-model@Builtin"
    assert store.tenants[runtime_id]["rerank_id"] == "rerank-model@Builtin"
    assert store.user_tenants[(runtime_id, runtime_id)]["role"] == "owner"


class FakeGatewayTenantStore:
    def __init__(self):
        self.users = {}
        self.tenants = {}
        self.user_tenants = {}
        self.create_user_count = 0
        self.create_tenant_count = 0
        self.create_user_tenant_count = 0

    def atomic(self):
        return nullcontext()

    def get_user(self, runtime_id):
        return _record(self.users.get(runtime_id))

    def create_user(self, runtime_id, payload):
        self.create_user_count += 1
        self.users.setdefault(runtime_id, dict(payload))
        return self.get_user(runtime_id)

    def update_user(self, runtime_id, payload):
        self.users[runtime_id].update(payload)

    def get_tenant(self, runtime_id):
        return _record(self.tenants.get(runtime_id))

    def create_tenant(self, runtime_id, payload):
        self.create_tenant_count += 1
        self.tenants.setdefault(runtime_id, dict(payload))
        return self.get_tenant(runtime_id)

    def update_tenant(self, runtime_id, payload):
        self.tenants[runtime_id].update(payload)

    def get_user_tenant(self, user_id, tenant_id):
        return _record(self.user_tenants.get((user_id, tenant_id)))

    def create_user_tenant(self, user_id, tenant_id, payload):
        self.create_user_tenant_count += 1
        self.user_tenants.setdefault((user_id, tenant_id), dict(payload))
        return self.get_user_tenant(user_id, tenant_id)

    def update_user_tenant(self, relation_id, payload):
        for record in self.user_tenants.values():
            if record["id"] == relation_id:
                record.update(payload)
                return


def _record(value):
    if value is None:
        return None
    return SimpleNamespace(**value)

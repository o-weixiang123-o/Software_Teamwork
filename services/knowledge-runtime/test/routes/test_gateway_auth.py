from pathlib import Path

from api.utils.gateway_auth import SERVICE_TOKEN_HEADER, route_allows_gateway_auth, service_token_is_valid
from api.utils.runtime_scope import runtime_index_id, runtime_scope_id, runtime_subject


def test_service_token_validation_fails_closed_when_env_unset(monkeypatch):
    monkeypatch.delenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", raising=False)

    assert not service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-token"})


def test_service_token_validation_uses_constant_runtime_token(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", "runtime-token")

    assert service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-token"})
    assert not service_token_is_valid({SERVICE_TOKEN_HEADER: "wrong-token"})
    assert not service_token_is_valid({})


def test_runtime_scope_defaults_to_single_global_namespace(monkeypatch):
    monkeypatch.delenv("KNOWLEDGE_RUNTIME_SCOPE_ID", raising=False)

    assert runtime_scope_id() == "knowledge_runtime"


def test_runtime_scope_hashes_oversized_ids(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SCOPE_ID", "runtime-" + "a" * 64)

    normalized = runtime_scope_id()

    assert len(normalized) == 32
    assert normalized.startswith("rt_")


def test_runtime_subject_is_not_loaded_from_user_tables(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SCOPE_ID", "runtime_scope")

    subject = runtime_subject()

    assert subject.id == "runtime_scope"
    assert subject.is_active == "1"


def test_runtime_index_defaults_to_single_global_index(monkeypatch):
    monkeypatch.delenv("KNOWLEDGE_RUNTIME_INDEX_ID", raising=False)

    assert runtime_index_id() == "runtime"


def test_runtime_route_auth_types_must_include_gateway():
    assert route_allows_gateway_auth(None)
    assert route_allows_gateway_auth(["JWT", "GATEWAY"])
    assert not route_allows_gateway_auth(["JWT", "API", "BETA"])
    assert not route_allows_gateway_auth([])


def test_document_routes_do_not_keep_legacy_auth_only_declarations(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_SERVICE_TOKEN", "runtime-secret")
    assert service_token_is_valid({SERVICE_TOKEN_HEADER: "runtime-secret"})

    route_source = Path(__file__).parents[2] / "api" / "apps" / "restful_apis" / "document_api.py"
    assert "@login_required(auth_types=[" not in route_source.read_text(encoding="utf-8")
    assert not route_allows_gateway_auth(["JWT", "API", "BETA"])

from pathlib import Path

from deploy import check_runtime_dependencies as deps


BASE_CONFIG = """
ragflow:
  host: 127.0.0.1
es:
  hosts: http://localhost:9200
user_default_llm:
  default_models:
    embedding_model:
      name: bge-m3
      factory: Builtin
      api_key: local
      base_url: http://127.0.0.1:6380
"""


def write_config(tmp_path: Path, text: str = BASE_CONFIG) -> Path:
    path = tmp_path / "service_conf.yaml"
    path.write_text(text, encoding="utf-8")
    return path


def test_dependency_check_reports_missing_es_and_builtin_embedding(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={},
        http_checker=lambda _url: (_ for _ in ()).throw(OSError("connection refused")),
    )

    assert any("Elasticsearch endpoint" in issue for issue in issues)
    assert any("Default Builtin embedding is not usable" in issue for issue in issues)


def test_dependency_check_accepts_configured_external_embedding(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "SILICONFLOW",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
            "KNOWLEDGE_RUNTIME_MODEL_API_KEY": "sk-test",
        },
        http_checker=lambda _url: None,
    )

    assert issues == []


def test_dependency_check_requires_key_for_external_embedding(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "SILICONFLOW",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
        },
        http_checker=lambda _url: None,
    )

    assert any("KNOWLEDGE_RUNTIME_MODEL_API_KEY is empty" in issue for issue in issues)


def test_worker_dependency_check_requires_nltk_data(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA": "1",
            "NLTK_DATA": str(tmp_path / "missing-nltk"),
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "SILICONFLOW",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
            "KNOWLEDGE_RUNTIME_MODEL_API_KEY": "sk-test",
        },
        http_checker=lambda _url: None,
    )

    assert any("Worker startup requires NLTK data" in issue for issue in issues)


def test_worker_dependency_check_accepts_provisioned_nltk_data(tmp_path):
    path = write_config(tmp_path)
    nltk_data = tmp_path / "nltk_data"
    (nltk_data / "tokenizers" / "punkt_tab").mkdir(parents=True)
    (nltk_data / "corpora" / "wordnet").mkdir(parents=True)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_REQUIRE_NLTK_DATA": "1",
            "NLTK_DATA": str(nltk_data),
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "SILICONFLOW",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
            "KNOWLEDGE_RUNTIME_MODEL_API_KEY": "sk-test",
        },
        http_checker=lambda _url: None,
    )

    assert issues == []


def test_dependency_check_accepts_ai_gateway_embedding_with_service_token(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "AI_GATEWAY",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
            "KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN": "local-service-token",
        },
        http_checker=lambda _url: None,
    )

    assert issues == []


def test_dependency_check_requires_ai_gateway_service_token(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "AI_GATEWAY",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
        },
        http_checker=lambda _url: None,
    )

    assert any("KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY=AI_GATEWAY requires" in issue for issue in issues)


def test_dependency_check_validates_configured_rerank_credentials(tmp_path):
    path = write_config(tmp_path)

    issues = deps.validate(
        path,
        environ={
            "KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY": "AI_GATEWAY",
            "KNOWLEDGE_RUNTIME_EMBEDDING_MODEL": "BAAI/bge-m3",
            "KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN": "local-service-token",
            "KNOWLEDGE_RUNTIME_RERANK_FACTORY": "SILICONFLOW",
            "KNOWLEDGE_RUNTIME_RERANK_MODEL": "BAAI/bge-reranker-v2-m3",
        },
        http_checker=lambda _url: None,
    )

    assert any("KNOWLEDGE_RUNTIME_RERANK_FACTORY/MODEL are set" in issue for issue in issues)

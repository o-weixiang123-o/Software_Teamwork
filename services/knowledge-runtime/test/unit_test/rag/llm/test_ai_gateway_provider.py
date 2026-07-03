from unittest.mock import MagicMock, patch

import numpy as np
import pytest

from common.exceptions import ModelException
from rag.llm.ai_gateway_utils import normalize_ai_gateway_endpoint
from rag.llm.embedding_model import AIGatewayEmbed
from rag.llm.rerank_model import AIGatewayRerank

pytestmark = pytest.mark.p1


AI_GATEWAY_ENV_KEYS = [
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN",
    "AI_GATEWAY_SERVICE_TOKEN",
    "INTERNAL_SERVICE_TOKEN",
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_CALLER_SERVICE",
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_EMBEDDING_PROFILE_ID",
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_RERANK_PROFILE_ID",
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_REQUEST_ID",
    "KNOWLEDGE_RUNTIME_AI_GATEWAY_TIMEOUT_SECONDS",
    "X_REQUEST_ID",
    "REQUEST_ID",
]


@pytest.fixture(autouse=True)
def clean_ai_gateway_env(monkeypatch):
    for key in AI_GATEWAY_ENV_KEYS:
        monkeypatch.delenv(key, raising=False)


def _response(payload, status=200, text=""):
    response = MagicMock()
    response.status_code = status
    response.text = text
    response.json.return_value = payload
    return response


def test_normalize_ai_gateway_endpoint_accepts_base_or_endpoint():
    assert normalize_ai_gateway_endpoint("http://gateway/internal/v1", "embeddings") == "http://gateway/internal/v1/embeddings"
    assert normalize_ai_gateway_endpoint("http://gateway/internal/v1/embeddings", "embeddings") == "http://gateway/internal/v1/embeddings"
    assert normalize_ai_gateway_endpoint("gateway:8086/internal/v1", "embeddings") == "http://gateway:8086/internal/v1/embeddings"


def test_ai_gateway_embedding_sends_internal_headers_and_preserves_batch_order(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN", "svc-token")
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_CALLER_SERVICE", "knowledge")
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_EMBEDDING_PROFILE_ID", "default-embedding")
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_REQUEST_ID", "req-embedding-test")

    response = _response(
        {
            "object": "list",
            "model": "BAAI/bge-m3",
            "data": [
                {"object": "embedding", "index": 1, "embedding": [0.2, 0.3]},
                {"object": "embedding", "index": 0, "embedding": [0.1, 0.0]},
            ],
            "usage": {"total_tokens": 7},
        }
    )
    provider = AIGatewayEmbed("", "BAAI/bge-m3", base_url="http://gateway/internal/v1")

    with patch("rag.llm.embedding_model.requests.post", return_value=response) as post:
        vectors, tokens = provider.encode(["alpha", "beta"])

    assert np.allclose(vectors, [[0.1, 0.0], [0.2, 0.3]])
    assert tokens == 7
    assert post.call_args.args[0] == "http://gateway/internal/v1/embeddings"
    payload = post.call_args.kwargs["json"]
    assert payload == {
        "profile_id": "default-embedding",
        "model": "BAAI/bge-m3",
        "input": ["alpha", "beta"],
        "encoding_format": "float",
    }
    headers = post.call_args.kwargs["headers"]
    assert headers["X-Service-Token"] == "svc-token"
    assert headers["X-Caller-Service"] == "knowledge"
    assert headers["X-Request-Id"] == "req-embedding-test"
    assert post.call_args.kwargs["timeout"] == 30.0


def test_ai_gateway_embedding_generates_request_id_when_missing(monkeypatch):
    monkeypatch.setenv("AI_GATEWAY_SERVICE_TOKEN", "svc-token")
    provider = AIGatewayEmbed("", "BAAI/bge-m3", base_url="http://gateway/internal/v1")
    response = _response(
        {
            "object": "list",
            "model": "BAAI/bge-m3",
            "data": [{"object": "embedding", "index": 0, "embedding": [0.1]}],
            "usage": {"total_tokens": 1},
        }
    )

    with patch("rag.llm.embedding_model.requests.post", return_value=response) as post:
        provider.encode(["alpha"])

    request_id = post.call_args.kwargs["headers"]["X-Request-Id"]
    assert request_id.startswith("stw-kgw-")


def test_ai_gateway_embedding_requires_service_token():
    with pytest.raises(ValueError, match="AI Gateway provider requires"):
        AIGatewayEmbed("", "BAAI/bge-m3", base_url="http://gateway/internal/v1")


def test_ai_gateway_embedding_raises_model_exception_on_non_2xx(monkeypatch):
    monkeypatch.setenv("INTERNAL_SERVICE_TOKEN", "svc-token")
    provider = AIGatewayEmbed("", "BAAI/bge-m3", base_url="http://gateway/internal/v1")
    response = _response({"error": {"message": "profile mismatch"}}, status=400, text='{"error":"profile mismatch"}')

    with patch("rag.llm.embedding_model.requests.post", return_value=response):
        with pytest.raises(ModelException) as exc:
            provider.encode(["alpha"])

    assert "profile mismatch" in str(exc.value)


def test_ai_gateway_rerank_sends_documents_and_maps_scores(monkeypatch):
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_SERVICE_TOKEN", "svc-token")
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_RERANK_PROFILE_ID", "default-rerank")
    monkeypatch.setenv("KNOWLEDGE_RUNTIME_AI_GATEWAY_REQUEST_ID", "req-rerank-test")
    response = _response(
        {
            "object": "list",
            "model": "BAAI/bge-reranker-v2-m3",
            "data": [
                {"index": 1, "document_id": "1", "score": 0.91},
                {"index": 0, "document_id": "0", "score": 0.12},
            ],
            "usage": {"total_tokens": 11},
        }
    )
    provider = AIGatewayRerank("", "BAAI/bge-reranker-v2-m3", base_url="http://gateway/internal/v1")

    with patch("rag.llm.rerank_model.requests.post", return_value=response) as post:
        rank, tokens = provider.similarity("query", ["first doc", "second doc"])

    assert np.allclose(rank, [0.12, 0.91])
    assert tokens == 11
    assert post.call_args.args[0] == "http://gateway/internal/v1/rerankings"
    payload = post.call_args.kwargs["json"]
    assert payload == {
        "profile_id": "default-rerank",
        "model": "BAAI/bge-reranker-v2-m3",
        "query": "query",
        "documents": [{"id": "0", "text": "first doc"}, {"id": "1", "text": "second doc"}],
        "top_n": 2,
    }
    headers = post.call_args.kwargs["headers"]
    assert headers["X-Service-Token"] == "svc-token"
    assert headers["X-Caller-Service"] == "knowledge"
    assert headers["X-Request-Id"] == "req-rerank-test"


def test_ai_gateway_rerank_generates_request_id_when_missing(monkeypatch):
    monkeypatch.setenv("INTERNAL_SERVICE_TOKEN", "svc-token")
    response = _response(
        {
            "object": "list",
            "model": "BAAI/bge-reranker-v2-m3",
            "data": [{"index": 0, "document_id": "0", "score": 0.9}],
            "usage": {"total_tokens": 1},
        }
    )
    provider = AIGatewayRerank("", "BAAI/bge-reranker-v2-m3", base_url="http://gateway/internal/v1")

    with patch("rag.llm.rerank_model.requests.post", return_value=response) as post:
        provider.similarity("query", ["doc"])

    assert post.call_args.kwargs["headers"]["X-Request-Id"].startswith("stw-kgw-")


def test_ai_gateway_rerank_raises_model_exception_on_bad_index(monkeypatch):
    monkeypatch.setenv("INTERNAL_SERVICE_TOKEN", "svc-token")
    response = _response(
        {
            "object": "list",
            "model": "BAAI/bge-reranker-v2-m3",
            "data": [{"index": 3, "document_id": "3", "score": 0.9}],
        }
    )
    provider = AIGatewayRerank("", "BAAI/bge-reranker-v2-m3", base_url="http://gateway/internal/v1")

    with patch("rag.llm.rerank_model.requests.post", return_value=response):
        with pytest.raises(ModelException, match="unexpected reranking response index"):
            provider.similarity("query", ["doc"])


def test_ai_gateway_rerank_raises_model_exception_on_non_2xx(monkeypatch):
    monkeypatch.setenv("INTERNAL_SERVICE_TOKEN", "svc-token")
    provider = AIGatewayRerank("", "BAAI/bge-reranker-v2-m3", base_url="http://gateway/internal/v1")
    response = _response({"error": {"message": "gateway down"}}, status=503, text='{"error":"gateway down"}')

    with patch("rag.llm.rerank_model.requests.post", return_value=response):
        with pytest.raises(ModelException) as exc:
            provider.similarity("query", ["doc"])

    assert "gateway down" in str(exc.value)

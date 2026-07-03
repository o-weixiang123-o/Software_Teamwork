import importlib.util
import sys
from pathlib import Path
from types import SimpleNamespace
from types import ModuleType

import numpy as np
import pytest

from common.metadata_utils import (
    MetadataFilterFallbackTooLarge,
    apply_meta_data_filter,
    metadata_filter_in_memory_fallback_limit,
)
from rag.svr.task_executor_refactor.constants import DATASET_SCOPE_TASK_DOC_ID, is_dataset_scope_task_doc_id
from rag.svr.task_executor_refactor.embedding_utils import EmbeddingUtils


class _FakeEmbeddingModel:
    def __init__(self):
        self.inputs = None

    def encode(self, texts):
        self.inputs = list(texts)
        return np.ones((len(texts), 2)), len(texts)

    def encode_queries(self, query):
        self.inputs = query
        return np.ones(2), 1


def _load_llm_bundle_class(monkeypatch):
    class _StubRuntimeLLMBundle:
        def __init__(self, *args, **kwargs):
            pass

        def close(self):
            pass

    api_mod = ModuleType("api")
    db_mod = ModuleType("api.db")
    services_mod = ModuleType("api.db.services")
    monkeypatch.setitem(sys.modules, "api", api_mod)
    monkeypatch.setitem(sys.modules, "api.db", db_mod)
    monkeypatch.setitem(sys.modules, "api.db.services", services_mod)
    monkeypatch.setitem(sys.modules, "api.db.db_models", ModuleType("api.db.db_models"))
    sys.modules["api.db.db_models"].LLM = object()
    monkeypatch.setitem(sys.modules, "api.db.services.common_service", ModuleType("api.db.services.common_service"))
    sys.modules["api.db.services.common_service"].CommonService = object
    monkeypatch.setitem(sys.modules, "api.db.services.runtime_llm_service", ModuleType("api.db.services.runtime_llm_service"))
    sys.modules["api.db.services.runtime_llm_service"].RuntimeLLMBundle = _StubRuntimeLLMBundle
    monkeypatch.setitem(sys.modules, "common.token_utils", ModuleType("common.token_utils"))
    sys.modules["common.token_utils"].num_tokens_from_string = lambda text: len(str(text))

    source = Path(__file__).parents[2] / "api" / "db" / "services" / "llm_service.py"
    spec = importlib.util.spec_from_file_location("_llm_service_under_test", source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.LLMBundle


def _bundle(monkeypatch):
    LLMBundle = _load_llm_bundle_class(monkeypatch)
    bundle = object.__new__(LLMBundle)
    bundle.model_config = {"llm_name": "fake-embedding", "llm_factory": "Builtin"}
    bundle.max_length = 100
    bundle.mdl = _FakeEmbeddingModel()
    return bundle


def test_llm_bundle_rejects_empty_embedding_inputs_without_placeholder(monkeypatch):
    bundle = _bundle(monkeypatch)

    with pytest.raises(ValueError, match="Embedding input at index 1 is empty"):
        bundle.encode(["usable", "   "])

    assert bundle.mdl.inputs is None


def test_llm_bundle_rejects_empty_query_without_placeholder(monkeypatch):
    bundle = _bundle(monkeypatch)

    with pytest.raises(ValueError, match="Embedding query is empty"):
        bundle.encode_queries("")

    assert bundle.mdl.inputs is None


def test_embedding_utils_skips_whitespace_chunks_without_none_placeholder():
    docs = [
        {"docnm_kwd": "Title1", "content_with_weight": "   \n\n  "},
        {"docnm_kwd": "Title2", "content_with_weight": "usable"},
    ]

    titles, contents = EmbeddingUtils.prepare_texts_for_embedding(docs)

    assert titles == ["Title2"]
    assert contents == ["usable"]
    assert "None" not in contents


def test_dataset_scope_task_doc_id_is_explicit_and_guarded():
    assert DATASET_SCOPE_TASK_DOC_ID == "graph_raptor_x"
    assert is_dataset_scope_task_doc_id(DATASET_SCOPE_TASK_DOC_ID)
    assert not is_dataset_scope_task_doc_id("doc-real")


def test_metadata_filter_in_memory_fallback_limit_defaults_and_parses():
    assert metadata_filter_in_memory_fallback_limit({}) == 10000
    assert metadata_filter_in_memory_fallback_limit({"METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT": "7"}) == 7
    assert metadata_filter_in_memory_fallback_limit({"METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT": "bad"}) == 10000


@pytest.mark.asyncio
async def test_metadata_filter_rejects_large_in_memory_fallback(monkeypatch):
    monkeypatch.setenv("METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT", "1")
    prompts_mod = ModuleType("rag.prompts")
    generator_mod = ModuleType("rag.prompts.generator")

    async def _unused_gen_meta_filter(*args, **kwargs):
        raise AssertionError("manual metadata filters must not call gen_meta_filter")

    generator_mod.gen_meta_filter = _unused_gen_meta_filter
    monkeypatch.setitem(sys.modules, "rag.prompts", prompts_mod)
    monkeypatch.setitem(sys.modules, "rag.prompts.generator", generator_mod)

    with pytest.raises(MetadataFilterFallbackTooLarge, match="cap is 1"):
        await apply_meta_data_filter(
            {"method": "manual", "manual": [{"key": "source", "op": "=", "value": "manual"}]},
            metas={"source": {"manual": ["doc-1", "doc-2"]}},
            question="manual",
            chat_mdl=SimpleNamespace(),
        )

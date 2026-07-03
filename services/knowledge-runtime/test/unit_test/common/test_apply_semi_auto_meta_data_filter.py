import pytest
from common.metadata_utils import MetadataFilterFallbackTooLarge, apply_meta_data_filter, metadata_filter_in_memory_fallback_limit
from unittest.mock import MagicMock, AsyncMock, patch

@pytest.mark.asyncio
async def test_apply_meta_data_filter_semi_auto_key():
    meta_data_filter = {
        "method": "semi_auto",
        "semi_auto": ["key1", "key2"]
    }
    metas = {
        "key1": {"val1": ["doc1"]},
        "key2": {"val2": ["doc2"]}
    }
    question = "find val1"
    
    chat_mdl = MagicMock()
    
    with patch("rag.prompts.generator.gen_meta_filter", new_callable=AsyncMock) as mock_gen:
        mock_gen.return_value = {"conditions": [{"key": "key1", "op": "=", "value": "val1"}], "logic": "and"}
        
        doc_ids = await apply_meta_data_filter(meta_data_filter, metas, question, chat_mdl)
        assert doc_ids == ["doc1"]
        
        # Check that constraints is an empty dict by default for legacy
        mock_gen.assert_called_once()
        args, kwargs = mock_gen.call_args
        assert kwargs["constraints"] == {}

@pytest.mark.asyncio
async def test_apply_meta_data_filter_semi_auto_key_and_operator():
    meta_data_filter = {
        "method": "semi_auto",
        "semi_auto": [{"key": "key1", "op": ">"}, "key2"]
    }
    metas = {
        "key1": {"10": ["doc1"]},
        "key2": {"val2": ["doc2"]}
    }
    question = "find key1 > 5"
    
    chat_mdl = MagicMock()
    
    with patch("rag.prompts.generator.gen_meta_filter", new_callable=AsyncMock) as mock_gen:
        mock_gen.return_value = {"conditions": [{"key": "key1", "op": ">", "value": "5"}], "logic": "and"}
        
        doc_ids = await apply_meta_data_filter(meta_data_filter, metas, question, chat_mdl)
        assert doc_ids == ["doc1"]
        
        # Check that constraints are correctly passed
        mock_gen.assert_called_once()
        args, kwargs = mock_gen.call_args
        assert kwargs["constraints"] == {"key1": ">"}


def test_metadata_filter_in_memory_fallback_limit_defaults_and_parses():
    assert metadata_filter_in_memory_fallback_limit({}) == 10000
    assert metadata_filter_in_memory_fallback_limit({"METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT": "7"}) == 7
    assert metadata_filter_in_memory_fallback_limit({"METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT": "bad"}) == 10000


@pytest.mark.asyncio
async def test_apply_meta_data_filter_rejects_large_in_memory_fallback(monkeypatch):
    monkeypatch.setenv("METADATA_FILTER_IN_MEMORY_FALLBACK_LIMIT", "1")

    with pytest.raises(MetadataFilterFallbackTooLarge, match="cap is 1"):
        await apply_meta_data_filter(
            {"method": "manual", "manual": [{"key": "source", "op": "=", "value": "manual"}]},
            metas={"source": {"manual": ["doc-1", "doc-2"]}},
            question="manual",
            chat_mdl=MagicMock(),
        )


@pytest.mark.asyncio
async def test_apply_meta_data_filter_does_not_fallback_when_pushdown_reports_too_large(monkeypatch):
    from api.db.services.doc_metadata_service import DocMetadataService

    called = []

    def _raise_too_large(cls, kb_ids, filters, logic):
        raise MetadataFilterFallbackTooLarge("metadata filter matched 20000 documents; cap is 10000")

    monkeypatch.setattr(DocMetadataService, "filter_doc_ids_by_meta_pushdown", classmethod(_raise_too_large))

    with pytest.raises(MetadataFilterFallbackTooLarge):
        await apply_meta_data_filter(
            {"method": "manual", "manual": [{"key": "source", "op": "=", "value": "manual"}]},
            metas_loader=lambda: called.append("loaded") or {"source": {"manual": ["doc-1"]}},
            question="manual",
            chat_mdl=MagicMock(),
            kb_ids=["kb-1"],
        )

    assert called == []

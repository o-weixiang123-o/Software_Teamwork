import os
from unittest import mock

import pytest

from api.db.services import chunk_feedback_service as cfs


@pytest.mark.parametrize(
    "chunk,expected",
    [
        ({"similarity": 0.8, "vector_similarity": 0.3}, 0.8),
        ({"term_similarity": 0.5}, 0.5),
        ({}, 0.0),
        ({"similarity": "bad"}, 0.0),
    ],
)
def test_retrieval_signal(chunk, expected):
    assert cfs._retrieval_signal(chunk) == expected


def test_split_integer_budget_largest_remainder():
    assert cfs._split_integer_budget([0.7, 0.3], 10) == [7, 3]
    assert sum(cfs._split_integer_budget([1.0, 1.0, 1.0], 5)) == 5


def test_allocate_deltas_uniform():
    rows = [("c1", "kb1"), ("c2", "kb1")]
    deltas = cfs._allocate_deltas_uniform(rows, 1)
    assert deltas == [("c1", "kb1", 1), ("c2", "kb1", 1)]


def test_allocate_deltas_relevance():
    rows = [
        ("c1", "kb1", {"similarity": 0.9}),
        ("c2", "kb1", {"similarity": 0.1}),
    ]
    deltas = cfs._allocate_deltas_relevance(rows, 1)
    assert sum(abs(d) for _, _, d in deltas) == 1
    assert deltas[0][2] == 1


def test_apply_feedback_disabled(monkeypatch):
    monkeypatch.setattr(cfs, "CHUNK_FEEDBACK_ENABLED", False)
    result = cfs.ChunkFeedbackService.apply_feedback(
        "scope-1",
        {"chunks": [{"id": "c1", "dataset_id": "kb1"}]},
        True,
    )
    assert result["disabled"] is True
    assert result["success_count"] == 0


def test_apply_feedback_updates_chunks(monkeypatch):
    monkeypatch.setattr(cfs, "CHUNK_FEEDBACK_ENABLED", True)
    monkeypatch.setattr(cfs, "CHUNK_FEEDBACK_WEIGHTING", "uniform")

    with mock.patch.object(
        cfs.ChunkFeedbackService,
        "update_chunk_weight",
        return_value=True,
    ) as update_mock:
        reference = {
            "chunks": [
                {"id": "c1", "dataset_id": "kb1"},
                {"id": "c2", "dataset_id": "kb1"},
            ]
        }
        result = cfs.ChunkFeedbackService.apply_feedback("scope-1", reference, True)

    assert result["success_count"] == 2
    assert result["fail_count"] == 0
    assert update_mock.call_count == 2

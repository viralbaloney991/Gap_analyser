import numpy as np
import pytest
from classifier.embedder import build_index, search_index, IndexEntry

SAMPLE_TECHNIQUES = [
    {
        "id": "T1110",
        "name": "Brute Force",
        "description": "Adversaries may use brute force techniques to gain access. Password spraying targets many accounts with common passwords.",
        "detection": "Monitor authentication logs for repeated failures.",
    },
    {
        "id": "T1078",
        "name": "Valid Accounts",
        "description": "Adversaries may obtain and abuse credentials of existing accounts. Cloud accounts may be targeted via AssumeRole.",
        "detection": "Monitor for unusual account usage.",
    },
    {
        "id": "T1562",
        "name": "Impair Defenses",
        "description": "Adversaries may maliciously modify components of a victim's environment. Disabling security tools or logging.",
        "detection": "Monitor for changes to security tool configurations.",
    },
]


def test_build_index_returns_entries():
    entries = build_index(SAMPLE_TECHNIQUES)
    assert len(entries) == 3
    assert all(isinstance(e, IndexEntry) for e in entries)


def test_build_index_preserves_ids():
    entries = build_index(SAMPLE_TECHNIQUES)
    ids = [e.technique_id for e in entries]
    assert "T1110" in ids
    assert "T1078" in ids


def test_search_returns_top_k():
    entries = build_index(SAMPLE_TECHNIQUES)
    results = search_index("failed login attempts brute force password", entries, top_k=2)
    assert len(results) == 2


def test_search_brute_force_ranks_first():
    entries = build_index(SAMPLE_TECHNIQUES)
    results = search_index("repeated failed login attempts brute force", entries, top_k=3)
    assert results[0]["technique_id"] == "T1110"


def test_search_scores_between_0_and_1():
    entries = build_index(SAMPLE_TECHNIQUES)
    results = search_index("assume role cloud account", entries, top_k=3)
    for r in results:
        assert 0.0 <= r["score"] <= 1.0

import pytest
from unittest.mock import patch, MagicMock
from fastapi.testclient import TestClient


MOCK_TECHNIQUES = [
    {"id": "T1110", "name": "Brute Force", "description": "password spraying brute force login failures", "detection": "Monitor auth logs"},
    {"id": "T1078", "name": "Valid Accounts", "description": "assume role cloud credentials abuse", "detection": "Monitor account usage"},
]


@pytest.fixture
def client():
    import classifier.main as m
    from classifier.embedder import build_index

    # Directly set module-level globals so the running app sees them
    original_techniques = m.techniques
    original_index = m.index_entries

    m.techniques = list(MOCK_TECHNIQUES)
    m.index_entries = build_index(MOCK_TECHNIQUES)

    from classifier.main import app, lifespan_setup
    test_client = TestClient(app)

    yield test_client

    # Restore originals after test
    m.techniques = original_techniques
    m.index_entries = original_index


def test_health(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    data = resp.json()
    assert data["status"] == "ok"
    assert data["techniques"] == 2


def test_classify_returns_candidates(client):
    resp = client.post("/classify", json={
        "name": "Okta - Brute Force Login",
        "query": "outcome.result:FAILURE AND eventType:user.session.start",
        "app": "okta",
        "subsystem": "okta-audit",
    })
    assert resp.status_code == 200
    results = resp.json()
    assert isinstance(results, list)
    assert len(results) <= 3
    assert all("technique_id" in r and "score" in r for r in results)


def test_classify_brute_force_detected(client):
    resp = client.post("/classify", json={
        "name": "Repeated Login Failures",
        "query": "failed_logins:>5",
        "app": "",
        "subsystem": "",
    })
    results = resp.json()
    top = results[0]["technique_id"]
    assert top == "T1110"


def test_classify_empty_query(client):
    resp = client.post("/classify", json={
        "name": "Some Alert",
        "query": "",
        "app": "",
        "subsystem": "",
    })
    assert resp.status_code == 200

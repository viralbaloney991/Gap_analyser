# Local MITRE Classifier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace cloud-LLM MITRE classification with a local semantic similarity sidecar + Llama validation pipeline, and switch alert suggestion generation to Mistral.

**Architecture:** A Python FastAPI sidecar loads MITRE ATT&CK STIX once at startup, builds a sentence-transformer embedding index persisted to disk, and serves `/classify` returning top-3 technique candidates. Go calls it, then validates via Llama 70B (confirm/reject/promote to sub-techniques), caches in Redis. Mistral replaces nemotron for the suggestions endpoint.

**Tech Stack:** Python 3.11+, FastAPI, sentence-transformers (all-MiniLM-L6-v2), numpy, Go 1.21+, Redis, NVIDIA NIM (Llama 70B validator + Mistral suggester)

---

## File Map

**Create:**
- `classifier/mitre.py` — fetch MITRE STIX, parse techniques
- `classifier/embedder.py` — build embedding index, cosine similarity search
- `classifier/main.py` — FastAPI service wiring mitre + embedder
- `classifier/requirements.txt` — Python dependencies
- `classifier/tests/test_mitre.py` — tests for STIX parsing
- `classifier/tests/test_embedder.py` — tests for search
- `internal/classifier/client.go` — Go HTTP client for sidecar
- `internal/classifier/client_test.go` — Go tests for client
- `internal/llm/validator.go` — Llama validation of candidates
- `internal/llm/validator_test.go` — tests for validator

**Modify:**
- `internal/config/config.go` — add ClassifierConfig, ValidatorModel, SuggestionModel to LLMConfig
- `clients.yaml` — add classifier endpoint, validator_model, suggestion_model
- `internal/llm/mitre_mapper.go` — add BatchClassifyAndValidate replacing old BatchMapMITRE
- `internal/api/handlers.go` — wire new pipeline; use suggestion_model for suggestions endpoint

---

## Task 1: Python sidecar — MITRE data loading

**Files:**
- Create: `classifier/mitre.py`
- Create: `classifier/tests/test_mitre.py`
- Create: `classifier/requirements.txt`

- [ ] **Step 1: Create requirements.txt**

```
fastapi==0.111.0
uvicorn==0.29.0
sentence-transformers==3.0.0
numpy==1.26.4
requests==2.31.0
pytest==8.2.0
httpx==0.27.0
```

- [ ] **Step 2: Write the failing test**

Create `classifier/tests/test_mitre.py`:

```python
import json
import pytest
from unittest.mock import patch
from classifier.mitre import parse_techniques, fetch_stix


SAMPLE_STIX = {
    "objects": [
        {
            "type": "attack-pattern",
            "id": "attack-pattern--0a3ead4e-6d47-4ccb-854c-a6a4f9d96b22",
            "name": "OS Credential Dumping",
            "description": "Adversaries may attempt to dump credentials to obtain account login information.",
            "x_mitre_deprecated": False,
            "x_mitre_detection": "Monitor for unexpected processes interacting with lsass.exe",
            "external_references": [
                {"source_name": "mitre-attack", "external_id": "T1003"}
            ]
        },
        {
            "type": "attack-pattern",
            "id": "attack-pattern--deprecated",
            "name": "Old Technique",
            "description": "Deprecated.",
            "x_mitre_deprecated": True,
            "external_references": [
                {"source_name": "mitre-attack", "external_id": "T9999"}
            ]
        },
        {
            "type": "course-of-action",
            "name": "Not a technique",
        }
    ]
}


def test_parse_techniques_filters_deprecated():
    techniques = parse_techniques(SAMPLE_STIX)
    ids = [t["id"] for t in techniques]
    assert "T1003" in ids
    assert "T9999" not in ids


def test_parse_techniques_excludes_non_attack_pattern():
    techniques = parse_techniques(SAMPLE_STIX)
    assert len(techniques) == 1


def test_parse_techniques_fields():
    techniques = parse_techniques(SAMPLE_STIX)
    t = techniques[0]
    assert t["id"] == "T1003"
    assert t["name"] == "OS Credential Dumping"
    assert "dump credentials" in t["description"]
    assert "lsass" in t["detection"]
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/classifier
pip install -r requirements.txt
python -m pytest tests/test_mitre.py -v
```

Expected: `ModuleNotFoundError: No module named 'classifier'`

- [ ] **Step 4: Implement `classifier/mitre.py`**

```python
import json
import os
import requests

STIX_URL = "https://raw.githubusercontent.com/mitre/cti/master/enterprise-attack/enterprise-attack.json"


def fetch_stix(url: str = STIX_URL) -> dict:
    resp = requests.get(url, timeout=30)
    resp.raise_for_status()
    return resp.json()


def parse_techniques(stix: dict) -> list[dict]:
    """Parse MITRE ATT&CK techniques from STIX bundle. Excludes deprecated entries."""
    techniques = []
    for obj in stix.get("objects", []):
        if obj.get("type") != "attack-pattern":
            continue
        if obj.get("x_mitre_deprecated", False):
            continue

        technique_id = None
        for ref in obj.get("external_references", []):
            if ref.get("source_name") == "mitre-attack":
                technique_id = ref.get("external_id")
                break
        if not technique_id:
            continue

        techniques.append({
            "id": technique_id,
            "name": obj.get("name", ""),
            "description": obj.get("description", ""),
            "detection": obj.get("x_mitre_detection", ""),
        })

    return techniques
```

- [ ] **Step 5: Add `classifier/__init__.py`**

```python
# classifier package
```

- [ ] **Step 6: Run test to verify it passes**

```bash
python -m pytest tests/test_mitre.py -v
```

Expected: `3 passed`

- [ ] **Step 7: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude
git add classifier/
git commit -m "feat: python classifier sidecar - MITRE STIX parsing"
```

---

## Task 2: Python sidecar — embedding index

**Files:**
- Create: `classifier/embedder.py`
- Create: `classifier/tests/test_embedder.py`

- [ ] **Step 1: Write the failing test**

Create `classifier/tests/test_embedder.py`:

```python
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
python -m pytest tests/test_embedder.py -v
```

Expected: `ModuleNotFoundError: No module named 'classifier.embedder'`

- [ ] **Step 3: Implement `classifier/embedder.py`**

```python
import pickle
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

MODEL_NAME = "all-MiniLM-L6-v2"
CACHE_PATH = Path(__file__).parent / "embeddings.pkl"

_model: SentenceTransformer | None = None


def _get_model() -> SentenceTransformer:
    global _model
    if _model is None:
        _model = SentenceTransformer(MODEL_NAME)
    return _model


@dataclass
class IndexEntry:
    technique_id: str
    name: str
    embedding: np.ndarray


def _technique_text(t: dict) -> str:
    """Build the text representation used for embedding."""
    parts = [t["name"], t["description"]]
    if t.get("detection"):
        parts.append(t["detection"])
    return " ".join(parts)[:1000]  # cap at 1000 chars


def build_index(techniques: list[dict]) -> list[IndexEntry]:
    """Embed all techniques and return index entries."""
    model = _get_model()
    texts = [_technique_text(t) for t in techniques]
    embeddings = model.encode(texts, convert_to_numpy=True, show_progress_bar=False)
    # L2-normalise for cosine similarity via dot product
    norms = np.linalg.norm(embeddings, axis=1, keepdims=True)
    embeddings = embeddings / np.maximum(norms, 1e-8)
    return [
        IndexEntry(technique_id=t["id"], name=t["name"], embedding=embeddings[i])
        for i, t in enumerate(techniques)
    ]


def search_index(query: str, entries: list[IndexEntry], top_k: int = 3) -> list[dict]:
    """Return top-k techniques most similar to query."""
    model = _get_model()
    query_vec = model.encode([query], convert_to_numpy=True)[0]
    query_vec = query_vec / max(np.linalg.norm(query_vec), 1e-8)

    matrix = np.stack([e.embedding for e in entries])
    scores = matrix @ query_vec

    top_idx = np.argsort(scores)[-top_k:][::-1]
    return [
        {
            "technique_id": entries[i].technique_id,
            "name": entries[i].name,
            "score": float(scores[i]),
        }
        for i in top_idx
    ]


def load_or_build(techniques: list[dict]) -> list[IndexEntry]:
    """Load index from disk if available, otherwise build and persist."""
    if CACHE_PATH.exists():
        with open(CACHE_PATH, "rb") as f:
            return pickle.load(f)
    entries = build_index(techniques)
    with open(CACHE_PATH, "wb") as f:
        pickle.dump(entries, f)
    return entries
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
python -m pytest tests/test_embedder.py -v
```

Expected: `5 passed`

- [ ] **Step 5: Commit**

```bash
git add classifier/embedder.py classifier/tests/test_embedder.py
git commit -m "feat: classifier sidecar - sentence-transformer embedding index"
```

---

## Task 3: Python sidecar — FastAPI service

**Files:**
- Create: `classifier/main.py`
- Create: `classifier/tests/test_main.py`

- [ ] **Step 1: Write the failing test**

Create `classifier/tests/test_main.py`:

```python
import pytest
from unittest.mock import patch, MagicMock
from fastapi.testclient import TestClient


MOCK_TECHNIQUES = [
    {"id": "T1110", "name": "Brute Force", "description": "password spraying brute force login failures", "detection": "Monitor auth logs"},
    {"id": "T1078", "name": "Valid Accounts", "description": "assume role cloud credentials abuse", "detection": "Monitor account usage"},
]


@pytest.fixture
def client():
    with patch("classifier.main.techniques", MOCK_TECHNIQUES), \
         patch("classifier.main.index_entries", None):
        from classifier.main import app, lifespan_setup
        # Manually trigger setup with mock data
        import classifier.main as m
        from classifier.embedder import build_index
        m.index_entries = build_index(MOCK_TECHNIQUES)
        return TestClient(app)


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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
python -m pytest tests/test_main.py -v
```

Expected: `ModuleNotFoundError: No module named 'classifier.main'`

- [ ] **Step 3: Implement `classifier/main.py`**

```python
import logging
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from pydantic import BaseModel

from classifier.mitre import fetch_stix, parse_techniques
from classifier.embedder import load_or_build, search_index, IndexEntry

logging.basicConfig(level=logging.INFO)
log = logging.getLogger(__name__)

techniques: list[dict] = []
index_entries: list[IndexEntry] = []


@asynccontextmanager
async def lifespan(app: FastAPI):
    global techniques, index_entries
    log.info("Loading MITRE ATT&CK data...")
    stix = fetch_stix()
    techniques = parse_techniques(stix)
    log.info(f"Parsed {len(techniques)} techniques")
    index_entries = load_or_build(techniques)
    log.info(f"Index ready ({len(index_entries)} entries)")
    yield


app = FastAPI(lifespan=lifespan)


class ClassifyRequest(BaseModel):
    name: str
    query: str = ""
    app: str = ""
    subsystem: str = ""


def lifespan_setup():
    """Exposed for tests to trigger setup manually."""
    pass


@app.get("/health")
def health():
    return {"status": "ok", "techniques": len(techniques)}


@app.post("/classify")
def classify(req: ClassifyRequest):
    query_text = " ".join(filter(None, [req.name, req.app, req.subsystem, req.query]))
    return search_index(query_text, index_entries, top_k=3)


if __name__ == "__main__":
    uvicorn.run("classifier.main:app", host="0.0.0.0", port=8001, reload=False)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
python -m pytest tests/test_main.py -v
```

Expected: `4 passed`

- [ ] **Step 5: Smoke test the sidecar manually**

```bash
cd /Users/aviral.baloni/Desktop/claude
python -m classifier.main &
sleep 5
curl http://localhost:8001/health
curl -s -X POST http://localhost:8001/classify \
  -H "Content-Type: application/json" \
  -d '{"name":"CloudTrail - Delete Trail","query":"eventName:DeleteTrail","app":"cloudtrail","subsystem":"cloudtrail"}'
```

Expected health: `{"status":"ok","techniques":650}` (approx)
Expected classify: JSON array with T1562.008 in top results

- [ ] **Step 6: Kill test sidecar**

```bash
pkill -f "classifier.main"
```

- [ ] **Step 7: Commit**

```bash
git add classifier/main.py classifier/tests/test_main.py
git commit -m "feat: classifier sidecar - FastAPI service"
```

---

## Task 4: Go classifier client

**Files:**
- Create: `backend/internal/classifier/client.go`
- Create: `backend/internal/classifier/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/classifier/client_test.go`:

```go
package classifier_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"coralogix-alert-analyzer/internal/classifier"
)

func TestClassifyAlert_ReturnsCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classify" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]classifier.Candidate{
			{TechniqueID: "T1110", Name: "Brute Force", Score: 0.91},
			{TechniqueID: "T1078", Name: "Valid Accounts", Score: 0.74},
		})
	}))
	defer srv.Close()

	c := classifier.NewClient(srv.URL)
	candidates, err := c.ClassifyAlert(context.Background(), "Okta Brute Force", "failed_logins:>5", "okta", "okta-audit")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].TechniqueID != "T1110" {
		t.Errorf("expected T1110 first, got %s", candidates[0].TechniqueID)
	}
}

func TestClassifyAlert_SidecarDown_ReturnsEmpty(t *testing.T) {
	c := classifier.NewClient("http://localhost:19999") // nothing listening here
	candidates, err := c.ClassifyAlert(context.Background(), "Alert", "query", "", "")

	if err == nil {
		t.Fatal("expected error when sidecar is down")
	}
	if candidates != nil {
		t.Errorf("expected nil candidates, got %v", candidates)
	}
}

func TestIsHealthy_SidecarUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "techniques": 650})
	}))
	defer srv.Close()

	c := classifier.NewClient(srv.URL)
	if !c.IsHealthy(context.Background()) {
		t.Error("expected healthy")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go test ./internal/classifier/... 2>&1
```

Expected: `cannot find package "coralogix-alert-analyzer/internal/classifier"`

- [ ] **Step 3: Implement `backend/internal/classifier/client.go`**

```go
package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Candidate is a MITRE technique candidate returned by the classifier sidecar.
type Candidate struct {
	TechniqueID string  `json:"technique_id"`
	Name        string  `json:"name"`
	Score       float64 `json:"score"`
}

// Client calls the Python classifier sidecar over HTTP.
type Client struct {
	endpoint   string
	httpClient *http.Client
}

// NewClient creates a classifier Client for the given sidecar endpoint (e.g. "http://localhost:8001").
func NewClient(endpoint string) *Client {
	return &Client{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type classifyRequest struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	App       string `json:"app"`
	Subsystem string `json:"subsystem"`
}

// ClassifyAlert calls POST /classify on the sidecar and returns top-K candidates.
// Returns a non-nil error if the sidecar is unreachable.
func (c *Client) ClassifyAlert(ctx context.Context, name, query, app, subsystem string) ([]Candidate, error) {
	payload, _ := json.Marshal(classifyRequest{
		Name:      name,
		Query:     query,
		App:       app,
		Subsystem: subsystem,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/classify", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("classifier sidecar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classifier returned %d", resp.StatusCode)
	}

	var candidates []Candidate
	if err := json.NewDecoder(resp.Body).Decode(&candidates); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return candidates, nil
}

// IsHealthy returns true if the sidecar /health endpoint responds OK.
func (c *Client) IsHealthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	return err == nil && resp.StatusCode == http.StatusOK
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/classifier/... -v
```

Expected: `3 passed`

- [ ] **Step 5: Commit**

```bash
git add internal/classifier/
git commit -m "feat: Go classifier client for Python sidecar"
```

---

## Task 5: Config changes

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/clients.yaml`

- [ ] **Step 1: Update `config/config.go`**

Add `ClassifierConfig` and new LLM fields:

```go
// ClassifierConfig holds settings for the local MITRE classifier sidecar.
type ClassifierConfig struct {
	Endpoint string `yaml:"endpoint"` // e.g. "http://localhost:8001"
}
```

Add to `Config` struct:
```go
type Config struct {
	MondayAPIToken string                  `yaml:"monday_api_token"`
	MondayBoardID  int64                   `yaml:"monday_board_id"`
	Clients        map[string]ClientConfig `yaml:"clients"`
	LLM            LLMConfig               `yaml:"llm"`
	Classifier     ClassifierConfig        `yaml:"classifier"`
}
```

Add to `LLMConfig`:
```go
// ValidatorProvider/Model: Llama used to confirm/reject classifier candidates.
ValidatorProvider string `yaml:"validator_provider"`
ValidatorModel    string `yaml:"validator_model"`

// SuggestionProvider/Model: model used for gap alert suggestion generation.
SuggestionProvider string `yaml:"suggestion_provider"`
SuggestionModel    string `yaml:"suggestion_model"`
```

- [ ] **Step 2: Update `clients.yaml`**

```yaml
classifier:
  endpoint: "http://localhost:8001"

llm:
  default_provider: "nvidia"
  nvidia_api_key: "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"
  nvidia_model: "nvidia/nemotron-3-super-120b-a12b"
  nvidia_endpoint: "https://integrate.api.nvidia.com/v1/chat/completions"

  validator_provider: "nvidia"
  validator_model: "meta/llama-3.3-70b-instruct"

  suggestion_provider: "nvidia"
  suggestion_model: "mistralai/mistral-large-latest"

  classifier_provider: "nvidia"
  classifier_model: "meta/llama-3.3-70b-instruct"
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./... 2>&1
```

Expected: no output (clean build)

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go clients.yaml
git commit -m "feat: add classifier endpoint + validator/suggestion model config"
```

---

## Task 6: Llama validator

**Files:**
- Create: `backend/internal/llm/validator.go`
- Create: `backend/internal/llm/validator_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/llm/validator_test.go`:

```go
package llm

import (
	"testing"

	"coralogix-alert-analyzer/internal/classifier"
)

func TestParseValidationResult_ConfirmedAndRejected(t *testing.T) {
	raw := `{"confirmed": ["T1078.004", "T1110"], "rejected": ["T1021"]}`
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 2 {
		t.Fatalf("expected 2 confirmed, got %d", len(confirmed))
	}
	if confirmed[0] != "T1078.004" || confirmed[1] != "T1110" {
		t.Errorf("unexpected confirmed: %v", confirmed)
	}
}

func TestParseValidationResult_EmptyConfirmed(t *testing.T) {
	raw := `{"confirmed": [], "rejected": ["T1021", "T1110"]}`
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 0 {
		t.Errorf("expected 0 confirmed, got %d", len(confirmed))
	}
}

func TestParseValidationResult_StripsMarkdownFences(t *testing.T) {
	raw := "```json\n{\"confirmed\": [\"T1562.008\"], \"rejected\": []}\n```"
	confirmed, err := parseValidationResult(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(confirmed) != 1 || confirmed[0] != "T1562.008" {
		t.Errorf("unexpected confirmed: %v", confirmed)
	}
}

func TestBuildValidatorUserMessage(t *testing.T) {
	candidates := []classifier.Candidate{
		{TechniqueID: "T1110", Name: "Brute Force", Score: 0.91},
		{TechniqueID: "T1078", Name: "Valid Accounts", Score: 0.74},
	}
	msg := buildValidatorMessage("Okta Brute Force", "failed_logins:>5", "okta", "okta-audit", candidates)

	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	// Must contain alert name
	if !contains_str(msg, "Okta Brute Force") {
		t.Error("message missing alert name")
	}
	// Must contain both candidate IDs
	if !contains_str(msg, "T1110") || !contains_str(msg, "T1078") {
		t.Error("message missing candidate IDs")
	}
	// Must contain scores
	if !contains_str(msg, "0.91") {
		t.Error("message missing score")
	}
}

func contains_str(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/llm/... -run TestParseValidation -v
go test ./internal/llm/... -run TestBuildValidator -v
```

Expected: `undefined: parseValidationResult`

- [ ] **Step 3: Implement `backend/internal/llm/validator.go`**

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"coralogix-alert-analyzer/internal/classifier"
)

var validatorSystemPrompt = `You are a MITRE ATT&CK expert. A semantic classifier has identified candidate techniques for a security alert. Your job is to:
1. Confirm candidates that are genuinely detected by this alert's query logic
2. Reject candidates that do not match what the alert actually detects
3. Where the evidence is specific enough, promote a parent technique to a sub-technique (e.g. T1078 → T1078.004 for cloud account activity)

Respond ONLY with JSON in this exact format:
{"confirmed": ["T1078.004"], "rejected": ["T1021"]}

No markdown, no explanation. If all candidates are wrong, return {"confirmed": [], "rejected": [...]}.`

type validationResult struct {
	Confirmed []string `json:"confirmed"`
	Rejected  []string `json:"rejected"`
}

// ValidateCandidates asks Llama to confirm/reject/promote classifier candidates for an alert.
// Returns the confirmed technique IDs. Uses FastMode (no extended reasoning needed).
func ValidateCandidates(ctx context.Context, provider Provider, name, query, app, subsystem string, candidates []classifier.Candidate) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	userMsg := buildValidatorMessage(name, query, app, subsystem, candidates)

	resp, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: validatorSystemPrompt,
		UserMessage:  userMsg,
		MaxTokens:    256,
		FastMode:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("validator LLM: %w", err)
	}

	return parseValidationResult(resp)
}

func buildValidatorMessage(name, query, app, subsystem string, candidates []classifier.Candidate) string {
	var sb strings.Builder
	sb.WriteString("Alert: ")
	sb.WriteString(name)
	if app != "" {
		sb.WriteString(" | App: ")
		sb.WriteString(app)
	}
	if subsystem != "" {
		sb.WriteString(" | Subsystem: ")
		sb.WriteString(subsystem)
	}
	if query != "" {
		sb.WriteString("\nQuery: ")
		sb.WriteString(query)
	}
	sb.WriteString("\n\nCandidates:\n")
	for i, c := range candidates {
		sb.WriteString(fmt.Sprintf("%d. %s - %s (score: %.2f)\n", i+1, c.TechniqueID, c.Name, c.Score))
	}
	return sb.String()
}

func parseValidationResult(raw string) ([]string, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.SplitN(cleaned, "\n", 2)
		if len(lines) > 1 {
			cleaned = lines[1]
		}
		if idx := strings.LastIndex(cleaned, "```"); idx > 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	var result validationResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse validation result: %w (raw: %.100s)", err, raw)
	}
	if result.Confirmed == nil {
		return []string{}, nil
	}
	return result.Confirmed, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/llm/... -run "TestParseValidation|TestBuildValidator" -v
```

Expected: `4 passed`

- [ ] **Step 5: Commit**

```bash
git add internal/llm/validator.go internal/llm/validator_test.go
git commit -m "feat: Llama validator for classifier candidates"
```

---

## Task 7: Wire the full pipeline in handlers.go

**Files:**
- Modify: `backend/internal/llm/mitre_mapper.go` — add `BatchClassifyAndValidate`
- Modify: `backend/internal/api/handlers.go` — replace `BatchMapMITRE` with new pipeline; wire classifier + validator

- [ ] **Step 1: Add `BatchClassifyAndValidate` to `mitre_mapper.go`**

Add this function to the bottom of `backend/internal/llm/mitre_mapper.go`:

```go
// BatchClassifyAndValidate runs the two-stage MITRE mapping pipeline:
// 1. Classifier sidecar → top-3 semantic candidates per alert
// 2. Llama validator → confirmed technique IDs
// Results are cached per-alert in Redis for 7 days.
// Falls back gracefully: if sidecar is down, candidates are empty and validator is skipped.
func BatchClassifyAndValidate(
	ctx context.Context,
	classifierClient ClassifierClientIface,
	validatorProvider Provider,
	store MITRECacheStore,
	inputs []AlertInput,
) map[string][]string {
	result := make(map[string][]string, len(inputs))
	var mu sync.Mutex

	type work struct {
		input    AlertInput
		cacheKey string
	}

	// Check cache first.
	var uncached []work
	for _, inp := range inputs {
		key := mitreCachePrefix + alertHash(inp.Name, inp.Query, inp.App, inp.Subsystem)
		if val, ok := store.GetString(ctx, key); ok {
			var techs []string
			if err := json.Unmarshal([]byte(val), &techs); err == nil {
				result[inp.ID] = techs
				continue
			}
		}
		uncached = append(uncached, work{input: inp, cacheKey: key})
	}

	log.Printf("INFO [classifier] total=%d cached=%d to_map=%d", len(inputs), len(inputs)-len(uncached), len(uncached))

	if len(uncached) == 0 {
		return result
	}

	jobs := make(chan work, len(uncached))
	for _, w := range uncached {
		jobs <- w
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < mitreWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				techs := classifyAndValidateSingle(ctx, classifierClient, validatorProvider, w.input)
				if data, err := json.Marshal(techs); err == nil {
					store.SetString(ctx, w.cacheKey, string(data), mitreCacheTTL)
				}
				mu.Lock()
				result[w.input.ID] = techs
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return result
}

func classifyAndValidateSingle(
	ctx context.Context,
	classifierClient ClassifierClientIface,
	validatorProvider Provider,
	inp AlertInput,
) []string {
	aCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Stage 1: classifier sidecar
	candidates, err := classifierClient.ClassifyAlert(aCtx, inp.Name, inp.Query, inp.App, inp.Subsystem)
	if err != nil {
		log.Printf("WARN [classifier] alert=%s: %v", inp.ID, err)
		return []string{}
	}
	if len(candidates) == 0 {
		return []string{}
	}

	// Stage 2: Llama validation
	confirmed, err := ValidateCandidates(aCtx, validatorProvider, inp.Name, inp.Query, inp.App, inp.Subsystem, candidates)
	if err != nil {
		log.Printf("WARN [validator] alert=%s: %v — using raw candidates", inp.ID, err)
		// Fall back to raw classifier output
		techs := make([]string, 0, len(candidates))
		for _, c := range candidates {
			techs = append(techs, c.TechniqueID)
		}
		return techs
	}
	return confirmed
}
```

Also add the `ClassifierClientIface` interface to `mitre_mapper.go` (above `BatchClassifyAndValidate`):

```go
// ClassifierClientIface allows injecting the classifier client (or a mock in tests).
type ClassifierClientIface interface {
	ClassifyAlert(ctx context.Context, name, query, app, subsystem string) ([]classifier.Candidate, error)
}
```

Add import `"coralogix-alert-analyzer/internal/classifier"` to the import block.

- [ ] **Step 2: Update `handlers.go` — replace BatchMapMITRE with new pipeline**

Replace the `// Build LLM MITRE mappings...` block in `HandleAnalyze` with:

```go
// Build MITRE mappings via classifier sidecar + Llama validator.
// Only runs on security alerts with no existing label/T-code coverage.
// Results are cached per-alert in Redis for 7 days.
var llmMappings map[string][]string
if h.cache != nil {
    baseCfg := llm.ProviderConfig{
        AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
        ClaudeModel:     h.config.LLM.ClaudeModel,
        NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
        NvidiaModel:     h.config.LLM.NvidiaModel,
        NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
    }
    validatorProvider, err := llm.NewClassifierProvider(
        h.config.LLM.ValidatorProvider,
        h.config.LLM.ValidatorModel,
        baseCfg,
    )
    if err != nil {
        log.Printf("WARN [analyze] validator provider unavailable: %v", err)
    } else {
        classifierClient := classifier.NewClient(h.config.Classifier.Endpoint)
        var inputs []llm.AlertInput
        for _, a := range alerts {
            if !coralogix.IsSecurityAlert(a) || coralogix.HasExistingMITRE(a) {
                continue
            }
            app, subsystem := coralogix.ExtractAppSubsystem(a.TypeDef)
            inputs = append(inputs, llm.AlertInput{
                ID:        a.ID,
                Name:      a.Name,
                Query:     coralogix.ExtractLuceneQuery(a.TypeDef),
                App:       app,
                Subsystem: subsystem,
            })
        }
        log.Printf("INFO [analyze] MITRE pipeline: %d/%d alerts need classification", len(inputs), len(alerts))
        if len(inputs) > 0 {
            llmMappings = llm.BatchClassifyAndValidate(ctx, classifierClient, validatorProvider, h.cache, inputs)
        }
    }
}
```

Add `"coralogix-alert-analyzer/internal/classifier"` to imports in `handlers.go`.
Remove the old `BatchMapMITRE` import usage.

- [ ] **Step 3: Update suggestions handler to use suggestion_model**

In `HandleSuggestions`, replace:
```go
providerName := req.Provider
if providerName == "" {
    providerName = h.config.LLM.DefaultProvider
}
provider, err := llm.NewProvider(providerName, llm.ProviderConfig{...})
```

With:
```go
providerName := req.Provider
if providerName == "" {
    if h.config.LLM.SuggestionProvider != "" {
        providerName = h.config.LLM.SuggestionProvider
    } else {
        providerName = h.config.LLM.DefaultProvider
    }
}
suggestionModel := h.config.LLM.SuggestionModel
baseCfg := llm.ProviderConfig{
    AnthropicAPIKey: h.config.LLM.AnthropicAPIKey,
    ClaudeModel:     h.config.LLM.ClaudeModel,
    NvidiaAPIKey:    h.config.LLM.NvidiaAPIKey,
    NvidiaModel:     h.config.LLM.NvidiaModel,
    NvidiaEndpoint:  h.config.LLM.NvidiaEndpoint,
}
provider, err := llm.NewClassifierProvider(providerName, suggestionModel, baseCfg)
```

- [ ] **Step 4: Build to verify**

```bash
go build ./... 2>&1
```

Expected: no output

- [ ] **Step 5: Commit**

```bash
git add internal/llm/mitre_mapper.go internal/api/handlers.go
git commit -m "feat: wire classifier + Llama validator pipeline into analyze handler"
```

---

## Task 8: End-to-end test run

- [ ] **Step 1: Start Python sidecar**

```bash
cd /Users/aviral.baloni/Desktop/claude
pip install -r classifier/requirements.txt
python -m classifier.main &
sleep 10  # wait for STIX download + index build
curl http://localhost:8001/health
```

Expected: `{"status":"ok","techniques":650}` (approx)

- [ ] **Step 2: Clear Redis cache**

```bash
redis-cli KEYS "mitre_llm_v1:*" | xargs redis-cli DEL
echo "Cache cleared"
```

- [ ] **Step 3: Rebuild and restart Go backend**

```bash
cd /Users/aviral.baloni/Desktop/claude/backend
go build -o /tmp/analyzer ./cmd/server/ && pkill -f "/tmp/analyzer" 2>/dev/null
sleep 1 && /tmp/analyzer &
sleep 2 && curl -s http://localhost:8080/api/clients
```

Expected: `["Deel"]`

- [ ] **Step 4: Run full analysis**

```bash
curl -s -X POST "http://localhost:8080/api/analyze?refresh=true" \
  -H "Content-Type: application/json" \
  -d '{"client":"Deel"}' \
  --max-time 600 > /tmp/analyze_result.json
echo "Done: $?"
```

- [ ] **Step 5: Evaluate results**

```bash
python3 -c "
import json, subprocess
from collections import Counter

data = json.load(open('/tmp/analyze_result.json'))
summary = data['mitre_coverage']['summary']
print(f'Coverage: {summary[\"coverage_percent\"]}% ({summary[\"covered_techniques\"]}/{summary[\"total_techniques\"]})')
print(f'Sub-techniques: {summary[\"covered_sub_techniques\"]}/{summary[\"total_sub_techniques\"]}')

keys = subprocess.run(['redis-cli','KEYS','mitre_llm_v1:*'], capture_output=True, text=True).stdout.strip().split()
counts = Counter()
for k in keys:
    val = subprocess.run(['redis-cli','GET',k], capture_output=True, text=True).stdout.strip()
    try:
        t = json.loads(val)
        counts[len(t)] += 1
    except: pass
total = sum(counts.values())
print(f'LLM cache: {total} | empty: {counts[0]} ({100*counts[0]//total if total else 0}%) | mapped: {total-counts[0]}')
"
```

Expected: coverage above 55%, fewer empty responses than the pure cloud-LLM approach.

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "feat: local MITRE classifier pipeline - sidecar + Llama validation + Mistral suggestions"
```

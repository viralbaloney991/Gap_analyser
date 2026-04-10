# Local MITRE Classifier Design

**Date:** 2026-04-01
**Status:** Approved

## Problem

The current cloud LLM approach for MITRE technique classification (NVIDIA NIM) produces 85% empty responses and occasional hallucinated technique IDs. It is API-dependent, slow on cold cache, and accuracy is limited by the general LLM's knowledge of the MITRE taxonomy.

## Solution

A three-stage local pipeline:

1. **Python classifier sidecar** — semantic similarity against MITRE ATT&CK documentation, fully local, no API calls
2. **Llama 70B (validation)** — confirms/rejects candidates and promotes to sub-techniques
3. **Mistral (generation)** — generates detection alert suggestions for coverage gaps (replaces current nemotron)

---

## Architecture

```
Alert (name + query + app + subsystem)
         │
         ▼
[1] Python Classifier Sidecar (localhost:8001)
    - MITRE STIX loaded once at startup
    - sentence-transformers (all-MiniLM-L6-v2, 22MB, CPU)
    - Embeddings persisted to embeddings.pkl
    - Returns top-3 technique candidates + cosine similarity scores
         │
         ▼
[2] Llama 70B via NVIDIA NIM (validation)
    - Receives alert context + top-3 pre-screened candidates
    - Confirms correct techniques, rejects wrong ones
    - Promotes parent techniques to sub-techniques where evidence is specific
    - Returns {"confirmed": [...], "rejected": [...]}
         │
         ▼
    Redis cache (7-day TTL, key: mitre_llm_v1:<hash>)

[3] Mistral via NVIDIA NIM (gap suggestions)
    - Replaces nemotron for the suggestions endpoint
    - Generates Coralogix alert recommendations for uncovered techniques
```

---

## Components

### Python Sidecar (`classifier/`)

```
classifier/
  main.py          # FastAPI app
  mitre.py         # STIX fetch + technique parsing
  embedder.py      # Index build + cosine similarity
  embeddings.pkl   # Persisted index (gitignored)
  requirements.txt
```

**Startup behaviour:**
1. Check for `embeddings.pkl` on disk
2. If missing: fetch MITRE ATT&CK Enterprise STIX from `https://github.com/mitre/cti` → parse technique ID + name + description + examples → build embedding index → save to disk
3. If present: load from disk (~100ms)
4. Serve indefinitely — restart to pick up new MITRE releases

**API:**
- `GET /health` → `{"status": "ok", "techniques": 650}`
- `POST /classify` → body: `{name, query, app, subsystem}` → response: `[{technique_id, name, score}]` (top-3, sorted by score)

**Model:** `sentence-transformers/all-MiniLM-L6-v2`
- 22MB download
- CPU-only, no GPU required
- ~10ms per classification

### Go Classifier Client (`internal/classifier/client.go`)

```go
type Candidate struct {
    TechniqueID string
    Name        string
    Score       float64
}

func (c *Client) ClassifyAlert(ctx, input AlertInput) ([]Candidate, error)
```

Calls sidecar over localhost HTTP. Returns empty slice (not error) if sidecar is unavailable — pipeline degrades gracefully to Llama-only or rule-based.

### Go Validator (`internal/llm/validator.go`)

```go
func ValidateCandidates(ctx, provider, input AlertInput, candidates []Candidate) ([]string, error)
```

Llama prompt receives:
- Alert name, app, subsystem, query
- Top-3 candidates with scores
- Instruction to confirm/reject and promote to sub-techniques

Returns confirmed technique IDs only. Uses `FastMode: true` (no reasoning tokens needed for validation).

### Configuration (`clients.yaml`)

```yaml
llm:
  # Existing
  default_provider: "nvidia"
  nvidia_api_key: "..."
  nvidia_model: "nvidia/nemotron-3-super-120b-a12b"

  # Validator (Llama — confirms classifier candidates)
  validator_provider: "nvidia"
  validator_model: "meta/llama-3.3-70b-instruct"

  # Suggestions (Mistral — generates detection alert recommendations)
  suggestion_provider: "nvidia"
  suggestion_model: "mistralai/mistral-large-latest"

classifier:
  endpoint: "http://localhost:8001"
```

---

## Data Flow

### MITRE Mapping (per alert, uncovered only)

```
1. Go → POST /classify (sidecar)       ~10ms, local
2. Sidecar → top-3 candidates
3. Go → Llama validator (NVIDIA NIM)   ~3-5s, API
4. Llama → confirmed techniques
5. Go → Redis SET (7-day TTL)
```

On subsequent requests: Redis HIT → skip steps 1-4 entirely.

Only security alerts with no existing label/description T-code coverage enter this pipeline (~300-400 alerts for Deel).

### Gap Suggestion Generation (on-demand, unchanged flow)

```
User clicks "Generate" on uncovered technique
→ Go → Mistral (NVIDIA NIM)
→ Returns up to 6 alert suggestions
→ Rendered in frontend detail panel
```

---

## Error Handling

| Failure | Behaviour |
|---------|-----------|
| Sidecar down | Skip classification, pass empty candidates to validator; validator falls back to rule-based mapper |
| Sidecar returns 0 candidates | Skip validator call, use rule-based only |
| Llama validation fails | Log warning, use raw classifier candidates with T-ID validation against `techniqueToTactics` |
| All LLM unavailable | Features still extracted via rule-based mapper + labels + description T-codes |

The pipeline is fully additive — every stage is optional and degrades gracefully.

---

## MITRE Data Refresh

MITRE releases ATT&CK updates ~twice per year. To pick up new techniques:
1. Delete `classifier/embeddings.pkl`
2. Restart the sidecar — it re-fetches and rebuilds the index automatically

No code changes required.

---

## Out of Scope

- GPU acceleration
- Fine-tuning on labeled alert data
- Real-time MITRE update detection
- Multi-tenant classifier (single shared sidecar serves all clients)

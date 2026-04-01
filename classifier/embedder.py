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
    return " ".join(parts)[:1000]


def build_index(techniques: list[dict]) -> list[IndexEntry]:
    """Embed all techniques and return index entries. Embeddings are L2-normalised for cosine similarity via dot product."""
    model = _get_model()
    texts = [_technique_text(t) for t in techniques]
    embeddings = model.encode(texts, convert_to_numpy=True, show_progress_bar=False)
    norms = np.linalg.norm(embeddings, axis=1, keepdims=True)
    embeddings = embeddings / np.maximum(norms, 1e-8)
    return [
        IndexEntry(technique_id=t["id"], name=t["name"], embedding=embeddings[i])
        for i, t in enumerate(techniques)
    ]


def search_index(query: str, entries: list[IndexEntry], top_k: int = 3) -> list[dict]:
    """Return top-k techniques most similar to query, sorted by score descending."""
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
    """Load index from disk if available, otherwise build and persist to embeddings.pkl."""
    if CACHE_PATH.exists():
        with open(CACHE_PATH, "rb") as f:
            return pickle.load(f)
    entries = build_index(techniques)
    with open(CACHE_PATH, "wb") as f:
        pickle.dump(entries, f)
    return entries

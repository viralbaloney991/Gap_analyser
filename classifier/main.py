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

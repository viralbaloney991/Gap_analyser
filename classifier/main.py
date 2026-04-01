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
_startup_error: str = ""


@asynccontextmanager
async def lifespan(app: FastAPI):
    global techniques, index_entries, _startup_error
    log.info("Loading MITRE ATT&CK data...")
    try:
        stix = fetch_stix()
        techniques = parse_techniques(stix)
        log.info(f"Parsed {len(techniques)} techniques")
        index_entries = load_or_build(techniques)
        log.info(f"Index ready ({len(index_entries)} entries)")
    except Exception as exc:
        log.error(f"Startup failed: {exc}")
        _startup_error = str(exc)
    yield


app = FastAPI(lifespan=lifespan)


class ClassifyRequest(BaseModel):
    name: str
    query: str = ""
    app: str = ""
    subsystem: str = ""


@app.get("/health")
def health():
    if _startup_error:
        return {"status": "degraded", "error": _startup_error, "techniques": 0}
    return {"status": "ok", "techniques": len(techniques)}


@app.post("/classify")
def classify(req: ClassifyRequest):
    query_text = " ".join(filter(None, [req.name, req.app, req.subsystem, req.query]))
    return search_index(query_text, index_entries, top_k=3)


if __name__ == "__main__":
    uvicorn.run("classifier.main:app", host="0.0.0.0", port=8001, reload=False)

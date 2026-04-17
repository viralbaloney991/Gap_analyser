---
title: Project Cleanup
date: 2026-04-17
status: approved
---

# Project Cleanup

Remove dead code, stale binaries, and tracked build artifacts accumulated during development.

## Changes

### 1. Delete `backend/cmd/benchmark/` (entire directory)
The Mistral vs MiniMax benchmark tool was created during the Apr 16 model migration investigation. The migration is complete (Mistral is now in production). The file contains a **hardcoded NVIDIA API key** and has no ongoing utility.

### 2. Delete compiled binaries from working tree
- `backend/benchmark` — compiled benchmark binary (untracked)
- `backend/coralogix-alert-analyzer` — compiled server binary at wrong path (untracked)

Both are already absent from git. Physical deletion only.

### 3. Untrack committed Python bytecode
Four `.pyc` files were committed before `.gitignore` covered `__pycache__/`:
- `classifier/__pycache__/__init__.cpython-314.pyc`
- `classifier/__pycache__/mitre.cpython-314.pyc`
- `classifier/tests/__pycache__/__init__.cpython-314.pyc`
- `classifier/tests/__pycache__/test_mitre.cpython-314-pytest-8.2.0.pyc`

Fix: `git rm --cached` to remove from index; files remain on disk and are now covered by `.gitignore`.

### 4. Delete `Idea.md`
Original pre-build design scratch notes. The project is shipped; this file has no ongoing value.

## Out of Scope
- `docs/superpowers/` — historical specs and plans, kept for reference
- `dev.sh` — active development tool
- `frontend/dist/` — already gitignored, not tracked
- `backend/debug_alerts.json` — already gitignored, not tracked

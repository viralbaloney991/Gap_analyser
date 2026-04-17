# Project Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove dead code, untracked binaries, committed bytecode, and a stale scratch doc from the repo.

**Architecture:** Pure deletion — no new code, no refactoring. Four independent operations applied in sequence and committed together as one clean-up commit.

**Tech Stack:** git, bash

---

### Task 1: Delete the benchmark tool and its compiled binary

The `backend/cmd/benchmark/` directory contains a one-off Mistral vs MiniMax benchmark script that was used during the Apr 16 model migration. The migration is complete and the file contains a hardcoded NVIDIA API key — it must not stay in the repo.

`backend/benchmark` is the compiled binary produced by that tool; it lives in the backend root and is untracked.

**Files:**
- Delete: `backend/cmd/benchmark/mistral_bench.go` (and the `benchmark/` dir)
- Delete: `backend/benchmark` (compiled binary, working tree only)

- [ ] **Step 1: Verify the directory and binary exist**

```bash
ls backend/cmd/benchmark/
ls -lh backend/benchmark
```

Expected: `mistral_bench.go` listed; `backend/benchmark` is a Mach-O executable.

- [ ] **Step 2: Remove the benchmark source directory from git and disk**

```bash
git rm -r backend/cmd/benchmark/
```

Expected output: `rm 'backend/cmd/benchmark/mistral_bench.go'`

- [ ] **Step 3: Delete the compiled binary from the working tree**

```bash
rm backend/benchmark
```

No output expected.

- [ ] **Step 4: Verify the files are gone**

```bash
ls backend/cmd/           # should show only: server/
ls backend/benchmark 2>&1 # should print: No such file or directory
```

---

### Task 2: Delete the stray compiled server binary

`backend/coralogix-alert-analyzer` is a compiled server binary at the wrong path (the canonical binary lives at `backend/bin/server`). It is untracked and should not be here.

**Files:**
- Delete: `backend/coralogix-alert-analyzer` (working tree only)

- [ ] **Step 1: Confirm it is untracked**

```bash
git status backend/coralogix-alert-analyzer
```

Expected: `?? backend/coralogix-alert-analyzer`

- [ ] **Step 2: Delete it**

```bash
rm backend/coralogix-alert-analyzer
```

- [ ] **Step 3: Confirm it is gone**

```bash
ls backend/coralogix-alert-analyzer 2>&1
```

Expected: `No such file or directory`

---

### Task 3: Untrack committed Python bytecode

Four `.pyc` files were committed before `.gitignore` covered `__pycache__/`. They need to be removed from the git index (not from disk — Python regenerates them).

**Files to untrack (git rm --cached only):**
- `classifier/__pycache__/__init__.cpython-314.pyc`
- `classifier/__pycache__/mitre.cpython-314.pyc`
- `classifier/tests/__pycache__/__init__.cpython-314.pyc`
- `classifier/tests/__pycache__/test_mitre.cpython-314-pytest-8.2.0.pyc`

- [ ] **Step 1: Confirm all four are currently tracked**

```bash
git ls-files classifier/__pycache__/ classifier/tests/__pycache__/
```

Expected — four lines:
```
classifier/__pycache__/__init__.cpython-314.pyc
classifier/__pycache__/mitre.cpython-314.pyc
classifier/tests/__pycache__/__init__.cpython-314.pyc
classifier/tests/__pycache__/test_mitre.cpython-314-pytest-8.2.0.pyc
```

- [ ] **Step 2: Remove them from the index**

```bash
git rm --cached \
  classifier/__pycache__/__init__.cpython-314.pyc \
  classifier/__pycache__/mitre.cpython-314.pyc \
  classifier/tests/__pycache__/__init__.cpython-314.pyc \
  classifier/tests/__pycache__/test_mitre.cpython-314-pytest-8.2.0.pyc
```

Expected: four `rm '...'` lines.

- [ ] **Step 3: Confirm they are no longer tracked**

```bash
git ls-files classifier/__pycache__/ classifier/tests/__pycache__/
```

Expected: no output (empty).

- [ ] **Step 4: Confirm the files still exist on disk (Python needs them)**

```bash
ls classifier/__pycache__/
```

Expected: files still present locally.

---

### Task 4: Delete Idea.md

`Idea.md` is the original pre-build design scratch notes. The project is shipped and this file has no ongoing value.

**Files:**
- Delete: `Idea.md`

- [ ] **Step 1: Remove from git and disk**

```bash
git rm Idea.md
```

Expected: `rm 'Idea.md'`

---

### Task 5: Commit everything

- [ ] **Step 1: Review staged changes**

```bash
git status
git diff --cached --stat
```

Expected staged deletions:
- `backend/cmd/benchmark/mistral_bench.go` deleted
- `classifier/__pycache__/__init__.cpython-314.pyc` deleted
- `classifier/__pycache__/mitre.cpython-314.pyc` deleted
- `classifier/tests/__pycache__/__init__.cpython-314.pyc` deleted
- `classifier/tests/__pycache__/test_mitre.cpython-314-pytest-8.2.0.pyc` deleted
- `Idea.md` deleted

- [ ] **Step 2: Commit**

```bash
git commit -m "$(cat <<'EOF'
chore: remove dead code, stale binaries, and tracked bytecode

- Delete backend/cmd/benchmark/ (one-off Mistral vs MiniMax benchmark,
  migration complete; contained hardcoded NVIDIA API key)
- Delete backend/benchmark and backend/coralogix-alert-analyzer compiled
  binaries from working tree (untracked, wrong locations)
- Untrack classifier __pycache__ .pyc files committed before .gitignore
  covered them (files remain on disk, now properly ignored)
- Delete Idea.md (pre-build scratch notes, project is shipped)

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 3: Verify clean state**

```bash
git status
```

Expected: working tree clean (or only showing the untracked `dev.sh` modification and `backend/benchmark` / `backend/coralogix-alert-analyzer` gone).

```bash
git show --stat HEAD
```

Expected: shows the 6 deletions above.

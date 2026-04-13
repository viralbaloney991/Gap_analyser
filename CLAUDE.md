# Coralogix Alert Analyzer — Development Workflow

## Team Workflow (mandatory for all features and fixes)

Every change follows this sequence. No step may be skipped.

| Step | Role | Skill |
|------|------|-------|
| 1. Understand & design | PM + Architect | `superpowers:brainstorming` → `superpowers:writing-plans` |
| 2. Build | Developer | `superpowers:test-driven-development` + `superpowers:subagent-driven-development` |
| 3. UI work | UI/UX | `frontend-design:frontend-design` |
| 4. Parallel tasks | Team | `superpowers:dispatching-parallel-agents` |
| 5. Review | Reviewer | `superpowers:requesting-code-review` |

**Rules:**
- No code before a written plan
- No merge before code review
- Independent tasks run in parallel (backend/frontend, multiple bugs)
- Each role is a separate agent with isolated context

## Stack

- **Backend:** Go (`backend/`) — similarity engine, LLM enrichment, REST API
- **Frontend:** React + TypeScript + Vite (`frontend/`)
- **DB:** NeonDB (PostgreSQL) for alert store, Redis for caching
- **LLM providers:** Anthropic Claude, NVIDIA NIM, Google Gemini

## Key Invariants

- Similarity weights must always sum to exactly 1.00 (`engine.go`)
- All detection rules and logic must be client-agnostic and scalable across all clients
- Prioritise accuracy over speed/convenience; verify results before presenting

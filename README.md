# Coralogix Alert Analyzer

Coralogix Alert Analyzer is a full-stack application for reviewing configured detections, mapping MITRE ATT&CK coverage, identifying redundant alert logic, and generating follow-up detection suggestions.

## Project Structure

- `backend/`: Go API server that fetches Coralogix alerts, reads client configuration, integrates Monday.com metadata, computes MITRE coverage, and runs similarity analysis.
- `frontend/`: React + TypeScript + Vite UI for selecting clients, running analysis, and viewing results.
- `Idea.md`: original product and workflow notes for the project.

## Core Capabilities

- Analyze active alert definitions for a configured client
- Highlight MITRE ATT&CK coverage and uncovered techniques
- Group similar detections to surface overlap and merge opportunities
- Correlate integrations with alert coverage
- Generate LLM-powered suggestions for uncovered techniques

## Requirements

- Go `1.25.6`
- Node.js with npm
- Redis recommended for caching at `localhost:6379` (the backend will still run without it)
- Valid Coralogix and Monday.com credentials in configuration

## Configuration

The backend reads configuration from `backend/clients.yaml` by default. You can override the path with `CONFIG_PATH`.

Expected values include:

- `monday_api_token`
- `monday_board_id`
- `clients.<name>.api_key`
- `clients.<name>.region`
- `clients.<name>.monday_group_id`
- `llm.default_provider`
- `llm.claude_model` or `llm.nvidia_model`

API keys for LLM providers can be supplied through environment variables instead of YAML:

- `ANTHROPIC_API_KEY`
- `NVIDIA_API_KEY`

Other useful backend environment variables:

- `PORT` default: `8080`
- `CORS_ORIGIN` default: `http://localhost:5173`
- `CONFIG_PATH` default: `clients.yaml`
- `REDIS_ADDR` default: `localhost:6379`
- `FRONTEND_DIST` default: `../../frontend/dist`

For the frontend, set `VITE_API_URL` if the API is not running on `http://localhost:8080`.

## Running Locally

Start the backend:

```bash
cd backend
go run ./cmd/server
```

Start the frontend in a second terminal:

```bash
cd frontend
npm install
npm run dev
```

Build the frontend:

```bash
cd frontend
npm run build
```

Once the frontend is built, the Go server can serve static assets from `frontend/dist`.

## API Endpoints

- `GET /api/health`
- `GET /api/clients`
- `POST /api/analyze`
- `POST /api/suggestions`

## Notes

- The backend runs the Monday.com fetch and Coralogix alert retrieval in parallel during analysis.
- Cached analysis responses are returned when Redis is available and `refresh=true` is not used.
- The checked-in `frontend/README.md` is still the default Vite template; this root README describes the actual repository.

# Frontend — SIEM Alert Analyser

React + TypeScript + Vite single-page application for the SIEM Alert Analyser platform.

## Stack

- **React 18** — UI framework
- **TypeScript** — type safety throughout
- **Vite** — build tool and dev server
- **Lucide React** — icon set

## Structure

```
src/
├── components/
│   ├── ClientSelector.tsx     # Tenant/environment picker — first screen
│   ├── AlertInsights.tsx      # Main results view: insights, noise, suggestions, gaps
│   ├── MITREHeatmap.tsx       # ATT&CK tactic/technique coverage heatmap
│   ├── ThreatGraph.tsx        # Force-directed alert relationship graph
│   ├── DetectionBuilder.tsx   # Detection rule wizard (MITRE technique → query)
│   ├── HuntView.tsx           # AI threat hunt — streaming SSE results
│   ├── IntegrationSummary.tsx # Integration ↔ alert coverage table
│   └── NoisePills.tsx         # Lookback window selector (7d / 14d / 30d / 90d)
├── services/
│   └── api.ts                 # API client — REST calls + SSE streaming
├── types/
│   └── index.ts               # Shared TypeScript interfaces
├── data/
│   └── mitre-catalog.ts       # Local MITRE ATT&CK technique catalog
└── utils/
    └── export.ts              # Report export helpers
```

## Development

```bash
npm install
npm run dev        # dev server at http://localhost:5173
npm run build      # production build → dist/
npm run preview    # preview production build locally
```

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `VITE_API_URL` | `http://localhost:8080` | Backend API base URL |

Set in a `.env.local` file (not committed):

```
VITE_API_URL=http://localhost:8080
```

## Key Patterns

**API calls** — all requests go through `src/services/api.ts`. REST endpoints use `fetch` with JSON; the threat hunt uses `EventSource` for server-sent events (SSE).

**State** — component-local `useState`/`useEffect`. No global state library; analysis results are passed down from `App.tsx` as props.

**Types** — all API response shapes are typed in `src/types/index.ts`. Backend JSON field names are snake_case; TypeScript interfaces match them directly (no camelCase conversion).

**Styling** — single `App.css` with CSS custom properties for theming. No CSS-in-JS or utility framework.

# Frontend Redesign — CXAlert Analyzer
**Date:** 2026-04-23  
**Status:** Approved for implementation

---

## Overview

A full aesthetic overhaul of the CXAlert Analyzer frontend, replacing the neon-green terminal aesthetic with a refined dark + brand green design system. All four views are updated. No component is removed; four functional gaps are filled.

**Scope:** Design quality + missing functionality  
**Approach:** Full App.css rewrite + targeted TSX updates per component  
**New packages:** Google Fonts (Red Hat Display), Framer Motion  

---

## Design System

### Typography
| Role | Font | Weight | Usage |
|------|------|--------|-------|
| Display / Headings | Red Hat Display | 700–900 | Page titles, section headings, stat numbers |
| Body | Red Hat Display | 400–500 | Descriptions, body text, labels |
| Data / Code | IBM Plex Mono | 300–500 | IDs, metrics, queries, status tags, badges |

**Rules:**
- All IDs, alert names, technique IDs, similarity scores, query syntax → IBM Plex Mono
- All prose, headings, stat numerals, navigation → Red Hat Display
- No monospace for multi-sentence paragraph text

### Color Tokens
```css
--bg:            #080d14   /* Deep navy-black base */
--bg-mid:        #0e1520   /* Secondary background */
--surface:       #131c2b   /* Card / panel backgrounds */
--surface-2:     #1a2537   /* Hover states, nested surfaces */
--border:        rgba(16,185,129,0.12)  /* Default borders */
--border-bright: rgba(16,185,129,0.32)  /* Interactive borders */
--accent:        #10b981   /* Emerald green — primary accent */
--accent-dim:    rgba(16,185,129,0.10)  /* Accent backgrounds */
--text:          #f1f5f9   /* Primary text */
--text-sec:      #94a3b8   /* Secondary text */
--text-dim:      #64748b   /* Tertiary / labels */
--danger:        #ef4444   /* Errors, blind spots, noise */
--danger-dim:    rgba(239,68,68,0.10)
--warn:          #f59e0b   /* Warnings, low coverage */
--warn-dim:      rgba(245,158,11,0.10)
--indigo:        #818cf8   /* Duplicate cards */
--indigo-dim:    rgba(99,102,241,0.12)
--sky:           #38bdf8   /* Coverage gap cards */
--sky-dim:       rgba(56,189,248,0.10)
```

### Spacing & Shape
- Border radius: `4px` (small), `6px` (card), `8px` (panel), `10px` (floating)
- Card top-edge accent: `1px` gradient line (`transparent → --accent → transparent`, opacity 0.4) — used on stat cards, priority items, general surface cards
- Card left-border accent: `3px solid` (color varies by card type — see AlertInsights) — AlertInsights insight cards use **left-border only**, not the top-edge gradient, to avoid visual conflict between two simultaneous accents

---

## App Shell

### Header (all views)
- Logo: `CX` + `Alert` in accent color, Red Hat Display 700
- Breadcrumb: `Clients / ClientName / ViewName` in IBM Plex Mono 0.62rem
- Right: contextual action buttons (back, nav) + pulsing dot status indicator
- Height: 52px, `border-bottom: 1px solid --border`

### View Transitions
- Use **Framer Motion** `AnimatePresence` + `motion.div` on each view
- Transition: `opacity 0→1`, `translateY 8px→0`, duration 200ms, ease-out
- Applied at the router level in `App.tsx`

---

## Component Designs

### 1. ClientSelector

**Layout:** Centered hero + search bar + region groups + sticky CTA  
**Hero:** Red Hat Display 900, `3rem`, letter-spacing `-0.03em`, "Select a *client*" with accent on "client"  
**Eyebrow:** IBM Plex Mono, uppercase, letter-spacing 0.2em, with flanking `::before`/`::after` lines  
**Search bar:** Full-width (max 480px), IBM Plex Mono input, live filter with client count display  
**Region groups:** IBM Plex Mono 0.6rem label with `::after` fade line  
**Client cards:**
- `background: --surface`, `border: 1px solid --border`, `border-radius: 6px`
- Hover: `translateY(-1px)`, border → `--border-bright`
- Selected: `border-color: --accent`, `background: --accent-dim`, `2px` top border in `--accent`
- Card content: Red Hat Display name (0.875rem 600) + IBM Plex Mono city/region + alert count in accent
- Pulsing dot top-right corner

**Sticky CTA:**
- Fixed bottom bar with gradient fade: `linear-gradient(0deg, --bg 70%, transparent)`
- Shows selected client name + primary "Analyze →" button
- Only visible when a client is selected

**Missing functionality added:**
- Search/filter bar (currently absent)
- Empty search result state: "No clients match..." message

---

### 2. IntegrationSummary

**Layout:** 240px left panel + flexible right panel  
**Breadcrumb:** In header, not a separate back button

**Left panel:**
- Client name label (IBM Plex Mono 0.58rem uppercase) + name (Red Hat Display 1.1rem 700)
- 2×3 stats grid: Red Hat Display 800 numeral + IBM Plex Mono label
  - Blind Spots stat: `background: --danger-dim`, `border-color: rgba(239,68,68,0.25)`, danger-colored numeral
- Section divider (IBM Plex Mono eyebrow)
- Action buttons: icon box + title (Red Hat Display 600) + description (IBM Plex Mono 0.58rem dim)
  - Active state: `--accent-dim` background, `--border-bright` border

**Right panel:**
- Header: "Integrations" title + coverage percentage (Red Hat Display 800 in accent)
- Table: sticky header (IBM Plex Mono 0.6rem uppercase labels)
  - Row: integration name (Red Hat Display 600) + type (IBM Plex Mono 0.6rem dim) stacked
  - Status + Coverage badges side by side
  - Alert count right-aligned in IBM Plex Mono
  - Blind spot rows: `background: rgba(239,68,68,0.04)`

**Cache badge:** IBM Plex Mono, indigo accent (`--indigo-dim` background)

**Missing functionality added:**
- Error state: red-accented panel if API fetch fails, with retry button
- Empty state: "No integrations found" with descriptive text

---

### 3. AlertInsights

**Layout:** 260px left panel + flexible right panel

**Left panel — Model bar:**
- Dropdown (IBM Plex Mono 0.68rem) + `↺ Run` button (accent background)
- `border-bottom: 1px solid --border`

**Left panel — Content (min font size: 0.78rem throughout):**
- Section eyebrows: IBM Plex Mono 0.58rem uppercase + `::after` fade line
- Summary: Red Hat Display 0.8rem, `--text-sec`, line-height 1.65
- Priority list: numbered items (IBM Plex Mono accent number + Red Hat Display body), each in a surface card
- Signals: 2×2 grid of pill cards — IBM Plex Mono numeral + label

**Right panel — Tabs:**
- IBM Plex Mono 0.65rem uppercase, `border-bottom: 2px solid` active indicator
- Count badge: `--surface-2` background (inactive), `--accent-dim` background (active)
- 7 tabs: Duplicates · Families · Merge · Coverage · Noise · Unique · Recommendations

**Card type differentiation (left-border color):**
| Type | Left border | Badge color |
|------|-------------|-------------|
| Duplicate | `--indigo` | indigo |
| Noise | `--danger` | red |
| Family | `--accent` | green |
| Merge | `--warn` | amber |
| Coverage gap | `--sky` | sky blue |

**Duplicate card specifics:**
- Alert pair: `Tag ↔ Tag` layout using `--surface-2` mono tags
- Similarity bar: color-gradient progress bar (`--danger` → `--warn` → `--accent` based on score)
- Body: Red Hat Display 0.8rem prose with inline IBM Plex Mono for field names

**Noise card specifics:**
- Missing fields: flex-wrap pill list with `--danger-dim` background tags

**Missing functionality added:**
- Loading state: skeleton shimmer in left panel summary + card area
- Error state: red-accented block with LLM failure message + "Retry with [model]" link
- Empty state per tab: centered icon + title + description

---

### 4. MITREHeatmap

**Layout:** Full-width, header + toolbar + scrollable heatmap columns

**Toolbar (replaces separate summary bar + legend):**
- Left: stat chips — Overall % (accent), Techniques, Uncovered (warn), Sub-techniques — separated by `1px` dividers
- Right: inline legend (color dots + IBM Plex Mono labels) + Heatmap/Graph toggle

**Coverage colors (tactic header top bar + technique cells):**
```
None:    #1e2535   Low:     #7c2d12
Partial: #92400e   Good:    #065f46   Full: #10b981
```

**Tactic columns:**
- `3px` top-border in coverage color
- Header: Red Hat Display 0.72rem 600 name + IBM Plex Mono 0.58rem count (`N/Total covered`)
- Zero-coverage tactic: count in `--danger`
- Technique cells: `26px` height, IBM Plex Mono 0.55rem ID centered

**Detail panel (fixed bottom-right, 320px):**
- IBM Plex Mono technique ID + tactic (0.68rem accent)
- Red Hat Display technique name (0.875rem 600)
- Provider dropdown + Generate button
- Query blocks: `--bg` background, provider label (IBM Plex Mono uppercase accent), query text with field names in accent

**Force graph view:** Unchanged from current behavior, restyled with new color tokens.

**Missing functionality added:**
- Technique hover tooltip (technique name, not just ID)
- Empty detail panel state when nothing is selected
- Error state if query generation fails

---

## Shared Functional States

### Loading / Skeleton
```css
.skeleton {
  background: linear-gradient(90deg, --surface 25%, --surface-2 50%, --surface 75%);
  background-size: 200% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
}
```
Applied in: AlertInsights left panel (3 lines), AlertInsights tab content (2 cards), IntegrationSummary table rows (3 rows)

### Error State
```
background: --danger-dim
border: 1px solid rgba(239,68,68,0.25)
border-radius: 8px; padding: 16px
```
- Icon (⚠), title (Red Hat Display 600 danger), body (IBM Plex Mono 0.68rem), retry link (accent, underlined)

### Empty State
```
display: flex; flex-direction: column; align-items: center;
padding: 48px; gap: 10px; color: --text-dim
```
- Large icon (opacity 0.3), Red Hat Display title (`--text-sec`), IBM Plex Mono body description

---

## Implementation Approach

1. **App.css** — full rewrite, structured as: reset → tokens → base → layout → components (per view section)
2. **App.tsx** — add Framer Motion `AnimatePresence` wrapper + `motion.div` per view
3. **index.html** — add Google Fonts `<link>` for Red Hat Display
4. **package.json** — add `framer-motion`
5. **Component TSX files** — targeted updates for:
   - New classNames per card type (AlertInsights)
   - Skeleton/error/empty state JSX additions
   - Search input + filter logic (ClientSelector)
   - Sticky CTA logic (ClientSelector)
   - Breadcrumb in header: each component renders its own header section; `App.tsx` passes `clientName` and `currentView` as props so each component can build its own breadcrumb string

---

## Out of Scope

- Backend / API changes
- MITREHeatmap force graph logic (restyled only, no structural change)
- Mobile / responsive layout
- Keyboard accessibility / focus states
- New features not present in current UI

---

## Success Criteria

- [ ] All 4 views render with new design system (Red Hat Display + IBM Plex Mono, new color tokens)
- [ ] No monospace text used for multi-sentence prose
- [ ] Font sizes: minimum 0.78rem in all UI text (excluding legend dots / decorative labels)
- [ ] Loading skeletons shown during all async operations
- [ ] Error states shown on API failure with retry action
- [ ] Empty states shown when tabs/views have no data
- [ ] View transitions animated via Framer Motion
- [ ] Card types visually distinct (left-border color differentiation)
- [ ] Similarity bar replaces raw percentage number in duplicate cards

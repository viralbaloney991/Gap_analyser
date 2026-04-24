# Alert Insights UX Improvements — Design Spec
**Date:** 2026-04-24  
**Status:** Approved for implementation

---

## Problem

Three UX gaps in the Alert Insights panel (`AlertInsights.tsx`):

1. **Duplicates** — Cards show both alert names side-by-side but the pair relationship isn't immediately scannable, and there is no expand/collapse — all details (similarity bar, explanation) are always visible, making the list dense.

2. **Families** — The backend's naming strategy (`deriveFamilyName()`) maps the dominant MITRE tactic to a human label (e.g., "Defense Evasion Detections"). Multiple distinct families sharing the same dominant tactic render as 10 separate cards with identical titles, making the list appear broken.

3. **Noise** — Cards show a reason and missing-feature tags but the reason is buried below the fold; users don't understand at a glance why an alert was flagged as noisy.

---

## Solution

Three targeted frontend-only changes to `AlertInsights.tsx` and `App.css`. No backend changes.

### 1. Duplicates — accordion cards

All duplicate cards are **collapsed by default**: title + badge + chevron only. Clicking expands inline to reveal the ↔ alert-name pair tags, similarity bar, and explanation. Clicking again collapses.

### 2. Families — group same-named families

Before rendering, group `data.families` by `name` into a `Map<string, DetectionFamily[]>`. Render one accordion card per unique name. The badge shows `N groups · M alerts` (total alerts across all same-named families). Expanding reveals a flat list of all member alert names from every family in the group.

### 3. Noise — reason visible collapsed

Noise cards are also **collapsed by default**. The collapsed state shows: title + "Noisy" badge + the `reason` string (or `noise_explanations[i]` if LLM enrichment is loaded) as a one-liner subtitle in red. Expanding reveals the full explanation paragraph and missing-feature tags.

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `frontend/src/components/AlertInsights.tsx` | Add `expandedCards` state; rewrite duplicates, families, noise render sections; add family-grouping logic |
| `frontend/src/App.css` | Add `.insight-card--collapsed`, `.insight-card-chevron`, `.noise-reason-preview` styles |

### No backend changes. No new files.

---

## Implementation Detail

### State

Add a single `Set<string>` state to track which cards are expanded. Keys are `"dup-{i}"`, `"fam-{name}"`, `"noise-{i}"`.

```tsx
const [expandedCards, setExpandedCards] = useState<Set<string>>(new Set());

const toggleCard = (key: string) => {
  setExpandedCards(prev => {
    const next = new Set(prev);
    next.has(key) ? next.delete(key) : next.add(key);
    return next;
  });
};
```

### Duplicates section

```tsx
{activeTab === 'duplicates' && (
  data.duplicates?.length ? (
    data.duplicates.map((dup, i) => {
      const key = `dup-${i}`;
      const isOpen = expandedCards.has(key);
      const enrichedExplanation = effectiveReport?.enriched_dups?.[i];
      const simPct = Math.round((dup.similarity ?? 0) * 100);
      return (
        <div
          key={i}
          className={`insight-card insight-card--duplicate${isOpen ? ' insight-card--open' : ''}`}
          onClick={() => toggleCard(key)}
          style={{ cursor: 'pointer' }}
        >
          <div className="insight-card-header">
            <div className="insight-card-title">{dup.alert_names[0]}</div>
            <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <span className="badge badge--indigo">Duplicate</span>
              <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
            </div>
          </div>
          {isOpen && (
            <>
              <div className="alert-pair">
                {dup.alert_names.map((name, j) => (
                  <span key={j}>
                    {j > 0 && <span className="alert-pair-sep">↔</span>}
                    <span className="alert-tag">{name}</span>
                  </span>
                ))}
              </div>
              <div className="sim-bar-wrap">
                <span className="sim-bar-label">{simPct}% similar</span>
                <div className="sim-bar-track">
                  <div
                    className="sim-bar-fill"
                    style={{ width: `${simPct}%`, background: simBarGradient(dup.similarity ?? 0) }}
                  />
                </div>
              </div>
              {(enrichedExplanation || dup.explanation) && (
                <p className="insight-card-body">{enrichedExplanation ?? dup.explanation}</p>
              )}
            </>
          )}
        </div>
      );
    })
  ) : <div className="state-empty">…</div>
)}
```

### Families section — grouping logic

```tsx
// Group families by name before render
const familyGroups = useMemo(() => {
  const map = new Map<string, { families: DetectionFamily[]; totalAlerts: number }>();
  (data.families ?? []).forEach(fam => {
    const existing = map.get(fam.name);
    if (existing) {
      existing.families.push(fam);
      existing.totalAlerts += fam.alert_names.length;
    } else {
      map.set(fam.name, { families: [fam], totalAlerts: fam.alert_names.length });
    }
  });
  return Array.from(map.entries()); // [name, {families, totalAlerts}][]
}, [data.families]);
```

```tsx
{activeTab === 'families' && (
  familyGroups.length ? (
    familyGroups.map(([name, { families, totalAlerts }]) => {
      const key = `fam-${name}`;
      const isOpen = expandedCards.has(key);
      const groupCount = families.length;
      // Flatten all alert names from all same-named families
      const allAlertNames = families.flatMap(f => f.alert_names);
      return (
        <div
          key={name}
          className={`insight-card insight-card--family${isOpen ? ' insight-card--open' : ''}`}
          onClick={() => toggleCard(key)}
          style={{ cursor: 'pointer' }}
        >
          <div className="insight-card-header">
            <div className="insight-card-title">{name}</div>
            <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <span className="badge badge--green">
                {groupCount > 1 ? `${groupCount} groups · ` : ''}{totalAlerts} alerts
              </span>
              <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
            </div>
          </div>
          {isOpen && (
            <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
              {allAlertNames.map((alertName, j) => (
                <span key={j} className="alert-tag">{alertName}</span>
              ))}
            </div>
          )}
        </div>
      );
    })
  ) : <div className="state-empty">…</div>
)}
```

### Noise section

```tsx
{activeTab === 'noise' && (
  data.noise_alerts?.length ? (
    data.noise_alerts.map((noise: NoiseAlert, i) => {
      const key = `noise-${i}`;
      const isOpen = expandedCards.has(key);
      const explanation = effectiveReport?.noise_explanations?.[i];
      const reasonPreview = noise.reason ?? '';
      return (
        <div
          key={i}
          className={`insight-card insight-card--noise${isOpen ? ' insight-card--open' : ''}`}
          onClick={() => toggleCard(key)}
          style={{ cursor: 'pointer' }}
        >
          <div className="insight-card-header">
            <div className="insight-card-title">{noise.name}</div>
            <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
              <span className="badge badge--red">Noisy</span>
              <span className="insight-card-chevron">{isOpen ? '▼' : '▶'}</span>
            </div>
          </div>
          {reasonPreview && (
            <p className="noise-reason-preview">{reasonPreview}</p>
          )}
          {isOpen && (
            <>
              {explanation && (
                <p className="insight-card-body">{explanation}</p>
              )}
              {noise.missing_features?.length > 0 && (
                <div className="missing-features">
                  {noise.missing_features.map((feat, j) => (
                    <span key={j} className="missing-tag">{feat}</span>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      );
    })
  ) : <div className="state-empty">…</div>
)}
```

### New CSS rules (App.css)

```css
.insight-card-chevron { font-size: 0.6rem; color: var(--text-dim); flex-shrink: 0; }
.insight-card--open   { border-color: var(--border-bright); }
.noise-reason-preview { font-family: var(--font-mono); font-size: 0.65rem; color: var(--red, #f87171); margin: 4px 0 0; line-height: 1.4; }
```

---

## Behaviour after fix

| Section | Before | After |
|---------|--------|-------|
| Duplicates — list density | All cards fully expanded, dense | Collapsed by default; click to see pair + bar + explanation |
| Families — 10× "Defense Evasion Detections" | 10 separate identical-title cards | 1 grouped card: "Defense Evasion Detections — 10 groups · 34 alerts" |
| Families — member visibility | Alert name tags always visible | Collapsed by default; expand to see all member alert names flat-listed |
| Noise — reason visibility | Reason buried inside card body | Reason visible collapsed as red subtitle; expand for full explanation + tags |

---

## Out of Scope

- Backend naming changes to `deriveFamilyName()`
- Clicking alert tags to navigate/highlight in alerts table
- Changes to any other tab (Merge, Coverage, Unique, Recommendations)
- Changes to the similarity engine or enrichment logic

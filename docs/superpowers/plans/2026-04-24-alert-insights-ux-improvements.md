# Alert Insights UX Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add accordion expand/collapse to duplicates, families (with same-name grouping), and noise cards in the Alert Insights panel so each section is scannable and the reason for noise is always visible.

**Architecture:** All changes are in `AlertInsights.tsx` and `App.css`. A single `expandedCards: Set<string>` state drives open/closed for all three sections. Families are pre-grouped by name via `useMemo` before rendering so 10 same-named families collapse to 1 card. No backend changes.

**Tech Stack:** React 19, TypeScript, CSS custom properties. No new packages.

---

## Files

| File | Action |
|------|--------|
| `frontend/src/components/AlertInsights.tsx` | Add `useMemo` import; add `DetectionFamily` type import; add `expandedCards` state + `toggleCard` + `familyGroups` memo; rewrite duplicates, families, noise sections |
| `frontend/src/App.css` | Add `.insight-card-chevron`, `.insight-card--open`, `.noise-reason-preview` CSS rules |

---

## Task 1 — CSS rules and shared state foundation

**Files:**
- Modify: `frontend/src/App.css` (after line 622)
- Modify: `frontend/src/components/AlertInsights.tsx` (lines 1–2, 26)

**Context:** This task establishes the shared building blocks needed by Tasks 2–4. The CSS adds the chevron style, open-state border highlight, and noise reason preview colour. The state adds `expandedCards` (a `Set<string>`) and `toggleCard` (the toggle function). The `familyGroups` memo pre-groups families by name.

- [ ] **Step 1: Add new CSS rules to `App.css`**

Find line 622 in `frontend/src/App.css`:
```css
.insight-card--coverage  { border-left: 3px solid var(--sky);    }
```

Insert these three lines immediately after it:
```css
.insight-card--open      { border-color: var(--border-bright); }
.insight-card-chevron    { font-size: 0.6rem; color: var(--text-dim); flex-shrink: 0; }
.noise-reason-preview    { font-family: var(--font-mono); font-size: 0.65rem; color: #f87171; margin: 4px 0 0; line-height: 1.4; }
```

- [ ] **Step 2: Add `useMemo` to the React import in `AlertInsights.tsx`**

Find line 1 in `frontend/src/components/AlertInsights.tsx`:
```tsx
import { useState, useEffect } from 'react';
```

Replace with:
```tsx
import { useState, useEffect, useMemo } from 'react';
```

- [ ] **Step 3: Add `DetectionFamily` to the types import**

Find line 2 in `frontend/src/components/AlertInsights.tsx`:
```tsx
import type { SimilarityResult, InsightsReport, NoiseAlert } from '../types';
```

Replace with:
```tsx
import type { SimilarityResult, InsightsReport, NoiseAlert, DetectionFamily } from '../types';
```

- [ ] **Step 4: Add `expandedCards` state, `toggleCard`, and `familyGroups` memo**

Find lines 22–26 in `frontend/src/components/AlertInsights.tsx`:
```tsx
  const [activeTab, setActiveTab]         = useState<Tab>('duplicates');
  const [localReport, setLocalReport]     = useState<InsightsReport | null>(report);
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [regenError, setRegenError]       = useState(false);
```

Replace with:
```tsx
  const [activeTab, setActiveTab]         = useState<Tab>('duplicates');
  const [localReport, setLocalReport]     = useState<InsightsReport | null>(report);
  const [selectedModel, setSelectedModel] = useState<'mistral' | 'gemma'>('mistral');
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [regenError, setRegenError]       = useState(false);
  const [expandedCards, setExpandedCards] = useState<Set<string>>(new Set());

  const toggleCard = (key: string) => {
    setExpandedCards(prev => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  };

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
    return Array.from(map.entries());
  }, [data.families]);
```

- [ ] **Step 5: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output (no errors).

- [ ] **Step 6: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/App.css src/components/AlertInsights.tsx
git commit -m "feat(insights): add expandedCards state, toggleCard, familyGroups memo + CSS foundation"
```

---

## Task 2 — Duplicates accordion

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx` (lines 191–234)

**Context:** Replace the always-expanded duplicate cards with accordion cards. Collapsed state shows title + "Duplicate" badge + chevron. Expanded state shows the ↔ pair tags, similarity bar, and explanation. Card key is `"dup-{i}"`.

- [ ] **Step 1: Replace the duplicates render section**

Find this block (lines 191–234) in `frontend/src/components/AlertInsights.tsx`:
```tsx
          {/* ── DUPLICATES ── */}
          {activeTab === 'duplicates' && (
            data.duplicates?.length ? (
              data.duplicates.map((dup, i) => {
                // enriched_dups is string[] — use as plain string explanation
                const enrichedExplanation = effectiveReport?.enriched_dups?.[i];
                const simPct = Math.round((dup.similarity ?? 0) * 100);
                return (
                  <div key={i} className="insight-card insight-card--duplicate">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{dup.alert_names[0]}</div>
                      <span className="badge badge--indigo">Duplicate</span>
                    </div>
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
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No duplicates found</div>
                <div className="state-empty__body">All alerts have sufficiently distinct detection logic.</div>
              </div>
            )
          )}
```

Replace with:
```tsx
          {/* ── DUPLICATES ── */}
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
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No duplicates found</div>
                <div className="state-empty__body">All alerts have sufficiently distinct detection logic.</div>
              </div>
            )
          )}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/AlertInsights.tsx
git commit -m "feat(insights): duplicates accordion — collapsed by default, expand to see pair + similarity"
```

---

## Task 3 — Families grouping + accordion

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx` (lines 236–259, shifted by Task 2 additions)

**Context:** Replace the flat `data.families.map(...)` render with `familyGroups.map(...)`. Each unique family name renders as one accordion card. The badge shows `N groups · M alerts` when multiple same-named families exist, or just `M alerts` when unique. Expanding reveals a flat list of all member alert names across all families in the group. Card key is `"fam-{name}"`.

- [ ] **Step 1: Replace the families render section**

Find this block in `frontend/src/components/AlertInsights.tsx` (search for `{/* ── FAMILIES ── */}`):
```tsx
          {/* ── FAMILIES ── */}
          {activeTab === 'families' && (
            data.families?.length ? (
              data.families.map((fam, i) => (
                <div key={i} className="insight-card insight-card--family">
                  <div className="insight-card-header">
                    <div className="insight-card-title">{fam.name}</div>
                    <span className="badge badge--green">{fam.alert_ids.length} alerts</span>
                  </div>
                  <div className="alert-pair" style={{ flexDirection: 'column', alignItems: 'flex-start', gap: 4 }}>
                    {fam.alert_names.map((name, j) => (
                      <span key={j} className="alert-tag">{name}</span>
                    ))}
                  </div>
                </div>
              ))
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No families found</div>
                <div className="state-empty__body">No alert groupings detected in this client's stack.</div>
              </div>
            )
          )}
```

Replace with:
```tsx
          {/* ── FAMILIES ── */}
          {activeTab === 'families' && (
            familyGroups.length ? (
              familyGroups.map(([name, { families, totalAlerts }]) => {
                const key = `fam-${name}`;
                const isOpen = expandedCards.has(key);
                const groupCount = families.length;
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
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No families found</div>
                <div className="state-empty__body">No alert groupings detected in this client's stack.</div>
              </div>
            )
          )}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/AlertInsights.tsx
git commit -m "feat(insights): families grouping — collapse same-named families into one accordion card"
```

---

## Task 4 — Noise accordion with reason preview

**Files:**
- Modify: `frontend/src/components/AlertInsights.tsx` (noise section, search for `{/* ── NOISE ── */}`)

**Context:** Replace the always-expanded noise cards with accordion cards. The `reason` string (or LLM `noise_explanations[i]`) is always visible as a red one-liner subtitle even when collapsed. Expanding reveals the full explanation paragraph and missing-feature tags. Card key is `"noise-{i}"`.

- [ ] **Step 1: Replace the noise render section**

Find this block in `frontend/src/components/AlertInsights.tsx` (search for `{/* ── NOISE ── */}`):
```tsx
          {/* ── NOISE ── */}
          {activeTab === 'noise' && (
            data.noise_alerts?.length ? (
              data.noise_alerts.map((noise: NoiseAlert, i) => {
                // noise_explanations is string[] — use as plain string
                const explanation = effectiveReport?.noise_explanations?.[i];
                return (
                  <div key={i} className="insight-card insight-card--noise">
                    <div className="insight-card-header">
                      <div className="insight-card-title">{noise.name}</div>
                      <span className="badge badge--red">Noisy</span>
                    </div>
                    {(explanation || noise.reason) && (
                      <p className="insight-card-body">{explanation ?? noise.reason}</p>
                    )}
                    {noise.missing_features?.length > 0 && (
                      <div className="missing-features">
                        {noise.missing_features.map((feat, j) => (
                          <span key={j} className="missing-tag">{feat}</span>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No noisy alerts</div>
                <div className="state-empty__body">All alerts have sufficient field coverage for reliable detection.</div>
              </div>
            )
          )}
```

Replace with:
```tsx
          {/* ── NOISE ── */}
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
            ) : (
              <div className="state-empty">
                <div className="state-empty__icon">◎</div>
                <div className="state-empty__title">No noisy alerts</div>
                <div className="state-empty__body">All alerts have sufficient field coverage for reliable detection.</div>
              </div>
            )
          )}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend && npx tsc --noEmit
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd /Users/aviral.baloni/Desktop/claude/frontend
git add src/components/AlertInsights.tsx
git commit -m "feat(insights): noise accordion — reason always visible collapsed, expand for details + tags"
```

---

## Self-Review

**Spec coverage:**
- ✅ Duplicates accordion (collapsed by default, expand to see pair + bar + explanation) → Task 2
- ✅ Families same-name grouping (1 card per unique name, badge shows group count) → Task 3
- ✅ Families accordion (expand to see all member alert names flat-listed) → Task 3
- ✅ Noise reason visible collapsed (`.noise-reason-preview`) → Task 4
- ✅ Noise accordion (expand for full explanation + missing-feature tags) → Task 4
- ✅ Shared `expandedCards` state + `toggleCard` → Task 1
- ✅ `familyGroups` memo with `useMemo` → Task 1
- ✅ CSS foundation (`.insight-card--open`, `.insight-card-chevron`, `.insight-card-noise-preview`) → Task 1

**Placeholder scan:** No TBDs. All JSX is complete and exact. All CSS is explicit.

**Type consistency:**
- `expandedCards: Set<string>` → used as `expandedCards.has(key)` → consistent
- `toggleCard(key: string)` → called with `"dup-{i}"`, `"fam-{name}"`, `"noise-{i}"` → consistent
- `familyGroups: [string, { families: DetectionFamily[]; totalAlerts: number }][]` → destructured as `[name, { families, totalAlerts }]` → consistent
- `allAlertNames = families.flatMap(f => f.alert_names)` → `f.alert_names: string[]` from `DetectionFamily` → consistent

---
name: detection-engineering
description: Use when writing, reviewing, or improving SIEM detection rules, alert queries, Sigma rules, or MITRE technique coverage. Enforces canonical template, naming, severity, correlation windows, ECS field paths, and false-positive documentation.
---

# Detection Engineering Standards

## Canonical Detection Template

Every detection must conform to this template:

```yaml
title: "<Verb> <Subject> via <Method>"
status: experimental          # experimental | test | stable
logsource:
  product: "<vendor-slug>"    # windows | okta | crowdstrike-falcon | etc.
  service: "<service>"        # security | sysmon | system | etc.
detection:
  selection:
    <ecs.field.path>: "<value>"
  condition: selection
falsepositives:
  - "<at least one realistic FP scenario>"
level: "<critical|high|medium|low>"
tags:
  - attack.<tactic>
  - attack.<technique-id>
```

Three additional JSON fields accompany every detection (NOT inside the Sigma YAML):

| Field | Purpose |
|-------|---------|
| `window` | Correlation time window (e.g. `5m`) |
| `window_reason` | One sentence explaining the window choice |
| `lucene_query` | ECS-based Lucene translation of the Sigma condition |

---

## House Style Rules

### Naming

Pattern: `<Verb> <Subject> via <Method>`

- Correct: `Detect Credential Dump via LSASS Memory Access`
- Correct: `Detect Lateral Movement via Pass-the-Hash`
- Wrong: `Windows - Suspicious LSASS Access`
- Wrong: `Stage 1: Initial Compromise`

### Severity

| Label | When to use |
|-------|-------------|
| `critical` | Attacker-controlled code execution or confirmed data exfil |
| `high` | Privilege escalation, credential theft, lateral movement |
| `medium` | Discovery, persistence, suspicious but ambiguous behaviour |
| `low` | Informational, baseline anomaly, no confirmed impact |

### Correlation Windows

| Stage | Window |
|-------|--------|
| Initial access / execution | `1m` |
| Persistence / privilege escalation | `5m` |
| Discovery / collection | `15m` |
| Lateral movement / C2 | `30m` |

### ECS Field Discipline

All field references in `detection` blocks and `lucene_query` must use ECS paths:
- `process.name`, `process.command_line`, `process.parent.name`
- `registry.path`, `registry.value`
- `file.path`, `file.name`
- `source.ip`, `destination.ip`, `destination.port`
- `user.name`, `user.domain`

Product-specific field names (e.g. `CommandLine`, `TargetFilename`) are not permitted. The `logsource.product` field handles vendor translation.

### Lucene Translation Rule

Every Sigma `condition` block must have a corresponding `lucene_query`. No orphaned Sigma blocks.

---

## Quality Gates

Before accepting any detection output, verify:

1. **Field completeness** — `title`, `sigma_rule`, `lucene_query`, `window`, `window_reason`, `falsepositives`, `severity` must all be present and non-empty.
2. **Lucene parity** — `lucene_query` must be non-empty whenever `sigma_rule` is present.
3. **FP list non-empty** — `falsepositives` array must contain at least one realistic entry.

---

## Reviewing Detections

When asked to review a detection rule:
1. Check naming follows `<Verb> <Subject> via <Method>`
2. Verify ECS field paths (not vendor-specific names)
3. Confirm `falsepositives` has at least one realistic scenario
4. Check `level` aligns with severity table above
5. Verify `lucene_query` faithfully translates the Sigma `condition`
6. Check `window` matches the kill-chain stage table above

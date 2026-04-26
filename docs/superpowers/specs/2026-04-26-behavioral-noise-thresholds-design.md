# Behavioral Noise Threshold Tuning

**Date:** 2026-04-26
**Status:** Approved

## Summary

Widen the event-count window from 30 to 45 days and lower the trigger threshold from 20 to 15. An alert firing more than 15 times is considered noisy regardless of client; the wider window catches slow-burning but consistently noisy alerts.

## Changes

| Location | Before | After |
|----------|--------|-------|
| `engine.go` `behavioralNoiseThreshold` | `20` | `15` |
| `engine.go` noise reason string | `"last 30 days"` | `"last 45 days"` |
| `engine.go` constant comment | `triggers in 30 days` | `triggers in 45 days` |
| `handlers.go` `FetchAlertEventCounts` call | `30` | `45` |
| `handlers.go` comment | `30-day trigger counts` | `45-day trigger counts` |

## Scope

Global constants — applies uniformly across all clients. No per-client configurability (YAGNI). No API, model, or frontend changes.

## Tests

Any existing test asserting `triggerCount > 20` boundary must be updated to `> 15`.

# Audit: reporter

**Date**: 2026-03-29
**Scope**: `primordium/app/reporter/` — Sentry SDK wrapper

## Summary

Two files (116 lines total) providing a thin abstraction over the Sentry SDK:
- `Initialize(conf)` — configures the Sentry client
- `Shutdown()` — flushes buffered events
- `CaptureException`, `CaptureMessage`, `CaptureEvent` — event capture
- `EventID`, `Event` — type aliases for `sentry.EventID`, `sentry.Event`

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `sentry.go` | 98 | Sentry SDK wrapper |

## Findings

### I1: No test coverage

Zero test files exist.

## API Surface

| Symbol | Consumers | Used |
|---|---|---|
| `Initialize` | enoki (2 call sites) | Yes |
| `Shutdown` | enoki (3 call sites) | Yes |
| `CaptureException` | none | No |
| `CaptureMessage` | none | No |
| `CaptureEvent` | none | No |
| `EventID` (type alias) | none | No |
| `Event` (type alias) | none | No |
| `Config` | enoki (2 call sites) | Yes |

## Open Counts

| Severity | Count |
|---|---|
| INFORMATIONAL | 3 (I1: no tests, I2: flush result discarded, I3: undocumented options) |

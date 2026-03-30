# Audit: filesystem/umask

**Date**: 2026-03-29
**Scope**: `primordium/filesystem/umask` — cross-platform umask manipulation

## Summary

Tiny package (5 files, ~60 lines of production code) providing Get/Set for the
process umask with platform abstraction. Unix delegates to `syscall.Umask`;
Windows is a no-op returning 0. No tests.

The package deliberately zeroes the OS umask on first `Get()` call so the
application receives exact file permissions without silent stripping. The
captured original value is stored internally as a hardened default that
security-sensitive paths (e.g. `filesystem.WriteFile`) apply manually.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 43 | Package comment (design rationale, API contract) |
| `const.go` | 19 | `defaultUmask = 0o077` |
| `umask.go` | 61 | `Get()` and `Set()` API |
| `umask_unix.go` | 27 | `syscall.Umask` wrapper |
| `umask_windows.go` | 23 | No-op stub (returns 0) |

## API Surface

| Function | Description |
|---|---|
| `Get()` | Zeroes OS umask (once), returns internal mask |
| `Set(mask)` | Updates internal mask (and OS umask if changed) |

Internal: `atomic.go` uses `umask.Get()` to apply the mask manually on atomic writes.

## Findings

### L1: No tests

Zero test coverage for the entire package.

## Test Coverage

| Function | Tested | Notes |
|---|---|---|
| `Get` | **No** | |
| `Set` | **No** | |

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 1 |
| INFORMATIONAL | 0 |

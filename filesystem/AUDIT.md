# Audit: filesystem (root)

**Date**: 2026-03-29
**Scope**: `primordium/filesystem/` root-level files — `atomic.go`, `consts.go`,
`init.go`, `doc.go`, `atomic_test.go`

## Summary

Four production files + one test file (~165 lines production, ~309 lines test)
providing three public APIs:
- `Initialize(appName)` — one-time setup: zeroes OS umask, sets app name for
  directory resolution
- `WriteFile(filename, data, perm)` — atomic write via temp-file-then-rename
- Permission constants (`FilePermissionsDefault`, etc.)

`WriteFile` is adapted from containerd's `ioutils.go` (Apache 2.0).

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `consts.go` | 29 | Permission constants (0o644, 0o755, 0o600, 0o700) |
| `init.go` | 29 | `Initialize()`: umask + dirs setup |
| `atomic.go` | 89 | `WriteFile()`: atomic write with umask application |
| `atomic_test.go` | 309 | 8 tests: basic, overwrite, empty, permissions, atomicity, nonexistent dir, temp leak, large data |

## Findings

No open findings.

## API Surface

| Function | Consumers | Correct |
|---|---|---|
| `Initialize` | `app.New()` | Yes |
| `WriteFile` | ~21 files (store, hypha, cc0, etc.) | Yes |
| `FilePermissionsDefault` | ~14 uses | Yes |
| `DirPermissionsDefault` | ~8 uses | Yes |
| `FilePermissionsPrivate` | ~18 uses | Yes |
| `DirPermissionsPrivate` | ~3 uses | Yes |

## Test Coverage

| Function | Tested | Notes |
|---|---|---|
| `WriteFile` | Yes | Basic write, overwrite, empty data, permissions, atomicity (inode change), nonexistent dir error, temp file cleanup on success/failure, large data (1 MiB) |
| `Initialize` | **No** | Called implicitly by app startup; no direct unit test |

## Open Counts

None.

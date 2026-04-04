# Audit: filesystem/xos

**Date**: 2026-03-29
**Scope**: `primordium/filesystem/xos` — cross-platform file operations with `FILE_SHARE_DELETE` on Windows

## Summary

The package reimplements Go's `os.Open`/`os.OpenFile` on Windows using
`windows.CreateFile` directly, adding `FILE_SHARE_DELETE` to the share mode.
On Unix, all functions are thin passthroughs to `os`. Additional wrappers
(`ReadFile`, `ReadDir`, `Stat`, `Create`, `WriteFile`, `Truncate`,
`CreateTemp`, `MkdirTemp`) build on top of the core `Open`/`OpenFile`.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 19 | Package comment |
| `file_unix.go` | 47 | Unix: passthrough to `os.Open`, `os.OpenFile`, `os.Truncate` |
| `file_windows.go` | 242 | Windows: `CreateFile` with `FILE_SHARE_DELETE` |
| `create.go` | 153 | Cross-platform: `ReadFile`, `ReadDir`, `Stat`, `Create`, `WriteFile` |
| `tempfile.go` | 162 | Cross-platform: `CreateTemp`, `MkdirTemp`, helpers |

## Findings

### H3: appendMode not set on *os.File — INHERENT LIMITATION (Windows only)

**Platform**: Windows only. On Unix, `xos.OpenFile` delegates to `os.OpenFile`,
which sets `appendMode` normally — no deviation.

**What differs**: On Windows, `xos.OpenFile` uses `windows.CreateFile` then
`os.NewFile` to wrap the handle. `os.NewFile` does not set the internal
`appendMode` field. As a result, calling `(*File).WriteAt` on an `O_APPEND`
handle silently attempts the positional write instead of returning
`os.ErrPermission` as stdlib's `os.OpenFile` does.

**What is unaffected**: Regular `Write` calls on `O_APPEND` files work
identically to stdlib. The kernel enforces append semantics via
`FILE_APPEND_DATA` at the handle level, so `Write` always appends correctly.

**Real-world impact**: Extremely narrow. A caller would need to open a file
with `O_APPEND` via `xos.OpenFile` on Windows, then call `WriteAt` (positional
write) on that handle — contradictory usage that the stdlib guards against as
a safety check.

**Why unfixable**: `appendMode` is an unexported field on `os.file`. There is
no public API to set it from outside the `os` package. `unsafe` or
`go:linkname` approaches would break across Go versions due to struct layout
changes. This is an inherent limitation of any code that uses `os.NewFile`
instead of the internal `openFileNolog`.

Documented in code comment at `file_windows.go:120-124`.

### I2: nextRandom uses math/rand/v2

Uses `math/rand/v2.Uint64()` instead of `runtime_rand()`. Both produce quality
randomness in Go 1.22+ (ChaCha8 seeded from `runtime.rand`). Using
`runtime_rand` would require `go:linkname` into the runtime, which is fragile.
Performance difference only, no behavioral difference.

## API Surface

| Function | Platform | Stdlib equivalent | Correct |
|---|---|---|---|
| `Open` | Cross | `os.Open` | Yes |
| `OpenFile` | Cross | `os.OpenFile` | Yes (H3 limitation documented) |
| `Create` | Cross | `os.Create` | Yes |
| `ReadFile` | Cross | `os.ReadFile` | Yes |
| `WriteFile` | Cross | `os.WriteFile` | Yes |
| `ReadDir` | Cross | `os.ReadDir` | Yes |
| `Stat` | Cross | `os.Stat` | Yes (delegates to os.Stat) |
| `Truncate` | Cross | `os.Truncate` | Yes |
| `CreateTemp` | Cross | `os.CreateTemp` | Yes |
| `MkdirTemp` | Cross | `os.MkdirTemp` | Yes |

## Coverage Gaps

Functions in `os` that open file handles on Windows (via `syscall.Open` →
`CreateFile` with `FILE_SHARE_READ | FILE_SHARE_WRITE` but **no
`FILE_SHARE_DELETE`**) and are not wrapped by xos.

### Lstat — worth adding

`xos.Stat` delegates to `os.Stat`, which on Windows uses `GetFileAttributesEx`
(no handle) with fallbacks. `Lstat` must return info about the symlink itself,
which on Windows requires `CreateFile` with `FILE_FLAG_OPEN_REPARSE_POINT` to
avoid following the symlink. Cannot reuse the open-then-stat approach.

**Impact**: Any caller that needs symlink metadata on Windows while another
process holds a delete-pending handle on the target will fail with a sharing
violation.

### Getwd — not worth wrapping

Opens `.` as a directory handle. Transient, immediately closed.

**Impact**: Negligible. The handle is held for microseconds during a
`GetCurrentDirectory` equivalent. A sharing violation here would require
another process to have a delete-pending handle on the current working
directory itself — an unlikely scenario.

### CopyFS — not worth wrapping

Complex internal logic that opens destination files. Callers can use
`xos.OpenFile` to implement their own copy loop.

**Impact**: Callers doing bulk filesystem copies on Windows would need to avoid
`os.CopyFS` and use xos equivalents directly. This is a usage pattern issue,
not a bug — `os.CopyFS` was never part of xos's API surface.

### RemoveAll — not worth wrapping

Opens parent directories internally for recursive `unlinkat` removal. Would
require reimplementing significant stdlib internals.

**Impact**: On Windows, `os.RemoveAll` can fail with sharing violations on
directories that have delete-pending handles. This is a known Windows pain
point but is orthogonal to xos — `RemoveAll` opens directories, not the files
being deleted.

### Pipe — not applicable

Uses `CreateNamedPipe` + `CreateFile` for pipe handles, not file paths. Not
relevant to `FILE_SHARE_DELETE` concerns.

### DirFS — not worth wrapping

`os.DirFS` returns a `dirFS` type whose `Open` and `ReadFile` methods call
`os.Open`. Wrapping would require a full `xos.DirFS` type. Callers can use
`xos.Open` directly.

**Impact**: Code using `os.DirFS` on Windows will open file handles without
`FILE_SHARE_DELETE`. Callers needing delete-tolerant directory traversal should
use `xos.Open`/`xos.ReadDir` instead.

### Root — not worth wrapping

`os.Root` (Go 1.24+) has its own `syscall.Open` call path in
`rootOpenFileNolog`. Would require significant reimplementation of the
root-scoped open logic.

**Impact**: Code using `os.OpenRoot` on Windows will open handles without
`FILE_SHARE_DELETE`. Root-scoped file access is primarily a security feature
(preventing path traversal); callers needing both root-scoping and
delete-tolerance would need a custom solution.

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 1 (inherent limitation, Windows only) |
| INFORMATIONAL | 1 (accepted) |

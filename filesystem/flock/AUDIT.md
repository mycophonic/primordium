# Audit: filesystem/flock

**Date**: 2026-03-29
**Scope**: `primordium/filesystem/flock` — advisory file locking, platform-independent

## Summary

Minimal, well-structured package providing platform-independent advisory file locking.
Derived from Go's internal `cmd/go/internal/lockedfile/internal/filelock`.
Unix uses `syscall.Flock`; Windows uses `windows.LockFileEx` with sidecar `.lock` files
(since Windows cannot lock directories directly).

6 source files, ~280 lines of production code, ~330 lines of tests.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 20 | Package comment |
| `lock.go` | 137 | Platform-independent API |
| `lock_unix.go` | 108 | Unix flock implementation |
| `lock_windows.go` | 111 | Windows LockFileEx implementation |
| `errors.go` | 31 | 4 sentinel errors |
| `lock_test.go` | 327 | Concurrency correctness tests (8 test functions) |

## API Surface

| Function | Description |
|---|---|
| `Lock(path)` | Blocking exclusive lock |
| `ReadOnlyLock(path)` | Blocking shared lock |
| `TryLock(path)` | Non-blocking exclusive lock |
| `TryReadOnlyLock(path)` | Non-blocking shared lock |
| `Unlock(*os.File)` | Release lock and close file |
| `WithLock(path, func)` | Scoped exclusive lock |
| `WithReadOnlyLock(path, func)` | Scoped shared lock |
| `Cleanup(path)` | Remove Windows sidecar `.lock` file (no-op on Unix) |

## Sentinels

| Sentinel | Purpose |
|---|---|
| `ErrLockFail` | Lock acquisition failed |
| `ErrLockWouldBlock` | Non-blocking lock would block |
| `ErrUnlockFail` | Unlock failed |
| `ErrLockIsNil` | Unlock called with nil |

Sentinels are standalone — callers (store/cache.go, store/content_store.go) wrap them with
`fault.ErrFilesystemFailure` at the call site. This is a reasonable design for a low-level utility.

## Findings

### I1: Tests use `!race` and time.Sleep synchronization

`lock_test.go:1` — `//go:build !race` excludes tests from race detection. Tests use
`time.Sleep` for goroutine coordination. Both are documented as intentional — filesystem
lock semantics make this difficult to avoid. The shared `concurrentKey` variable is
deliberately unprotected to prove the lock provides mutual exclusion.

## Test Coverage

| Function | Tested | Notes |
|---|---|---|
| `Lock` | Yes | Basic lock/unlock, nonexistent path |
| `ReadOnlyLock` | Yes | Concurrent reads, write-blocks-read |
| `TryLock` | Yes | ErrLockWouldBlock, success, nonexistent path |
| `TryReadOnlyLock` | Yes | Blocked by exclusive, shared compatibility |
| `Unlock` | Yes | Via Lock tests, nil returns ErrLockIsNil |
| `WithLock` | Yes | Concurrent write exclusion |
| `WithReadOnlyLock` | Yes | Multi-read, write waits |
| `Cleanup` | **No** | No-op on Unix, rm on Windows |

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 0 |
| INFORMATIONAL | 1 |

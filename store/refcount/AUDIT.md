# Audit: `store/refcount` package

**Date:** 2026-03-11
**Scope:** `github.com/mycophonic/primordium/store/refcount`
**Files:** doc.go, refcounted.go, refcounted_test.go, refcounted_unix_test.go

---

## 1. Package purpose

Cross-process reference-counted coordination for keyed filesystem resources. Uses `flock` for process-safe locking, with automatic cleanup when the last holder releases. Crash-resistant: OS releases file locks when a process dies.

Single consumer: `store/volatile.Volatile` (content-addressed ephemeral storage). Indirectly used by `store/stores.go` (global singleton) and quark.

---

## 2. API fitness

### 2.1 Factory is always called — idempotency is the caller's responsibility

`Acquire` calls factory on every acquisition, even if the resource already exists. The factory must check for existence itself (as `volatile.go:68` does with `xos.Stat`). This is documented and is the right tradeoff — the locker doesn't know what "exists" means for an arbitrary resource.

### 2.2 `NewLocker` panics on invalid path — consistent with project patterns

Matches the convention used elsewhere in the codebase (e.g., `pathcheck.Validate` failures are programming errors, not runtime errors).

---

## 3. Organization

- `doc.go`: Package documentation
- `refcounted.go`: Full implementation (Locker, Acquire, buildRelease, touchLockFile)
- `refcounted_test.go`: 10 tests (9 portable + 1 unix-only)

Single-file implementation for a focused package. Appropriate.

---

## 4. Test coverage

| Test                                         | What it covers                                                    |
|----------------------------------------------|-------------------------------------------------------------------|
| `TestLocker_InvalidKey`                      | Path traversal rejection, `ErrInvalidArgument` sentinel           |
| `TestLocker_ConcurrentAcquireSameKey`        | 20 goroutines, same key, factory call count asserted              |
| `TestLocker_ReleaseWhenLastHolder`           | Cleanup called, directory removed                                 |
| `TestLocker_MultipleHoldersPreventsCleanup`  | First release doesn't clean up, second does, exactly 1 cleanup    |
| `TestLocker_FactoryError`                    | Error propagation from factory                                    |
| `TestLocker_DoubleReleaseIsIdempotent`       | Second release is no-op, cleanup runs exactly once                |
| `TestLocker_OnlyLastReleaserCleanupRuns`     | Only last releaser's cleanup fires, creator's is discarded        |
| `TestLocker_ConcurrentAcquireAndRelease`     | Rapid acquire/release interleaving, directory cleaned up          |
| `TestLocker_StressConcurrentKeys`            | 10 keys x 5 goroutines, data integrity                           |
| `TestNewLocker_PanicsOnInvalidPath`          | Panic on path traversal (unix-only)                               |

---

## 5. Summary

| Area           | Rating | Notes                                                                     |
|----------------|--------|---------------------------------------------------------------------------|
| Correctness    | Good   | Locking protocol is sound; release is idempotent via `sync.Once`          |
| API fitness    | Good   | Clean factory pattern; idempotency and cleanup contracts documented       |
| Organization   | Good   | Single-file, focused implementation                                       |
| Test coverage  | Good   | 10 tests covering concurrency, cleanup, double-release, interleaving     |
| Security       | Good   | Path traversal blocked via `pathcheck`; private permissions on dirs/files |

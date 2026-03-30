# Audit: `store/volatile` package

**Date:** 2026-03-29 (updated from 2026-03-11)
**Scope:** `github.com/mycophonic/primordium/store/volatile`
**Files:** doc.go, volatile.go, volatile_test.go

---

## 1. Package purpose

Content-addressed ephemeral storage with file-based reference counting. Hashes content with a pluggable algorithm, uses the hex digest as the refcount key, writes the data file on first acquire, and delegates all locking/cleanup to `refcount.Locker`.

Thin wrapper — 77 lines of implementation. Single consumer: `store/stores.go` (global singleton with BLAKE2b256), surfaced to hypha via `app/hypha/pkg/core/cache/fs.go`.

---

## 2. Correctness

No issues. `filesystem.WriteFile` uses tmp-file + rename (truly atomic). Stat errors are propagated.

---

## 3. API fitness

### 3.1 `[]byte`-only input — appropriate for current use

`Acquire` takes `[]byte`, requiring the entire content in memory. No streaming API. The sole consumer (`hypha/cache/fs.go`) already has content in memory, so this is the right tradeoff.

### 3.2 Algorithm at construction — clean

Fixed at `New` time. All Acquires on the same store use the same hash. `stores.go` uses BLAKE2b256.

---

## 4. Organization

- `doc.go`: Package documentation
- `volatile.go`: Full implementation (Volatile, New, Acquire)
- `volatile_test.go`: 10 tests

Single-file implementation for a thin wrapper. Appropriate.

---

## 5. Test coverage

| Test                                          | What it covers                                                        |
|-----------------------------------------------|-----------------------------------------------------------------------|
| `TestVolatile_ConcurrentAcquire`              | 100 goroutines, same content, parallel execution, content, cleanup    |
| `TestVolatile_ConcurrentAcquireDifferentContent` | 50 goroutines, different content, unique paths, cleanup            |
| `TestVolatile_StaggeredRelease`               | 20 goroutines, staggered release, premature cleanup detection         |
| `TestVolatile_RapidAcquireRelease`            | 50 goroutines × 10 cycles × 5 contents, content verification         |
| `TestVolatile_AcquireAfterFullRelease`        | Reacquisition after full release, same path, content correct          |
| `TestVolatile_EmptyContent`                   | Empty byte slice, file exists and is empty                            |
| `TestVolatile_LargeContent`                   | 1MB content, byte-level verification                                  |
| `TestVolatile_ContentAddressed`               | Same content → same path, different content → different path          |
| `TestVolatile_DifferentAlgorithms`            | All 6 algorithms produce correct content and unique digests           |
| `TestVolatile_AlgorithmConsistency`           | Two instances, same algorithm → same path                             |

Good concurrency and edge-case coverage. No missing test cases identified.

---

## 6. Summary

| Area           | Rating | Notes                                                                    |
|----------------|--------|--------------------------------------------------------------------------|
| Correctness    | Good   | No issues                                                                |
| API fitness    | Good   | Clean, minimal API; `[]byte` appropriate for current consumer            |
| Organization   | Good   | Single-file thin wrapper                                                 |
| Test coverage  | Good   | 10 tests with strong concurrency, edge-case, and algorithm coverage      |

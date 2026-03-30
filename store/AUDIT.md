# Audit: package `store`

**Date:** 2026-03-29
**Scope:** `/store/stores.go`, `/store/doc.go` (parent package only, sub-packages excluded)

---

## Package Purpose

Provides global singleton access to three storage subsystems via lazy initialization:
- `GetCache()` -- content-addressed persistent blob cache
- `GetContent()` -- identifier-to-digest index over the blob cache
- `GetVolatile()` -- ephemeral content-addressed storage with reference counting

Each is initialized once via `sync.Once` and stored in package-level globals.

---

## Code Correctness

### Panic on initialization failure

All three `Get*` functions panic with raw errors if directory resolution fails:

- `GetCache()` (line 52): `panic(err)` — raw.
- `GetContent()` (line 67): `panic(err)` — raw. Also line 72 for `content.New` failure.
- `GetVolatile()` (line 84): `panic(err)` — raw.

All three are consistent — none wrap with `fault` sentinels. If these panics are expected to be caught by `shutdown.Run`'s recover handler, the raw errors will lack categorization. Wrapping with `fault.ErrSystemFailure` would make them classifiable.

### `GetContent` also panics on `content.New` failure

`GetContent()` lines 71-73 also panic if `content.New` fails (e.g., index file corruption or permission denied). This is a secondary initialization step beyond directory resolution. The other two functions have no equivalent secondary failure mode — `cache.New` and `volatile.New` are infallible constructors.

### `GetContent` passes `nil` for options

`GetContent()` line 72 calls `content.New(filepath.Join(cacheDir, contentSubdir), nil)`. Looking at `content.New`, nil opts means all defaults (initial cap 0 which becomes 1024, no max cap, default quota). This is correct.

### `GetVolatile` hardcodes BLAKE2b256

`GetVolatile()` line 89 hardcodes `digest.BLAKE2b256`. This is a reasonable default for ephemeral content (fast, 32-byte output), but it is not configurable. Since the volatile store hashes content to derive file paths, changing the algorithm later would orphan existing temporary files. Documenting the algorithm choice would be valuable.

### ~~No `Close` for content store~~ — RESOLVED

`store.Shutdown()` now closes the content store if it was initialized.
Registered via `shutdown.Register(store.Shutdown)` in `app.New`.

---

## API Fitness

### Global singletons with no configuration

The three `Get*` functions accept no parameters. All configuration (paths, quotas, algorithms) is hardcoded. This is documented in `doc.go` as "global application convenience instantiation" and explicitly notes that "specialized code may still create their own separate storage." This design is fit for its stated purpose.

### No way to reset or replace singletons

The `sync.Once` pattern means the first call wins permanently. This makes the singletons untestable in isolation -- a test cannot inject a temp directory or mock store. The sub-packages are individually testable (and well-tested), so this is primarily a concern for integration testing.

### Thread safety

All three functions are safe for concurrent use via `sync.Once`. The returned types (`*cache.Cache`, `*content.Store`, `*volatile.Volatile`) are documented as safe for concurrent access across processes.

### Dependency on `dirs.CacheDir()` / `dirs.RuntimeDir()`

These functions require `dirs.SetAppName` to have been called first (via `filesystem.Initialize` in `app.New`). If any `Get*` function is called before `app.New`, the app name will be empty, and directory paths will be wrong (e.g., `~/.cache//store` with a double slash). There is no guard or assertion for this precondition.

---

## Organization

The package is well-structured:
- Parent (`store/`) owns singleton lifecycle only
- Sub-packages own behavior: `cache/`, `content/`, `volatile/`, `refcount/`, `index/`
- No business logic leaks between layers

The constants `cacheSubdir`, `contentSubdir`, `volatileSubdir` are appropriately scoped to the parent.

The `doc.go` accurately describes the package's role.

---

## Test Coverage

**There are no tests for the `store` parent package.** The coverage report shows 0% for all three `Get*` functions.

This is expected given the design: the functions depend on `dirs.SetAppName` having been called and create real directories. Testing them would require `app.New` or direct `dirs.SetAppName` calls, and the `sync.Once` prevents re-initialization between tests.

**Sub-package coverage is strong:**
- `cache/`: 19 tests covering write/read, digest mismatch, concurrent read-while-write, GC under/over quota, GC preserves in-use entries, rapid acquire/close, partial write abandonment, and multi-digest concurrency.
- `content/`: 23 tests covering miss with/without digest, cache hit preservation, wrong digest recovery, fetch failures, deduplication, empty/large content, truncated fetch, concurrent access (same/different identifiers), invalidation, slow fetch with concurrent readers, and stress tests. Plus algorithm ID stability pinning test.
- `volatile/`: 8 tests covering concurrent acquire (same/different content), staggered release, rapid acquire/release, re-acquire after full release, empty/large content, content addressing, and algorithm consistency.
- `refcount/`: Tests exist including invalid key validation and concurrent scenarios.
- `index/`: Tests and benchmarks exist with good coverage of the mmap'd hash table.

### Test quality assessment

The sub-package tests are well-designed:
- They treat code as a blackbox, testing observable behavior not implementation details.
- They validate specific error types (e.g., `fault.ErrHashMismatch`, `fault.ErrWriteFailure`), not generic errors.
- Concurrency tests use proper synchronization (`sync.WaitGroup`, channels, `atomic.Int64`).
- Edge cases are covered: empty content, large content, partial writes, writer abandonment, re-acquire after release.
- The algorithm ID stability test (`TestAlgorithmIDStability`) is a particularly good defensive test -- it guards against accidental on-disk format changes.

One minor observation: `TestStore_ConcurrentDifferentIdentifiers` uses `string(rune('A'+id))` to generate identifiers, which produces non-printing characters for `id >= 26`. This does not affect correctness (the identifier is hashed), but makes debugging harder if a test fails.

---

## Summary of Findings

| Finding                                                       | Severity |
|---------------------------------------------------------------|----------|
| ~~No shutdown handler for content store~~                     | RESOLVED |
| All three `Get*` panics use raw errors, no `fault` wrapping   | Medium   |
| Zero test coverage for parent package                         | Medium   |
| No guard against calling `Get*` before `dirs.SetAppName`      | Medium   |
| `GetVolatile` algorithm choice undocumented                   | Low      |

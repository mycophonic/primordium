# Audit: primordium/r2

**Date:** 2026-03-29
**Scope:** `r2/` — `doc.go`, `const.go`, `errors.go`, `r2.go`, `download.go`, `upload.go`, `download_test.go`, `upload_test.go`
**Consumers:** Only tests within the package itself import `primordium/r2` directly. All production consumers go through `cc0/primo/cc0r2`, which wraps `primordium/r2` with cc0-specific ETag caching, zstd compression/decompression, and tar extraction.

---

## Overall Assessment

Well-structured package. Clean separation between client setup (`r2.go`), error mapping (`errors.go`), download (`download.go`), and upload (`upload.go`). Resume logic for both downloads (byte-range + ETag) and uploads (multipart state persistence) is sound. Tests are thorough and use a fake S3 backend (`gofakes3`), covering the critical edge cases.

---

## Findings

### Note

**N1. Test quality is excellent.** 18 download tests (3 Stat + 15 Download) covering: stat exists, stat not-found, stat ETag stripping, success, not-found, already-complete, ETag mismatch, resume partial, resume ETag mismatch, resume oversized, fully-downloaded temp, context cancellation, size mismatch, idempotency, directory creation, hierarchical keys, path traversal, and move atomicity. 10 upload tests covering: zero/negative size, single part, multi-part, concurrent workers, exact boundary, small part size normalization, state cleanup on success, overwrite existing object, and path traversal. All tests use `t.Parallel()`.

**N2. Error mapping in `errors.go` is comprehensive.** Covers cancellation, network send errors, all not-found variants, invalid params, ser/deser errors, and HTTP status codes.

**N3. The `partWorker` concurrency model is clean.** Mutex-protected shared state, first-error capture, goroutine-per-part with semaphore concurrency control.

**N4. `moveToData` is not atomic for the pair** (data file + ETag sidecar), but self-heals on next invocation. Tested and intentional.

---

## Test Coverage

| Area | Coverage | Notes |
|------|----------|-------|
| `Stat` | Good (3 tests) | Exists, not-found, ETag stripping |
| `Download` | Excellent (15 tests) | All resume paths, cancellation, idempotency, atomicity |
| `Upload` | Good (10 tests) | Multipart, concurrency, boundary, state cleanup, overwrite |
| `validateObjectKey` | Good | Tested via download/upload path traversal tests |
| `mapErr` / `mapHTTPStatus` | None | Only exercised indirectly via fake S3 errors |
| `copyWithProgress` | Indirect | Exercised by download tests, no unit tests for edge cases |
| `read` (range request) | Indirect | Exercised by resume test |

Gap: no direct unit tests for `mapErr` type-switch branches. The fake S3 backend may not produce all AWS SDK error types (e.g. `smithy.SerializationError`).

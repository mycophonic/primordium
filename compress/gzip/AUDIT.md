# Audit: compress/gzip

Audited: 2026-03-29

## Scope

Files audited:
- `doc.go` — package documentation
- `gzip.go` — gzip decompressor registration

## Code Correctness

**`init` registration.** Registers with magic bytes `0x1F, 0x8B`, which is the correct
gzip magic number (RFC 1952). Correct.

**`New` function.** Uses `pgzip.NewReader` (klauspost/pgzip v1.2.6), which is a
parallel gzip implementation. Unlike the stdlib `compress/gzip`, pgzip decompresses
using multiple goroutines for better throughput on large streams.

**Error wrapping.** On failure, wraps the error with `fault.ErrReadFailure` using the
double `%w` pattern (`fmt.Errorf("%w: gzip decoder: %w", ...)`). This allows callers
to check both `errors.Is(err, fault.ErrReadFailure)` and unwrap the underlying pgzip
error. Correct use of the Go 1.20+ multiple `%w` feature.

**Return type.** `pgzip.NewReader` returns `(*pgzip.Reader, error)`. `pgzip.Reader`
implements `io.ReadCloser` (it has both `Read` and `Close` methods), so returning `gr`
directly is correct. Unlike bzip2 and lz4, no `NopCloser` wrapper is needed, and
`Close` will properly release pgzip's internal goroutines.

**Resource cleanup.** Because `pgzip.Reader` spawns goroutines for parallel
decompression, calling `Close` is important. The parent `compress.Decompress` returns
the reader to the caller with a documented "must close" contract. Correct.

## API Fitness

Same side-effect registration pattern as the other format packages. `New` is exported
for direct use. Matches `compress.Decompressor` signature.

## Organization

Single-file package. Minimal and appropriate.

## Test Coverage

**No test files in this package.** Testing is done from `compress_test.go` via:
- `TestDecompress_Gzip` — creates gzip data with `pgzip.NewWriter`, then decompresses
  via `compress.Decompress`.

The `New` error path (pgzip.NewReader returning an error) is not tested. This would
require feeding corrupt-but-magic-matching bytes to trigger a header parse failure.

## Summary

| Priority | Item |
|----------|------|
| Low | Add a test for `New` with corrupt gzip data (valid magic, invalid header) to cover the error path |

The package is correct and minimal. No bugs found.

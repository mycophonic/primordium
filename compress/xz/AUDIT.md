# Audit: compress/xz

Audited: 2026-03-29

## Scope

Files audited:
- `doc.go` — package documentation
- `xz.go` — XZ decompressor registration

## Code Correctness

**`init` registration.** Registers with magic bytes `0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00`,
which is the correct 6-byte XZ magic number (per the XZ file format specification).
This is the longest magic sequence among all registered formats, so it determines the
`peekSize` in the parent registry. Correct.

**`New` function.** Uses `xz.NewReader` (ulikunitz/xz v0.5.15). The function returns
`(io.ReadCloser, error)` by wrapping the `*xz.Reader` in `io.NopCloser`.

**Error wrapping.** Same double `%w` pattern as gzip: wraps with `fault.ErrReadFailure`.
Correct.

**`NopCloser` appropriateness.** The ulikunitz/xz `Reader` does not implement
`io.Closer`. It does not spawn goroutines or hold external resources. `NopCloser` is
appropriate.

**`xz.NewReader` failure mode.** Unlike bzip2 and lz4, `xz.NewReader` can return an
error during construction (it reads and validates the stream header). This means the
error path in `New` is reachable. The error wrapping is correct.

## API Fitness

Same side-effect registration pattern. `New` is exported. Matches
`compress.Decompressor` signature.

## Organization

Single-file package. Minimal and appropriate.

## Test Coverage

**No test files in this package.** Testing is done from `compress_test.go` via:
- `TestDecompress_XZ` — creates XZ data with `xz.NewWriter`, then decompresses via
  `compress.Decompress`.

The `New` error path is not directly tested. Triggering it requires valid magic bytes
followed by an invalid XZ stream header.

## Summary

| Priority | Item |
|----------|------|
| Low | Add a test for `New` with corrupt XZ data to cover the error path |

The package is correct and minimal. No bugs found.

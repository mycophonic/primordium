# Audit: compress/lz4

Audited: 2026-03-29

## Scope

Files audited:
- `doc.go` — package documentation
- `lz4.go` — LZ4 decompressor registration

## Code Correctness

**`init` registration.** Registers with magic bytes `0x04, 0x22, 0x4D, 0x18`, which is
the correct LZ4 frame magic number. Correct.

**`New` function.** Uses `lz4.NewReader` (pierrec/lz4/v4 v4.1.26). The function wraps
the result in `io.NopCloser`. This is worth examining: `lz4.NewReader` returns a
`*lz4.Reader` which does implement `io.Reader` but does not implement `io.Closer`. So
`NopCloser` is necessary to satisfy the `io.ReadCloser` return type.

**No resource leak.** The pierrec/lz4 reader does not spawn goroutines or hold external
resources. `NopCloser` is appropriate.

**Error handling.** `lz4.NewReader` does not return an error (signature is
`func NewReader(r io.Reader) *Reader`). The `New` function always returns `nil` error.
Decompression errors surface during `Read`. This is the same pattern as bzip2. Correct.

## API Fitness

Same side-effect registration pattern. `New` is exported. Matches
`compress.Decompressor` signature.

## Organization

Single-file package. Minimal and appropriate.

## Test Coverage

**No test files in this package.** Testing is done from `compress_test.go` via:
- `TestDecompress_LZ4` — creates LZ4 data with `lz4.NewWriter`, then decompresses via
  `compress.Decompress`.

The test in `compress_test.go` does not check errors from `lz4.NewWriter.Write` and
`lz4.NewWriter.Close` (lines 59-60). These are ignored. While unlikely to fail writing
to a `bytes.Buffer`, this is inconsistent with the other format tests (gzip and xz
check writer errors).

## Summary

| Priority | Item |
|----------|------|
| Low | Add direct unit test for `New` |

The package is correct and minimal. No bugs found.

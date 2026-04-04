# Audit: compress/bzip2

Audited: 2026-03-29

## Scope

Files audited:
- `doc.go` — package documentation
- `bzip2.go` — bzip2 decompressor registration

## Code Correctness

**`init` registration.** Registers with magic bytes `0x42, 0x5A, 0x68` ("BZh"), which
is the correct bzip2 magic signature. The three bytes match the fixed header of every
bzip2 stream.

**`New` function.** Wraps `compress/bzip2.NewReader` (stdlib) in `io.NopCloser`. The
stdlib `bzip2.NewReader` returns an `io.Reader`, not `io.ReadCloser`, so `NopCloser` is
necessary to satisfy the `Decompressor` return type.

**Error handling.** `compress/bzip2.NewReader` does not return an error — it returns
`io.Reader` only. The `New` function always returns `nil` error. This is correct: the
stdlib bzip2 reader defers all error checking to the first `Read` call. If the stream
is corrupt, the error will surface during `Read`, not during construction.

**No resource leak.** `NopCloser` means `Close()` is a no-op. The stdlib bzip2 reader
has no resources to release (no goroutines, no file handles), so this is correct.

## API Fitness

The package exists solely for side-effect registration. The exported `New` function
allows direct use if needed, which is a reasonable design. The function signature
matches `compress.Decompressor`.

## Organization

Single-file package. Minimal and appropriate for a registration shim.

## Test Coverage

**No test files in this package.** Testing is done from `compress_test.go` via:
- `TestDecompress_Bzip2` — shells out to system `bzip2` to create test data, then
  decompresses via `compress.Decompress`.

The `New` function is indirectly tested through the registry. A direct unit test
calling `bzip2.New` with known-good compressed bytes would provide independent
coverage. The current test is also fragile because it depends on the `bzip2` system
binary being present (`t.Skip` if missing).

## Summary

| Priority | Item |
|----------|------|
| Low | Add a direct unit test for `New` using embedded bzip2 test data (avoids system binary dependency) |

The package is correct and minimal. No bugs found.

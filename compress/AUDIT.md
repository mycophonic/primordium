# Audit: compress

Audited: 2026-03-29

## Scope

Files audited:
- `doc.go` — package documentation
- `compress.go` — Compress/Decompress, format registry, zstd built-in
- `const.go` — `inputBufSize` constant (256KB read buffer)
- `errors.go` — ErrPathTraversal sentinel
- `tar.go` — Tar/Untar streaming archive operations
- `compress_test.go` — compression/decompression tests
- `tar_test.go` — tar round-trip and path traversal tests

## Code Correctness

### compress.go

**Registry concurrency.** `Register` holds `formatsMu` (write lock), and `Decompress`
holds a read lock to snapshot `formats` and `peekSize`. This is correct. The snapshot
pattern (copy slice header + peekSize under RLock, then release) avoids holding the lock
during I/O.

**~~`hasPrefix` vs `bytes.HasPrefix`.~~** Replaced with `bytes.HasPrefix`.

**`Compress` goroutine error handling.** The goroutine in `Compress` correctly propagates
errors via `pipeWriter.CloseWithError`. If `io.Copy` fails, the copy error takes
priority over the encoder close error. This is correct behavior.

**`Decompress` peek tolerance.** Line 124: `if err != nil && len(header) == 0` — this
means if `Peek` returns a partial header (e.g., a 2-byte file when peekSize is 6), it
will still attempt magic matching with the partial data. `hasPrefix` handles this safely
since it checks `len(data) < len(prefix)`. Correct.

**`Decompress` buffered reader.** A 256KB `bufio.Reader` wraps the input before passing
to decompressors. This is beneficial for formats whose decoders make many small reads
(e.g., bzip2), amortizing syscall overhead. Correct design decision.

**`zstdReader.Close`.** The `zstd.Decoder.Close()` method does not return an error
(it is `func (d *Decoder) Close()`), so wrapping it to always return nil is correct
per the klauspost/compress v1.18.4 API.

### tar.go

**Path traversal protection in `Untar`.** Line 120 uses `filepath.Clean` on
`header.Name` and then checks that the resolved `target` path starts with
`filepath.Clean(destDir) + string(os.PathSeparator)`. This is the standard Go idiom
for preventing zip-slip / tar-slip. Correct.

**Edge case: root entry.** If a tar entry has Name equal to `"."` or `""`,
`filepath.Join(destDir, filepath.Clean("."))` yields `destDir` itself. The prefix check
`strings.HasPrefix(destDir, destDir + "/")` would be false, so such an entry would
trigger ErrPathTraversal. In practice, tar archives do not normally contain `.` entries
for the root, but if one did, Untar would reject it. This is conservative and safe.

**Symlink handling.** `Untar` skips all entry types besides `TypeDir` and `TypeReg`.
Symlinks are silently skipped. This is documented in the function comment. Correct.

**`Tar` — symlink target.** `tar.FileInfoHeader(info, "")` passes empty string for the
link target. If the walked directory contains symlinks, they will have an empty link
target in the header. Since `Untar` skips symlinks, this is consistent within the
package, but callers using a different tar extractor would get broken symlinks. This is
a known limitation, not a bug.

**`writeTar` error from `filepath.Walk`.** Walk errors are propagated. The tar writer is
closed after the walk completes, so a walk error causes `writeTar` to return before
`tarWriter.Close()`. This means the pipe will receive the error via
`pipeWriter.CloseWithError` — correct.

### errors.go

Single sentinel error `ErrPathTraversal`. Clean, appropriate.

### uncompress.go

Empty file containing only the license header and `package compress`. Appears to be a
placeholder for planned decompression code that was absorbed into `compress.go`. Dead file.

## API Fitness

The API is clean and well-designed for its use case:

- **`Compress(io.Reader) (io.ReadCloser, error)`** — streaming zstd compression.
  Returns a pipe-based reader. Caller must close. Clear contract.

- **`Decompress(io.Reader) (io.ReadCloser, error)`** — auto-detecting streaming
  decompression. Registry-based, extensible via side-effect imports. This is the
  standard Go pattern (cf. `image.Decode`, `database/sql`).

- **`Register(name, magic, Decompressor)`** — format registration. Simple, correct.

- **`Tar(baseDir, relDir) io.ReadCloser`** — note that `Tar` returns `io.ReadCloser`
  (no error return). Errors from `writeTar` are surfaced when the caller reads from the
  returned reader (via the pipe). This is a deliberate streaming design. Callers must
  check `io.ReadAll` or `io.Copy` errors to detect failures.

- **`Untar(io.Reader, string) error`** — straightforward extraction with path traversal
  protection.

**Asymmetry observation.** `Compress` only supports zstd. `Decompress` supports all
registered formats. This is intentional per the comment at the top of compress.go (the
upload-side compression is noted as "not yet migrated"). The doc.go correctly describes
this: "streaming zstd compression and registry-based decompression."

## Organization

- **File layout is logical.** Compression/decompression in `compress.go`, tar in `tar.go`,
  errors in `errors.go`, docs in `doc.go`.

- **Empty files.** `uncompress.go` and `const.go` serve no purpose and should be removed.

- **Sub-package pattern.** Each format gets its own sub-package for side-effect import.
  This is the idiomatic Go approach and avoids pulling in all decoder dependencies
  when only one format is needed.

## Test Coverage and Quality

### compress_test.go

**Coverage.** Tests cover:
- Zstd round-trip (`TestCompressDecompress_Roundtrip`) — compress then decompress.
- Per-format decompression: LZ4, zstd, gzip, xz, bzip2 — each creates compressed data
  with the native library, then feeds it through `compress.Decompress`.
- Unknown format rejection (`TestDecompress_UnknownFormat`).
- Empty input rejection (`TestDecompress_EmptyInput`).

**Quality observations:**

1. **`TestDecompress_Bzip2` uses `exec.Command("bzip2")`** — this shells out to the
   system `bzip2` binary to create test data, with a `t.Skip` if the binary is missing.
   This is reasonable since the Go stdlib `compress/bzip2` package only provides a
   reader, not a writer. However, the test is fragile on systems without bzip2
   installed (e.g., minimal containers). All other format tests use the Go library
   directly to create test data.

2. **Error type assertions.** `TestDecompress_UnknownFormat` and
   `TestDecompress_EmptyInput` only check `err == nil` vs `err != nil`. They do not
   verify the error wraps `fault.ErrNotImplemented` or `fault.ErrReadFailure`. Per the
   testing guidelines, tests should validate specific error types.

3. **`Compress` error path not tested.** There is no test for `Compress` receiving a
   reader that errors mid-stream. The goroutine error path (`copyErr != nil`) is
   untested.

4. **`Decompress` with a reader that returns partial peek.** No test for a reader that
   returns fewer bytes than `peekSize` but is not empty (e.g., a 2-byte input).

5. **`compressed` reader from `Compress` is never closed in the round-trip test.**
   Line 33 feeds `compressed` directly into `Decompress`. The `compressed` reader
   (a `*io.PipeReader`) is consumed by `Decompress` but never explicitly closed. In
   this test it is harmless because the goroutine completes and closes the pipe writer,
   but it would be more correct to `defer compressed.Close()`.

6. **Missing test: `Register` is not directly tested.** Registration is implicitly
   tested via the side-effect imports in the test file, but there is no test for
   duplicate registration, registration ordering, or the `peekSize` tracking logic.

### tar_test.go

**Coverage.** Tests cover:
- `Tar` creating correct archive structure (`TestTar`).
- `Untar` extracting files and subdirectories (`TestUntar`).
- Path traversal rejection (`TestUntar_PathTraversal`).
- Round-trip `Tar` into `Untar` (`TestTar_Roundtrip`).

**Quality observations:**

1. **Good: `TestUntar_PathTraversal` checks `errors.Is(err, compress.ErrPathTraversal)`.**
   This properly validates the specific sentinel error.

2. **Missing: empty tar test.** No test for `Untar` with an empty archive.

3. **Missing: `Tar` with empty directory.** No test for archiving a directory that
   contains no files.

4. **Missing: `Tar` error propagation.** No test for `Tar` when the source directory
   does not exist. The error would surface when reading from the returned `io.ReadCloser`,
   but this path is untested.

### Sub-package test coverage

**The sub-packages (bzip2, gzip, lz4, xz) have zero test files.** All testing is done
from `compress_test.go` in the parent package via side-effect imports. This is
acceptable for the registration pattern, but means the `New` functions are only tested
indirectly. A direct test in each sub-package would catch regressions in the
decompressor construction itself, independent of the registry.

## Additional Observations

1. **`hasPrefix` can be replaced with `bytes.HasPrefix`.** Importing `bytes` is already
   not present in compress.go, but the function is trivial and a stdlib replacement
   exists. This is minor.

2. **`Decompress` snapshots `formats` slice header but not the underlying array.** Since
   `Register` only ever appends, existing slice elements are never modified, so the
   snapshot is safe. If `Register` ever mutated existing entries, this would be a data
   race. Current code is correct.

3. **No compression-level control.** `Compress` uses default zstd settings. There is no
   way to pass encoder options. This is fine if the use case is fixed, but limits reuse.
   The comment in compress.go suggests the upload compression was in a different
   location with potentially different settings.

4. **`decompressZstd` allocates a new `zstd.Decoder` per call.** The klauspost/compress
   zstd decoder is expensive to allocate. For high-throughput use, a decoder pool would
   be more efficient. This may or may not matter for the actual use case.

## Summary

The compress package is well-structured, correct, and follows idiomatic Go patterns.
The main areas for improvement are:

| Priority | Item | Status |
|----------|------|--------|
| ~~Low~~ | ~~Replace hand-rolled `hasPrefix` with `bytes.HasPrefix`~~ | RESOLVED |
| ~~Medium~~ | ~~Add specific error type assertions in `TestDecompress_UnknownFormat` and `TestDecompress_EmptyInput`~~ | RESOLVED |
| ~~Medium~~ | ~~Add test for `Compress` with a failing reader (goroutine error path)~~ | RESOLVED |
| ~~Medium~~ | ~~Add test for `Decompress` with partial-peek input~~ | RESOLVED |
| ~~Low~~ | ~~Close `compressed` reader in round-trip test~~ | RESOLVED |
| ~~Low~~ | ~~Add tests for edge cases: empty tar, nonexistent source dir for `Tar`~~ | RESOLVED |

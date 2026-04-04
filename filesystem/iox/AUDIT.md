# Audit: filesystem/iox

**Date**: 2026-03-29
**Scope**: `primordium/filesystem/iox` — buffered I/O wrappers

## Summary

Clean, well-tested package providing three buffered I/O wrappers: `Reader`,
`ReadSeeker`, and `ReadWriter`. `Reader` and `ReadWriter` delegate to `bufio`;
`ReadSeeker` is a hand-rolled buffered read-seeker with optimized small forward
seeks. All types optionally close the underlying source if it implements `io.Closer`.

8 files, ~200 lines of production code, ~280 lines of tests. No production callers
yet (only test imports).

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 17 | Package comment (describes Reader, ReadSeeker, ReadWriter) |
| `const.go` | 19 | `defaultBufferSize = 4096` |
| `reader.go` | 61 | Buffered `io.Reader` + optional `io.Closer` |
| `readseeker.go` | 134 | Buffered `io.ReadSeeker` + optional `io.Closer` |
| `readwriter.go` | 75 | Buffered `io.ReadWriter` + optional `io.Closer` |
| `reader_test.go` | 119 | Reader tests (5 tests) |
| `readseeker_test.go` | 250 | ReadSeeker tests (10 tests) |
| `readwriter_test.go` | 136 | ReadWriter tests (4 tests) |

## API Surface

| Type | Description |
|---|---|
| `Reader` | Buffered reader (wraps `bufio.Reader`) |
| `ReadSeeker` | Buffered read-seeker with seek optimization |
| `ReadWriter` | Buffered read-writer (wraps `bufio.Reader` + `bufio.Writer`) |

Each type has `New*()` and `New*WithSize()` constructors, plus `Close()`.

## Findings

## Test Coverage

| Type | Tests | Coverage | Notes |
|---|---|---|---|
| `Reader` | 5 | Good | Buffering, data integrity, close passthrough, non-closer, custom size |
| `ReadSeeker` | 10 | Excellent | Buffering, data integrity, large bypass, seek invalidation, SeekCurrent within/beyond/negative, SeekEnd, close |
| `ReadWriter` | 4 | Good | Read+write, close+flush, non-closer flush, custom size |

The `ReadSeeker` tests are particularly thorough, including a negative `SeekCurrent`
regression test that validates the buffer offset adjustment.

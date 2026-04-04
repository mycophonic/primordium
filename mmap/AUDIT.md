# Audit: mmap

**Date**: 2026-03-29
**Scope**: `primordium/mmap/` — platform-specific memory-mapped file I/O

## Summary

Three files (221 lines total) providing three public APIs:
- `MapFile(f, size)` — maps a file into read-write shared memory
- `UnmapFile(data, mapping)` — unmaps previously mapped memory
- `SyncFile(data, f)` — flushes the mapped region to disk

Unix uses `syscall.Mmap`/`Munmap` and raw `SYS_MSYNC`. Windows uses
`CreateFileMapping`/`MapViewOfFile`/`FlushViewOfFile`/`UnmapViewOfFile`/
`CloseHandle`.

The `Mapping` type holds platform-specific state: empty struct on Unix (no
extra state needed), handle + base address on Windows.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `mmap_unix.go` | 88 | Unix: `syscall.Mmap`, `Munmap`, `SYS_MSYNC` |
| `mmap_windows.go` | 115 | Windows: `CreateFileMapping`, `MapViewOfFile`, etc. |

## Findings

No open findings.

### I1: Size parameter is `int` — 2 GB cap on 32-bit platforms only

`MapFile` takes `size int`. On 64-bit platforms (all current targets), `int`
is 64 bits — no practical size limit beyond available address space. On
32-bit platforms, `int` is 32 bits, capping mappings at 2 GB. This mirrors
`syscall.Mmap` which also takes `int` — an inherent limitation of the Go
syscall API, not a bug in this package.

## API Surface

| Function | Consumers | Correct |
|---|---|---|
| `MapFile` | `store/index/index.go` (3 call sites) | Yes |
| `UnmapFile` | `store/index/index.go` (4 call sites) | Yes |
| `SyncFile` | `store/index/index.go` (3 call sites) | Yes |
| `Mapping` | `store/index/index.go` (4 references) | Yes |

## Platform Notes

### Unix
- `SyncFile` uses `syscall.Syscall(SYS_MSYNC, ...)` directly because Go's
  `syscall` package has no `Msync` wrapper. Correct on both Linux and macOS.
- `Mapping` is an empty struct — `syscall.Munmap` only needs the slice.

### Windows
- `SyncFile` calls both `FlushViewOfFile` (flush to filesystem cache) and
  `FlushFileBuffers` (flush to physical disk) for full durability parity
  with Unix's `msync(MS_SYNC)`.
- `UnmapFile` correctly calls both `UnmapViewOfFile` and `CloseHandle` even
  if the first fails — both resources must be released.
- `MapFile` properly calls `CloseHandle` on the mapping handle if
  `MapViewOfFile` fails.
- Uses `unsafe.Slice` to construct the byte slice from the mapped address.

## Open Counts

| Severity | Count                     |
|---|---------------------------|
| INFORMATIONAL | 2 (I1: 32-bit size limit) |

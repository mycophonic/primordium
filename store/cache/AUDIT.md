# Audit: `store/cache`

**Date**: 2026-03-29 (updated from 2026-03-11)
**Scope**: Content-addressed persistent storage with read-while-write support

3 files, ~673 lines of production code, ~1782 lines of tests.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 5 | Package comment |
| `cache.go` | 673 | Cache, cacheReader, cacheWriter, inProgressReader, GC, touchFile |
| `cache_test.go` | 1782 | 24 tests: write/read, concurrency, failure modes, GC, edge cases |

## Architecture

Content-addressed blob cache with read-while-write support across processes.
Callers get `(reader, writer, error)` — reader always non-nil on success,
writer non-nil only on cache miss. Readers tail the temp file being written,
polling the write lock for completion. Hash is verified on writer Close();
mismatches delete the temp file and propagate `ErrWriteFailure` to readers.

### On-disk layout

```
<rootDir>/
  <prefix>/                         # first 2 hex chars of encoded hash (256 buckets)
    <algorithm>-<encoded>/          # e.g. sha256-e3b0c44...
      data                          # completed content
      _data                         # temp file during active write
      lock.read                     # shared read lock (GC protection)
      lock.write                    # exclusive write lock (writer signal)
```

### Locking protocol

1. **Shard lock** (exclusive on `<prefix>/`): serializes directory creation
   within a bucket. Held briefly — released after acquiring entry dir lock.
2. **Entry dir lock** (exclusive on `<algorithm>-<encoded>/`): serializes
   state inspection (temp file, data file, write lock probe). Released after
   setup; re-acquired by writer during finalization.
3. **Read lock** (shared on `lock.read`): held by all active readers. Prevents
   GC from evicting the entry. Multiple readers hold concurrent shared locks.
4. **Write lock** (exclusive on `lock.write`): held by the active writer for
   the duration of the write. `TryLock` probes determine whether a write is
   in progress. Released after finalization (rename or delete of temp file).

### Consumers

| Caller | Functions used |
|---|---|
| `store/content.Store` | `New`, `Acquire` (×3), `GarbageCollect`, `GCStats` |
| `store.ContentStore` | `Acquire` (×3) |
| `store/stores.go` | `New` (global singleton, default quota) |

`Exists` has zero production callers.

## API Surface

| Symbol | Description |
|---|---|
| `Cache` | Struct: `rootDir string`, `quota int64` |
| `New(root, quota)` | Constructor; quota 0 → `DefaultCacheQuota` |
| `Acquire(dgst digest.Digest)` | `(io.ReadCloser, io.WriteCloser, error)` — reader always, writer on miss |
| `Exists(dgst digest.Digest)` | Lockless best-effort check; TOCTOU by design |
| `GarbageCollect()` | `(GCStats, error)` — evicts unlocked entries over quota |
| `GCStats` | `EntriesFreed`, `BytesFreed`, `Remaining`, `Quota` |
| `DefaultCacheQuota` | 50 GB (`50 << 30`) |
| `cacheReader` | Reads completed `data` file, holds shared read lock |
| `cacheWriter` | Writes to `_data`, verifies hash on Close, renames to `data` |
| `inProgressReader` | Tails `_data` during active write, polls write lock for completion |

## Findings

### M1: GC eviction has no ordering

`gcScanBucket` returns candidates in `ReadDir` order (alphabetical by directory
name). Entries in low-hex shards ("00") are always evicted before high-hex
shards ("ff"). No access time is tracked, so LRU is not possible without
additional metadata. At the default 50 GB quota this is unlikely to matter in
practice, but under tight quotas the bias is systematic.

### L1: `Exists()` has zero production callers

Defined, tested, but never called outside tests. Dead code. If retained for
future use, document the intent; otherwise remove.

### L2: No `Close()` method on `Cache`

`Cache` is stateless (no open handles, no goroutines), so `Close()` is not
strictly needed. But callers have no way to express "I'm done with this cache"
for future-proofing if state is added. Minor.

### L3: `inProgressReader.Read()` fixed-interval polling

`cachePollInterval = 10ms`. For typical network-sourced content this is fine.
For very slow sources or very fast sources, a backoff or adaptive poll would
be more efficient. Low priority — 10ms is a reasonable default.

### L4: `acquireActiveWriter` panic path (line 402)

If write lock is held but neither temp nor data file exists, the function
panics with "invariant violation". This path is unreachable under the normal
locking protocol: the entry dir lock serializes temp-file creation
(`becomeWriter` creates temp before releasing dir lock) and finalization
(`cacheWriter.Close()` holds dir lock while cleaning up). The panic is a
correct safety net for truly broken state.

### I1: Argument explosion in helper functions

`acquireNoActiveWriter` takes 9 arguments, `acquireActiveWriter` takes 8,
`becomeWriter` takes 9. All are file handles and paths from the locking
protocol. A struct would reduce the parameter count but adds indirection
to a critical path. Current `revive:disable:argument-limit` suppression is
pragmatic.

### I2: `cacheWriter` not safe for double-Close — DOCUMENTED

Calling `Close()` twice would attempt to close an already-closed file and
re-acquire locks. This is the caller's responsibility (consistent with
`os.File` behavior). Documented inline on all three `Close()` methods.

### I3: Directory name includes algorithm prefix

`strings.Replace(dgst.String(), ":", "-", 1)` produces directories like
`sha256-e3b0c44...`. Two digests with different algorithms but identical
hash bytes get separate directories. This is correct — they are different
content addresses — but means the cache is not algorithm-agnostic. A
migration to a different default algorithm would not deduplicate existing
entries.

## Test Coverage

| Test | What it covers |
|---|---|
| `WriteAndRead` | Miss → write → hit → read, content verified |
| `ReadNotExists` | Acquire non-existent → writer returned |
| `WriteDigestMismatch` | Hash mismatch → `ErrHashMismatch`, reader → `ErrWriteFailure` |
| `WriteAlreadyExists` | Second acquire → reader only |
| `ConcurrentReadWhileWrite` | 1 MB chunked write, reader attaches mid-write |
| `ConcurrentReadWhileWriteFails` | Reader sees `ErrWriteFailure` on hash mismatch |
| `MultipleReadersComplete` | 10 concurrent readers on completed content |
| `LargeContent` | 10 MB write and read |
| `EmptyContent` | Zero-byte write and read |
| `SequentialWriteThenRead` | Write all, close writer, then read (no concurrent goroutine) |
| `SequentialWriteThenReadMismatch` | Sequential write wrong digest → correct errors |
| `ConcurrentWritersRace` | 20 goroutines → exactly 1 writer, all read correct content |
| `ReaderAttachesMidWrite` | Readers at 0/10/50/100 ms during 512 KB write |
| `RapidAcquireClose` | 100 rapid acquire/close cycles, content intact |
| `WriterAbandonmentNoWrite` | Close without writing: empty (valid) + non-empty (mismatch) |
| `PartialWriteAbandon` | Half-write → mismatch → retry succeeds |
| `MultipleConcurrentDigests` | 10 different digests concurrently, all verified |
| `StressReadersWhileWriting` | 50 readers during 1 MB write |
| `GC_UnderQuota` | GC no-op, content preserved |
| `GC_OverQuota` | GC removes entries exceeding quota |
| `GC_PreservesInUseEntries` | GC skips entries with active readers |
| `GC_StatsAccuracy` | Stats: quota, bytes freed, remaining |
| `GC_ConcurrentWithAcquire` | GC concurrent with Acquire, no corruption |
| `Exists` | Present, absent, different digest |

### Untested paths

| Path | Notes |
|---|---|
| GC on empty cache | `ReadDir` returns no entries; trivial but not exercised |
| `Exists` during active write | Should return false (checks `data`, not `_data`); not tested |
| `cacheReader.Close()` error from `file.Close()` | Lock error path is silently dropped if file error occurs first |
| Double-Close on writer or reader | Undefined behavior (I2); no test |

## Open Counts

| Severity | Count |
|---|---|
| MEDIUM | 1 |
| LOW | 5 |
| INFORMATIONAL | 3 |

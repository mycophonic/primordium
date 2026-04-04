# store/index Audit

Audited: 2026-03-29 (updated from 2026-03-11)

Files reviewed:
- `doc.go` (35 lines)
- `index.go` (1232 lines)
- `pidcheck_unix.go` (29 lines)
- `pidcheck_windows.go` (38 lines)
- `index_test.go` (1805 lines)
- `index_bench_test.go` (316 lines)

Dependencies examined: `mmap`, `flock`, `xos` packages.

---

## 1. Code Correctness

### 1.1 Lock Word CAS in Stale Writer Detection

In `rlock()`, stale writer detection resets the entire lock word to zero.
This is safe because no reader can increment the count while the write flag
is set (readers spin when they see the write flag). So when CAS fires, the
reader count is guaranteed to be zero. Confirmed correct.

Same pattern in `wlock()` for stale writer detection. Confirmed correct.

For stale reader recovery in `wlock()`: the writer CAS-replaces the stale
reader count with its own write lock. This is safe IF all active readers
have PID slots. If more than `maxReaderSlots` (64) reader processes are
active, unslotted readers won't be detected and the writer could overwrite
a live reader count. This limitation is documented in `doc.go`, the
`maxReaderSlots` constant, and the `claimReaderSlot` fallthrough comment.

### 1.2 growLocked Lock Ordering

`growLocked()` acquires locks in the order: wlock (cross-process atomic +
in-process mutex) then flock (advisory file lock). `New()` acquires flock
then releases it before any wlock/rlock. Since New never holds both
simultaneously, there is no deadlock cycle. Confirmed correct.

### 1.3 ensureLockFile Truncate on Every New

`ensureLockFile()` truncates the lock file to `lockFileSize` on every
`New`. If another process has the lock file mmap'd and the file is already
the correct size, `Truncate` to the same size is a no-op. If the file is
smaller (e.g., from a previous version), it grows harmlessly. This is fine.

---

## 2. API Fitness

### Strengths

- **Clean CRUD surface**: Get/Put/Delete/ForEach/Len/Cap/Sync/Close covers
  all hash table operations.
- **Options pattern**: InitialCap, MaxCap, ValSize are cleanly configurable.
- **Opaque values**: The index stores raw bytes without imposing semantics.
  Callers encode/decode as they see fit.
- **Crash recovery**: Journal-based grow recovery is a solid design that
  handles the most dangerous operation (in-place resize) safely.
- **Cross-process safety**: Atomic lock word + PID slot stale detection is
  well-engineered.
- **Lifecycle separation**: `New` doesn't own resources; caller manages
  cache and index lifecycles independently.

### Minor Concerns

1. **No conditional insert**: Can't do "insert if not exists" atomically.
   The caller must Get then Put, with a window between them in multi-process
   scenarios. This is fine for the current use case (content store) where
   racing inserts produce the same digest, but worth noting.

2. **Record.Value always full-size**: `readRecord` returns a `[]byte` of
   exactly `valSize` bytes, including zero padding. This is a deliberate
   trade-off of a fixed-width record store — callers encode length
   information in their own value format (e.g., algorithm ID determines
   digest size). Not a problem in practice.

3. **ForEach holds rlock for full scan**: If the table is large and the
   callback is slow, writers are blocked for the entire iteration. The doc
   comment correctly warns against calling Index methods inside the callback.

---

## 3. Organization

Well-organized. The single-file approach (`index.go` at 1232 lines) is
appropriate -- the code is cohesive and splitting it would add import
complexity without benefit. Platform-specific PID checks are properly
separated via build tags.

Internal structure is clean:
- Constants and types (lines 38-136)
- Public API (lines 138-481)
- Private helpers grouped by concern: file management, header/record
  marshaling, hash table operations, journal, locking

---

## 4. Test Coverage (38 tests, 9 benchmarks)

| Area | Tests | Quality |
|---|---|---|
| Basic CRUD | 7 | Put/Get roundtrip, update, delete, non-existent key, key 0, tombstone reuse, GetNonExistent |
| Value boundaries | 4 | Empty, oversized, exact-size, short (zero-padded) |
| Options validation | 3 | Invalid valSize, valSize mismatch on reopen, InitialCap > MaxCap |
| Error paths | 3 | Corrupt magic, wrong version, file too small |
| Grow correctness | 6 | Data preservation, reopen, tombstone pressure, max cap, open-time max cap, ForEach consistency |
| Journal recovery | 4 | Normal, empty table, corrupt, size mismatch |
| Concurrent access | 3 | Read/write/grow, delete during grow, grow contention |
| Stale lock recovery | 2 | Stale reader (writer acquires), stale writer (reader acquires) |
| ForEach | 2 | Empty index, early stop |
| Misc | 3 | Sync, double Close, Len/Cap fresh index |
| Directory creation | 1 | `TestNewCreatesParentDirectory` -- New with nonexistent parent dirs |

Tests are well-written: external test package, all parallel, clean helpers,
proper synchronization in concurrent tests, verify both positive and
negative outcomes.

No remaining test gaps identified. The previously noted gap (New with
nonexistent parent directory) is now covered by `TestNewCreatesParentDirectory`.

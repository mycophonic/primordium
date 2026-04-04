# store/content Audit

Audited: 2026-03-29 (updated from 2026-03-11)

Files reviewed:
- `doc.go` (5 lines)
- `store.go` (401 lines)
- `store_test.go` (1153 lines)
- `encoding_test.go` (40 lines)

Dependencies examined: `cache`, `index`, `digest`, `fault` packages.

---

## 1. Code Correctness

### 1.1 Stale Index Decode Error Silently Swallowed

In `Acquire`, if `decodeValue(rec.Value)` fails (corrupt index value), the
error is silently swallowed and falls through to fetch. This is correct
self-healing behavior -- the fresh fetch overwrites the corrupt index entry.
For a cache, silent self-healing is the right call.

### 1.2 fetchAndHash Buffers Entire Content in Memory

`fetchAndHash` calls `readAllSized(source)` to compute the digest, buffering
the entire content in memory. `readAllSized` optimizes for sources that
implement `Size() int64` by preallocating the buffer. For the known use case
(audio metadata, album art), this is fine. The caller can avoid buffering by
providing a digest, which routes to `fetchWithDigest` and streams directly.

### 1.3 Hash Key Collision Is Self-Healing

If two identifiers hash to the same uint64 key, the second `Put` overwrites
the first's index entry. This is a ping-pong scenario but self-healing and
vanishingly unlikely with 64 bits of entropy (P(collision) ~ 2.7e-10
for 100,000 identifiers).

### 1.4 Acquire Takes digest.Digest Interface

`Acquire` takes `dgst digest.Digest` (an interface), not a string. When
non-nil, it is passed directly to `s.cache.Acquire(dgst)` and to
`fetchWithDigest`. No string parsing or normalization needed -- the Digest
is already validated at construction time. Confirmed correct.

---

## 2. API Fitness

### Strengths

- **Minimal surface**: `New`, `Close`, `Acquire`, `Invalidate`,
  `GarbageCollect` -- complete for the use case.
- **Self-contained**: Store owns its cache and index under a root directory.
  Caller provides a path and optional capacity options.
- **Digest-optional Acquire**: Callers that know the digest get streaming;
  callers that don't get buffered hashing. Good API split.
- **FetchFunc type**: Clean callback abstraction, named and documented.
- **Invalidate semantics**: Removes index mapping only, cache blobs are
  shared and GC'd separately. Correct for content-addressed storage.

### No Concerns

The API is well-fitted to its use case. Nothing to flag.

---

## 3. Organization

Clean. Single file (401 lines), well-structured:
- Options and constructor: `Options`, `New`, `Close`, `GarbageCollect`
- Public API: `Acquire`, `Invalidate`
- Private fetch methods: `fetchWithDigest`, `fetchAndHash`
- Encoding helpers: `encodeDigest`, `decodeValue`, `hashKey`, `readAllSized`

The `doc.go` is accurate and concise.

Background writes use `s.wg.Go(func() { ... })` to track in-flight
operations. `Close()` calls `s.wg.Wait()` to drain before releasing
the index.

The `fetchWithDigest` and `fetchAndHash` methods each have a duplicated
"blob exists but index missing" tail (write index, return reader). This is
acceptable -- the two functions use different digest sources (caller-provided
vs computed), so sharing the tail would require a parameter without saving
meaningful lines.

---

## 4. Test Coverage (31 tests)

| Area | Tests | Quality |
|---|---|---|
| Basic flows | 4 | Miss +/- digest, hit, preserved timestamp |
| Digest mismatch | 2 | Wrong digest, no poisoning |
| Fetch failures | 3 | +/- digest, no poisoning |
| Content dedup | 3 | Same content diff IDs, same digest diff IDs, digest path cache hit |
| Empty/large content | 4 | Empty +/- digest, 5MB +/- digest |
| Truncated fetch | 3 | +/- digest, no poisoning |
| Concurrency | 5 | Same ID, diff IDs, with digest, mixed, rapid |
| Edge cases | 2 | Empty identifier, 10KB identifier |
| Invalidate | 2 | Triggers refetch, non-existent |
| Slow fetch + stress | 2 | Concurrent readers, mixed operations |
| Encoding stability | 1 | `TestAlgorithmIDStability` pins on-disk algorithm IDs (encoding_test.go) |

Tests are well-written: external package (store_test.go) and internal
package (encoding_test.go), all parallel, clean helpers (`computeDigest`,
`fetchFunc`, `failingFetch`, `countingFetch`, `truncatedReader`), good use
of `atomic.Int64` for fetch counting, poison tests verify recovery.

The `encoding_test.go` test pins the `algorithmToID` and `algorithmFromID`
maps -- any change to these on-disk IDs would break existing indexes.

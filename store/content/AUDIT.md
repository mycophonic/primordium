# store/content Audit

Audited: 2026-08-01 (updated from 2026-03-29, itself updated from 2026-03-11)

Files reviewed:
- `doc.go` (21 lines)
- `store.go` (622 lines)
- `store_test.go` (1311 lines)
- `encoding_test.go` (56 lines)

Dependencies examined: `cache`, `index`, `digest`, `fault` packages.

> **Scope note**: this pass updated the factual description of the package to
> match the current code — digest-less staging now writes to disk rather than
> buffering in memory, `AcquireFile` was added, and BLAKE3-256 became the
> staging algorithm. The correctness and API sections were re-read against the
> current source, but `AcquireFile`'s pinning and GC interaction have not had a
> dedicated adversarial review since they landed.

---

## 1. Code Correctness

### 1.1 Stale Index Decode Error Silently Swallowed

In `Acquire`, if `decodeValue(rec.Value)` fails (corrupt index value), the
error is silently swallowed and falls through to fetch. This is correct
self-healing behavior -- the fresh fetch overwrites the corrupt index entry.
For a cache, silent self-healing is the right call.

### 1.2 Digest-less Fetches Stage to Disk, Not Memory

`stage` writes the source to a scratch file under `root/staging` while hashing
it in the same pass, then rewinds and returns the open descriptor. Callers hand
this path arbitrarily large blobs — a flattened container rootfs is the
motivating case — so buffering in memory would make peak memory track content
size. Staging keeps it flat.

The scratch file is unlinked immediately after creation and used only through
the descriptor, so the kernel reclaims the space on close, including when the
process is killed mid-fetch. No stale-file sweep is needed and concurrent
callers cannot interfere.

Peak memory is instead bounded by `stageBufferSize` (16MiB) per concurrent
`stage` call — a deliberate constant, sized against measured throughput. See
§5.

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

### 1.5 L1: encodeDigest Panics on Algorithms Absent from the ID Registry

`encodeDigest` panics when `algorithmToID` has no entry for the digest's
algorithm. `digest.MD5` is a complete, exported, constructible algorithm in the
`digest` package — its import comment states it exists for external digest
verification — but it has no ID here. A caller passing an MD5 digest to
`Acquire` or `AcquireFile` reaches `encodeDigest` with no validation in
between, turning caller input into a crash.

Latent, not live: nothing in-tree constructs an MD5 digest or passes one to the
content store, and the only external `Acquire` caller is
`store/volatile/volatile.go`. ID 8 is free if MD5 should be supported;
alternatively `encodeDigest` could return an error rather than panic.

---

## 2. API Fitness

### Strengths

- **Minimal surface**: `New`, `Close`, `Acquire`, `AcquireFile`, `Invalidate`,
  `GarbageCollect` -- complete for the use case.
- **Self-contained**: Store owns its cache and index under a root directory.
  Caller provides a path and optional capacity options.
- **Digest-optional Acquire**: Callers that know the digest get streaming;
  callers that don't get staged hashing. Good API split.
- **AcquireFile for non-stream consumers**: Returns a complete, immutable,
  GC-pinned file for consumers that cannot tail an in-flight fetch — attaching
  a cached blob to a VM as a read-only block device is the motivating case.
- **FetchFunc type**: Clean callback abstraction, named and documented.
- **Invalidate semantics**: Removes index mapping only, cache blobs are
  shared and GC'd separately. Correct for content-addressed storage.

### No Concerns

The API is well-fitted to its use case. Nothing to flag.

---

## 3. Organization

Clean. Single file (622 lines), well-structured:
- Constants and registries: `stagingDir`, `stageBufferSize`, `algorithmToID`,
  `algorithmFromID`
- Options and constructor: `Options`, `New`, `Close`, `GarbageCollect`
- Public API: `Acquire`, `AcquireFile`, `Invalidate`
- Private fetch methods: `commitDigestless`, `fetchWithDigest`, `fetchAndHash`
- Encoding and staging helpers: `stage`, `encodeDigest`, `decodeValue`,
  `hashKey`

The `doc.go` is accurate and concise.

Background writes use `s.wg.Go(func() { ... })` to track in-flight
operations. `Close()` calls `s.wg.Wait()` to drain before releasing
the index.

Both `fetchWithDigest` and `fetchAndHash` write the index *before* the
writer's commit-and-unlock, so a drained reader is guaranteed to observe the
entry. `commitDigestless` exists because `Acquire`'s digest-less path commits
in a background goroutine after its reader drains, which would leave
`AcquireFile` racing the index write.

The `fetchWithDigest` and `fetchAndHash` methods each have a duplicated
"blob exists but index missing" tail (write index, return reader). This is
acceptable -- the two functions use different digest sources (caller-provided
vs computed), so sharing the tail would require a parameter without saving
meaningful lines.

---

## 4. Test Coverage (34 tests)

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
| AcquireFile | 2 | Cold and warm paths, blob shared with `Acquire` |
| Staging algorithm | 1 | `TestStore_DigestlessStagingUsesBLAKE3` |
| Encoding stability | 1 | `TestAlgorithmIDStability` pins on-disk algorithm IDs (encoding_test.go) |

Tests are well-written: external package (store_test.go) and internal
package (encoding_test.go), all parallel, clean helpers (`computeDigest`,
`fetchFunc`, `failingFetch`, `countingFetch`, `truncatedReader`), good use
of `atomic.Int64` for fetch counting, poison tests verify recovery.

The `encoding_test.go` test pins the `algorithmToID` and `algorithmFromID`
maps -- any change to these on-disk IDs would break existing indexes.

`TestStore_DigestlessStagingUsesBLAKE3` checks the staging algorithm
indirectly: it stages content without a digest, then re-acquires the same
bytes under a different identifier using an independently computed BLAKE3
digest and a fetch that fails if called. That catches content being hashed
with one algorithm but labelled with another, which comparing digest values
alone would miss.

---

## 5. Staging Algorithm and Buffer Size

Digest-less staging assigns BLAKE3-256 (registry ID 7). The digest names the
blob in the cache and is persisted in the index, so this is on-disk layout
rather than an implementation detail.

The change from BLAKE2b-256 is backward compatible: ID 5 still decodes, so
existing index entries and blobs resolve unchanged. Only newly staged content
gets BLAKE3. The cost is that content already stored under a BLAKE2b name will
not deduplicate against a new BLAKE3 name — transient double-storage, bounded
by quota and reclaimed by GC.

`stageBufferSize` is 16MiB because this BLAKE3 implementation parallelises
across goroutines *within* each `Write` call, so throughput scales with buffer
size. Measured staging 512MB from a streaming source on arm64 with 18 cores:

| 256KiB | 1MiB | 2MiB | 4MiB | 8MiB | 16MiB | 32MiB | 64MiB |
|---|---|---|---|---|---|---|---|
| 1950 | 3224 | 3728 | 4429 | 4733 | 4936 | 5100 | 5234 MB/s |

`io.Copy`'s default 32KiB would be the worst-performing option of any
algorithm available (1146MB/s). The curve flattens past 16MiB, and the buffer
is allocated per concurrent `stage` call, so the constant deliberately stops
short of the peak: the remaining ~6% costs four times the resident footprint
under concurrent fetches.

Measurements are arm64-only. The implementation ships amd64 assembly and the
same parallelism, so the ordering is expected to hold there, but that is
inference rather than measurement.

---

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 1 (L1: `encodeDigest` panic on unregistered algorithm) |
| INFORMATIONAL | 0 |

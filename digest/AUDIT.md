# Audit: digest

**Date**: 2026-08-01 (updated from 2026-03-29)
**Scope**: `primordium/digest` — content digest types, parsing, and hashing

## Summary

Clean, well-tested package providing content-addressable digest types. Supports
MD5, SHA1, SHA256, SHA384, SHA512, BLAKE2b-256, BLAKE2b-512, and BLAKE3-256
(8 algorithms). Provides parsing and validation of `algorithm:hex` strings, a
programmatic constructor from raw bytes, a hash constructor registry, and a
path-hashing utility for content-addressable storage.

5 files, ~267 lines of production code, ~480 lines of tests.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `const.go` | 23 | `MaxDigestSize`, `shorten` |
| `digest.go` | 193 | Algorithm registry, Digest type, New/FromString constructors |
| `hash.go` | 33 | HashPath utility (BLAKE2b-256 truncated) |
| `digest_test.go` | 480 | Parsing, construction, round-trip, vectors, and hash tests |

## API Surface

| Symbol | Description |
|---|---|
| `MaxDigestSize` | Constant (64): largest raw byte length across all algorithms |
| `Algorithm` | String type for algorithm identifiers |
| `Algorithm.Hash()` | Returns a new `hash.Hash` for the algorithm |
| `Digest` | Interface: `Algorithm()`, `Encoded()`, `String()` |
| `New(alg, raw)` | Creates a `Digest` from algorithm and raw hash bytes |
| `FromString(s)` | Parses `algorithm:hex` into a `Digest` |
| `HashPath(path)` | BLAKE2b-256 of a path, truncated to 16 hex chars |
| `MD5`, `SHA1`, `SHA256`, `SHA384`, `SHA512`, `BLAKE2b256`, `BLAKE2b512`, `BLAKE3256` | Algorithm constants |

## Implementation Notes

### BLAKE3-256 is parallel, and sensitive to write size

`BLAKE3256` is backed by `lukechampine.com/blake3`, which distributes each
`Write` call across goroutines over the eigentrees of that buffer. Throughput
therefore scales with how much content a caller hands it per call rather than
with total content size — measured on arm64 with 18 cores, staging ran at
1146MB/s with a 32KiB write size against 4936MB/s at 16MiB. Callers hashing
bulk content should feed it large buffers; see `stageBufferSize` in
`store/content`.

Two consequences worth knowing:

- It saturates every available core on a single hash, so it does not scale
  across concurrent hashers the way the one-core-per-hash algorithms do. Past
  roughly four concurrent hashes, SHA-256 delivers more aggregate throughput.
- Upstream ships amd64 assembly only. On arm64 the compression function is
  pure Go, and the parallelism is what carries it.

## Findings

### L1: Three parallel maps must stay in sync (mitigated)

`hashConstructors`, `anchoredEncodedRegexps`, and `digestSizes` must all be
updated when adding a new algorithm. A missing entry in
`anchoredEncodedRegexps` for an algorithm present in `hashConstructors` would
cause a nil pointer dereference in `FromString`. A missing `digestSizes`
entry silently makes `New` reject a known algorithm.

`TestAlgorithmRegistriesConsistent` now walks every exported algorithm through
`Hash()` → `New()` → `FromString()`, which fails if any of the three maps is
missing an entry, so the hazard surfaces at test time rather than at run time.

The structural point stands: a single struct-based registry would make this a
compile-time guarantee instead of a test-time one.

### L2: No `Hash()` unknown-algorithm panic test

The `Hash()` panic path for unknown algorithms is not tested. Unchanged since
the previous audit.

## Test Coverage (14 tests)

| Function | Tested | Notes |
|---|---|---|
| `FromString` | Yes | All 8 algorithms, no colon, unknown alg, invalid encoded |
| `Algorithm.Hash` | Yes | All 8 algorithms, size verification, functional check |
| `Algorithm.Hash` (panic) | **No** | Unknown algorithm panic path untested (L2) |
| `New` | Yes | All 8 algorithms, unknown alg, wrong size |
| `New → hex round-trip` | Yes | Hash → raw → New → Encoded matches original |
| `FromString → New round-trip` | Yes | FromString → hex.DecodeString(Encoded()) → New → String matches |
| `HashPath` | Yes | Length, format, determinism, differentiation |
| `Digest.String` | Implicit | Via round-trip in TestFromString_ValidDigests |
| Registry consistency | Yes | All 8 algorithms across all three maps (L1) |
| BLAKE3-256 vectors | Yes | Official vectors at 0/1/1024/2048/3072 bytes |
| BLAKE3-256 chunking | Yes | Digest independent of caller write size, across chunk and eigentree boundaries |

The BLAKE3 vector and chunking tests exist because the digest is a persistence
contract — content is addressed by it, so an upstream change that altered
output would silently invalidate every stored blob. The chunking test covers
the parallel `Write` path specifically, where the digest must not depend on
how the caller splits its input.

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 2 (L1 mitigated by test, structural point open; L2 open) |
| INFORMATIONAL | 0 |

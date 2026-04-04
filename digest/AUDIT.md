# Audit: digest

**Date**: 2026-03-29
**Scope**: `primordium/digest` — content digest types, parsing, and hashing

## Summary

Clean, well-tested package providing content-addressable digest types. Supports
MD5, SHA1, SHA256, SHA384, SHA512, BLAKE2b-256, and BLAKE2b-512 (7 algorithms).
Provides parsing and validation of `algorithm:hex` strings, a programmatic
constructor from raw bytes, a hash constructor registry, and a path-hashing
utility for content-addressable storage.

4 files, ~183 lines of production code, ~360 lines of tests.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `digest.go` | 183 | Algorithm registry, Digest type, New/FromString constructors |
| `hash.go` | 37 | HashPath utility (BLAKE2b-256 truncated) |
| `digest_test.go` | 360 | Parsing, construction, round-trip, and hash tests |

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
| `MD5`, `SHA1`, `SHA256`, `SHA384`, `SHA512`, `BLAKE2b256`, `BLAKE2b512` | Algorithm constants |

## Findings

### L1: Three parallel maps must stay in sync

`hashConstructors`, `anchoredEncodedRegexps`, and `digestSizes` must all be
updated when adding a new algorithm. A missing entry in
`anchoredEncodedRegexps` for an algorithm present in `hashConstructors` would
cause a nil pointer dereference in `FromString`. A missing `digestSizes`
entry silently makes `New` reject a known algorithm. Not a bug today — all
three maps are consistent across all 7 algorithms — but a maintenance hazard.
A single struct-based registry would make this a compile-time guarantee.

### L2: No `Hash()` unknown-algorithm panic test

The `Hash()` panic path for unknown algorithms is not tested.

## Test Coverage

| Function | Tested | Notes |
|---|---|---|
| `FromString` | Yes | All 7 algorithms, no colon, unknown alg, invalid encoded |
| `Algorithm.Hash` | Yes | All 7 algorithms, size verification, functional check |
| `Algorithm.Hash` (panic) | **No** | Unknown algorithm panic path untested (L2) |
| `New` | Yes | All 7 algorithms, unknown alg, wrong size |
| `New → hex round-trip` | Yes | Hash → raw → New → Encoded matches original |
| `FromString → New round-trip` | Yes | FromString → hex.DecodeString(Encoded()) → New → String matches |
| `HashPath` | Yes | Length, format, determinism, differentiation |
| `Digest.String` | Implicit | Via round-trip in TestFromString_ValidDigests |

## Open Counts

| Severity | Count |
|---|---|
| HIGH | 0 |
| MEDIUM | 0 |
| LOW | 2 |
| INFORMATIONAL | 0 |
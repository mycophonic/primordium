# fault — Code & API Audit

Audited: 2026-03-11
Platform: darwin/arm64
Go: 1.24

---

## Package Overview

Shared sentinel error catalog used across all mycophonic projects. Provides a
flat set of 16 domain-agnostic error categories for consistent `errors.Is`
matching.

## File Inventory

| File | Purpose |
|---|---|
| `doc.go` | Package documentation |
| `errors.go` | 16 sentinel error declarations |
| `errors_test.go` | 6 test groups |

3 files total.

---

## API Assessment

### Error Catalog

| Sentinel | Message | Callers |
|---|---|---|
| `ErrSystemFailure` | "critical system failure" | Low (entropy failures) |
| `ErrMissingRequirements` | "requirements failed" | Dependency checks |
| `ErrNotImplemented` | "not implemented" | Feature stubs |
| `ErrInvalidArgument` | "invalid argument" | Input validation |
| `ErrFilesystemFailure` | "filesystem failure" | File open/close |
| `ErrReadFailure` | "failed to read resource" | Read operations |
| `ErrWriteFailure` | "failed to write resource" | Write operations |
| `ErrNotFound` | "resource not found" | Lookup misses |
| `ErrAuthenticationFailure` | "failed to authenticate" | Auth flows |
| `ErrCancelled` | "operation has been explicitly cancelled" | Context cancellation |
| `ErrTimeout` | "operation failed to complete in the allocated time span" | Timeout handling |
| `ErrCommandFailure` | "command failed" | External binaries |
| `ErrInvalidJSON` | "invalid JSON" | JSON marshal/unmarshal |
| `ErrHashMismatch` | "hash mismatch" | Content-addressable verification |
| `ErrNetworkCommunication` | "network error" | Transport failures |
| `ErrUnacceptableResponse` | "unacceptable response" | HTTP non-OK responses |

### Design Choices

**Flat taxonomy.** All 16 sentinels are independent peers created with
`errors.New`. None wraps another. This is a deliberate design: downstream
packages create their own hierarchies by wrapping fault sentinels (e.g.,
`db/sqlite` wraps `ErrFilesystemFailure` into `ErrOpen` and `ErrClose`).

This is the right approach. A hierarchy within the sentinels themselves
would force all consumers into a single error categorization model. Keeping
sentinels flat lets each package define its own `errors.Is` chains.

**Var declarations, not types.** All sentinels are `error` values, not custom
types. This means `errors.As` is not useful with them — only `errors.Is`.
For sentinel-pattern errors this is correct; there's no additional data to
extract via type assertion.

**Message style.** Messages are lowercase, unpunctuated noun phrases. This
follows the Go convention that error messages should be composable via
`fmt.Errorf("outer: %w", err)` without awkward capitalization.

The `ErrCancelled` and `ErrTimeout` messages are notably longer than the
others ("operation has been explicitly cancelled" vs. just "cancelled"). This
is a stylistic inconsistency but functionally irrelevant — error messages are
for humans, and `errors.Is` matching uses identity, not string comparison.

### Coverage of Error Domains

The 16 sentinels cover the domains actually used across the monorepo:
filesystem operations, network transport, authentication, JSON handling,
content-addressable storage, external commands, and lifecycle signals
(cancellation, timeout). No obvious domain gaps for the current codebase.

---

## Test Quality

### Coverage

`[no statements]` — expected. The package declares only `var` values; there
is no executable code to cover. All verification is in the test file.

### Test Inventory

| Test | What it covers |
|---|---|
| `TestSentinelErrors_Exist` | Non-nil, non-empty message for each sentinel |
| `TestSentinelErrors_Identity` | `errors.Is(err, err)` reflexivity |
| `TestSentinelErrors_Wrapping` | Single and double `%w` wrapping preserves `errors.Is` |
| `TestSentinelErrors_Distinctness` | Pairwise `errors.Is` returns false for different sentinels |
| `TestSentinelErrors_MultipleWrapping` | Multi-`%w` wrapping preserves both sentinels |
| `TestSentinelErrors_ErrorMessages` | Message contains expected keyword |

### Issue: `ErrTimeout` Missing from 3 of 4 Exhaustive Tests

`ErrTimeout` appears in `TestSentinelErrors_Distinctness` (paired against
`ErrCancelled`) but is absent from:

- `TestSentinelErrors_Exist` — 15 of 16 sentinels listed, `ErrTimeout` missing
- `TestSentinelErrors_Identity` — same 15, `ErrTimeout` missing
- `TestSentinelErrors_ErrorMessages` — same 15, `ErrTimeout` missing

`ErrTimeout` has 5 production callers (r2, system process management, ffmpeg/
ffprobe integration). It should be added to all three test tables.

### Observation: Partial Distinctness Coverage

`TestSentinelErrors_Distinctness` checks 5 pairs out of C(16,2) = 120
possible pairs. This is a pragmatic sampling — checking every pair would be
noisy. The selected pairs cover the most likely confusion cases (System vs.
Filesystem, NotFound vs. NotImplemented, Read vs. Write, Cancelled vs.
Timeout, InvalidArgument vs. InvalidJSON). Since all sentinels are created
with `errors.New` (distinct allocations), exhaustive pairing is unnecessary.

### Observation: Wrapping Test Covers 4 of 16 Sentinels

`TestSentinelErrors_Wrapping` tests wrapping for `ErrSystemFailure`,
`ErrFilesystemFailure`, `ErrNotFound`, and `ErrInvalidArgument`. Since the
wrapping behavior comes from Go's `fmt.Errorf` / `errors.Is` machinery (not
from anything in the package), testing 4 is sufficient to verify the
mechanism.

---

## Issues

### Bug: `ErrTimeout` Missing from Test Coverage

`ErrTimeout` is not tested for existence, identity, or message content. It is
the only sentinel out of 16 with incomplete test coverage. The fix is adding
it to the three test tables.

---

## Verdict

Minimal, correct package. The flat sentinel design is the right architecture
for a shared error catalog — it provides consistent `errors.Is` targets
without imposing hierarchy. The test suite is thorough with one gap:
`ErrTimeout` omitted from 3 exhaustive test tables. No code bugs (there is no
executable code). No API design issues.

# Audit: `system/machine`

**Date**: 2026-03-29 (updated from 2026-03-11)
**Package**: `github.com/mycophonic/primordium/system/machine`
**Files**: 5 (`doc.go`, `machine.go`, `host_darwin.go`, `host_linux.go`, `host_windows.go`)
**Public API**: `Host()`, `Info`, `Tier`, `TierLow`/`TierMid`/`TierHigh`
**Test coverage**: 0%

---

## Purpose

Collects host machine stats (CPU, RAM, disk) and classifies them into a capability tier (low/mid/high). Used to adapt application behavior to machine resources.

Currently called from one site: `cmd/test-debug/main.go`.

---

## Code Correctness

### `classify` logic is correct

Boundaries:
- **TierHigh**: cores >= 9 AND RAM >= 32 GB
- **TierLow**: cores <= 4 OR RAM <= 8 GB
- **TierMid**: everything else (5-8 cores AND 9-31 GB)

The OR in the low-tier condition means either weak CPU or low RAM alone disqualifies a machine. Both strong CPU and RAM required for high tier. Reasonable.

### `Tier.String()` panics on out-of-range values

```go
func (t Tier) String() string {
    return [...]string{"low", "mid", "high"}[t]
}
```

Any `Tier` value outside 0-2 causes an index-out-of-range panic. Since `Tier` is an exported `int`, callers can construct invalid values. Low risk in practice — `Host()` always sets it via `classify()` — but a `fmt.Sprintf("%v", badTier)` would crash.

### darwin: available memory estimate is rough

`(free + inactive) * pageSize` is an approximation. macOS has no `MemAvailable` equivalent. The comment acknowledges this. Acceptable for tier classification.

---

## API Fitness

| Aspect | Assessment |
|--------|------------|
| **`Host() (Info, error)`** | Clean, minimal. Returns all data in one call. |
| **No `context.Context`** | Acceptable for init-time use. darwin execs `vm_stat` (for available memory — no syscall equivalent exists) and linux reads two proc files — all fast. Would need a context if used in a request path, but that's not the current use case. |
| **`Info` struct** | Well-documented fields with units in comments. Tier is derived, not manually set. |
| **Platform coverage** | darwin, linux, windows — comprehensive. |
| **`dirs.HomeDir()` dependency** | Used to determine disk volume. Does not require `dirs.SetAppName()` — safe to call without prior init. |

---

## Organization

Clean platform split:
- `machine.go` — pure logic (types, constants, classify)
- `host_{darwin,linux,windows}.go` — platform-specific data collection
- Each platform file follows the same `Host()` → helpers pattern

---

## Test Coverage

0%. No test files.

### What's testable

**Pure logic (no platform dependency):**
- `classify` — boundary testing for all tier transitions
- `Tier.String()` — valid and invalid values

**Platform parsing helpers (build-tag gated but deterministic):**
- darwin: `parseVMStatPageSize`, `parseVMStatLine`
- linux: `parseMemInfoLine`

**Integration:**
- `Host()` — can be called in tests on the build platform to verify it returns non-zero values and doesn't error

---

## Findings

### No tests

The `classify` function has boundary logic that is exactly the kind of code where off-by-one errors hide. It should have exhaustive boundary tests. The parsing helpers are pure functions on string input -- trivially testable.

### Only one caller

`cmd/test-debug/main.go` is the sole user. If this package is intended for broader use (e.g., auto-tuning server parameters based on tier), the API is ready. If it's experimental/dead, that's worth noting.

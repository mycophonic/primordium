# Audit: primordium/pathcheck

**Date:** 2026-03-29
**Scope:** All files in `pathcheck/` — path.go, path_unix.go, path_windows.go, const.go, errors.go, doc.go, socket_unix.go, socket_linux.go, socket_windows.go, path_test.go, path_unix_test.go
**Lines of code:** ~100 (source) + ~90 (test)

---

## Summary

Small, focused package enforcing platform-specific filesystem path validation. Three public functions: `Validate` (full path), `ValidateComponent` (single component), and `ValidateSocket` (socket path length). Proper build constraints for Unix/macOS/Linux/Windows. Clean error wrapping through `fault.ErrInvalidArgument`. Correct security posture — rejects path traversal, null bytes, reserved device names.

---

## Findings

---

### INFORMATIONAL

**I1. `ValidateComponent` correctly guards direct callers against `/`.** The `reservedCharacters` regex on Unix includes `/`, which matters when `ValidateComponent` is called directly (not via `Validate`). External callers (`store/refcount/refcounted.go`, `r2/r2.go`, `filesystem/dirs/init.go`) call `ValidateComponent` directly on keys/names, so this check is essential.

**I2. Windows validation cannot be tested on macOS/Linux.** The Windows-specific tests in `path_test.go:127-149` are gated behind `runtime.GOOS == "windows"`. The Windows `validatePlatformSpecific` (device names, trailing dot/space, control chars) is never exercised on non-Windows CI. This is inherent to build-constrained code, not a bug.

**I3. Error design is good.** `ErrInvalidPath` wraps `fault.ErrInvalidArgument` via `fmt.Errorf`, making it distinguishable via `errors.Is`. Internal errors (`errInvalidPathTooLong`, `errForbiddenChars`, etc.) are joined via `errors.Join`, so callers can match on both the sentinel and the specific cause.

**I4. Test quality is solid.** Path traversal, boundary lengths, valid/invalid components, double separators, and error message content are all verified. Tests use `t.Parallel()` throughout. The `gotest.tools/v3/assert` dependency is used cleanly.

---

## Usage Summary

| Caller | Function | Purpose |
|---|---|---|
| `store/refcount/refcounted.go` | `Validate()` | Validates root directory at `NewLocker` init (panics on invalid) |
| `store/refcount/refcounted.go` | `ValidateComponent()` | Validates resource keys before path construction |
| `r2/r2.go` | `ValidateComponent()` | Validates S3 object key components before local filesystem use |
| `filesystem/dirs/init.go` | `ValidateComponent()` | Validates app name in `SetAppName` |

## Test Coverage Summary

| Function | Coverage | Notes |
|---|---|---|
| `Validate` | Full | Path traversal, valid paths, double separators |
| `ValidateComponent` | Full | Length boundary, empty/whitespace, platform chars, keywords |
| `ValidateSocket` | Full (per-platform) | Boundary lengths, error message content |
| `validatePlatformSpecific` (unix) | Full | Null bytes, slashes, `.` and `..` |
| `validatePlatformSpecific` (windows) | Untested on non-Windows | Device names, trailing dot/space, control chars |

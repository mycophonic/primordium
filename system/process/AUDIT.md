# Audit: primordium/system/process

**Date:** 2026-03-29
**Scope:** `system/process/` — `doc.go`, `process.go`, `process_unix.go`, `process_windows.go`, `process_unix_test.go`
**Consumers:** `sclerotium/server/meilisearch`, `sclerotium/dgraph`, `sclerotium/server/qdrant` — all managing external database processes.

---

## Overall Assessment

Minimal, focused package. Single public function `StopProcess` with platform-specific implementations (SIGTERM→wait→SIGKILL on Unix, Kill directly on Windows). The `exited` channel contract is clean and all three consumers use it identically: `make(chan struct{})`, close on `cmd.Wait()` return.

---

## Findings

### Note

**N1. The `exited` channel contract is clear and consistent.** All consumers create the channel, launch a goroutine that calls `cmd.Wait()` then closes it. `StopProcess` only reads from it. No ownership ambiguity.

**N2. Windows implementation is appropriately simple.** Windows has no SIGTERM equivalent, so direct Kill is the right approach. The `shutdownTimeout` parameter is correctly ignored (underscore).

---

## Test Coverage

4 tests in `process_unix_test.go`:

| Test | What it covers |
|------|----------------|
| `TestStopProcess_NilProcess` | nil proc returns nil immediately |
| `TestStopProcess_GracefulShutdown` | SIGTERM kills a normal subprocess |
| `TestStopProcess_AlreadyExited` | Calling StopProcess on an already-exited process succeeds |
| `TestStopProcess_EscalatesToKill` | SIGTERM-trapping child forces escalation to SIGKILL after timeout |

| Area | Coverage | Notes |
|------|----------|-------|
| `StopProcess` (unix) | Good | All paths: nil, graceful, already-exited, escalation |
| `StopProcess` (windows) | None | No Windows CI |
| `waitForExit` | Indirect | Exercised by escalation test |

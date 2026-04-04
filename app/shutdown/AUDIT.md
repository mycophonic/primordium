# Audit: `app/shutdown` package

**Date:** 2026-03-11
**Scope:** `github.com/mycophonic/primordium/app/shutdown`
**Files:** doc.go, shutdown.go, signals_unix.go, signals_windows.go

---

## 1. Package purpose

Process lifecycle management: signal handling, registered cleanup handlers (LIFO, exactly-once via `sync.Once`), panic recovery, and supervised goroutine launch. Provides three shutdown paths — signal-triggered (via `SetDefaults`), main-function wrapper (`Run`), and goroutine panic (`Go`). All paths converge on the same `Shutdown()` function.

Consumers: `app/app.go` (calls `SetDefaults`), `enoki/cmd/enoki/main.go` and `enoki/app/app.go` (call `Register`), `quark/kit/shutdown.go` (calls `Register` + `Shutdown`).

---

## 2. Correctness

### 2.1 Second signal does not force-exit

After receiving the first signal, `signal.Stop(sigChan)` deregisters the channel (line 51). A second Ctrl+C is not caught — the user must wait up to the 10s timeout. A common pattern is "second signal = immediate `os.Exit(1)`" for impatient operators. Current behavior is safe but may feel unresponsive if a handler blocks.

### 2.2 Handler panic aborts remaining handlers

If handler `i` panics, handlers `i-1` through `0` never run. No recovery in the `Shutdown` loop. Documented explicitly in `doc.go` ("crashes with a stack trace rather than being silently swallowed"). This is a conscious tradeoff: visibility over resilience. An alternative would be to recover per-handler and continue, but the current design is defensible.

### 2.3 `Register` after `Shutdown` is silently lost

If `Shutdown()` has already fired (via `sync.Once`), a subsequent `Register` call succeeds (appends to the slice) but the handler never executes. No error, no warning. This is observable in `quark/kit/shutdown.go` — if the signal goroutine fires `Shutdown()` before `kit.Shutdown()` is called, the GC handler registered at line 12 is silently dropped.

---

## 3. API fitness

### 3.1 `SetDefaults` + `Register` + `Shutdown` — clean core

The three-function core is straightforward. `SetDefaults` returns a cancellable context for signal propagation. `Register` accepts `func()` (no error return — handlers should log internally). `Shutdown` is idempotent. LIFO order mirrors `defer` semantics.

### 3.2 `Run` and `Go` — zero callers

`Run` (main-function wrapper with panic recovery + shutdown + `os.Exit`) and `Go` (goroutine launcher with panic recovery) are exported but unused across the entire codebase. All consumers use the `SetDefaults` + `Register` pattern and manage their own main loops.

`Run` exists to solve a real problem — ensuring `Shutdown()` runs on all exit paths including normal return. Consumers that don't use `Run` must call `Shutdown()` themselves or rely solely on signal-triggered shutdown.

### 3.3 Platform-specific signal files — correct

Unix: SIGINT, SIGTERM, SIGHUP, SIGQUIT. Windows: SIGINT, SIGTERM. Build tags correctly partition the signal lists. SIGHUP and SIGQUIT are appropriate for Unix daemon lifecycle.

---

## 4. Organization

- `doc.go`: Thorough documentation covering all covered and uncovered shutdown scenarios
- `shutdown.go`: Full implementation (~150 lines) — `SetDefaults`, `Register`, `Shutdown`, `Run`, `Go`
- `signals_unix.go`: Unix signal list with build tags
- `signals_windows.go`: Windows signal list with build tags

Clean separation. `doc.go` is notably well-written — explicit about what panics are and aren't covered, and why SIGKILL is out of scope.

---

## 5. Test coverage

No tests. No test file exists.

The package is inherently difficult to unit test due to `os.Exit` calls, global mutable state (`sync.Once`, global slice), and signal handling. Testing would require subprocess execution (`exec.Command` + checking exit codes). The implementation is small and straightforward, which partially mitigates the absence.

`Shutdown` LIFO ordering and `Register` concurrency could be tested in isolation if `os.Exit` were factored out (e.g., an `exitFunc` field for testing), but this would add complexity to a deliberately minimal package.

---

## 6. Summary

| Area           | Rating    | Notes                                                                           |
|----------------|-----------|---------------------------------------------------------------------------------|
| Correctness    | Good      | Sound core; 2.3 (silent late-register) is latent                                 |
| API fitness    | Good      | Clean three-function core; `Run`/`Go` unused but well-designed                  |
| Organization   | Good      | Clean platform split; excellent doc.go                                          |
| Test coverage  | None      | No tests; inherently hard to test due to os.Exit and global state               |

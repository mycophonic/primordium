# Audit: package `app`

**Date:** 2026-03-29
**Scope:** `/app/app.go`, `/app/doc.go` (parent package only, sub-packages excluded)

---

## Package Purpose

Provides a single `New()` function that initializes application lifecycle subsystems in order: filesystem, logger, network, crash reporter (Sentry), rlimit, and shutdown signal handling. Registers teardown handlers for reporter and stores. Returns a context that cancels on shutdown signals.

---

## Code Correctness

### Initialization order is correct and deliberate

The comment-documented ordering (filesystem -> logger -> network -> sentry -> rlimit -> shutdown) is sound. Filesystem must come first because `dirs.SetAppName` gates all path resolution. Logger must come before Sentry because `reporter.Initialize` logs on success/failure via `slog`. Shutdown is last so that the returned context inherits all prior state.

### `logger.SetDefaultsForLogger` context parameter unused

`app.go:54` passes `ctx` to `logger.SetDefaultsForLogger`, but the logger function's signature is `_ context.Context` -- it discards the parameter. This is not a bug, but the call site gives the impression that the context influences logger behavior when it does not.

### Sentry `PII: true` hardcoded

`app.go:64` unconditionally sets `PII: true` in the reporter config. This is not configurable via `Options`. If this is intentional (all callers want PII), it should be documented on `Options`. If not, it should be an option.

### `TracesSampleRate: 1.0` hardcoded

Similarly, `app.go:67` hardcodes a 100% trace sample rate. For production services this may be excessive. The `reporter.Config` already supports a zero-means-default pattern, but `app.New` bypasses it by always passing `1.0`.

---

## API Fitness

### `Options` struct design

`New()` panics immediately with a clear message (`"app.New: Options.Name must not be empty"`) if `Name` is empty, before any subsystem initialization.

### `New` returns only `context.Context`

There is no way for the caller to know if any of the initialization steps failed (beyond Sentry, which logs). `filesystem.Initialize`, `network.SetDefaults`, and `rlimit.RaiseNoFileLimit` are all fire-and-forget. This is acceptable for a "best effort" initialization pattern, but it means the caller cannot distinguish between a fully initialized app and a partially initialized one.

---

## Organization

The package is minimal (one code file, one doc file) and well-focused. The `New()` function is the only exported symbol besides `Options`. Sub-packages (`logger`, `reporter`, `shutdown`) are cleanly separated by concern.

The doc.go comment is accurate: "Package app provides application lifecycle helpers (initialization for filesystem, network, logger) and shutdown."

---

## Test Coverage

**There are no tests for the `app` parent package.** The test report confirms 0% coverage for this file.

This is the most significant finding. `New()` is untestable in its current form because:

1. It calls `filesystem.Initialize` which uses `sync.Once` via `dirs.SetAppName` -- once set, it cannot be reset between tests.
2. It calls `network.SetDefaults` which modifies global `http.DefaultClient` and similar globals.
3. It calls `rlimit.RaiseNoFileLimit` which modifies process-level resource limits.
4. It calls `shutdown.SetDefaults` which registers signal handlers via `signal.Notify` and uses `sync.Once`.

All of these are global side effects that cannot be isolated in tests. The function is effectively an integration point.

**Recommendation:** At minimum, a single integration test (possibly with `TestMain`) could verify that `New()` does not panic with valid `Options` and returns a non-nil context. The global-once nature of the subsystems means only one such test can run per process, but that is still better than zero.

---

## Summary of Findings

| Finding                                                  | Severity |
|----------------------------------------------------------|----------|
| Zero test coverage for parent package                    | High     |
| `PII` and `TracesSampleRate` hardcoded, not configurable | Low      |
| Unused context parameter passed to logger                | Cosmetic |

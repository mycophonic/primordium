# Audit: `app/logger` package

**Date:** 2026-03-11
**Scope:** `github.com/mycophonic/primordium/app/logger`
**Files:** doc.go, logger.go

---

## 1. Package purpose

Configures zerolog as a backend for stdlib `slog` via the `slog-zerolog` bridge. Single entry point `SetDefaultsForLogger` sets up console output to stderr, applies a log level (explicit or from `LOG_LEVEL` env var), wires zerolog as the `slog.Default` handler, and returns whether debug mode is active.

Single consumer: `app/app.go:46` — called once at startup. Zerolog is only imported here; the rest of the codebase uses `slog`.

---

## 2. Correctness

### 2.1 `Disabled` mapped to `slog.LevelInfo`

`zerologToSlog` maps `zerolog.Disabled` to `slog.LevelInfo`. When `zerolog.Disabled` is set, zerolog emits nothing — but slog would still emit info-level messages to the zerolog handler, which would then drop them. No user-visible bug (messages are suppressed either way), but the slog level should be set higher than any real level to prevent unnecessary handler invocations. `slog.LevelError + 1` or a large sentinel would be more accurate.

### 2.2 Unused context parameter

`SetDefaultsForLogger` accepts `context.Context` but discards it (`_ context.Context`). No current use for it — the function only touches global state (zerolog global level, slog default).

---

## 3. API fitness

### 3.1 Variadic level — clean

`level ...zerolog.Level` lets callers either pass an explicit level or omit it to fall back to `LOG_LEVEL` env var. The caller (`app.New`) uses the env var path.

### 3.2 Bool return for debug mode — pragmatic

Returns `effectiveLevel == zerolog.DebugLevel`. Used by `app.New` to configure Sentry's debug flag. Trace level returns `false`, which is debatable — trace is strictly more verbose than debug. If Sentry debug output is desired at trace level too, the check should be `effectiveLevel <= zerolog.DebugLevel`.

### 3.3 zerolog leak in public API

`SetDefaultsForLogger` takes `zerolog.Level` as a parameter type, exposing the zerolog dependency to callers. Currently only `app.go` calls it and doesn't pass a level, so the leak is dormant. If other callers ever need to pass an explicit level, they'd need to import zerolog directly.

---

## 4. Organization

- `doc.go`: Package documentation
- `logger.go`: Full implementation (88 lines) — `SetDefaultsForLogger`, `zerologToSlog`

Minimal, appropriate for a thin configuration wrapper.

---

## 5. Test coverage

No tests. No test file exists.

The package is a thin configuration wrapper over two third-party libraries with no branching logic of its own beyond the level mapping. `zerologToSlog` is the only function with non-trivial logic (switch statement with 8 cases + default).

---

## 6. Summary

| Area           | Rating    | Notes                                                                    |
|----------------|-----------|--------------------------------------------------------------------------|
| Correctness    | Good      | Functional; `Disabled` → `slog.LevelInfo` mapping is imprecise but safe |
| API fitness    | Good      | Clean variadic level; zerolog type in public signature is dormant leak   |
| Organization   | Good      | Minimal, appropriate                                                     |
| Test coverage  | None      | No tests; thin wrapper, low risk                                         |

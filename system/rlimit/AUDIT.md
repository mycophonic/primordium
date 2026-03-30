# Audit: `system/rlimit`

**Date**: 2026-03-11
**Package**: `github.com/mycophonic/primordium/system/rlimit`
**Files**: 3 (`doc.go`, `rlimit_unix.go`, `rlimit_windows.go`)
**Public API**: `RaiseNoFileLimit()`
**Test coverage**: 100%

---

## Purpose

Ensure exec'd subprocesses (qdrant, meilisearch, dgraph) inherit a high RLIMIT_NOFILE.

Since Go 1.19, the runtime auto-raises the soft limit for the Go process itself, but **restores the original low limit before exec'ing children**. The only way to opt out of that restoration is an explicit `syscall.Setrlimit` call that succeeds. That's what this package does.

Called from 4 sites: `app.New()`, and the `Start()` methods of qdrant, meilisearch, and dgraph servers.

---

## Findings

No remaining issues.

---

## API Fitness

| Aspect | Assessment |
|--------|------------|
| **Hardcoded constant** | 65536 as a floor is reasonable for the use case. The function now preserves higher limits when present. |
| **No error return** | Acceptable for best-effort init-time call. |
| **Windows no-op** | Correct — Windows doesn't have RLIMIT_NOFILE. |
| **Build tag separation** | Clean `unix`/`windows` split. |

# filesystem/dirs — Code & API Audit

Audited: 2026-03-29
Platform: darwin/arm64
Go: 1.24

---

## Package Overview

Cross-platform XDG-style directory resolution. Maps logical locations (data,
config, cache, runtime, bin) to platform-specific filesystem paths and ensures
they exist via `MkdirAll`.

17 files across the monorepo import this package. `DataDir()` is by far the
most-called function (~25 call sites in sclerotium and cc0 CLIs alone).

## File Inventory

| File | Purpose |
|---|---|
| `doc.go` | Package documentation |
| `init.go` | `SetAppName` — sets the global app name used in path construction |
| `locations.go` | `HomeDir`, `RuntimeDir`, `DataDir`, `ConfigDir`, `CacheDir`, `BinDir` |

3 files. No test files.

---

## API Assessment

### Initialization

```go
var (
	nameOnce sync.Once
	name     string
)

func SetAppName(appName string)
```

Package-level global, set once during app startup via `filesystem.Initialize`
→ `dirs.SetAppName`. Protected by `sync.Once` — subsequent calls are silently
ignored. `SetAppName` validates the app name via `pathcheck.ValidateComponent`
and panics on invalid input (empty, path traversal, etc.).

If `SetAppName` is never called, `name` retains its zero value (empty string).
Directory functions would then construct paths with a trailing separator
(e.g., `~/.local/share/` on Linux, `~/Library/Application Support/` on macOS),
pointing at the base XDG directory itself rather than an app-specific
subdirectory. There is no guard to catch this — callers must ensure
`SetAppName` is called before any directory function.

### Directory Functions

| Function | Returns | Failure Mode | Creates Dir |
|---|---|---|---|
| `HomeDir()` | `string` | Panics | No |
| `RuntimeDir()` | `(string, error)` | Returns error | Yes |
| `DataDir()` | `(string, error)` | Returns error (panics via `HomeDir` if home unknown) | Yes |
| `ConfigDir()` | `(string, error)` | Panics if config dir unknown, returns error on MkdirAll | Yes |
| `CacheDir(sub ...string)` | `(string, error)` | Returns error (panics via `HomeDir` if home unknown) | Yes |
| `BinDir()` | `(string, error)` | Returns error | Yes |

The panic-vs-error split is actually consistent:
- **Panic**: "Cannot determine WHERE the directory should be" — indicates a
  fundamentally broken system (no `$HOME`, no `os.UserConfigDir`).
- **Error**: "Cannot CREATE the directory" — filesystem permission or I/O issue.

`DataDir`, `CacheDir`, and `RuntimeDir` can also panic indirectly via
`HomeDir()`, so the distinction is about the primary failure mode, not a
guarantee that errors are the only non-panic path.

### Platform Resolution

Correct for all three platforms:

| Location | Linux | macOS | Windows |
|---|---|---|---|
| Data | `$XDG_DATA_HOME/<name>` or `~/.local/share/<name>` | `~/Library/Application Support/<name>` | `%LOCALAPPDATA%\<name>` |
| Config | `os.UserConfigDir()/<name>` | (same as data via UserConfigDir) | `%AppData%\<name>` |
| Cache | `$XDG_CACHE_HOME/<name>` or `~/.cache/<name>` | `~/Library/Caches/<name>` | `%LOCALAPPDATA%\<name>\cache` |
| Runtime | `$XDG_RUNTIME_DIR/<name>` or `os.TempDir()/<name>` | `os.TempDir()/<name>` | `os.TempDir()/<name>` |
| Bin | cache + `/bin` | cache + `/bin` | cache + `\bin` |

The Windows fallbacks (when `%LOCALAPPDATA%` is unset) correctly construct
paths from `HomeDir()` + `AppData\Local`, matching the standard location.

`ConfigDir` delegates to `os.UserConfigDir()` from the standard library,
while `DataDir` and `CacheDir` implement their own resolution. This means
`ConfigDir`'s Linux behavior follows whatever Go's stdlib does for
`$XDG_CONFIG_HOME`, while `DataDir` independently reads `$XDG_DATA_HOME`.
Both are correct, but the implementation approach differs.

### `CacheDir` Variadic Subdirectory

```go
func CacheDir(sub ...string) (string, error)
```

Clean API — allows `dirs.CacheDir("discogs-api")` to create a namespaced
subdirectory in one call. Only `CacheDir` has this; the other functions don't.
This asymmetry is fine since cache is the only location where callers commonly
need subdirectories (multiple caches per app, vs. one data/config dir).

### `BinDir`

Delegates to `CacheDir()` then appends `"bin"`. Double `MkdirAll` (once in
`CacheDir`, once for `cache/bin`) is correct — the parent may exist while the
child doesn't.

---

## Issues

### Bug: No Tests

Zero test files, zero coverage. For a package with:
- 3 platform-specific code paths (Linux/macOS/Windows)
- XDG environment variable fallbacks
- Directory creation side effects
- Panic conditions

this is a significant gap. The platform resolution logic is non-trivial
(env var → fallback chains) and varies per OS. At minimum, the `getDataDir`
and `getCacheDir` functions should be tested with controlled environment
variables.

Testable without filesystem side effects: mock `$XDG_DATA_HOME` etc. via
`t.Setenv`, verify returned paths without actually calling `MkdirAll`
(test `getDataDir`/`getCacheDir` directly, or test the public functions
with `t.TempDir()`-based env vars).

---

## Verdict

Functionally correct platform resolution logic. The XDG fallback chains are
right, the Windows paths are right, the panic-vs-error split is internally
consistent. Remaining issue: zero test coverage for a package with
platform-specific branching and environment variable handling. This package
is load-bearing (17 importers, ~40 call sites) and untested.

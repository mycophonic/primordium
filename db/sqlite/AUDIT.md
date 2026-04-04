# db/sqlite — Code & API Audit

Audited: 2026-03-11
Platform: darwin/arm64
Go: 1.24

---

## Package Overview

SQLite database wrapper providing driver-agnostic connection management with
pre-configured pragma profiles for different workloads (read-only, read-write,
bulk import, vacuum). Uses `database/sql` as the abstraction layer.

76 files across the monorepo import this package.

## File Inventory

| File | Purpose |
|---|---|
| `doc.go` | Package documentation |
| `db.go` | `DB` type, `OpenWithDriver`, `VacuumInto`, pragmas, metadata, exec |
| `errors.go` | Sentinel errors (`ErrOpen`, `ErrClose`, `ErrRead`, `ErrWrite`) |
| `pure.go` | `Open` convenience function (modernc.org/sqlite driver) |
| `scan.go` | `Deref` helper for nullable string columns |
| `db_test.go` | 16 tests |

6 files total.

---

## API Assessment

### Pragma Configurations

Four pre-configured profiles, each well-documented:

| Profile | Use Case | Key Properties |
|---|---|---|
| `PragmasReadOnly` | Serving databases | WAL, NORMAL sync, 64 MB cache |
| `PragmasReadWrite` | Occasional writes | WAL, FULL sync, foreign keys ON |
| `PragmasImport` | Bulk import (disposable DB) | No journal, no sync, 4 GB cache, 8 GB mmap |
| `PragmasVacuum` | VACUUM operations | No journal, no sync, 4 GB cache, 8 GB mmap |

The import pragmas comment correctly warns about ordering: `page_size` and
`auto_vacuum` must precede `journal_mode`. This ordering constraint is subtle
and easy to get wrong; the documentation is valuable.

All profiles set `MaxConns: 1`. For SQLite, a single-connection pool avoids
locking contention and is the simplest correct choice. WAL mode does support
concurrent readers, but the single-connection approach eliminates an entire
class of `SQLITE_BUSY` errors. For the workloads in this codebase (serve
pre-built databases, bulk import), this is the right tradeoff.

### `OpenWithDriver`

```go
func OpenWithDriver(ctx context.Context, driver, path string, pragmas Pragmas) (*DB, error)
```

Clean design. Driver-agnostic core allows both modernc (pure Go) and CGo
SQLite drivers to be used interchangeably. Pragmas are applied sequentially,
with proper cleanup on failure (connection closed if any pragma fails).

The `MaxConns` conversion from `uint` to `int` uses `min(pragmas.MaxConns,
uint(math.MaxInt))` — correct clamping for the `uint → int` narrowing.

### `Open`

```go
func Open(ctx context.Context, path string) (*DB, error)
```

Convenience wrapper that hardcodes the modernc driver (`"sqlite"`) and
`PragmasReadOnly`. This is the standard entry point for read-only consumers.
23 callers across the codebase.

### `VacuumInto`

```go
func VacuumInto(ctx context.Context, driver, path, destPath string) (err error)
```

Uses named return with deferred close to capture both the vacuum error and
the close error. The logic is correct: `closeErr` only overwrites `err` when
the main operation succeeded (`err == nil`). If vacuum fails, the close error
is silently dropped, which is the right priority.

`VACUUM INTO ?` uses parameterized binding for the destination path — good,
though SQLite's VACUUM INTO does not have SQL injection risk (file path, not
data). The habit of using parameters is still correct.

### `SetMetadata` / `GetMetadata`

Key-value pair operations on a `metadata` table. Assumes the table exists
(caller's responsibility to create it). `GetMetadata` returns `("", nil)` for
missing keys rather than an error — this is a reasonable design choice that
avoids forcing callers to handle `ErrNotFound` for an expected condition.

### `ExecStatements`

```go
func ExecStatements(ctx context.Context, raw string) error
```

Executes semicolon-delimited SQL atomically in a transaction. Handles empty
and whitespace-only input gracefully (returns nil without beginning a
transaction). Relies on the modernc driver's support for multi-statement
`ExecContext` — this is not universal across Go SQL drivers, but is verified
by the `TestExecStatementsAtomicity` test which proves that a failing second
statement rolls back the first.

### `Deref`

```go
func Deref(s *string) string
```

Simple nil-safe string dereference. 13 callers across `cc0/musicbrainz/local`
and `cc0/discogs/local`. Trivially correct.

This is a generic utility (not SQLite-specific) that happens to live here
because its callers all do SQL row scanning. The naming and placement are
pragmatic rather than architecturally pure, which is fine for a helper this
small.

### Error Hierarchy

```
fault.ErrFilesystemFailure
  └── sqlite.ErrOpen ("sqlite open failure")
  └── sqlite.ErrClose ("sqlite close failure")
fault.ErrReadFailure = sqlite.ErrRead
fault.ErrWriteFailure = sqlite.ErrWrite
```

`ErrOpen` and `ErrClose` are sub-errors of `fault.ErrFilesystemFailure`
(wrapped via `fmt.Errorf("%w: ...")`) — correct, since open/close failures are
filesystem-level. `ErrRead` and `ErrWrite` directly alias `fault.ErrReadFailure`
and `fault.ErrWriteFailure` — also correct, since read/write failures are
broader than filesystem issues.

All error returns in the package use `fmt.Errorf("%w: ...: %w", sentinel,
underlyingErr)` consistently, enabling both `errors.Is(err, sqlite.ErrWrite)`
and `errors.Is(err, underlyingErr)` at call sites. Good practice.

---

## Test Quality

### Coverage

86.3% statement coverage. 16 tests, all passing.

The uncovered statements are:

1. `Open()` in `pure.go` — untested directly (delegates to `OpenWithDriver`
   which is thoroughly tested). 23 callers use it in production.
2. `Deref()` in `scan.go` — untested. 13 callers use it in production.
3. `VacuumInto` close-error path (line 129-130) — the deferred close returning
   an error when the main operation succeeded. Hard to trigger in tests without
   mocking `*sql.DB`.

### Test Inventory

| Test | What it covers |
|---|---|
| `TestOpen` | Open + Conn() non-nil + Close |
| `TestOpenInvalidDriver` | ErrOpen on bad driver name |
| `TestOpenFailingPragma` | ErrOpen on invalid pragma, connection cleanup |
| `TestOpenMaxConns` | MaxConns propagation to pool stats |
| `TestSetGetMetadata` | Round-trip metadata storage |
| `TestSetMetadataOverwrite` | INSERT OR REPLACE behavior |
| `TestGetMetadataMissing` | Missing key returns ("", nil) |
| `TestExecStatements` | Multi-statement execution, row verification |
| `TestExecStatementsAtomicity` | Failing statement rolls back earlier statements |
| `TestExecStatementsEmpty` | Empty string returns nil |
| `TestExecStatementsWhitespace` | Whitespace-only returns nil |
| `TestExecStatementsCancelledContext` | Cancelled context returns ErrWrite |
| `TestVacuumInto` | End-to-end vacuum into new file, data preserved |
| `TestVacuumIntoExistingDest` | ErrWrite when dest exists |
| `TestSetMetadataNoTable` | ErrWrite when table missing |
| `TestGetMetadataNoTable` | ErrRead when table missing |

Good coverage of error paths. The atomicity test is particularly important —
it verifies that the multi-statement transaction pattern actually works with
the modernc driver.

### Missing Tests

- `Deref(nil)` and `Deref(&"value")` — trivial but untested.
- `Open()` — the pure-go convenience wrapper. Not tested directly.
- Close-error propagation in `VacuumInto`.

---

## Issues

### Minor: `ErrTimeout` Omitted from Fault Test (Observation)

This is a fault package issue, not db/sqlite, but noted here since
`db/sqlite/errors.go` depends on `fault`. See the fault AUDIT.md for details.

### Minor: `Deref` Untested

13 production callers, zero test coverage. The function is trivially correct
(3 lines, nil check + dereference), so the risk is negligible. Adding two
assertions would bring it to 100%.

### Minor: `Open` Convenience Wrapper Untested

23 production callers use `Open()`. It's a one-liner delegating to
`OpenWithDriver`, so the risk is negligible. Testing it would exercise the
modernc driver import side effect.

---

## Verdict

Well-designed package. The pragma profiles encode real operational knowledge
(ordering constraints, resource sizing, crash safety tradeoffs) and the
documentation explains the reasoning. Error handling is consistent and uses
proper sentinel wrapping. The test suite covers the important paths including
transaction atomicity and error type propagation. The coverage gap (86.3%) is
in trivially correct code paths. No bugs found.

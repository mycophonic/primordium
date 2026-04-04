# Audit: `format` package

**Date:** 2026-03-29
**Scope:** `github.com/mycophonic/primordium/format`
**Files:** doc.go, format.go, json.go, console.go, markdown.go, format_test.go

---

## 1. Package purpose

General-purpose output formatting for structured key-value data (`Data` structs). Provides JSON, Markdown, and Console formatters behind a `Formatter` interface. Used by haustorium (audio quality checker) and hypha (audio analyzer/doctor).

---

## 2. Correctness

No issues.

---

## 3. API fitness

### 3.1 `GetFormatter` allocates on every call

Each call returns `&JSON{}`, `&Markdown{}`, `&Console{}`. Since all three are stateless (zero-size structs with pointer receivers), they could be package-level singletons. Not a performance issue in practice — formatters are called once per command invocation — but it's an unnecessary allocation pattern.

### 3.2 `Formatter` interface has only one method

`PrintAll([]*Data, io.Writer) error` is the only method. The `[]*Data` slice parameter forces callers to buffer all results before printing. For directory scans with many files, a streaming interface (`Print(*Data, io.Writer) error` called per-entry) would be more memory-efficient. JSON would need special handling (array delimiters), but Console and Markdown already process entries independently.

---

## 4. Organization

### 4.1 File structure — clean

One file per formatter + format.go for the interface + doc.go. This is well organized.

### 4.2 `sortedKeys` and `separateFields` — duplicated pattern

`markdown.go` defines `sortedKeys` and `separateFields` as package-private helpers. `console.go` has its own inline key-sorting logic in `printMap`. These could share a single `sortedKeys` helper, but it's minor.

### 4.3 `wrapKeyError` only used by Console

`console.go:31` defines `wrapKeyError` as a package-level function, but only Console uses it. It should be a Console method or just inlined.

---

## 5. Test coverage — ADDRESSED

16 tests added in `format_test.go` (all P0 and P1 items covered):

- `GetFormatter` dispatch + invalid kind error type
- JSON round-trip (encode → decode → verify structure)
- JSON empty meta
- Console: basic scalars, nested maps, slices, empty meta, multi-entry separator
- Markdown: top-level scalars (2.1 regression), nested maps, slices, pipe escaping (2.4 regression), empty meta, multi-entry separator, mixed scalars + nested

---

## 6. Summary

| Area | Rating | Notes                                           |
|------|--------|-------------------------------------------------|
| Correctness | Good |                                                 |
| API fitness | Adequate |                                             |
| Organization | Good | Clean file-per-formatter layout                 |
| Test coverage | Good | 16 tests covering all formatters and edge cases |
| Code quality | Good | Consistent error wrapping, clean recursion      |

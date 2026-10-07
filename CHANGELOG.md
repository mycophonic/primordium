# Changelog

All notable changes to primordium are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html). Releases before
v0.11.1 are recorded only by their tags.

## [Unreleased]

### Added

- `filesystem/pathcheck.Platform`, with `Linux()`, `Darwin()`, `Windows()`
  and `Native()`: validate paths for a platform other than the one the
  program runs on. The package-level functions are unchanged and use
  `Native()`.

### Changed

- `filesystem`'s `FilePermissionsDefault`, `DirPermissionsDefault`,
  `FilePermissionsPrivate` and `DirPermissionsPrivate` are typed
  `os.FileMode`, no longer untyped: using one as another integer type, such
  as a `uint32` mode, needs a conversion.
- `filesystem/pathcheck` documents its contract: `.` and `..` are refused,
  a user's path is validated in its `filepath.Abs` form, and validation does
  not keep a path inside a directory.
- `filesystem/pathcheck` on Windows:
  - counts a name's length in UTF-16 code units, as Windows does, not bytes;
  - takes `/` as a separator, as Windows does, except in a long path
    (`\\?\…`), where it is refused;
  - refuses device paths (`\\.\…`, `\\?\Volume{…}\…`, `//?/…`) instead of
    stripping their prefix; long paths to a drive or a share are accepted.
- `filesystem/pathcheck.ValidateSocket` allows 108 bytes on illumos and
  Solaris, not 104.

### Fixed

- `filesystem/pathcheck` on Darwin and Windows refuses a name that is not valid
  UTF-8: macOS refuses to create it, and Windows stores it under another name.
- `filesystem/pathcheck` on Darwin refuses a name holding a code point
  unassigned in Unicode 9.0, noncharacters included, as Apple documents APFS
  does. Characters assigned since 9.0 are refused too: Apple documents no later
  version.
- `network/transporter`: each client has a connection pool of its own, cloned
  from `http.DefaultTransport`'s configuration at its first request. Before,
  all clients shared `http.DefaultTransport`'s pool: closing one client's idle
  connections closed every client's, and could fail a request that had just
  taken a connection. Limits on the pool, such as `network.SetDefaults`'
  100 connections per host, now hold per client.
- `filesystem/pathcheck` on Windows refuses a long path to a drive that does
  not start at its root: `\\?\C:` is the volume device, as `\\.\C:` is, and
  `\\?\C:name` is relative to the drive's current directory, which Windows
  rules out after `\\?\`.
- `store/refcount`: an `Acquire` whose factory fails no longer leaves the
  key's entry directory behind: it is released as a holder is, so the entry
  goes when no other holder has it.
- `filesystem/pathcheck` on Windows refuses a UNC path that does not name a
  server and a share (`\\server`, `\\?\UNC\server`): together they form
  its volume, and a server alone is no filesystem entry.
- `filesystem/pathcheck` on Windows refuses an empty name in a long path
  (`\\?\C:\a\\b`): Windows does not normalize what follows `\\?\`, so the
  doubled separator is not one separator there, as it is elsewhere.

## [0.11.1] - 2026-10-05

### Changed

- `github.com/forkcloser/xz` v1.0.1 and `github.com/forkcloser/blake3` v1.0.1.

### Fixed

- `compress/xz`: a source whose `Read` returns `(0, nil)`, which `io.Reader`
  allows, no longer fails decompression with "breader.ReadByte: no data"
  (forkcloser/xz v1.0.1).

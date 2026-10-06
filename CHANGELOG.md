# Changelog

All notable changes to primordium are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html). Releases before
v0.11.1 are recorded only by their tags.

## [Unreleased]

### Added

- `filesystem/pathcheck.Platform` (`Linux()`, `Darwin()`, `Windows()`, and
  `Native()`): `Validate`, `ValidateComponent` and `ValidateSocket` for a
  platform other than the one the program runs on, such as checking a path
  for a Linux guest from a macOS host. The package-level functions are
  unchanged and check for `Native()`.

### Changed

- `filesystem/pathcheck` on Windows counts a component's length in UTF-16
  code units, as Windows does, instead of bytes: a name of up to 255
  non-ASCII characters is no longer refused.
- `filesystem/pathcheck` on Windows refuses device paths instead of stripping
  their prefix: `\\.\…` (such as `\\.\pipe\…`), a `\\?\` path naming neither
  a drive nor a share (such as a volume GUID path, `\\?\Volume{…}\…`), and the
  forward-slash form `//?/…`. A device is not a filesystem entry. Long paths
  (`\\?\C:\…`, `\\?\UNC\server\share\…`) are still accepted, and a UNC path's
  server and share are checked as components.
- `filesystem/pathcheck` on Windows takes `/` as a separator, as Windows does,
  so `C:/Users/me` is accepted, except in a long path, where Windows takes it
  as a character and pathcheck refuses it.
- `filesystem/pathcheck.ValidateSocket` on illumos and Solaris allows their
  108-byte `sun_path`, not 104.

- `filesystem/pathcheck` documents its contract: every component must be a
  name a filesystem entry can carry, so `.` and `..` are refused. A path a
  user typed is checked in its `filepath.Abs` form. `Validate` says nothing
  about where a path points; confinement is `os.Root`'s or
  `filepath.IsLocal`'s job.

## [0.11.1] - 2026-10-05

### Changed

- `github.com/forkcloser/xz` v1.0.1 and `github.com/forkcloser/blake3` v1.0.1.

### Fixed

- `compress/xz`: a source whose `Read` returns `(0, nil)`, which `io.Reader`
  allows, no longer fails decompression with "breader.ReadByte: no data"
  (forkcloser/xz v1.0.1).

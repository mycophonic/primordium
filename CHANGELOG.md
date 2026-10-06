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

## [0.11.1] - 2026-10-05

### Changed

- `github.com/forkcloser/xz` v1.0.1 and `github.com/forkcloser/blake3` v1.0.1.

### Fixed

- `compress/xz`: a source whose `Read` returns `(0, nil)`, which `io.Reader`
  allows, no longer fails decompression with "breader.ReadByte: no data"
  (forkcloser/xz v1.0.1).

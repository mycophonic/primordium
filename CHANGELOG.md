# Changelog

All notable changes to primordium are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html). Releases before
v0.11.1 are recorded only by their tags.

## [Unreleased]

## [0.11.1] - 2026-10-05

### Changed

- `github.com/forkcloser/xz` v1.0.1 and `github.com/forkcloser/blake3` v1.0.1.

### Fixed

- `compress/xz`: a source whose `Read` returns `(0, nil)`, which `io.Reader`
  allows, no longer fails decompression with "breader.ReadByte: no data"
  (forkcloser/xz v1.0.1).

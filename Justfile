# This file is the project's own.
# Add recipes leveraging provided `do` ready-made recipes, or create your own.
# The import must be kept: it mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# Project configuration, ported from the old Makefile:
#   COVER_MIN := 30                          → TEST_GO_COVER_MIN
#   LICENSE_IGNORES := --ignore gotest.tools → LINT_GO_LICENSES_FLAGS
# go-licenses cannot resolve the LICENSE of the imported gotest.tools/v3
# submodule (google/go-licenses#186), so it is ignored — exactly as the old
# `lint-licenses` target did.
export TEST_GO_COVER_MIN := '30'
export LINT_GO_LICENSES_FLAGS := '--ignore=gotest.tools/v3'

# The FIRST recipe defined here becomes `just`'s default.
# The shared default covers the language-agnostic linters (limen, just, aqua,
# links, yaml, shell, dockerfile, commits); the Go linters (golangci-lint,
# govulncheck, go mod tidy, go-licenses) live in their own submodule and are
# added explicitly. Together they reproduce the old `make lint`.
[doc('Lint everything (shared linters + Go linters)')]
lint: do::lint::default do::lint::go::default

# Shared fixers plus the Go fixers (golangci-lint --fix, go mod tidy) — the
# same split as `lint`, mirroring the old `make fix`.
[doc('Auto-fix what can be fixed (shared fixers + Go fixers)')]
fix: do::fix::default do::fix::go::default

# Unit, race, bench, and cover — the old `make test` (the coverage gate reads
# TEST_GO_COVER_MIN above). No build/install: primordium is a library, with no
# cmd/ binaries to build.
[doc('Run the Go test suite: unit, race, bench, cover')]
test: do::test::go::unit do::test::go::race do::test::go::bench do::test::go::cover

/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go stdlib src/os/tempfile_test.go for xos package testing.

package xos_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/filesystem/xos"
)

func TestCreateTemp(t *testing.T) {
	t.Parallel()

	nonexistentDir := filepath.Join(t.TempDir(), "_not_exists_")

	f, err := xos.CreateTemp(nonexistentDir, "foo")
	if f != nil || err == nil {
		t.Errorf("CreateTemp(%q, `foo`) = %v, %v", nonexistentDir, f, err)
	}
}

func TestCreateTempPattern(t *testing.T) {
	t.Parallel()

	tests := []struct{ pattern, prefix, suffix string }{
		{"tempfile_test", "tempfile_test", ""},
		{"tempfile_test*", "tempfile_test", ""},
		{"tempfile_test*xyz", "tempfile_test", "xyz"},
	}

	for _, test := range tests {
		f, err := xos.CreateTemp("", test.pattern)
		if err != nil {
			t.Errorf("CreateTemp(..., %q) error: %v", test.pattern, err)

			continue
		}

		//revive:disable:defer
		defer os.Remove(f.Name())

		base := filepath.Base(f.Name())
		f.Close()

		if !strings.HasPrefix(base, test.prefix) || !strings.HasSuffix(base, test.suffix) {
			t.Errorf("CreateTemp pattern %q created bad name %q; want prefix %q & suffix %q",
				test.pattern, base, test.prefix, test.suffix)
		}
	}
}

func TestCreateTempBadPattern(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	const sep = string(os.PathSeparator)

	tests := []struct {
		pattern string
		wantErr bool
	}{
		{"ioutil*test", false},
		{"tempfile_test*foo", false},
		{"tempfile_test" + sep + "foo", true},
		{"tempfile_test*" + sep + "foo", true},
		{"tempfile_test" + sep + "*foo", true},
		{sep + "tempfile_test" + sep + "*foo", true},
		{"tempfile_test*foo" + sep, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()

			tmpfile, err := xos.CreateTemp(tmpDir, tt.pattern)
			if tmpfile != nil {
				defer tmpfile.Close()
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateTemp(..., %#q) succeeded, expected error", tt.pattern)
				}

				if !errors.Is(err, xos.ErrPatternHasSeparator) {
					t.Errorf("CreateTemp(..., %#q): %v, expected ErrPatternHasSeparator", tt.pattern, err)
				}
			} else if err != nil {
				t.Errorf("CreateTemp(..., %#q): %v", tt.pattern, err)
			}
		})
	}
}

func TestMkdirTemp(t *testing.T) {
	t.Parallel()

	name, err := xos.MkdirTemp("/_not_exists_", "foo")
	if name != "" || err == nil {
		t.Errorf("MkdirTemp(`/_not_exists_`, `foo`) = %v, %v", name, err)
	}

	tests := []struct {
		pattern                string
		wantPrefix, wantSuffix string
	}{
		{"tempfile_test", "tempfile_test", ""},
		{"tempfile_test*", "tempfile_test", ""},
		{"tempfile_test*xyz", "tempfile_test", "xyz"},
	}

	dir := filepath.Clean(os.TempDir())

	runTestMkdirTemp := func(t *testing.T, pattern, wantRePat string) {
		t.Helper()

		name, err := xos.MkdirTemp(dir, pattern)
		if name == "" || err != nil {
			t.Fatalf("MkdirTemp(dir, %q) = %v, %v", pattern, name, err)
		}

		defer os.Remove(name)

		re := regexp.MustCompile(wantRePat)
		if !re.MatchString(name) {
			t.Errorf(
				"MkdirTemp(%q, %q) created bad name\n\t%q\ndid not match pattern\n\t%q",
				dir,
				pattern,
				name,
				wantRePat,
			)
		}
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()

			wantRePat := "^" + regexp.QuoteMeta(
				filepath.Join(dir, tt.wantPrefix),
			) + "[0-9]+" + regexp.QuoteMeta(
				tt.wantSuffix,
			) + "$"
			runTestMkdirTemp(t, tt.pattern, wantRePat)
		})
	}

	// Separately testing "*xyz" (which has no prefix). That is when constructing the
	// pattern to assert on, as in the previous loop, using filepath.Join for an empty
	// prefix filepath.Join(dir, ""), produces the pattern:
	//     ^<DIR>[0-9]+xyz$
	// yet we just want to match
	//     "^<DIR>/[0-9]+xyz"
	t.Run("*xyz", func(t *testing.T) {
		wantRePat := "^" + regexp.QuoteMeta(
			dir,
		) + regexp.QuoteMeta(
			string(filepath.Separator),
		) + "[0-9]+xyz$"
		runTestMkdirTemp(t, "*xyz", wantRePat)
	})
}

// test that we return a nice error message if the dir argument to TempDir doesn't
// exist (or that it's empty and TempDir doesn't exist).
func TestMkdirTempBadDir(t *testing.T) {
	t.Parallel()

	badDir := filepath.Join(t.TempDir(), "not-exist")

	_, err := xos.MkdirTemp(badDir, "foo")

	var pe *fs.PathError
	if !errors.As(err, &pe) || !os.IsNotExist(err) || pe.Path != badDir {
		t.Errorf("TempDir error = %#v; want PathError for path %q satisfying IsNotExist", err, badDir)
	}
}

func TestMkdirTempBadPattern(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	const sep = string(os.PathSeparator)

	tests := []struct {
		pattern string
		wantErr bool
	}{
		{"ioutil*test", false},
		{"tempfile_test*foo", false},
		{"tempfile_test" + sep + "foo", true},
		{"tempfile_test*" + sep + "foo", true},
		{"tempfile_test" + sep + "*foo", true},
		{sep + "tempfile_test" + sep + "*foo", true},
		{"tempfile_test*foo" + sep, true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()

			_, err := xos.MkdirTemp(tmpDir, tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Errorf("MkdirTemp(..., %#q) succeeded, expected error", tt.pattern)
				}

				if !errors.Is(err, xos.ErrPatternHasSeparator) {
					t.Errorf("MkdirTemp(..., %#q): %v, expected ErrPatternHasSeparator", tt.pattern, err)
				}
			} else if err != nil {
				t.Errorf("MkdirTemp(..., %#q): %v", tt.pattern, err)
			}
		})
	}
}

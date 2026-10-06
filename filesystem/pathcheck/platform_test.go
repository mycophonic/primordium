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

package pathcheck_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// platforms names each platform, for subtests and messages.
var platforms = map[string]pathcheck.Platform{ //nolint:gochecknoglobals // read-only test table
	"linux":   pathcheck.Linux(),
	"darwin":  pathcheck.Darwin(),
	"windows": pathcheck.Windows(),
}

func nameOf(platform pathcheck.Platform) string {
	for name, candidate := range platforms {
		if candidate == platform {
			return name
		}
	}

	return "unknown"
}

func TestNative(t *testing.T) {
	t.Parallel()

	want, ok := map[string]pathcheck.Platform{
		"linux":   pathcheck.Linux(),
		"android": pathcheck.Linux(),
		"illumos": pathcheck.Linux(),
		"solaris": pathcheck.Linux(),
		"windows": pathcheck.Windows(),
	}[runtime.GOOS]
	if !ok {
		want = pathcheck.Darwin()
	}

	assert.Equal(t, pathcheck.Native(), want)
}

func TestPlatformValidate(t *testing.T) {
	t.Parallel()

	posix := []pathcheck.Platform{pathcheck.Linux(), pathcheck.Darwin()}

	tests := []struct {
		path      string
		platforms []pathcheck.Platform
		valid     bool
	}{
		{"/usr/local/bin", posix, true},
		{"/home/user/.config", posix, true},
		{"/foo//bar", posix, true},
		{`/a\b/c:d`, posix, true},
		{"/a/nul/aux.txt", posix, true},
		{"/a/../b", posix, false},
		{"./a", posix, false},
		{"/a/b\x00c", posix, false},
		{`C:\Users\me\file.txt`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`C:\Users/me\file.txt`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`\\?\C:\long/path`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\?\UNC\server/share\dir`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{"C:/Users/me/file.txt", []pathcheck.Platform{pathcheck.Windows()}, true},
		{`c:relative\file`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`\\server\share\dir\file`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`C:\a\..\b`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`D:\dir\nul.txt`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`C:\dir\file.`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`C:\dir\a:b`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\?\C:\long\path`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`\\?\unc\server\share\dir`, []pathcheck.Platform{pathcheck.Windows()}, true},
		{`\\?\C:\a\..\b`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\?\C:\dir\nul`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\?\Volume{0}\dir`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\.\pipe\docker_engine`, []pathcheck.Platform{pathcheck.Windows()}, false},
		{`\\.\C:\file`, []pathcheck.Platform{pathcheck.Windows()}, false},
	}

	for _, tc := range tests {
		for _, platform := range tc.platforms {
			t.Run(nameOf(platform)+":"+tc.path, func(t *testing.T) {
				t.Parallel()

				err := platform.Validate(tc.path)
				if tc.valid {
					assert.NilError(t, err)
				} else {
					assert.ErrorIs(t, err, pathcheck.ErrInvalidPath)
				}
			})
		}
	}
}

func TestPlatformComponentRules(t *testing.T) {
	t.Parallel()

	windowsOnly := []string{
		`a\b`, "com².whatever", "lpT2", "Prn.", "nUl", "AUX",
		"A<A", "A>A", "A:A", `A"A`, "A|A", "A?A", "A*A", "A\x01A",
		"end.dot.", "end.space ",
	}

	for _, name := range windowsOnly {
		assert.ErrorIs(t, pathcheck.Windows().ValidateComponent(name), pathcheck.ErrInvalidPath, name)
		assert.NilError(t, pathcheck.Linux().ValidateComponent(name), name)
		assert.NilError(t, pathcheck.Darwin().ValidateComponent(name), name)
	}

	for _, platform := range []pathcheck.Platform{pathcheck.Linux(), pathcheck.Darwin(), pathcheck.Windows()} {
		for _, name := range []string{".", "..", "", "   ", "a/b", "a\x00b"} {
			assert.ErrorIs(
				t,
				platform.ValidateComponent(name),
				pathcheck.ErrInvalidPath,
				"%s %q",
				nameOf(platform),
				name,
			)
		}

		for _, name := range []string{"test", ".start.dot", "mid.dot", "∞"} {
			assert.NilError(t, platform.ValidateComponent(name), "%s %q", nameOf(platform), name)
		}
	}
}

func TestPlatformComponentLength(t *testing.T) {
	t.Parallel()

	// "é" is two UTF-8 bytes and one UTF-16 code unit; "😀" is four bytes and
	// two code units.
	tests := []struct {
		name    string
		posix   bool
		windows bool
	}{
		{strings.Repeat("a", 255), true, true},
		{strings.Repeat("a", 256), false, false},
		{strings.Repeat("é", 127), true, true},
		{strings.Repeat("é", 128), false, true},
		{strings.Repeat("é", 255), false, true},
		{strings.Repeat("é", 256), false, false},
		{strings.Repeat("😀", 127), false, true},
		{strings.Repeat("😀", 128), false, false},
	}

	check := func(err error, valid bool) bool {
		return (err == nil) == valid && (valid || errors.Is(err, pathcheck.ErrInvalidPath))
	}

	for _, tc := range tests {
		assert.Assert(t, check(pathcheck.Linux().ValidateComponent(tc.name), tc.posix),
			"linux, %d bytes", len(tc.name))
		assert.Assert(t, check(pathcheck.Darwin().ValidateComponent(tc.name), tc.posix),
			"darwin, %d bytes", len(tc.name))
		assert.Assert(t, check(pathcheck.Windows().ValidateComponent(tc.name), tc.windows),
			"windows, %d bytes", len(tc.name))
	}
}

func TestPlatformValidateSocket(t *testing.T) {
	t.Parallel()

	limits := map[pathcheck.Platform]int{
		pathcheck.Linux():   107,
		pathcheck.Darwin():  103,
		pathcheck.Windows(): 107,
	}

	for platform, limit := range limits {
		name := nameOf(platform)

		assert.NilError(t, platform.ValidateSocket(strings.Repeat("x", limit)), name)

		err := platform.ValidateSocket(strings.Repeat("x", limit+1))
		assert.ErrorIs(t, err, pathcheck.ErrInvalidPath, name)
		assert.ErrorContains(t, err, name)
	}
}

func TestPlatformInvalidUTF8(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"\xff", "a\xc3", "a\xed\xa0\x80b"} {
		assert.NilError(t, pathcheck.Linux().ValidateComponent(name), "linux stores any bytes: %q", name)
		assert.ErrorIs(t, pathcheck.Darwin().ValidateComponent(name), pathcheck.ErrInvalidPath, "darwin: %q", name)
		assert.ErrorIs(t, pathcheck.Windows().ValidateComponent(name), pathcheck.ErrInvalidPath, "windows: %q", name)
		assert.ErrorIs(t, pathcheck.Darwin().Validate("/dir/"+name), pathcheck.ErrInvalidPath, "darwin path: %q", name)
	}
}

func TestDarwinUnicode9(t *testing.T) {
	t.Parallel()

	refused := map[string]string{
		"unassigned":                  "a\u0378b",
		"Unicode 11 (pleading face)":  "a\U0001F97Ab",
		"Unicode 16 (face with bags)": "a\U0001FAE9b",
		"noncharacter U+FFFE":         "a\uFFFEb",
		"noncharacter U+FDD0":         "a\uFDD0b",
		"noncharacter U+10FFFF":       "a\U0010FFFFb",
	}

	for label, name := range refused {
		assert.ErrorIs(t, pathcheck.Darwin().ValidateComponent(name), pathcheck.ErrInvalidPath, label)
		assert.ErrorIs(t, pathcheck.Darwin().Validate("/dir/"+name), pathcheck.ErrInvalidPath, label)
		assert.NilError(t, pathcheck.Linux().ValidateComponent(name), label)
	}

	for label, name := range map[string]string{
		"Unicode 6.1 (grinning face)": "a\U0001F600b",
		"Unicode 9.0 (face palm)":     "a\U0001F926b",
		"latin":                       "café",
		"private use":                 "a\uE000b",
	} {
		assert.NilError(t, pathcheck.Darwin().ValidateComponent(name), label)
	}
}

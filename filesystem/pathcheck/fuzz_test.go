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
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// seeds are names around every rule's edge: relative components, reserved
// characters and device names, trailing dots and spaces, lengths at the 255
// limit in bytes and in UTF-16 code units, invalid UTF-8, and code points
// outside Unicode 9.0 (unassigned, a noncharacter, one assigned since).
func seeds() []string {
	return []string{
		"", " ", ".", "..", "...", "a", ".hidden", "a.b", "a b",
		"a:b", "a\\b", "a/b", "a\x00b", "a\x01b", "a\x1fb", "a?b", "a*b", "a<b", "a>b", "a\"b", "a|b",
		"nul", "NUL.txt", "aux", "com1", "com5", "com9", "com¹", "com²", "com³", "com0",
		"lpt1", "lpt5", "lpt9.log", "lpt¹", "lpt²", "lpt³", "con.", "console", "end.", "end ",
		"C:", "C:x", `\\?\C:`, `\\.\pipe`,
		strings.Repeat("a", 255), strings.Repeat("a", 256),
		strings.Repeat("é", 128), strings.Repeat("😀", 127), strings.Repeat("😀", 128),
		"\xff", "a\xc3", "CONIN$", "e\u0301", "\U000AAAAA", "\uFFFE", "\U0001FAE9",
	}
}

func invalidOrNil(t *testing.T, err error, what string) {
	t.Helper()

	if err != nil && !errors.Is(err, pathcheck.ErrInvalidPath) {
		t.Fatalf("%s: error %v is not ErrInvalidPath", what, err)
	}
}

// assigned9 is every code point assigned in Unicode 9.0, read straight from
// Unicode's DerivedAge.txt for that version, noncharacters left out: an
// account of the source independent of the package's generated table.
var assigned9 = sync.OnceValue(func() map[rune]bool { //nolint:gochecknoglobals // read once, shared by the fuzz workers
	file, err := os.Open(filepath.Join("testdata", "ucd", "DerivedAge-9.0.0.txt"))
	if err != nil {
		panic(err)
	}
	defer func() { _ = file.Close() }()

	set := map[rune]bool{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		data, comment, _ := strings.Cut(scanner.Text(), "#")

		points, _, found := strings.Cut(data, ";")
		if !found || strings.Contains(comment, "noncharacter") {
			continue
		}

		low, high, isRange := strings.Cut(strings.TrimSpace(points), "..")
		if !isRange {
			high = low
		}

		lo, errLow := strconv.ParseInt(low, 16, 32)
		hi, errHigh := strconv.ParseInt(high, 16, 32)

		if errLow != nil || errHigh != nil {
			panic(scanner.Text())
		}

		for point := rune(lo); point <= rune(hi); point++ {
			set[point] = true
		}
	}

	return set
})

// darwinName restates the Darwin rules: Linux's, plus valid UTF-8 and only code
// points assigned in Unicode 9.0.
func darwinName(name string) bool {
	set := assigned9()

	return posixName(name) && utf8.ValidString(name) &&
		!strings.ContainsFunc(name, func(r rune) bool { return !set[r] })
}

// posixName restates the Linux rules for a component: at most 255 bytes, not
// blank, no NUL or "/", and not "." or "..".
func posixName(name string) bool {
	return len(name) <= 255 &&
		strings.TrimSpace(name) != "" &&
		!strings.ContainsAny(name, "\x00/") &&
		name != "." && name != ".."
}

// windowsName restates the Windows rules for a component: valid UTF-8, at most
// 255 UTF-16 code units, not blank, no control character or any of <>:"/\|?*, no trailing
// dot or space, and not a device name (CON, PRN, AUX, NUL, COM1-9, LPT1-9, the
// digit possibly a superscript), alone or before an extension.
func windowsName(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	device := slices.ContainsFunc(windowsDevices(), func(device string) bool {
		return strings.EqualFold(base, device)
	})

	return utf8.ValidString(name) &&
		len(utf16.Encode([]rune(name))) <= 255 &&
		strings.TrimSpace(name) != "" &&
		!strings.ContainsAny(name, "<>:\"/\\|?*") &&
		!strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 }) &&
		!strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") &&
		!device
}

func windowsDevices() []string {
	devices := []string{"con", "prn", "aux", "nul"}

	for _, digit := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
		devices = append(devices, "com"+digit, "lpt"+digit)
	}

	return devices
}

// FuzzValidateComponent checks that no name makes ValidateComponent panic or
// return a foreign error on any platform, and that each platform accepts
// exactly the names its rules, restated in posixName and windowsName, describe.
func FuzzValidateComponent(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		for _, tc := range []struct {
			platform pathcheck.Platform
			valid    bool
		}{
			{pathcheck.Linux(), posixName(name)},
			{pathcheck.Darwin(), darwinName(name)},
		} {
			err := tc.platform.ValidateComponent(name)
			invalidOrNil(t, err, "posix")

			if (err == nil) != tc.valid {
				t.Fatalf("posix ValidateComponent(%q) = %v, want valid=%v", name, err, tc.valid)
			}
		}

		err := pathcheck.Windows().ValidateComponent(name)
		invalidOrNil(t, err, "windows")

		if (err == nil) != windowsName(name) {
			t.Fatalf("windows ValidateComponent(%q) = %v, want valid=%v", name, err, windowsName(name))
		}
	})
}

// FuzzValidate checks that no path makes Validate panic on any platform, and
// that a path is as valid as its components: two accepted names joined under a
// root are accepted, and a refused name anywhere in a path refuses it. On
// Windows, "/" separates in an ordinary path and not in a long one.
func FuzzValidate(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed, "b")
	}

	f.Fuzz(func(t *testing.T, first, second string) {
		for _, platform := range []pathcheck.Platform{pathcheck.Linux(), pathcheck.Darwin(), pathcheck.Windows()} {
			invalidOrNil(t, platform.Validate(first), "raw path")
		}

		for _, platform := range []pathcheck.Platform{pathcheck.Linux(), pathcheck.Darwin()} {
			okFirst := platform.ValidateComponent(first) == nil
			okSecond := platform.ValidateComponent(second) == nil
			joined := platform.Validate("/" + first + "/" + second)

			if okFirst && okSecond && joined != nil {
				t.Fatalf("posix Validate(/%q/%q) = %v, but both names are valid", first, second, joined)
			}

			if !okFirst && first != "" && !strings.Contains(first, "/") && joined == nil {
				t.Fatalf("posix Validate(/%q/%q) accepted the refused name %q", first, second, first)
			}
		}

		windows := pathcheck.Windows()
		if windows.ValidateComponent(first) != nil || windows.ValidateComponent(second) != nil {
			return
		}

		for _, path := range []string{
			`C:\` + first + `\` + second,
			"C:/" + first + "/" + second,
			`\\?\C:\` + first + `\` + second,
			`\\server\share\` + first + `\` + second,
			`\\?\UNC\server\share\` + first + `\` + second,
		} {
			if err := windows.Validate(path); err != nil {
				t.Fatalf("windows Validate(%q) = %v, but both names are valid", path, err)
			}
		}

		for _, long := range []string{
			`\\?\C:\` + first + "/" + second,
			`\\?\UNC\server\share\` + first + "/" + second,
		} {
			if windows.Validate(long) == nil {
				t.Fatalf("windows Validate(%q) accepted a / in a long path", long)
			}
		}
	})
}

// FuzzValidateSocket checks that a socket path is accepted exactly when it
// fits the platform's sun_path, terminator included.
func FuzzValidateSocket(f *testing.F) {
	f.Add("/run/app.sock")
	f.Add(strings.Repeat("x", 103))
	f.Add(strings.Repeat("x", 104))
	f.Add(strings.Repeat("x", 107))
	f.Add(strings.Repeat("x", 108))

	f.Fuzz(func(t *testing.T, path string) {
		for _, tc := range []struct {
			platform pathcheck.Platform
			limit    int
		}{
			{pathcheck.Linux(), 107},
			{pathcheck.Darwin(), 103},
			{pathcheck.Windows(), 107},
		} {
			err := tc.platform.ValidateSocket(path)
			invalidOrNil(t, err, "socket")

			if (err == nil) != (len(path) <= tc.limit) {
				t.Fatalf("ValidateSocket(len %d) = %v, limit %d", len(path), err, tc.limit)
			}
		}
	})
}

// FuzzNativeNamesAreCreatable checks the contract against the host: a name the
// running platform's rules accept can be created, in an empty directory, as a
// regular file that the directory then lists under exactly that name. A host
// that refuses the name, alters it (a dropped trailing character, another
// Unicode form), or opens a device instead fails it. The converse does not
// hold: pathcheck is stricter than a filesystem on purpose.
func FuzzNativeNamesAreCreatable(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		if pathcheck.Native().ValidateComponent(name) != nil {
			return
		}

		dir := t.TempDir()

		file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("pathcheck accepts %q, but the host cannot create it: %v", name, err)
		}

		if err = file.Close(); err != nil {
			t.Fatalf("closing %q: %v", name, err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("listing the directory holding %q: %v", name, err)
		}

		if len(entries) != 1 || entries[0].Name() != name || !entries[0].Type().IsRegular() {
			t.Fatalf("pathcheck accepts %q, but the host made %v of it", name, entries)
		}
	})
}

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

// The bounded check: every input up to a size, over an alphabet drawn from the
// contract's classes (each class's edges, one step past them, and a member
// between), held to the contract on every platform. The alphabet is the one
// thing chosen by hand; a class missing from it goes unchecked, so a class the
// fuzz targets find belongs here.

import (
	"errors"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// atoms are the characters of every class the contract names.
func atoms() []string {
	return []string{
		// letters, the dot, spaces: ASCII, Unicode, and a control that is one
		"a", "Z", ".", " ", "\t", "\u00a0", "\u3000",
		// controls 0-31, and one past them
		"\x00", "\x01", "\x10", "\x1e", "\x1f", "\x7f",
		// reserved characters, and their non-reserved neighbours
		"<", ">", ":", "\"", "/", "\\", "|", "?", "*", "=", ";", "'", "{", "}", "@", "[", "`",
		// one, two, three and four bytes; one and two UTF-16 units
		"é", "€", "😀",
		// invalid UTF-8: a lone byte, a truncated sequence, an encoded surrogate
		"\xff", "\xc3", "\xed\xa0\x80",
		// outside Unicode 9.0: a noncharacter, never assigned, assigned since
		"\uFFFE", "\U000AAAAA", "\U0001FAE9",
	}
}

// deviceAtoms are Windows' device names, with each variant the contract names
// and their nearest neighbours that are not devices.
func deviceAtoms() []string {
	atoms := []string{"con", "CON", "Con", "prn", "aux", "nul", "NUL", "co", "cons", "conx"}

	for _, prefix := range []string{"com", "LPT"} {
		for _, digit := range []string{"0", "1", "5", "9", "10", "¹", "²", "³", "⁴", "x"} {
			atoms = append(atoms, prefix+digit)
		}
	}

	return atoms
}

// pathPrefixes are the forms a path starts with: POSIX roots, drive letters at
// and past the ends of the letters, UNC paths with and without a share, long
// paths to a root, a volume, a relative drive and a share, and device paths.
func pathPrefixes() []string {
	return []string{
		"", "/", "\\", "C:", "c:", "C:\\", "C:/", "a:", "z:", "A:", "Z:", "@:", "[:", "`:", "{:", "1:", "CC:",
		`\\server\share\`, `//server/share/`, `\/server\share\`, `\\server`, `\\server\`, `\\`,
		`\\?\C:\`, `\\?\z:\`, `\\?\@:\`, `\\?\C:`, `\\?\C:x`, `\\?\C:/`, `\\?\c:\\`,
		`\\?\UNC\server\share\`, `\\?\unc\s\s\`, `\\?\UNC\server`, `\\?\UNC\server\`, `\\?\UNC\`, `\\?\UNC\\s\s\`,
		`\\.\`, `\\.\pipe\`, `\\.\C:`, `\\?\Volume{x}\`, `//?/C:/`, `\\?/C:/`, `//./C:/`,
	}
}

func checkComponent(t *testing.T, c contract, name string) {
	t.Helper()

	err := c.platform.ValidateComponent(name)
	if err != nil && !errors.Is(err, pathcheck.ErrInvalidPath) {
		t.Fatalf("%s ValidateComponent(%q): error %v is not ErrInvalidPath", c.name, name, err)
	}

	if want := c.component(name); (err == nil) != want {
		t.Fatalf("%s ValidateComponent(%q) = %v, the contract says valid=%v", c.name, name, err, want)
	}
}

func checkPath(t *testing.T, c contract, path string) {
	t.Helper()

	err := c.platform.Validate(path)
	if err != nil && !errors.Is(err, pathcheck.ErrInvalidPath) {
		t.Fatalf("%s Validate(%q): error %v is not ErrInvalidPath", c.name, path, err)
	}

	if want := c.path(path); (err == nil) != want {
		t.Fatalf("%s Validate(%q) = %v, the contract says valid=%v", c.name, path, err, want)
	}
}

func checkSocket(t *testing.T, c contract, path string) {
	t.Helper()

	err := c.platform.ValidateSocket(path)
	if err != nil && !errors.Is(err, pathcheck.ErrInvalidPath) {
		t.Fatalf("%s ValidateSocket(len %d): error %v is not ErrInvalidPath", c.name, len(path), err)
	}

	if want := socketFits(path, c.sunPath); (err == nil) != want {
		t.Fatalf("%s ValidateSocket(len %d) = %v, the contract says fits=%v", c.name, len(path), err, want)
	}
}

// TestBoundedComponents: every name of up to three atoms, and every device
// name alone, followed by each atom, and with an extension.
func TestBoundedComponents(t *testing.T) {
	t.Parallel()

	alphabet := atoms()

	for _, c := range contracts() {
		var walk func(prefix string, depth int)

		walk = func(prefix string, depth int) {
			checkComponent(t, c, prefix)

			if depth < 3 {
				for _, atom := range alphabet {
					walk(prefix+atom, depth+1)
				}
			}
		}

		walk("", 0)

		for _, device := range deviceAtoms() {
			for _, atom := range append([]string{""}, alphabet...) {
				checkComponent(t, c, device+atom)
				checkComponent(t, c, device+atom+"txt")
			}
		}
	}
}

// TestBoundedUnicodeEdges: every code point at and one past each end of every
// span Unicode 9.0 dates, alone and after a letter.
func TestBoundedUnicodeEdges(t *testing.T) {
	t.Parallel()

	for _, c := range contracts() {
		for _, span := range unicode9Spans() {
			for _, r := range []rune{span[0] - 1, span[0], span[1], span[1] + 1} {
				if r < 0 || r > 0x10FFFF {
					continue
				}

				checkComponent(t, c, string(r))
				checkComponent(t, c, "a"+string(r))
			}
		}
	}
}

// TestBoundedLengths: every atom repeated to one under, at, and one over each
// limit, in bytes and in UTF-16 units, and padded to it with a letter.
func TestBoundedLengths(t *testing.T) {
	t.Parallel()

	for _, c := range contracts() {
		for _, atom := range atoms() {
			for _, size := range []int{len(atom), len([]rune(atom)) + strings.Count(atom, "😀")} {
				for _, limit := range []int{254, 255, 256} {
					for _, count := range []int{limit/size - 1, limit / size, limit/size + 1} {
						name := strings.Repeat(atom, count)
						checkComponent(t, c, name)
						checkComponent(t, c, name+strings.Repeat("a", max(0, limit-len(name))))
					}
				}
			}
		}
	}
}

// TestBoundedPaths: every path of up to three components from a small set,
// over every separator, after every prefix.
func TestBoundedPaths(t *testing.T) {
	t.Parallel()

	components := []string{"", "a", ".", "..", "nul", "a:b", "a b ", "?", "é", "\xff", "\U000AAAAA"}
	separators := []string{"/", "\\", "//", "\\\\", "/\\"}

	for _, c := range contracts() {
		for _, prefix := range pathPrefixes() {
			checkPath(t, c, prefix)

			for _, sep := range separators {
				checkPath(t, c, prefix+sep)

				for _, a := range components {
					checkPath(t, c, prefix+a)
					checkPath(t, c, prefix+a+sep)

					for _, b := range components {
						checkPath(t, c, prefix+a+sep+b)

						for _, d := range components {
							checkPath(t, c, prefix+a+sep+b+sep+d)
						}
					}
				}
			}
		}
	}
}

// TestBoundedSockets: every length from empty to past the largest limit, in
// one-byte and two-byte characters.
func TestBoundedSockets(t *testing.T) {
	t.Parallel()

	for _, c := range contracts() {
		for length := range 121 {
			checkSocket(t, c, strings.Repeat("x", length))
			checkSocket(t, c, strings.Repeat("é", length/2)+strings.Repeat("x", length%2))
		}
	}
}

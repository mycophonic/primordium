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

// The contract, restated from the platforms' documentation, each rule citing
// its source. It shares nothing with the package's code: the bounded check and
// the fuzz targets hold the package to it.

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// contract is one platform's rules, next to the Platform held to them.
type contract struct {
	name      string
	platform  pathcheck.Platform
	component func(string) bool
	path      func(string) bool
	sunPath   int // sockaddr_un's sun_path size, its NUL included
}

func contracts() []contract {
	return []contract{
		{"linux", pathcheck.Linux(), linuxName, func(p string) bool { return posixPath(p, linuxName) }, 108},
		{"darwin", pathcheck.Darwin(), darwinName, func(p string) bool { return posixPath(p, darwinName) }, 104},
		{"windows", pathcheck.Windows(), windowsName, windowsPath, 108},
	}
}

// pathcheckName holds pathcheck's own rules, on every platform (package and
// Platform docs): "." and ".." are not names, and a blank name is refused.
func pathcheckName(name string) bool {
	return name != "." && name != ".." && strings.TrimSpace(name) != ""
}

// linuxName: NAME_MAX is 255 bytes (include/uapi/linux/limits.h); a name holds
// any byte but "/" and NUL.
func linuxName(name string) bool {
	return pathcheckName(name) && len(name) <= 255 && !strings.ContainsAny(name, "/\x00")
}

// darwinName: NAME_MAX is 255 bytes (xnu bsd/sys/syslimits.h); APFS "accepts
// only valid UTF-8 encoded filenames for creation" and "doesn't allow files to
// be created with filenames that contain unassigned codepoints in the Unicode
// 9.0 standard" (Apple, APFS FAQ, 2018-06-04).
func darwinName(name string) bool {
	if !linuxName(name) || !utf8.ValidString(name) {
		return false
	}

	return !strings.ContainsFunc(name, func(r rune) bool { return !assignedIn9(r) })
}

// windowsName (Microsoft, "Naming Files, Paths, and Namespaces" and "Maximum
// Path Length Limitation"): at most 255 characters, UTF-16 code units to the
// Win32 API; none of < > : " / \ | ? *, NUL or 1-31; no trailing space or
// period; not CON, PRN, AUX, NUL, COM1-9, COM¹²³, LPT1-9 or LPT¹²³, alone or
// "followed immediately by an extension". pathcheck adds valid UTF-8: a name
// that is not cannot be stored as itself.
func windowsName(name string) bool {
	if !pathcheckName(name) || !utf8.ValidString(name) || len(utf16.Encode([]rune(name))) > 255 {
		return false
	}

	if strings.ContainsFunc(name, func(r rune) bool { return r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) }) {
		return false
	}

	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return false
	}

	base, _, _ := strings.Cut(name, ".")
	for _, device := range windowsDevices() {
		if strings.EqualFold(base, device) {
			return false
		}
	}

	return true
}

func windowsDevices() []string {
	devices := []string{"CON", "PRN", "AUX", "NUL"}
	for _, digit := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
		devices = append(devices, "COM"+digit, "LPT"+digit)
	}

	return devices
}

// posixPath: successive slashes resolve as one (POSIX pathname resolution), so
// empty components are not names; every other component is one.
func posixPath(path string, name func(string) bool) bool {
	for component := range strings.SplitSeq(path, "/") {
		if component != "" && !name(component) {
			return false
		}
	}

	return true
}

// windowsPath (Microsoft, "File path formats on Windows systems", "Maximum Path
// Length Limitation", "Naming Files, Paths, and Namespaces", CreateFileW):
//   - "\" and "/" both separate, repeated separators collapse, and a drive
//     letter prefixes a path;
//   - a UNC path is "fully qualified": its server and share form its volume;
//   - after `\\?\`, the string goes "straight to the file system": only "\"
//     separates, nothing collapses, and the path names a drive's root
//     (`\\?\C:\`; `\\?\C:` is the volume) or a share (`\\?\UNC\server\share`);
//   - a device path (`\\.\`, any other `\\?\`) is no filesystem entry, and is
//     refused for its "." or "?" component.
func windowsPath(path string) bool {
	if rest, ok := strings.CutPrefix(path, `\\?\`); ok {
		switch {
		case len(rest) >= 3 && isLetter(rest[0]) && rest[1] == ':' && rest[2] == '\\':
			return longComponents(rest[3:], 0)
		case len(rest) >= 4 && strings.EqualFold(rest[:4], `UNC\`):
			return longComponents(rest[4:], 2)
		default:
			return false
		}
	}

	components := strings.FieldsFunc(path, func(r rune) bool { return r == '\\' || r == '/' })

	if isSeparator(path, 0) && isSeparator(path, 1) && len(components) < 2 {
		return false
	}

	if len(path) >= 2 && isLetter(path[0]) && path[1] == ':' {
		components = strings.FieldsFunc(path[2:], func(r rune) bool { return r == '\\' || r == '/' })
	}

	for _, component := range components {
		if !windowsName(component) {
			return false
		}
	}

	return true
}

// longComponents checks what follows a long path's root: "\"-separated names,
// none empty but for a trailing separator, and at least minimum of them.
func longComponents(rest string, minimum int) bool {
	var components []string
	if rest != "" {
		components = strings.Split(strings.TrimSuffix(rest, `\`), `\`)
	}

	if len(components) < minimum {
		return false
	}

	for _, component := range components {
		if !windowsName(component) {
			return false
		}
	}

	return true
}

func isSeparator(path string, i int) bool {
	return i < len(path) && (path[i] == '\\' || path[i] == '/')
}

func isLetter(b byte) bool { return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' }

// socketFits: a socket path fits sun_path with its NUL: sun_path[108] on Linux
// (unix(7)), sun_path[104] on Darwin (xnu bsd/sys/un.h), 108 on Windows
// (pathcheck's Platform docs).
func socketFits(path string, sunPath int) bool { return len(path) < sunPath }

// assignedIn9: a code point is assigned in Unicode 9.0 when DerivedAge.txt for
// 9.0.0 dates it, and it is not a noncharacter, which Unicode leaves
// unassigned (U+FDD0..U+FDEF, and the last two of every plane).
func assignedIn9(r rune) bool {
	if r >= 0xFDD0 && r <= 0xFDEF || r&0xFFFE == 0xFFFE {
		return false
	}

	spans := unicode9Spans()
	i := sort.Search(len(spans), func(i int) bool { return spans[i][1] >= r })

	return i < len(spans) && spans[i][0] <= r
}

// unicode9Spans reads the code point spans DerivedAge.txt dates, sorted.
//
//nolint:gochecknoglobals // parsed once, shared by the fuzz workers
var unicode9Spans = sync.OnceValue(
	func() [][2]rune {
		file, err := os.Open(filepath.Join("testdata", "ucd", "DerivedAge-9.0.0.txt"))
		if err != nil {
			panic(err)
		}
		defer func() { _ = file.Close() }()

		var spans [][2]rune

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			data, _, _ := strings.Cut(scanner.Text(), "#")

			points, _, found := strings.Cut(data, ";")
			if !found {
				continue
			}

			low, high, isRange := strings.Cut(strings.TrimSpace(points), "..")
			if !isRange {
				high = low
			}

			lo, errLow := strconv.ParseInt(low, 16, 32)
			hi, errHigh := strconv.ParseInt(high, 16, 32)

			if errLow != nil || errHigh != nil {
				panic("DerivedAge.txt: " + scanner.Text())
			}

			spans = append(spans, [2]rune{rune(lo), rune(hi)})
		}

		if err := scanner.Err(); err != nil {
			panic(err)
		}

		sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })

		return spans
	},
)

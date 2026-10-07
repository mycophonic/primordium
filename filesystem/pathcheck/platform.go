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

package pathcheck

//go:generate go run gen_unicode9.go

import (
	"runtime"
	"strings"
	"unicode/utf16"
)

// Platform is a set of path rules: how a path splits into components, which
// names a component may carry, and how long a Unix socket path may be. The
// platforms are Linux, Darwin and Windows; its unexported method keeps another
// package from implementing it, other than by embedding a Platform.
type Platform interface {
	// Validate validates a full path by checking each component.
	// Returns an error if any component is invalid, including a "." or ".."
	// component: validate a user's relative path in its absolute form (see the
	// package documentation).
	Validate(path string) error

	// ValidateComponent enforces the platform's restrictions on a single path
	// component: its length, a name that is not blank, and the platform's
	// reserved characters and names.
	ValidateComponent(pathComponent string) error

	// ValidateSocket checks that a Unix socket path fits the platform's
	// sockaddr_un sun_path, NUL terminator included: 108 bytes on Linux and
	// Windows, 104 on macOS. The limit applies to the string
	// handed to bind or dial, so check that string, not a shorter form of it.
	ValidateSocket(path string) error

	sealed()
}

type platform struct {
	name      string
	socketMax int
}

// Linux returns the Linux platform, which also stands for illumos and Solaris.
//
//nolint:iface // opaque: the sealed interface is the API, as crypto/ecdh's curves are
func Linux() Platform {
	//nolint:goconst // the GOOS name, plainer spelled out
	return platform{name: "linux", socketMax: socketPathMaxLinux}
}

// Darwin returns the macOS platform. It refuses a name that is not valid UTF-8,
// or that holds a code point unassigned in Unicode 9.0: the APFS rule Apple
// documents, kept at the version it documents.
//
//nolint:iface // opaque: the sealed interface is the API, as crypto/ecdh's curves are
func Darwin() Platform {
	return platform{name: "darwin", socketMax: socketPathMaxDarwin}
}

// Windows returns the Windows platform. It accepts a drive letter, and a long
// path's `\\?\C:` or `\\?\UNC\`, before the first component, and refuses device
// paths and names that are not valid UTF-8, which Windows stores under another
// name.
//
//nolint:iface // opaque: the sealed interface is the API, as crypto/ecdh's curves are
func Windows() Platform {
	//nolint:goconst // the GOOS name, plainer spelled out
	return platform{name: "windows", socketMax: socketPathMaxWindows}
}

// Native returns the platform this program runs on.
func Native() Platform {
	switch runtime.GOOS {
	case "windows":
		return Windows()
	case "linux", "android", "illumos", "solaris":
		return Linux()
	default:
		return Darwin()
	}
}

func (platform) sealed() {}

// split returns the part of path that holds its components, and what
// separates them. On Windows, a drive letter ("C:") goes, and so does the
// long-path prefix before a drive's root (`\\?\C:\`) or before a share
// (`\\?\UNC\`). Both "\" and "/" separate, except in a long path, where Windows
// takes "/" as a character. A UNC share needs nothing more, its server and
// share being names. Any other `\\?\` path is left whole, and so is every
// `\\.\` device path, to be refused for its "?" or "." component: a device is
// not a filesystem entry. That includes `\\?\C:`, the volume itself, as
// `\\.\C:` is, and `\\?\C:name`, relative to the drive's current directory,
// which a long path cannot be.
func (p platform) split(path string) (string, func(rune) bool) {
	if p.name != "windows" {
		return path, isSlash
	}

	if rest, ok := strings.CutPrefix(path, longPathPrefix); ok {
		switch {
		case len(rest) >= len(longUNCPrefix) && strings.EqualFold(rest[:len(longUNCPrefix)], longUNCPrefix):
			return rest[len(longUNCPrefix):], isBackslash
		case hasDriveLetter(rest) && len(rest) > 2 && rest[2] == '\\':
			return rest[2:], isBackslash
		}
	}

	if hasDriveLetter(path) {
		return path[2:], isSlashOrBackslash
	}

	return path, isSlashOrBackslash
}

// isUNC reports a Windows path to a share, `\\server\share` or
// `\\?\UNC\server\share`, either separator in the first. A device path
// (`\\.\`, `\\?\`) starts the same way and is not one.
func (p platform) isUNC(path string) bool {
	if p.name != "windows" {
		return false
	}

	if rest, ok := strings.CutPrefix(path, longPathPrefix); ok {
		return len(rest) >= len(longUNCPrefix) && strings.EqualFold(rest[:len(longUNCPrefix)], longUNCPrefix)
	}

	if len(path) < 2 || !isSlashOrBackslash(rune(path[0])) || !isSlashOrBackslash(rune(path[1])) {
		return false
	}

	device := len(path) > 2 && (path[2] == '.' || path[2] == '?') &&
		(len(path) == 3 || isSlashOrBackslash(rune(path[3])))

	return !device
}

// countFields counts the non-empty components: Win32 collapses repeated
// separators after a UNC path's first two. A long path, which Win32 does not
// normalize, holds no empty component to skip.
func countFields(components string, isSeparator func(rune) bool) int {
	count := 0
	for range strings.FieldsFuncSeq(components, isSeparator) {
		count++
	}

	return count
}

func isSlash(r rune) bool { return r == '/' }

func isBackslash(r rune) bool { return r == '\\' }

func isSlashOrBackslash(r rune) bool { return r == '/' || r == '\\' }

func hasDriveLetter(path string) bool {
	return len(path) >= 2 && path[1] == ':' && isASCIILetter(path[0])
}

func isASCIILetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

// componentLength counts as the platform does: Windows in UTF-16 code units.
func (p platform) componentLength(component string) int {
	if p.name == "windows" {
		return len(utf16.Encode([]rune(component)))
	}

	return len(component)
}

func (p platform) validateSpecific(component string) error {
	if p.name == "windows" {
		return validateWindows(component)
	}

	return validatePosix(component)
}

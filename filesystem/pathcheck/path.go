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

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validate checks path for the platform this program runs on: see
// Platform.Validate.
func Validate(path string) error {
	return Native().Validate(path) //nolint:wrapcheck // pathcheck's own error, already ErrInvalidPath
}

// ValidateComponent checks a single path component for the platform this
// program runs on: see Platform.ValidateComponent.
func ValidateComponent(pathComponent string) error {
	return Native().ValidateComponent(pathComponent) //nolint:wrapcheck // pathcheck's own error, already ErrInvalidPath
}

// ValidateSocket checks a Unix socket path's length for the platform this
// program runs on: see Platform.ValidateSocket.
func ValidateSocket(path string) error {
	return Native().ValidateSocket(path) //nolint:wrapcheck // pathcheck's own error, already ErrInvalidPath
}

func (p platform) Validate(path string) error {
	components, isSeparator := p.split(path)

	if p.isUNC(path) && countFields(components, isSeparator) < 2 {
		return fmt.Errorf("%w: %q", errors.Join(ErrInvalidPath, errUNCWithoutShare), path)
	}

	if p.longHasEmptyComponent(path, components) {
		return fmt.Errorf("%w: %q", errors.Join(ErrInvalidPath, errInvalidPathEmpty), path)
	}

	for component := range strings.FieldsFuncSeq(components, isSeparator) {
		if err := p.ValidateComponent(component); err != nil {
			return fmt.Errorf("%w: invalid path component %q", err, component)
		}
	}

	return nil
}

func (p platform) ValidateComponent(pathComponent string) error {
	// https://en.wikipedia.org/wiki/Comparison_of_file_systems#Limits
	if p.componentLength(pathComponent) > pathComponentMaxLength {
		return errors.Join(ErrInvalidPath, errInvalidPathTooLong)
	}

	if strings.TrimSpace(pathComponent) == "" {
		return errors.Join(ErrInvalidPath, errInvalidPathEmpty)
	}

	// Linux filesystems store a name's bytes as they are; macOS refuses a name
	// that is not UTF-8, and Windows stores it under another name, its invalid
	// bytes replaced.
	if p.name != "linux" && !utf8.ValidString(pathComponent) { //nolint:goconst // the GOOS name, plainer spelled out
		return errors.Join(ErrInvalidPath, errInvalidEncoding)
	}

	// Unicode 9.0, not the running macOS's: Apple documents that APFS refuses a
	// code point unassigned in Unicode 9.0, and documents no later version.
	if p.name == "darwin" &&
		strings.ContainsFunc(pathComponent, func(r rune) bool { return !unicode.Is(unicode9, r) }) {
		return errors.Join(ErrInvalidPath, errUnassignedCodePoint)
	}

	if err := p.validateSpecific(pathComponent); err != nil {
		return errors.Join(ErrInvalidPath, err)
	}

	return nil
}

func (p platform) ValidateSocket(path string) error {
	maxLen := p.socketMax - 1

	if len(path) > maxLen {
		return fmt.Errorf("%w: socket path exceeds %s limit of %d bytes (got %d): %s",
			ErrInvalidPath, p.name, maxLen, len(path), path)
	}

	return nil
}

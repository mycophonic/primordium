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

package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mycophonic/primordium/fault"
)

var (
	errForbiddenChars    = errors.New("forbidden characters in path component")
	errForbiddenKeywords = errors.New("forbidden keywords in path component")
)

// ValidatePathComponent will enforce os specific filename restrictions on a single path component.
func ValidatePathComponent(pathComponent string) error {
	// https://en.wikipedia.org/wiki/Comparison_of_file_systems#Limits
	if len(pathComponent) > pathComponentMaxLength {
		return errors.Join(ErrInvalidPath, errInvalidPathTooLong)
	}

	if strings.TrimSpace(pathComponent) == "" {
		return errors.Join(ErrInvalidPath, errInvalidPathEmpty)
	}

	if err := validatePlatformSpecific(pathComponent); err != nil {
		return errors.Join(ErrInvalidPath, err)
	}

	return nil
}

// ValidatePath validates a full path by checking each component.
// Returns an error if any component is invalid.
func ValidatePath(path string) error {
	// Strip volume name (e.g., "C:" on Windows) — it is not a path component
	path = path[len(filepath.VolumeName(path)):]

	// Iterate over path components
	for component := range strings.SplitSeq(path, string(os.PathSeparator)) {
		// Skip empty components (from leading/trailing/double separators)
		if component == "" {
			continue
		}

		if err := ValidatePathComponent(component); err != nil {
			return fmt.Errorf("%w: invalid path component %q: %w", fault.ErrInvalidArgument, component, err)
		}
	}

	return nil
}

// ValidateSocketPath checks that a Unix socket path does not exceed OS-specific limits.
// Unix sockets have a hard limit on path length due to the fixed-size sun_path field
// in struct sockaddr_un:
//   - Linux: 108 bytes (including null terminator)
//   - macOS/BSD: 104 bytes (including null terminator)
//
// Returns an error if the path is too long for the current platform.
func ValidateSocketPath(path string) error {
	// Need room for null terminator, so max usable length is maxSocketPathLen - 1
	maxLen := maxSocketPathLen - 1

	if len(path) > maxLen {
		return fmt.Errorf("%w: socket path exceeds %s limit of %d bytes (got %d): %s",
			fault.ErrInvalidArgument, runtime.GOOS, maxLen, len(path), path)
	}

	return nil
}

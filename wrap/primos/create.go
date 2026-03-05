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

package primos

import (
	"io"
	"os"
	"slices"
	"strings"
)

// ReadFile reads the named file and returns its contents.
// Equivalent to os.ReadFile but with FILE_SHARE_DELETE on Windows.
func ReadFile(path string) ([]byte, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	//nolint:wrapcheck // Thin wrapper matching os.ReadFile signature.
	return io.ReadAll(file)
}

// ReadDir reads the named directory and returns its entries sorted by name.
// Equivalent to os.ReadDir but with FILE_SHARE_DELETE on Windows.
func ReadDir(path string) ([]os.DirEntry, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	entries, err := file.ReadDir(-1)

	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	return entries, err //nolint:wrapcheck // Thin wrapper matching os.ReadDir signature.
}

// Stat returns file info for the named path.
// Equivalent to os.Stat but with FILE_SHARE_DELETE on Windows.
func Stat(path string) (os.FileInfo, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	//nolint:wrapcheck // Thin wrapper matching os.Stat signature.
	return file.Stat()
}

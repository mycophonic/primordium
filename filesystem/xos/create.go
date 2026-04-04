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

package xos

import (
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
)

// ReadFile reads the named file and returns its contents.
// Equivalent to os.ReadFile but with FILE_SHARE_DELETE on Windows.
func ReadFile(path string) ([]byte, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return readFileContents(file)
}

// readFileContents reads an entire file, pre-allocating based on the file's
// stat size to avoid unnecessary re-allocations. Matches the stdlib's
// os.ReadFile behavior.
func readFileContents(file *os.File) ([]byte, error) {
	var size int

	zeroSize := true

	if fi, statErr := file.Stat(); statErr == nil {
		if s := fi.Size(); int64(int(s)) == s {
			size = int(s)
			zeroSize = s == 0
		}
	}

	size++ // One extra byte so the final read returns 0 + io.EOF.

	if size < minReadBuf {
		size = minReadBuf
	}

	data := make([]byte, 0, size)

	for {
		n, err := file.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]

		if err != nil {
			if err == io.EOF {
				err = nil
			}

			return data, err
		}

		capRemain := cap(data) - len(data)
		if capRemain == 0 || (zeroSize && capRemain < minReadBuf) {
			data = slices.Grow(data, minReadBuf)
		}
	}
}

// ReadDir reads the named directory and returns its entries sorted by name.
// Equivalent to os.ReadDir but with FILE_SHARE_DELETE on Windows.
func ReadDir(path string) ([]os.DirEntry, error) {
	file, err := Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	// Match stdlib openDir behavior: fstat and reject non-directories early.
	fi, err := file.Stat()
	if err != nil {
		return nil, err //nolint:wrapcheck // Thin wrapper matching os.ReadDir signature.
	}

	if !fi.IsDir() {
		return nil, &os.PathError{Op: "fdopendir", Path: path, Err: syscall.ENOTDIR}
	}

	entries, err := file.ReadDir(-1)

	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	return entries, err //nolint:wrapcheck // Thin wrapper matching os.ReadDir signature.
}

// Stat returns file info for the named path.
// Equivalent to os.Stat. On Windows, os.Stat uses GetFileAttributesEx (no
// handle needed) with fallbacks, matching the stdlib's three-tier approach.
//
//nolint:wrapcheck // Thin wrapper matching os.Stat signature.
func Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// Create creates or truncates the named file. If the file already exists,
// it is truncated. If the file does not exist, it is created with mode 0o666
// (before umask). The associated file descriptor has mode O_RDWR.
// Equivalent to os.Create but with FILE_SHARE_DELETE on Windows.
func Create(path string) (*os.File, error) {
	return OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, defaultPerm)
}

// WriteFile writes data to the named file, creating it if necessary.
// If the file does not exist, WriteFile creates it with permissions perm (before umask);
// otherwise WriteFile truncates it before writing, without changing permissions.
// Equivalent to os.WriteFile but with FILE_SHARE_DELETE on Windows.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	file, err := OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	_, err = file.Write(data)

	if closeErr := file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}

	return err
}

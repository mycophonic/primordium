//go:build windows

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

// Windows file operations with FILE_SHARE_DELETE.
//
// Go's os.Open/os.OpenFile open handles with FILE_SHARE_READ|FILE_SHARE_WRITE
// but not FILE_SHARE_DELETE. This prevents os.Rename or os.Remove while any
// handle is open. These wrappers use CreateFile directly with FILE_SHARE_DELETE
// added, matching Rust's standard library behavior.
//
// Flag mapping follows Go's internal syscall.Open logic.

package filesystem

import (
	"os"

	"golang.org/x/sys/windows"
)

const (
	shareMode = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

	// opOpen is the PathError.Op for file open operations.
	opOpen = "open"

	// ownerWrite is the Unix owner-write permission bit, used to map to FILE_ATTRIBUTE_READONLY.
	ownerWrite os.FileMode = 0o200 //nolint:mnd // Standard Unix permission bit.
)

// appendAccess combines the file access rights Go sets for O_APPEND on Windows:
// FILE_APPEND_DATA (0x0004) + FILE_WRITE_ATTRIBUTES (0x0100) +
// FILE_WRITE_EA (0x0010) + STANDARD_RIGHTS_WRITE (0x0002_0000) +
// SYNCHRONIZE (0x0010_0000).
const appendAccess = 0x00120114

// Open opens a file for reading. The handle includes FILE_SHARE_DELETE so
// that concurrent rename or deletion by other handles is permitted.
func Open(path string) (*os.File, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	handle, err := windows.CreateFile(
		pathp,
		windows.GENERIC_READ,
		shareMode,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	return os.NewFile(uintptr(handle), path), nil
}

// OpenFile opens a file with the given flags and permissions. The handle
// includes FILE_SHARE_DELETE so that concurrent rename or deletion by
// other handles is permitted.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	access := accessMode(flag)
	createmode := creationDisposition(flag)
	attrs := fileAttributes(flag, perm)

	handle, err := windows.CreateFile(pathp, access, shareMode, nil, createmode, attrs, 0)
	if err != nil {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	file := os.NewFile(uintptr(handle), path)

	// Truncate after open, matching Go's O_TRUNC handling (avoids CREATE_ALWAYS
	// which would replace FILE_ATTRIBUTE_READONLY files).
	if flag&os.O_TRUNC != 0 {
		if truncErr := file.Truncate(0); truncErr != nil {
			_ = file.Close()

			return nil, &os.PathError{Op: opOpen, Path: path, Err: truncErr}
		}
	}

	return file, nil
}

// accessMode maps Go os.O_* flags to Windows desired access flags.
func accessMode(flag int) uint32 {
	var access uint32

	switch flag & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY:
		access = windows.GENERIC_WRITE
	case os.O_RDWR:
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
	default: // O_RDONLY (0) and any unexpected combination
		access = windows.GENERIC_READ
	}

	if flag&os.O_CREATE != 0 {
		access |= windows.GENERIC_WRITE
	}

	if flag&os.O_APPEND != 0 {
		if flag&os.O_TRUNC == 0 {
			access &^= windows.GENERIC_WRITE
		}

		access |= appendAccess
	}

	return access
}

// creationDisposition maps Go os.O_* flags to Windows creation disposition.
func creationDisposition(flag int) uint32 {
	switch {
	case flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL):
		return windows.CREATE_NEW
	case flag&os.O_CREATE != 0:
		return windows.OPEN_ALWAYS
	default:
		return windows.OPEN_EXISTING
	}
}

// fileAttributes maps Go flags and permissions to Windows file attributes.
func fileAttributes(flag int, perm os.FileMode) uint32 {
	var attrs uint32 = windows.FILE_ATTRIBUTE_NORMAL

	if perm&ownerWrite == 0 {
		attrs = windows.FILE_ATTRIBUTE_READONLY
	}

	// Allow opening directories for reading.
	switch flag & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY, os.O_RDWR:
	default:
		attrs |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}

	if flag&os.O_SYNC != 0 {
		attrs |= windows.FILE_FLAG_WRITE_THROUGH
	}

	return attrs
}

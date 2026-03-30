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

package xos

import (
	"errors"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procCreateFileW is the raw CreateFileW procedure handle.
// Used instead of windows.CreateFile so we can capture GetLastError even for
// successful calls (needed to detect ERROR_ALREADY_EXISTS from OPEN_ALWAYS).
//
//nolint:gochecknoglobals
var procCreateFileW = windows.NewLazySystemDLL("kernel32.dll").
	NewProc("CreateFileW")

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
	return OpenFile(path, os.O_RDONLY, 0)
}

// OpenFile opens a file with the given flags and permissions. The handle
// includes FILE_SHARE_DELETE so that concurrent rename or deletion by
// other handles is permitted.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	if len(path) == 0 {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: windows.ERROR_FILE_NOT_FOUND}
	}

	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	access := accessMode(flag)
	createmode := creationDisposition(flag)
	attrs := fileAttributes(flag, perm)

	handle, alreadyExists, err := createFileShareDelete(pathp, access, createmode, attrs)
	if err != nil {
		// Map ERROR_ACCESS_DENIED to EISDIR when the target is a directory
		// opened without FILE_FLAG_BACKUP_SEMANTICS (i.e., for writing).
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) &&
			attrs&windows.FILE_FLAG_BACKUP_SEMANTICS == 0 {
			fa, faErr := windows.GetFileAttributes(pathp)
			if faErr == nil && fa&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
				err = syscall.EISDIR
			}
		}

		return nil, &os.PathError{Op: opOpen, Path: path, Err: err}
	}

	// Truncate after open on the raw handle, matching Go's O_TRUNC handling.
	// Only truncate when the file already existed — newly created files are
	// already empty. This matches the stdlib's condition check using
	// ERROR_ALREADY_EXISTS from OPEN_ALWAYS.
	if flag&os.O_TRUNC != 0 &&
		(createmode == windows.OPEN_EXISTING || (createmode == windows.OPEN_ALWAYS && alreadyExists)) {
		if truncErr := windows.SetEndOfFile(handle); truncErr != nil {
			// Silently ignore truncation failure on pipes and character devices,
			// matching Go's internal syscall.Open behavior.
			if errors.Is(truncErr, windows.ERROR_INVALID_PARAMETER) {
				ft, ftErr := windows.GetFileType(handle)
				if ftErr == nil && (ft == windows.FILE_TYPE_PIPE || ft == windows.FILE_TYPE_CHAR) {
					truncErr = nil
				}
			}

			if truncErr != nil {
				windows.CloseHandle(handle) //nolint:errcheck,gosec // Best-effort cleanup.

				return nil, &os.PathError{Op: opOpen, Path: path, Err: truncErr}
			}
		}
	}

	// NOTE: os.NewFile does not set the internal appendMode field on Windows.
	// This means (*File).WriteAt will not return ErrPermission for O_APPEND files
	// as os.OpenFile does. The kernel-level append enforcement via FILE_APPEND_DATA
	// is still correct for Write — only WriteAt behavior differs. This is an
	// inherent limitation of os.NewFile; there is no public API to set appendMode.
	return os.NewFile(uintptr(handle), path), nil
}

// Truncate changes the size of the named file.
// The handle includes FILE_SHARE_DELETE so that concurrent rename or
// deletion by other handles is permitted.
//
//nolint:wrapcheck // Thin wrapper matching os.Truncate signature.
func Truncate(path string, size int64) error {
	file, err := OpenFile(path, os.O_WRONLY, defaultPerm)
	if err != nil {
		return err
	}

	defer file.Close()

	return file.Truncate(size)
}

// createFileShareDelete calls CreateFileW with FILE_SHARE_DELETE via raw syscall,
// capturing GetLastError even for successful calls. This lets us detect
// ERROR_ALREADY_EXISTS from OPEN_ALWAYS (the windows.CreateFile wrapper
// discards the error for valid handles).
func createFileShareDelete(
	name *uint16, access, createmode, attrs uint32,
) (handle windows.Handle, alreadyExists bool, err error) {
	rawHandle, _, lastErr := syscall.SyscallN(
		procCreateFileW.Addr(),
		uintptr(unsafe.Pointer(name)), //nolint:gosec // G103: required for CreateFileW syscall
		uintptr(access),
		uintptr(shareMode),
		0, // security attributes (nil = non-inheritable, matching Go's O_CLOEXEC default)
		uintptr(createmode),
		uintptr(attrs),
		0, // template file
	)

	handle = windows.Handle(rawHandle)

	if handle == windows.InvalidHandle {
		return handle, false, lastErr
	}

	if lastErr == windows.ERROR_ALREADY_EXISTS {
		alreadyExists = true
	}

	return handle, alreadyExists, nil
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

	// Prevent following symlinks during exclusive creation, matching Go's
	// internal syscall.Open behavior.
	if flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL) {
		attrs |= windows.FILE_FLAG_OPEN_REPARSE_POINT
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

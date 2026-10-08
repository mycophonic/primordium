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

package xos

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Rename renames (moves) oldpath to newpath, as os.Rename does, and on a volume
// that has the POSIX-semantics rename (NTFS from Windows 10 1709) replaces a
// newpath another handle holds open with FILE_SHARE_DELETE, as a rename does on
// Unix. A volume without it (FAT, exFAT, some shares) gets os.Rename
// (MoveFileEx), which refuses a held newpath: the property is the volume's.
// A directory is moved as os.Rename moves one.
//
//nolint:wrapcheck // Thin wrapper matching os.Rename signature.
func Rename(oldpath, newpath string) error {
	if posixRename(oldpath, newpath) {
		return nil
	}

	return os.Rename(oldpath, newpath)
}

// fileRenameInfo is FILE_RENAME_INFO with the Flags member of its union, as
// FileRenameInfoEx reads it.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [windows.MAX_LONG_PATH]uint16
}

// posixRename asks the filesystem to take newpath's name over at once, a handle
// still open on the old newpath keeping the old file until it closes, and
// reports whether it did. NTFS from Windows 10 1709 does, every time a rename
// can succeed at all; FAT, exFAT, some shares and older Windows refuse the
// request itself, every time, and os.Rename then does there what it does.
func posixRename(oldpath, newpath string) bool {
	target, err := filepath.Abs(newpath)
	if err != nil {
		return false
	}

	name, err := windows.UTF16FromString(target)
	if err != nil || len(name) > windows.MAX_LONG_PATH {
		return false
	}

	source, err := windows.UTF16PtrFromString(oldpath)
	if err != nil {
		return false
	}

	handle, err := windows.CreateFile(
		source,
		windows.DELETE|windows.SYNCHRONIZE,
		shareMode,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return false
	}

	defer func() { _ = windows.CloseHandle(handle) }()

	// A directory goes to os.Rename: the POSIX rename would replace an empty
	// directory in its way, as Unix does, where MoveFileEx refuses, and xos
	// parts from os over held files alone.
	var about windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &about) != nil ||
		about.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return false
	}

	info := fileRenameInfo{Flags: windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS}
	copy(info.FileName[:], name)
	info.FileNameLength = uint32((len(name) - 1) * 2) // #nosec G115 -- bounded by MAX_LONG_PATH above

	return windows.SetFileInformationByHandle(
		handle,
		windows.FileRenameInfoEx,
		(*byte)(unsafe.Pointer(&info)), // #nosec G103 -- the struct is what the call reads
		uint32(unsafe.Sizeof(info)),
	) == nil
}

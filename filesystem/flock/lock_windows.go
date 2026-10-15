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

// From internal go
//
// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// https://cs.opensource.google/go/go/+/master:src/cmd/go/internal/lockedfile/internal/filelock/filelock_windows.go

//nolint:wrapcheck // the platform layer; the portable functions wrap
package flock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"unicode/utf16"

	"github.com/forkcloser/xos"
	"golang.org/x/sys/windows"

	"github.com/mycophonic/primordium/filesystem/internal"
)

type lockType uint32

const (
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa365203(v=vs.85).aspx
	readLock  lockType = 0
	writeLock lockType = windows.LOCKFILE_EXCLUSIVE_LOCK

	reserved = 0
	allBytes = ^uint32(0)

	lockSuffix = ".lock"
	// maxComponent is the longest name NTFS and ReFS take, in UTF-16 units.
	maxComponent = 255
	// utf16Pair is the first code point UTF-16 spells in two units.
	utf16Pair = 0x10000
)

// sidecar is the lock file kept beside path: its name with ".lock" appended
// or, when that would not fit a name, the name cut to fit around a digest of
// the whole, so that two long names that start alike keep two lock files.
func sidecar(path string) string {
	base := filepath.Base(path)
	if len(utf16.Encode([]rune(base+lockSuffix))) <= maxComponent {
		return path + lockSuffix
	}

	sum := sha256.Sum256([]byte(base))
	tag := "~" + hex.EncodeToString(sum[:8]) + lockSuffix
	budget := maxComponent - len(tag)

	var (
		cut   []rune
		units int
	)

	for _, code := range base {
		width := 1
		if code >= utf16Pair {
			width = 2
		}

		if units+width > budget {
			break
		}

		units += width

		cut = append(cut, code)
	}

	return filepath.Join(filepath.Dir(path), string(cut)+tag)
}

func platformLock(path string, lockType lockType) (file *os.File, err error) {
	file, err = xos.OpenFile(sidecar(path), os.O_CREATE, internal.FilePermissionsPrivate)
	if err != nil {
		return nil, err
	}

	if err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		uint32(lockType), reserved, allBytes, allBytes, new(windows.Overlapped),
	); err != nil {
		if fileErr := file.Close(); fileErr != nil {
			err = errors.Join(err, fileErr)
		}

		return nil, err
	}

	return file, nil
}

func platformTryLock(path string, lockType lockType) (file *os.File, err error) {
	file, err = xos.OpenFile(sidecar(path), os.O_CREATE, internal.FilePermissionsPrivate)
	if err != nil {
		return nil, err
	}

	if err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		uint32(lockType)|windows.LOCKFILE_FAIL_IMMEDIATELY,
		reserved, allBytes, allBytes, new(windows.Overlapped),
	); err != nil {
		closeErr := file.Close()

		// ERROR_LOCK_VIOLATION indicates the lock is held by another process
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, errors.Join(ErrLockWouldBlock, closeErr)
		}

		return nil, errors.Join(err, closeErr)
	}

	return file, nil
}

// cleanupLockSidecar removes the lock file platformLock keeps beside the
// path, which Unlock's close leaves behind.
func cleanupLockSidecar(path string) {
	_ = os.Remove(sidecar(path))
}

func platformUnlock(file *os.File) (err error) {
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	return windows.UnlockFileEx(windows.Handle(file.Fd()), reserved, allBytes, allBytes, new(windows.Overlapped))
}

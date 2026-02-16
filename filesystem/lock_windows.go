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

package filesystem

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

type lockType uint32

const (
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa365203(v=vs.85).aspx
	readLock  lockType = 0
	writeLock lockType = windows.LOCKFILE_EXCLUSIVE_LOCK

	reserved = 0
	allBytes = ^uint32(0)

	lockPermission = FilePermissionsPrivate
)

//nolint:wrapcheck
func platformLock(path string, lockType lockType) (file *os.File, err error) {
	//nolint:gosec
	file, err = os.OpenFile(path+".lock", os.O_CREATE, lockPermission)
	if err != nil {
		return nil, err
	}

	if err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		uint32(lockType), reserved, allBytes, allBytes, new(windows.Overlapped)); err != nil {
		if fileErr := file.Close(); fileErr != nil {
			err = errors.Join(err, fileErr)
		}

		return nil, err
	}

	return file, nil
}

//nolint:wrapcheck
func platformTryLock(path string, lockType lockType) (file *os.File, err error) {
	//nolint:gosec
	file, err = os.OpenFile(path+".lock", os.O_CREATE, lockPermission)
	if err != nil {
		return nil, err
	}

	// Use LOCKFILE_FAIL_IMMEDIATELY for non-blocking
	if err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		uint32(lockType)|windows.LOCKFILE_FAIL_IMMEDIATELY,
		reserved, allBytes, allBytes, new(windows.Overlapped)); err != nil {
		if fileErr := file.Close(); fileErr != nil {
			err = errors.Join(err, fileErr)
		}

		// ERROR_LOCK_VIOLATION indicates the lock is held by another process
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrLockWouldBlock
		}

		return nil, err
	}

	return file, nil
}

//nolint:wrapcheck
func platformUnlock(file *os.File) (err error) {
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	return windows.UnlockFileEx(windows.Handle(file.Fd()), reserved, allBytes, allBytes, new(windows.Overlapped))
}

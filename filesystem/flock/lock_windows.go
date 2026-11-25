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
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

type lockKind uint32

const (
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa365203(v=vs.85).aspx
	shared    lockKind = 0
	exclusive lockKind = windows.LOCKFILE_EXCLUSIVE_LOCK

	reserved = 0
	allBytes = ^uint32(0)
)

func platformLock(file *os.File, kind lockKind, wait bool) error {
	flags := uint32(kind)
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}

	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, reserved, allBytes, allBytes, new(windows.Overlapped))
	if !wait && errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLockWouldBlock
	}

	return err
}

func platformUnlock(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), reserved, allBytes, allBytes, new(windows.Overlapped))
}

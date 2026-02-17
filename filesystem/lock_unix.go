//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

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

// Portions from internal go
//
// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package filesystem

import (
	"errors"
	"os"
	"syscall"
)

type lockType int16

const (
	readLock  lockType = syscall.LOCK_SH
	writeLock lockType = syscall.LOCK_EX
)

//nolint:wrapcheck
func platformLock(path string, lockType lockType) (*os.File, error) {
	//nolint:gosec
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	for {
		err = syscall.Flock(int(file.Fd()), int(lockType))
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}

	if err != nil {
		if fileErr := file.Close(); fileErr != nil {
			err = errors.Join(err, fileErr)
		}

		return nil, err
	}

	return file, nil
}

//nolint:wrapcheck
func platformTryLock(path string, lockType lockType) (*os.File, error) {
	//nolint:gosec
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Use LOCK_NB for non-blocking
	err = syscall.Flock(int(file.Fd()), int(lockType)|syscall.LOCK_NB)
	if err != nil {
		if fileErr := file.Close(); fileErr != nil {
			err = errors.Join(err, fileErr)
		}

		// Convert EWOULDBLOCK to our sentinel error
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
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

	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

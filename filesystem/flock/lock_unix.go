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

//nolint:wrapcheck // the platform layer; the portable functions wrap
package flock

import (
	"errors"
	"os"
	"syscall"
)

type lockKind int

const (
	shared    lockKind = syscall.LOCK_SH
	exclusive lockKind = syscall.LOCK_EX
)

func platformLock(file *os.File, kind lockKind, wait bool) error {
	how := int(kind)
	if !wait {
		how |= syscall.LOCK_NB
	}

	for {
		err := syscall.Flock(int(file.Fd()), how)

		switch {
		case errors.Is(err, syscall.EINTR):
		case !wait && (errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)):
			return ErrLockWouldBlock
		default:
			return err
		}
	}
}

func platformUnlock(file *os.File) error {
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

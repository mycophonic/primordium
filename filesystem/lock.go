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
// From go internal
//
// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package filesystem

// Package filelock provides a platform-independent API for advisory file
// locking. Calls to functions in this package on platforms that do not support
// advisory locks will return errors for which IsNotSupported returns true.

import (
	"errors"
	"os"
)

// Lock places an advisory write lock on the file, blocking until it can be
// locked.
//
// If Lock returns nil, no other process will be able to place a read or write
// lock on the file until this process exits, closes f, or calls Unlock on it.
func Lock(path string) (*os.File, error) {
	file, err := platformLock(path, writeLock)
	if err != nil {
		err = errors.Join(ErrLockFail, err)
	}

	return file, err
}

// ReadOnlyLock places an advisory read lock on the file, blocking until it can be locked.
//
// If ReadOnlyLock returns nil, no other process will be able to place a write lock on
// the file until this process exits, closes f, or calls Unlock on it.
func ReadOnlyLock(path string) (*os.File, error) {
	file, err := platformLock(path, readLock)
	if err != nil {
		err = errors.Join(ErrLockFail, err)
	}

	return file, err
}

// TryLock attempts to place an advisory write lock on the file without blocking.
//
// If the lock cannot be acquired immediately because another process holds a
// conflicting lock, TryLock returns ErrLockWouldBlock.
// If TryLock returns nil error, no other process will be able to place a read or write
// lock on the file until this process exits, closes f, or calls Unlock on it.
func TryLock(path string) (*os.File, error) {
	file, err := platformTryLock(path, writeLock)
	if err != nil {
		if !errors.Is(err, ErrLockWouldBlock) {
			err = errors.Join(ErrLockFail, err)
		}
	}

	return file, err
}

// TryReadOnlyLock attempts to place an advisory read lock on the file without blocking.
//
// If the lock cannot be acquired immediately because another process holds a
// conflicting lock, TryReadOnlyLock returns ErrLockWouldBlock.
// If TryReadOnlyLock returns nil error, no other process will be able to place a write
// lock on the file until this process exits, closes f, or calls Unlock on it.
func TryReadOnlyLock(path string) (*os.File, error) {
	file, err := platformTryLock(path, readLock)
	if err != nil {
		if !errors.Is(err, ErrLockWouldBlock) {
			err = errors.Join(ErrLockFail, err)
		}
	}

	return file, err
}

// Unlock removes an advisory lock placed on f by this process.
func Unlock(lock *os.File) error {
	if lock == nil {
		return ErrLockIsNil
	}

	err := platformUnlock(lock)
	if err != nil {
		err = errors.Join(ErrUnlockFail, err)
	}

	return err
}

// WithLock acquires a write lock on the file at path, and executes the provided function.
func WithLock(path string, function func() error) (err error) {
	file, err := Lock(path)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(Unlock(file), err)
	}()

	return function()
}

// WithReadOnlyLock acquires a read lock on the file at path, and executes the provided function.
func WithReadOnlyLock(path string, function func() error) (err error) {
	file, err := ReadOnlyLock(path)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(Unlock(file), err)
	}()

	return function()
}

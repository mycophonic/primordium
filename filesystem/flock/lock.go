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

package flock

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"github.com/forkcloser/xos"
)

// Lock is a lock held on a lock file, from the call that returned it until
// its Unlock. Each one is a holder of its own, in one process as across
// several: shared holders coexist, an exclusive holder coexists with none.
type Lock struct {
	file *os.File
}

// Exclusive locks the lock file at path for this holder alone, waiting until
// it can. Like every call here, it is ErrLockFail and fs.ErrNotExist when
// there is no file at path.
func Exclusive(path string) (*Lock, error) {
	return acquire(path, exclusive, true)
}

// Shared locks the lock file at path together with every other shared
// holder, waiting until no exclusive one is left.
func Shared(path string) (*Lock, error) {
	return acquire(path, shared, true)
}

// TryExclusive is Exclusive without the wait: ErrLockWouldBlock when another
// holder has the lock file.
func TryExclusive(path string) (*Lock, error) {
	return acquire(path, exclusive, false)
}

// TryShared is Shared without the wait: ErrLockWouldBlock when an exclusive
// holder has the lock file.
func TryShared(path string) (*Lock, error) {
	return acquire(path, shared, false)
}

// Unlock releases the lock. A nil Lock, or one already released, is
// ErrUnlockFail.
func (lock *Lock) Unlock() error {
	if lock == nil {
		return errors.Join(ErrUnlockFail, os.ErrInvalid)
	}

	// Closing releases the lock whatever the unlock said.
	if err := errors.Join(platformUnlock(lock.file), lock.file.Close()); err != nil {
		return errors.Join(ErrUnlockFail, err)
	}

	return nil
}

// WithExclusive runs function while holding the exclusive lock of the lock
// file at path, and returns its error.
func WithExclusive(path string, function func() error) error {
	return with(path, exclusive, function)
}

// WithShared runs function while holding a shared lock of the lock file at
// path, and returns its error.
func WithShared(path string, function func() error) error {
	return with(path, shared, function)
}

func with(path string, kind lockKind, function func() error) (err error) {
	lock, err := acquire(path, kind, true)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, lock.Unlock())
	}()

	return function()
}

// acquire opens the lock file, which must be there, and locks it.
func acquire(path string, kind lockKind, wait bool) (*Lock, error) {
	for {
		file, err := open(path, kind)
		if err != nil {
			return nil, errors.Join(ErrLockFail, err)
		}

		if err = lockFile(file, kind, wait); err != nil {
			err = errors.Join(err, file.Close())
			if !errors.Is(err, ErrLockWouldBlock) {
				err = errors.Join(ErrLockFail, err)
			}

			return nil, err
		}

		// The file was opened before it was locked, and may have been
		// removed or replaced in between: the lock is then on a file no
		// other caller opens, and excludes nobody. Starting over finds the
		// file now there, or none, which is the lock gone.
		current, err := atPath(file, path)
		if current {
			return &Lock{file: file}, nil
		}

		err = errors.Join(err, platformUnlock(file), file.Close())
		if err != nil {
			return nil, errors.Join(ErrLockFail, err)
		}
	}
}

// open opens the lock file with the least access its lock needs, so that a
// lock file the user may only read, in a tree mounted read-only or filled by
// another user, takes every lock the platform gives it. It creates nothing.
//
//nolint:wrapcheck // acquire, its only caller, wraps what it returns
func open(path string, kind lockKind) (*os.File, error) {
	// flock(2), on NFS: "in order to place an exclusive lock, the file must
	// be opened for writing". Where it may not be written, a lock on it
	// opened for reading is what is left, and every local filesystem gives
	// it.
	if kind == exclusive {
		file, err := xos.OpenFile(path, os.O_RDWR, 0)
		if err == nil || !(errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS)) {
			return file, err
		}
	}

	return xos.OpenFile(path, os.O_RDONLY, 0)
}

var errNotAFile = errors.New("not a regular file")

// lockFile locks file, which must be a regular file: Unix opens a directory
// for reading and locks it, and Windows does neither.
//
//nolint:wrapcheck // acquire, its only caller, wraps what it returns
func lockFile(file *os.File, kind lockKind, wait bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return errNotAFile
	}

	return platformLock(file, kind, wait)
}

// atPath reports whether file is the file now at path.
//
//nolint:wrapcheck // acquire, its only caller, wraps what it returns
func atPath(file *os.File, path string) (bool, error) {
	held, err := file.Stat()
	if err != nil {
		return false, err
	}

	found, err := xos.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return os.SameFile(held, found), nil
}

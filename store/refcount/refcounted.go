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

package refcount

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/flock"
	"github.com/mycophonic/primordium/filesystem/pathcheck"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Locker counts the holders of keyed directories under a root, across
// processes: a key's entry exists while any process holds it and goes with
// its last release. A holder that dies releases, the OS drops its lock.
type Locker struct {
	rootDir string
}

// New is a Locker over root. It panics when root is not a valid path.
func New(root string) *Locker {
	if err := pathcheck.Validate(root); err != nil {
		panic(fmt.Errorf("Locker: invalid rootDir path: %w", err))
	}

	return &Locker{rootDir: root}
}

// ResourceFactory fills an entry directory and returns the path a holder
// gets and a cleanup, which may be nil. It runs with exclusive access to the
// directory on every Acquire, so it decides itself what to do with an entry
// that is already filled. The cleanup runs in the last holder's release only,
// before the directory is removed; every other holder's is discarded.
type ResourceFactory func(dir string) (resourcePath string, cleanup func(), err error)

// Acquire holds key's entry: it runs factory on the entry directory and
// returns the path the factory returned, with release, which must be called
// when done and is idempotent. The last release removes the entry directory;
// one that cannot take its locks leaves the entry in place and warns, and the
// next hold of the key removes it. A key that is not a single path component
// is ErrInvalidArgument. A factory error is returned as it is, and the entry
// is released as a holder's is.
func (rc *Locker) Acquire(key string, factory ResourceFactory) (string, func(), error) {
	if err := pathcheck.ValidateComponent(key); err != nil {
		return "", nil, fmt.Errorf("%w: invalid key %q: %w", fault.ErrInvalidArgument, key, err)
	}

	resourceDir := filepath.Join(rc.rootDir, key)
	lockPath := filepath.Join(resourceDir, refCountedLockFile)

	if err := os.MkdirAll(rc.rootDir, filesystem.DirPermissionsPrivate); err != nil {
		return "", nil, fmt.Errorf("%w: store rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	// The root lock is held until the directory is locked: between the two, a
	// last release could remove the directory just created. The cost is that
	// every Acquire on the store waits here while another key's factory runs.
	globalLock, err := flock.Lock(rc.rootDir)
	if err != nil {
		return "", nil, fmt.Errorf("%w: global lock: %w", fault.ErrFilesystemFailure, err)
	}

	if err = os.MkdirAll(resourceDir, filesystem.DirPermissionsPrivate); err != nil {
		_ = flock.Unlock(globalLock)

		return "", nil, fmt.Errorf("%w: entry directory: %w", fault.ErrFilesystemFailure, err)
	}

	dirLock, err := flock.Lock(resourceDir)
	if err != nil {
		_ = flock.Unlock(globalLock)

		return "", nil, fmt.Errorf("%w: directory lock: %w", fault.ErrFilesystemFailure, err)
	}

	_ = flock.Unlock(globalLock)

	// The read lock on the lock file is the holder's count: a release tries an
	// exclusive lock on it, which succeeds only once no read lock remains.
	if err = touchLockFile(lockPath); err != nil {
		_ = flock.Unlock(dirLock)

		return "", nil, fmt.Errorf("%w: lock file: %w", fault.ErrFilesystemFailure, err)
	}

	readLock, err := flock.ReadOnlyLock(lockPath)
	if err != nil {
		_ = flock.Unlock(dirLock)

		return "", nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	resourcePath, cleanup, err := factory(resourceDir)
	if err != nil {
		// Released as a holder is, so that the entry goes when no other holder
		// has it. The directory lock goes first: release takes the global lock
		// before it, and taking it while holding the directory's would invert
		// Acquire's order.
		_ = flock.Unlock(dirLock)

		rc.buildRelease(resourceDir, lockPath, readLock, nil)()

		return "", nil, err
	}

	_ = flock.Unlock(dirLock)

	release := rc.buildRelease(resourceDir, lockPath, readLock, cleanup)

	return resourcePath, release, nil
}

// buildRelease is the release of one holder: idempotent, and the last one
// runs cleanup and removes the entry.
func (rc *Locker) buildRelease(dir, lockPath string, readLock *os.File, cleanup func()) func() {
	var once sync.Once

	return func() {
		once.Do(func() {
			globalLock, err := flock.Lock(rc.rootDir)
			if err != nil {
				_ = flock.Unlock(readLock)

				left(dir, err)

				return
			}

			dirLock, err := flock.Lock(dir)
			if err != nil {
				_ = flock.Unlock(globalLock)
				_ = flock.Unlock(readLock)

				left(dir, err)

				return
			}

			// Our own read lock goes before the try: flock counts each open
			// file as a holder of its own, ours included.
			_ = flock.Unlock(readLock)

			exclusiveLock, err := flock.TryLock(lockPath)
			if err != nil {
				_ = flock.Unlock(dirLock)
				_ = flock.Unlock(globalLock)

				left(dir, err)

				return
			}

			if cleanup != nil {
				cleanup()
			}

			_ = flock.Unlock(exclusiveLock)
			_ = os.RemoveAll(dir)

			// The directory's own lock file sits beside the directory on
			// Windows: it goes after the lock on it is released.
			_ = flock.Unlock(dirLock)

			flock.Cleanup(dir)

			_ = flock.Unlock(globalLock)
		})
	}
}

// left says that a release could not tell whether it was the last and left
// the entry in place, which the next hold of the key removes. A lock that
// would block is another holder, the usual case, and nothing to say.
func left(dir string, err error) {
	if errors.Is(err, flock.ErrLockWouldBlock) {
		return
	}

	slog.Warn("refcount: release left the entry in place", "dir", dir, "error", err)
}

func touchLockFile(path string) error {
	file, err := xos.OpenFile(path, os.O_CREATE|os.O_RDONLY, filesystem.FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

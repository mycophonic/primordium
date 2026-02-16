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

package filesystem

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mycophonic/primordium/fault"
)

const (
	refCountedLockFile = ".lock"
)

// Locker provides cross-process coordination for keyed resources.
// Uses file-based locking (flock) for safe concurrent access across multiple processes.
// Crash-resistant: OS automatically releases file locks when a process dies.
type Locker struct {
	rootDir string
}

// NewLocker creates a new Locker coordinator at the given rootDir directory.
// Panics if rootDir contains invalid path components.
func NewLocker(root string) *Locker {
	if err := ValidatePath(root); err != nil {
		panic(fmt.Errorf("Locker: invalid rootDir path: %w", err))
	}

	return &Locker{rootDir: root}
}

// ResourceFactory creates a resource and returns its path and optional cleanup function.
// Called with exclusive access to the entry directory when the resource doesn't exist.
// The cleanup function (if non-nil) is called when the last holder releases.
type ResourceFactory func(dir string) (resourcePath string, cleanup func(), err error)

// Acquire coordinates cross-process access to a keyed resource.
// If the resource exists, returns its path with a read lock held.
// If not, calls factory to create it.
// The returned release function MUST be called when done.
// When the last holder releases, the entry directory is cleaned up.
// The key must be a valid single path component (no separators).
func (rc *Locker) Acquire(key string, factory ResourceFactory) (string, func(), error) {
	if err := ValidatePathComponent(key); err != nil {
		return "", nil, fmt.Errorf("%w: invalid key %q: %w", fault.ErrInvalidArgument, key, err)
	}

	resourceDir := filepath.Join(rc.rootDir, key)
	lockPath := filepath.Join(resourceDir, refCountedLockFile)

	// Step 1: Acquire exclusive global lock on store rootDir
	if err := os.MkdirAll(rc.rootDir, DirPermissionsPrivate); err != nil {
		return "", nil, fmt.Errorf("%w: store rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	globalLock, err := Lock(rc.rootDir)
	if err != nil {
		return "", nil, fmt.Errorf("%w: global lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Create entry directory
	if err := os.MkdirAll(resourceDir, DirPermissionsPrivate); err != nil {
		_ = Unlock(globalLock)

		return "", nil, fmt.Errorf("%w: entry directory: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 3: Acquire exclusive lock on entry directory, release global lock
	dirLock, err := Lock(resourceDir)
	if err != nil {
		_ = Unlock(globalLock)

		return "", nil, fmt.Errorf("%w: directory lock: %w", fault.ErrFilesystemFailure, err)
	}

	_ = Unlock(globalLock)

	// Step 4: Touch lock file, acquire read lock on it
	if err := touchLockFile(lockPath); err != nil {
		_ = Unlock(dirLock)

		return "", nil, fmt.Errorf("%w: lock file: %w", fault.ErrFilesystemFailure, err)
	}

	readLock, err := ReadOnlyLock(lockPath)
	if err != nil {
		_ = Unlock(dirLock)

		return "", nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 5: Call factory to create resource (factory checks if already exists)
	resourcePath, cleanup, err := factory(resourceDir)
	if err != nil {
		_ = Unlock(readLock)
		_ = Unlock(dirLock)

		return "", nil, err
	}

	// Step 6: Release directory lock (read lock on lock file protects us)
	_ = Unlock(dirLock)

	// Build release function
	release := rc.buildRelease(resourceDir, lockPath, readLock, cleanup)

	return resourcePath, release, nil
}

// buildRelease creates the release function for an acquired resource.
func (rc *Locker) buildRelease(dir, lockPath string, readLock *os.File, cleanup func()) func() {
	return func() {
		// Step 1: Acquire exclusive global lock
		globalLock, err := Lock(rc.rootDir)
		if err != nil {
			// Can't get global lock - just release our read lock
			_ = Unlock(readLock)

			return
		}

		// Step 2: Acquire exclusive lock on entry directory
		dirLock, err := Lock(dir)
		if err != nil {
			// Can't get dir lock - release global and our read lock
			_ = Unlock(globalLock)
			_ = Unlock(readLock)

			return
		}

		// Step 3: Release our read lock on lock file
		_ = Unlock(readLock)

		// Step 4: TryLock on lock file - if succeeds, we're the last holder
		exclusiveLock, err := TryLock(lockPath)
		if err != nil {
			// Others still have read locks - leave resource in place
			_ = Unlock(dirLock)
			_ = Unlock(globalLock)

			return
		}

		// We're the last holder - run cleanup and delete the entry
		if cleanup != nil {
			cleanup()
		}

		_ = Unlock(exclusiveLock)
		_ = os.RemoveAll(dir)
		_ = Unlock(dirLock)
		_ = Unlock(globalLock)
	}
}

// touchLockFile creates the lock file if it doesn't exist.
func touchLockFile(path string) error {
	//nolint:gosec // Path is derived from user-provided key in controlled directory
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, FilePermissionsPrivate)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

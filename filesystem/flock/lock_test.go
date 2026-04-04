//go:build !race

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

//revive:disable:add-constant
package flock_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/flock"
)

// Note: these tests are NOT racy - the entire point here is to prove that the filesystem lock is effectively protecting
// against these conditions.
// Maybe the test could be rewritten to use different variables instead so we can use race on it.

const (
	mainroutine1 = "mainroutine1"
	mainroutine2 = "mainroutine2"
	routine1     = "routine1"
	routine2     = "routine2"
	routine3     = "routine3"
)

func TestLock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	// Lock acquisition
	file, err := flock.Lock(tempDir)
	assert.NilError(t, err, "acquiring a lock should succeed")
	err = flock.Unlock(file)
	assert.NilError(t, err, "releasing a lock should succeed")

	file, err = flock.ReadOnlyLock(tempDir)
	assert.NilError(t, err, "acquiring a read-only lock should succeed")
	file2, err := flock.ReadOnlyLock(tempDir)
	assert.NilError(t, err, "acquiring another read-only lock should succeed")
	err = flock.Unlock(file)
	assert.NilError(t, err, "releasing a read-only lock should succeed")
	err = flock.Unlock(file2)
	assert.NilError(t, err, "releasing another read-only lock should succeed")
}

func TestLockWriteConcurrent(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup

	var concurrentKey string

	tempDir := t.TempDir()

	waitGroup.Add(2)

	// Start a lock, set the key, sleep 1s and confirm the key is still the same
	go func() {
		defer waitGroup.Done()

		lErr := flock.WithLock(tempDir, func() error {
			concurrentKey = routine1

			time.Sleep(1 * time.Second)
			assert.Assert(t, concurrentKey == routine1, "Key:"+concurrentKey)

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	// Wait 0.5s, start another lock, set the key, sleep 1s and confirm the key is still the same
	go func() {
		defer waitGroup.Done()

		time.Sleep(500 * time.Millisecond)

		lErr := flock.WithLock(tempDir, func() error {
			concurrentKey = routine2

			time.Sleep(1 * time.Second)
			assert.Assert(t, concurrentKey == routine2, "Key:"+concurrentKey)

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	// Start a lock, set the key, wait 1s, confirm the key is still the same
	lErr := flock.WithLock(tempDir, func() error {
		concurrentKey = mainroutine1

		time.Sleep(1 * time.Second)
		assert.Assert(t, concurrentKey == mainroutine1, "Key:"+concurrentKey)

		return nil
	})
	assert.NilError(t, lErr, "locking should not error")

	// Wait 0.75s, start a lock, set the key, sleep 1s, confirm the key is unchanged
	time.Sleep(750 * time.Millisecond)

	lErr = flock.WithLock(tempDir, func() error {
		concurrentKey = mainroutine2

		time.Sleep(1 * time.Second)
		assert.Assert(t, concurrentKey == mainroutine2, "Key:"+concurrentKey)

		return nil
	})

	assert.NilError(t, lErr, "locking should not error")

	waitGroup.Wait()
}

func TestLockMultiRead(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup

	var concurrentKey string

	tempDir := t.TempDir()

	waitGroup.Add(3)

	// Start a readonly lock immediately
	// Then wait 1s inside the lock - confirm the key got changed by the second read routine
	go func() {
		t.Log("Entering routine 1")

		defer waitGroup.Done()

		lErr := flock.WithReadOnlyLock(tempDir, func() error {
			t.Log("Entering routine 1 read lock")

			concurrentKey = routine1

			time.Sleep(1 * time.Second)
			assert.Assert(t, concurrentKey == routine2, "Key:"+concurrentKey)

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	// Wait 0.5s before locking, then change the key
	go func() {
		t.Log("Entering routine 2")

		defer waitGroup.Done()

		time.Sleep(500 * time.Millisecond)

		lErr := flock.WithReadOnlyLock(tempDir, func() error {
			t.Log("Entering routine 2 read lock")

			concurrentKey = routine2

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	time.Sleep(50 * time.Millisecond)
	// Start a write lock, confirm we have waited for the read locks to finish, change the key
	go func() {
		t.Log("Entering routine 3")

		defer waitGroup.Done()

		lErr := flock.WithLock(tempDir, func() error {
			t.Log("Entering routine 3 write lock")
			assert.Assert(t, concurrentKey == routine2, "Key:"+concurrentKey)
			concurrentKey = routine3

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	waitGroup.Wait()
}

func TestLockWriteBlocksRead(t *testing.T) {
	t.Parallel()

	var waitGroup sync.WaitGroup

	var concurrentKey string

	tempDir := t.TempDir()

	waitGroup.Add(2)

	// Start a lock, set the key, sleep 1s and confirm the key is still the same
	go func() {
		defer waitGroup.Done()

		lErr := flock.WithLock(tempDir, func() error {
			time.Sleep(1 * time.Second)

			concurrentKey = routine1

			assert.Assert(t, concurrentKey == routine1, "Key:"+concurrentKey)

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	time.Sleep(50 * time.Millisecond)

	// Start a readonly lock immediately
	// Confirm the key has been set by the write lock
	go func() {
		defer waitGroup.Done()

		lErr := flock.WithReadOnlyLock(tempDir, func() error {
			assert.Assert(t, concurrentKey == routine1, "Key:"+concurrentKey)

			return nil
		})

		assert.NilError(t, lErr, "locking should not error")
	}()

	waitGroup.Wait()
}

func TestTryLock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Acquire exclusive lock
	file, err := flock.Lock(tempDir)
	assert.NilError(t, err)

	// TryLock should fail with ErrLockWouldBlock
	_, tryErr := flock.TryLock(tempDir)
	assert.Assert(t, errors.Is(tryErr, flock.ErrLockWouldBlock), "expected ErrLockWouldBlock, got: %v", tryErr)

	// Release the lock
	err = flock.Unlock(file)
	assert.NilError(t, err)

	// TryLock should now succeed
	file2, err := flock.TryLock(tempDir)
	assert.NilError(t, err)
	err = flock.Unlock(file2)
	assert.NilError(t, err)
}

func TestTryReadOnlyLock(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Acquire exclusive lock
	file, err := flock.Lock(tempDir)
	assert.NilError(t, err)

	// TryReadOnlyLock should fail (exclusive blocks shared)
	_, tryErr := flock.TryReadOnlyLock(tempDir)
	assert.Assert(t, errors.Is(tryErr, flock.ErrLockWouldBlock), "expected ErrLockWouldBlock, got: %v", tryErr)

	err = flock.Unlock(file)
	assert.NilError(t, err)

	// TryReadOnlyLock should succeed when no exclusive lock held
	file2, err := flock.TryReadOnlyLock(tempDir)
	assert.NilError(t, err)

	// Another TryReadOnlyLock should also succeed (shared locks are compatible)
	file3, err := flock.TryReadOnlyLock(tempDir)
	assert.NilError(t, err)

	err = flock.Unlock(file2)
	assert.NilError(t, err)
	err = flock.Unlock(file3)
	assert.NilError(t, err)
}

func TestUnlockNil(t *testing.T) {
	t.Parallel()

	err := flock.Unlock(nil)
	assert.Assert(t, errors.Is(err, flock.ErrLockIsNil))
}

func TestLockNonexistentPath(t *testing.T) {
	t.Parallel()

	_, err := flock.Lock("/nonexistent/path/that/does/not/exist")
	assert.Assert(t, errors.Is(err, flock.ErrLockFail))

	_, err = flock.TryLock("/nonexistent/path/that/does/not/exist")
	assert.Assert(t, errors.Is(err, flock.ErrLockFail))
}

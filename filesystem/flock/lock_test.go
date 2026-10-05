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

package flock_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/flock"
)

// waitLimit bounds how long a test waits for a blocked lock to be granted once
// it should be; reaching it means the lock was never granted, not that the
// runner is slow.
const waitLimit = 30 * time.Second

// waiter is a lock taken in its own goroutine, which blocks until the test
// releases what conflicts with it. When granted, it reports how many
// conflicting locks had been released by then; done carries its error.
type waiter struct {
	granted chan int32
	done    chan error
}

func startWaiter(released *atomic.Int32, lock func(string, func() error) error, path string) waiter {
	w := waiter{granted: make(chan int32, 1), done: make(chan error, 1)}

	go func() {
		w.done <- lock(path, func() error {
			w.granted <- released.Load()

			return nil
		})
	}()

	return w
}

// wait selects on done alone: granted is sent before done, so once done has
// arrived granted is already buffered, while a select over both ready channels
// would pick either.
func (w waiter) wait(t *testing.T) int32 {
	t.Helper()

	select {
	case err := <-w.done:
		assert.NilError(t, err, "the waiting lock should not error")
	case <-time.After(waitLimit):
		t.Fatal("the waiting lock was never granted")
	}

	select {
	case got := <-w.granted:
		return got
	default:
		t.Fatal("the waiting lock returned without running")
	}

	return 0
}

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

	tempDir := t.TempDir()

	var released atomic.Int32

	held, err := flock.Lock(tempDir)
	assert.NilError(t, err)

	second := startWaiter(&released, flock.WithLock, tempDir)

	_, tryErr := flock.TryLock(tempDir)
	assert.Assert(t, errors.Is(tryErr, flock.ErrLockWouldBlock), "a second write lock should block, got: %v", tryErr)

	released.Add(1)
	assert.NilError(t, flock.Unlock(held))

	assert.Equal(t, second.wait(t), int32(1), "the second write lock was granted while the first was held")
}

func TestLockMultiRead(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	var released atomic.Int32

	reader1, err := flock.ReadOnlyLock(tempDir)
	assert.NilError(t, err)

	// A second read lock is granted while the first is held: shared locks are compatible.
	reader2, err := flock.TryReadOnlyLock(tempDir)
	assert.NilError(t, err, "a second read lock should not block")

	writer := startWaiter(&released, flock.WithLock, tempDir)

	_, tryErr := flock.TryLock(tempDir)
	assert.Assert(t, errors.Is(tryErr, flock.ErrLockWouldBlock),
		"a write lock should block on two readers, got: %v", tryErr)

	released.Add(1)
	assert.NilError(t, flock.Unlock(reader1))

	_, tryErr = flock.TryLock(tempDir)
	assert.Assert(t, errors.Is(tryErr, flock.ErrLockWouldBlock),
		"a write lock should block on the remaining reader, got: %v", tryErr)

	released.Add(1)
	assert.NilError(t, flock.Unlock(reader2))

	assert.Equal(t, writer.wait(t), int32(2), "the write lock was granted while a reader was held")
}

func TestLockWriteBlocksRead(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	var released atomic.Int32

	held, err := flock.Lock(tempDir)
	assert.NilError(t, err)

	reader := startWaiter(&released, flock.WithReadOnlyLock, tempDir)

	_, tryErr := flock.TryReadOnlyLock(tempDir)
	assert.Assert(
		t,
		errors.Is(tryErr, flock.ErrLockWouldBlock),
		"a read lock should block on a writer, got: %v",
		tryErr,
	)

	released.Add(1)
	assert.NilError(t, flock.Unlock(held))

	assert.Equal(t, reader.wait(t), int32(1), "the read lock was granted while the write lock was held")
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

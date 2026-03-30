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

package refcount_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/refcount"
)

func TestLocker_InvalidKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	// Path traversal in key should be rejected
	_, _, err := locker.Acquire("..", func(dir string) (string, func(), error) {
		return filepath.Join(dir, "data"), nil, nil
	})
	if err == nil {
		t.Error("expected error for path traversal key")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("error = %v, want fault.ErrInvalidArgument", err)
	}
}

func TestLocker_ConcurrentAcquireSameKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const numGoroutines = 20

	const key = "shared-resource"

	var factoryCalls atomic.Int32

	var wg sync.WaitGroup

	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
				factoryCalls.Add(1)

				dataPath := filepath.Join(dir, "data")
				// Simulate some work
				if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
					return "", nil, err
				}

				return dataPath, nil, nil
			})
			if err != nil {
				t.Errorf("Acquire failed: %v", err)

				return
			}

			defer release()

			// Verify we got a valid path
			if _, err := xos.Stat(path); err != nil {
				t.Errorf("path %q not accessible: %v", path, err)
			}
		}()
	}

	wg.Wait()

	// Factory is called on every Acquire (callers are responsible for idempotency).
	if factoryCalls.Load() != numGoroutines {
		t.Errorf("factory called %d times, want %d", factoryCalls.Load(), numGoroutines)
	}
}

func TestLocker_ReleaseWhenLastHolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const key = "cleanup-test"

	var cleanupCalled atomic.Bool

	path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
			return "", nil, err
		}

		return dataPath, func() {
			cleanupCalled.Store(true)
		}, nil
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	resourceDir := filepath.Dir(path)

	// Release - should trigger cleanup since we're the only holder
	release()

	// Cleanup happens synchronously within release(), so no sleep needed
	// Cleanup should have been called
	if !cleanupCalled.Load() {
		t.Error("cleanup function was not called")
	}

	// Resource directory should be removed
	if _, err := xos.Stat(resourceDir); !os.IsNotExist(err) {
		t.Errorf("resource directory should be removed, got err: %v", err)
	}
}

func TestLocker_MultipleHoldersPreventsCleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const key = "multi-holder"

	var cleanupCalls atomic.Int32

	// First acquire
	path1, release1, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
			return "", nil, err
		}

		return dataPath, func() {
			cleanupCalls.Add(1)
		}, nil
	})
	if err != nil {
		t.Fatalf("First acquire failed: %v", err)
	}

	// Second acquire - same key
	path2, release2, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		// This factory should see existing data
		dataPath := filepath.Join(dir, "data")

		return dataPath, func() {
			cleanupCalls.Add(1)
		}, nil
	})
	if err != nil {
		t.Fatalf("Second acquire failed: %v", err)
	}

	// Both should get same path
	if path1 != path2 {
		t.Errorf("paths differ: %q vs %q", path1, path2)
	}

	resourceDir := filepath.Dir(path1)

	// Release first holder - release() is synchronous, no sleep needed
	release1()

	// Directory should still exist (second holder active)
	if _, err := xos.Stat(resourceDir); err != nil {
		t.Errorf("directory should still exist with active holder: %v", err)
	}

	// Cleanup should not have been called yet
	if cleanupCalls.Load() > 0 {
		t.Error("cleanup called while holder still active")
	}

	// Release second holder - release() is synchronous, no sleep needed
	release2()

	// Now directory should be gone
	if _, err := xos.Stat(resourceDir); !os.IsNotExist(err) {
		t.Errorf("directory should be removed after last release: %v", err)
	}

	// Exactly one cleanup should have fired (the last releaser's)
	if cleanupCalls.Load() != 1 {
		t.Errorf("cleanupCalls = %d, want 1", cleanupCalls.Load())
	}
}

func TestLocker_FactoryError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	factoryErr := errors.New("factory failed")

	_, _, err := locker.Acquire("fail-key", func(_ string) (string, func(), error) {
		return "", nil, factoryErr
	})
	if err == nil {
		t.Error("expected error from factory")
	}

	if !errors.Is(err, factoryErr) {
		t.Errorf("error = %v, want %v", err, factoryErr)
	}
}

func TestLocker_DoubleReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const key = "double-release"

	var cleanupCalls atomic.Int32

	path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
			return "", nil, err
		}

		return dataPath, func() {
			cleanupCalls.Add(1)
		}, nil
	})
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	resourceDir := filepath.Dir(path)

	// First release — should clean up
	release()

	if cleanupCalls.Load() != 1 {
		t.Fatalf("cleanupCalls after first release = %d, want 1", cleanupCalls.Load())
	}

	if _, err := xos.Stat(resourceDir); !os.IsNotExist(err) {
		t.Fatalf("directory should be removed after first release, got err: %v", err)
	}

	// Second release — should be a no-op (sync.Once)
	release()

	if cleanupCalls.Load() != 1 {
		t.Errorf("cleanupCalls after double release = %d, want 1", cleanupCalls.Load())
	}
}

func TestLocker_OnlyLastReleaserCleanupRuns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const key = "cleanup-ownership"

	var (
		cleanupA atomic.Bool
		cleanupB atomic.Bool
	)

	// First acquire — creator with cleanup A
	_, release1, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
			return "", nil, err
		}

		return dataPath, func() {
			cleanupA.Store(true)
		}, nil
	})
	if err != nil {
		t.Fatalf("First acquire failed: %v", err)
	}

	// Second acquire — reader with cleanup B
	_, release2, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")

		return dataPath, func() {
			cleanupB.Store(true)
		}, nil
	})
	if err != nil {
		t.Fatalf("Second acquire failed: %v", err)
	}

	// Release first (not last holder) — cleanup A should NOT run
	release1()

	if cleanupA.Load() {
		t.Error("cleanup A ran on non-last release")
	}

	// Release second (last holder) — only cleanup B should run
	release2()

	if cleanupA.Load() {
		t.Error("cleanup A ran — only last releaser's cleanup should execute")
	}

	if !cleanupB.Load() {
		t.Error("cleanup B did not run — last releaser's cleanup should execute")
	}
}

func TestLocker_ConcurrentAcquireAndRelease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const (
		key        = "interleaved"
		iterations = 20
	)

	var wg sync.WaitGroup

	wg.Add(iterations)

	for range iterations {
		go func() {
			defer wg.Done()

			path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
				dataPath := filepath.Join(dir, "data")
				if err := filesystem.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
					return "", nil, err
				}

				return dataPath, nil, nil
			})
			if err != nil {
				t.Errorf("Acquire failed: %v", err)

				return
			}

			// Verify resource is accessible while held
			if _, err := xos.Stat(path); err != nil {
				t.Errorf("path %q not accessible: %v", path, err)
			}

			// Immediate release — creates rapid acquire/release interleaving
			release()
		}()
	}

	wg.Wait()

	// After all releases, the resource directory should eventually be cleaned up.
	// Allow a brief window for the last release to complete cleanup.
	resourceDir := filepath.Join(dir, key)
	if _, err := xos.Stat(resourceDir); err == nil {
		t.Errorf("resource directory %q should be removed after all releases", resourceDir)
	}
}

func TestLocker_StressConcurrentKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := refcount.New(dir)

	const numKeys = 10

	const goroutinesPerKey = 5

	var wg sync.WaitGroup

	wg.Add(numKeys * goroutinesPerKey)

	for keyIdx := range numKeys {
		for range goroutinesPerKey {
			go func(key string) {
				defer wg.Done()

				path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
					dataPath := filepath.Join(dir, "data")
					if err := filesystem.WriteFile(dataPath, []byte(key), 0o600); err != nil {
						return "", nil, err
					}

					return dataPath, nil, nil
				})
				if err != nil {
					t.Errorf("Acquire(%q) failed: %v", key, err)

					return
				}

				// Hold for a bit to create contention
				time.Sleep(10 * time.Millisecond)

				// Verify data
				data, err := xos.ReadFile(path)
				if err != nil {
					t.Errorf("ReadFile(%q) failed: %v", path, err)
				} else if string(data) != key {
					t.Errorf("data = %q, want %q", data, key)
				}

				release()
			}(filepath.Base(t.TempDir()) + "-key-" + string(rune('a'+keyIdx)))
		}
	}

	wg.Wait()
}

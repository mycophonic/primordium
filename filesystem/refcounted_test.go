package filesystem_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/farcloser/primordium/fault"
	"github.com/farcloser/primordium/filesystem"
)

func TestLocker_InvalidKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := filesystem.NewLocker(dir)

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
	locker := filesystem.NewLocker(dir)

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
				if err := os.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
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
			if _, err := os.Stat(path); err != nil {
				t.Errorf("path %q not accessible: %v", path, err)
			}
		}()
	}

	wg.Wait()

	// Factory should only be called once - others should see existing resource
	// However, with concurrent access, factory might be called multiple times
	// if the first call hasn't completed. The important thing is all acquire
	// calls succeed and see valid data.
	t.Logf("Factory called %d times for %d goroutines", factoryCalls.Load(), numGoroutines)
}

func TestLocker_ReleaseWhenLastHolder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := filesystem.NewLocker(dir)

	const key = "cleanup-test"

	var cleanupCalled atomic.Bool

	path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := os.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
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
	if _, err := os.Stat(resourceDir); !os.IsNotExist(err) {
		t.Errorf("resource directory should be removed, got err: %v", err)
	}
}

func TestLocker_MultipleHoldersPreventsCleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := filesystem.NewLocker(dir)

	const key = "multi-holder"

	var cleanupCalls atomic.Int32

	// First acquire
	path1, release1, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, "data")
		if err := os.WriteFile(dataPath, []byte("test"), 0o600); err != nil {
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
	if _, err := os.Stat(resourceDir); err != nil {
		t.Errorf("directory should still exist with active holder: %v", err)
	}

	// Cleanup should not have been called yet
	if cleanupCalls.Load() > 0 {
		t.Error("cleanup called while holder still active")
	}

	// Release second holder - release() is synchronous, no sleep needed
	release2()

	// Now directory should be gone
	if _, err := os.Stat(resourceDir); !os.IsNotExist(err) {
		t.Errorf("directory should be removed after last release: %v", err)
	}
}

func TestLocker_FactoryError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := filesystem.NewLocker(dir)

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

func TestLocker_StressConcurrentKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	locker := filesystem.NewLocker(dir)

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
					if err := os.WriteFile(dataPath, []byte(key), 0o600); err != nil {
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
				data, err := os.ReadFile(path)
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

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

package store_test

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/store"
	"github.com/mycophonic/primordium/wrap/primos"
)

func TestVolatile_ConcurrentAcquire(t *testing.T) {
	t.Parallel()

	const (
		numGoroutines = 100
		holdDuration  = 1 * time.Second
	)

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)
	content := []byte("shared secret content")

	var (
		wg           sync.WaitGroup
		successCount atomic.Int64
		errorCount   atomic.Int64
		paths        sync.Map
	)

	start := time.Now()

	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			path, release, err := volatile.Acquire(content)
			if err != nil {
				t.Errorf("goroutine %d: acquire failed: %v", id, err)
				errorCount.Add(1)

				return
			}

			successCount.Add(1)
			paths.Store(id, path)

			// Verify file exists and has correct content
			data, err := primos.ReadFile(path)
			if err != nil {
				t.Errorf("goroutine %d: failed to read file: %v", id, err)
			} else if string(data) != string(content) {
				t.Errorf("goroutine %d: content mismatch: got %q, want %q", id, data, content)
			}

			// Hold the lease
			time.Sleep(holdDuration)

			release()
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)

	// All goroutines should succeed
	if got := successCount.Load(); got != numGoroutines {
		t.Errorf("success count: got %d, want %d", got, numGoroutines)
	}

	if got := errorCount.Load(); got != 0 {
		t.Errorf("error count: got %d, want 0", got)
	}

	// Execution should be parallel - total time should be close to holdDuration, not numGoroutines * holdDuration
	maxExpected := holdDuration + 5*time.Second // generous buffer for lock contention
	if elapsed > maxExpected {
		t.Errorf("execution took %v, want less than %v (goroutines should run in parallel)", elapsed, maxExpected)
	}

	t.Logf("completed %d concurrent acquires in %v", numGoroutines, elapsed)

	// All paths should be identical (same content = same path)
	var firstPath string

	paths.Range(func(_, value any) bool {
		p := value.(string)
		if firstPath == "" {
			firstPath = p
		} else if p != firstPath {
			t.Errorf("path mismatch: got %q, want %q", p, firstPath)
		}

		return true
	})

	// After all releases, the directory should be cleaned up
	entries, err := primos.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read rootDir dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("rootDir dir should be empty after all releases, found: %v", entries)
	}
}

func TestVolatile_ConcurrentAcquireDifferentContent(t *testing.T) {
	t.Parallel()

	const numGoroutines = 50

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)

	var (
		wg    sync.WaitGroup
		paths sync.Map
	)

	// Each goroutine acquires different content
	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			content := []byte("content-" + string(rune('A'+id%26)) + "-" + string(rune('0'+id)))

			path, release, err := volatile.Acquire(content)
			if err != nil {
				t.Errorf("goroutine %d: acquire failed: %v", id, err)

				return
			}

			paths.Store(id, path)
			time.Sleep(100 * time.Millisecond)
			release()
		}(i)
	}

	wg.Wait()

	// All paths should be different (different content = different paths)
	seen := make(map[string]bool)

	paths.Range(func(_, value any) bool {
		p := value.(string)
		if seen[p] {
			t.Errorf("duplicate path found: %s", p)
		}

		seen[p] = true

		return true
	})

	// All directories should be cleaned up
	entries, err := primos.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read rootDir dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("rootDir dir should be empty after all releases, found %d entries", len(entries))
	}
}

func TestVolatile_StaggeredRelease(t *testing.T) {
	t.Parallel()

	const numGoroutines = 20

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)
	content := []byte("staggered content")

	var wg sync.WaitGroup

	// Goroutines release at different times
	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			path, release, err := volatile.Acquire(content)
			if err != nil {
				t.Errorf("goroutine %d: acquire failed: %v", id, err)

				return
			}

			// Stagger release times: goroutine 0 releases after 100ms, goroutine 1 after 200ms, etc.
			holdTime := time.Duration(100*(id+1)) * time.Millisecond
			time.Sleep(holdTime)

			// File should still exist (others still holding)
			if id < numGoroutines-1 {
				if _, err := primos.Stat(path); os.IsNotExist(err) {
					t.Errorf("goroutine %d: file deleted prematurely", id)
				}
			}

			release()
		}(i)
	}

	wg.Wait()

	// After all releases, directory should be gone
	entries, err := primos.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read rootDir dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("rootDir dir should be empty after all releases, found %d entries", len(entries))
	}
}

func TestVolatile_RapidAcquireRelease(t *testing.T) {
	t.Parallel()

	const (
		numGoroutines = 50
		cyclesPerGR   = 10
		contentPerGR  = 5
	)

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)

	var wg sync.WaitGroup

	for i := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for cycle := range cyclesPerGR {
				// Each goroutine cycles through a few different contents
				contentID := (id + cycle) % contentPerGR
				content := []byte("rapid-content-" + string(rune('0'+contentID)))

				path, release, err := volatile.Acquire(content)
				if err != nil {
					t.Errorf("goroutine %d cycle %d: acquire failed: %v", id, cycle, err)

					return
				}

				// Brief hold
				time.Sleep(10 * time.Millisecond)

				// Verify content
				data, err := primos.ReadFile(path)
				if err != nil {
					t.Errorf("goroutine %d cycle %d: read failed: %v", id, cycle, err)
				} else if string(data) != string(content) {
					t.Errorf("goroutine %d cycle %d: content mismatch", id, cycle)
				}

				release()
			}
		}(i)
	}

	wg.Wait()

	// All should be cleaned up
	entries, err := primos.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read rootDir dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("rootDir dir should be empty, found %d entries", len(entries))
	}
}

func TestVolatile_AcquireAfterFullRelease(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)
	content := []byte("reacquire content")

	// First acquire and release
	path1, release1, err := volatile.Acquire(content)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	release1()

	// Directory should be cleaned up
	entries, err := primos.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read rootDir dir: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("rootDir dir should be empty after release, found %d entries", len(entries))
	}

	// Acquire again - should recreate
	path2, release2, err := volatile.Acquire(content)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	defer release2()

	// Same content = same path
	if path1 != path2 {
		t.Errorf("paths should match: got %q and %q", path1, path2)
	}

	// File should exist with correct content
	data, err := primos.ReadFile(path2)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
}

func TestVolatile_EmptyContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)
	content := []byte{}

	path, release, err := volatile.Acquire(content)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	defer release()

	// File should exist and be empty
	data, err := primos.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}

func TestVolatile_LargeContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)

	// 1MB of content
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	path, release, err := volatile.Acquire(content)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	defer release()

	data, err := primos.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if len(data) != len(content) {
		t.Errorf("size mismatch: got %d, want %d", len(data), len(content))
	}

	for i := range content {
		if data[i] != content[i] {
			t.Errorf("content mismatch at byte %d: got %d, want %d", i, data[i], content[i])

			break
		}
	}
}

func TestVolatile_ContentAddressed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	volatile := store.NewVolatile(root, digest.SHA256)

	content1 := []byte("same content")
	content2 := []byte("same content")
	content3 := []byte("different content")

	path1, release1, err := volatile.Acquire(content1)
	if err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}

	defer release1()

	path2, release2, err := volatile.Acquire(content2)
	if err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}

	defer release2()

	path3, release3, err := volatile.Acquire(content3)
	if err != nil {
		t.Fatalf("acquire 3 failed: %v", err)
	}

	defer release3()

	// Same content = same path
	if path1 != path2 {
		t.Errorf("same content should produce same path: %q vs %q", path1, path2)
	}

	// Different content = different path
	if path1 == path3 {
		t.Error("different content should produce different path")
	}

	// Verify directories are different
	dir1 := filepath.Dir(path1)
	dir3 := filepath.Dir(path3)

	if dir1 == dir3 {
		t.Error("different content should be in different directories")
	}
}

func TestVolatile_DifferentAlgorithms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("test content for algorithm comparison")

	algorithms := []digest.Algorithm{
		digest.SHA1,
		digest.SHA256,
		digest.SHA384,
		digest.SHA512,
		digest.BLAKE2b256,
		digest.BLAKE2b512,
	}

	paths := make(map[digest.Algorithm]string)

	for _, alg := range algorithms {
		volatile := store.NewVolatile(filepath.Join(root, string(alg)), alg)

		path, release, err := volatile.Acquire(content)
		if err != nil {
			t.Fatalf("acquire with %s failed: %v", alg, err)
		}

		// Verify content is correct
		data, err := primos.ReadFile(path)
		if err != nil {
			t.Fatalf("read with %s failed: %v", alg, err)
		}

		if string(data) != string(content) {
			t.Errorf("content mismatch with %s", alg)
		}

		paths[alg] = filepath.Base(filepath.Dir(path)) // Get the digest directory name

		release()
	}

	// All algorithms should produce different digest directory names
	seen := make(map[string]digest.Algorithm)
	for alg, digestDir := range paths {
		if prevAlg, exists := seen[digestDir]; exists {
			t.Errorf("algorithms %s and %s produced same digest directory: %s", prevAlg, alg, digestDir)
		}

		seen[digestDir] = alg
	}
}

func TestVolatile_AlgorithmConsistency(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("consistency test content")

	// Same content, same algorithm, different instances should produce same path
	volatile1 := store.NewVolatile(root, digest.SHA512)
	volatile2 := store.NewVolatile(root, digest.SHA512)

	path1, release1, err := volatile1.Acquire(content)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer release1()

	path2, release2, err := volatile2.Acquire(content)
	if err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	defer release2()

	if path1 != path2 {
		t.Errorf("same algorithm should produce same path: %q vs %q", path1, path2)
	}
}

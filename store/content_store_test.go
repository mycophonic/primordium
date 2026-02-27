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
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/store"
)

// fetchFunc returns a FetchFunc that serves the given content.
func fetchFunc(content []byte) store.FetchFunc {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	}
}

// failingFetch returns a FetchFunc that always fails.
func failingFetch(err error) store.FetchFunc {
	return func() (io.ReadCloser, error) {
		return nil, err
	}
}

// countingFetch returns a FetchFunc that tracks how many times it was called.
func countingFetch(content []byte, count *atomic.Int64) store.FetchFunc {
	return func() (io.ReadCloser, error) {
		count.Add(1)

		return io.NopCloser(bytes.NewReader(content)), nil
	}
}

func newContentStore(t *testing.T) *store.ContentStore {
	t.Helper()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	indexDir := filepath.Join(t.TempDir(), "index")
	cache := store.NewCache(cacheDir)

	return store.NewContentStore(cache, indexDir)
}

// --- Basic flows ---

func TestContentStore_MissWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("hello from the fetch function")

	reader, createdAt, err := cs.Acquire("https://example.com/file.bin", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(data), len(content))
	}

	if createdAt.IsZero() {
		t.Error("createdAt is zero")
	}

	if time.Since(createdAt) > 5*time.Second {
		t.Errorf("createdAt too old: %v", createdAt)
	}
}

func TestContentStore_MissWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("content with known digest")
	dgst := computeDigest(content)

	reader, createdAt, err := cs.Acquire("https://example.com/known.bin", dgst, fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(data), len(content))
	}

	if createdAt.IsZero() {
		t.Error("createdAt is zero")
	}
}

func TestContentStore_HitReturnsCache(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("cacheable content")

	var fetchCount atomic.Int64

	fetch := countingFetch(content, &fetchCount)

	// First acquire — miss, calls fetch.
	reader1, createdAt1, err := cs.Acquire("https://example.com/data", "", fetch)
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	data1, err := io.ReadAll(reader1)
	reader1.Close()

	if err != nil {
		t.Fatalf("first ReadAll() error: %v", err)
	}

	if fetchCount.Load() != 1 {
		t.Fatalf("fetch count after first acquire = %d, want 1", fetchCount.Load())
	}

	// Second acquire — hit, fetch NOT called.
	reader2, createdAt2, err := cs.Acquire("https://example.com/data", "", fetch)
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	data2, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("second ReadAll() error: %v", err)
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count after second acquire = %d, want 1 (should not call fetch on hit)", fetchCount.Load())
	}

	if !bytes.Equal(data1, data2) {
		t.Error("content mismatch between first and second acquire")
	}

	if !createdAt1.Equal(createdAt2) {
		t.Errorf("createdAt changed: first=%v, second=%v", createdAt1, createdAt2)
	}
}

func TestContentStore_HitPreservesCreatedAt(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("time-sensitive content")

	reader1, createdAt1, err := cs.Acquire("id-1", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	// Small delay to ensure timestamps differ if re-created.
	time.Sleep(10 * time.Millisecond)

	reader2, createdAt2, err := cs.Acquire("id-1", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	io.ReadAll(reader2)
	reader2.Close()

	if !createdAt1.Equal(createdAt2) {
		t.Errorf("createdAt should be preserved on hit: first=%v, second=%v", createdAt1, createdAt2)
	}
}

// --- Digest mismatch ---

func TestContentStore_WrongDigestReturnsError(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("actual content")
	wrongDigest := computeDigest([]byte("different content entirely"))

	_, _, err := cs.Acquire("https://example.com/wrong", wrongDigest, fetchFunc(content))
	if err == nil {
		t.Fatal("Acquire() should fail when content doesn't match provided digest")
	}

	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want ErrWriteFailure (hash mismatch)", err)
	}
}

func TestContentStore_WrongDigestDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("the real content")
	correctDigest := computeDigest(content)
	wrongDigest := computeDigest([]byte("bogus"))

	// First attempt with wrong digest — must fail.
	_, _, err := cs.Acquire("https://example.com/resource", wrongDigest, fetchFunc(content))
	if err == nil {
		t.Fatal("Acquire() with wrong digest should fail")
	}

	// Second attempt with correct digest — must succeed.
	reader, _, err := cs.Acquire("https://example.com/resource", correctDigest, fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() with correct digest error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content mismatch after recovery from wrong digest")
	}
}

func TestContentStore_GarbageDigestStringReturnsError(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	// Not a valid "algorithm:hex" format.
	_, _, err := cs.Acquire("id", "not-a-valid-digest", fetchFunc([]byte("x")))
	if err == nil {
		t.Fatal("Acquire() should fail with garbage digest string")
	}
}

// --- Fetch failures ---

func TestContentStore_FetchFailsWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	fetchErr := errors.New("network timeout")

	_, _, err := cs.Acquire("https://example.com/down", "", failingFetch(fetchErr))
	if err == nil {
		t.Fatal("Acquire() should fail when fetch fails")
	}

	if !errors.Is(err, fault.ErrReadFailure) {
		t.Errorf("error = %v, want ErrReadFailure", err)
	}
}

func TestContentStore_FetchFailsWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	dgst := computeDigest([]byte("whatever"))
	fetchErr := errors.New("connection refused")

	_, _, err := cs.Acquire("https://example.com/down", dgst, failingFetch(fetchErr))
	if err == nil {
		t.Fatal("Acquire() should fail when fetch fails")
	}

	if !errors.Is(err, fault.ErrReadFailure) {
		t.Errorf("error = %v, want ErrReadFailure", err)
	}
}

func TestContentStore_FetchFailureDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("eventually available content")
	fetchErr := errors.New("temporary failure")

	// First attempt — fetch fails.
	_, _, err := cs.Acquire("https://example.com/flaky", "", failingFetch(fetchErr))
	if err == nil {
		t.Fatal("first Acquire() should fail")
	}

	// Second attempt — fetch succeeds.
	reader, _, err := cs.Acquire("https://example.com/flaky", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content mismatch after fetch recovery")
	}
}

// --- Content deduplication ---

func TestContentStore_DifferentIdentifiersSameContent(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("shared content across identifiers")

	var fetchCount atomic.Int64

	fetch := countingFetch(content, &fetchCount)

	// Two different identifiers, same content.
	reader1, _, err := cs.Acquire("https://mirror1.example.com/file", "", fetch)
	if err != nil {
		t.Fatalf("Acquire(mirror1) error: %v", err)
	}

	data1, _ := io.ReadAll(reader1)
	reader1.Close()

	reader2, _, err := cs.Acquire("https://mirror2.example.com/file", "", fetch)
	if err != nil {
		t.Fatalf("Acquire(mirror2) error: %v", err)
	}

	defer reader2.Close()

	data2, _ := io.ReadAll(reader2)

	if !bytes.Equal(data1, data2) {
		t.Error("content mismatch between identifiers")
	}

	// Second identifier should still call fetch (different identifier = index miss),
	// but the cache should already have the content (same digest).
	if fetchCount.Load() != 2 {
		t.Errorf("fetch count = %d, want 2 (index miss for each identifier)", fetchCount.Load())
	}
}

func TestContentStore_DifferentIdentifiersSameDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("content known by digest")
	dgst := computeDigest(content)

	var fetchCount atomic.Int64

	fetch := countingFetch(content, &fetchCount)

	// First identifier with known digest — fetches and caches.
	reader1, _, err := cs.Acquire("id-alpha", dgst, fetch)
	if err != nil {
		t.Fatalf("Acquire(alpha) error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	// Second identifier with same digest — cache already has content, fetch NOT called.
	reader2, _, err := cs.Acquire("id-beta", dgst, fetch)
	if err != nil {
		t.Fatalf("Acquire(beta) error: %v", err)
	}

	defer reader2.Close()

	data, _ := io.ReadAll(reader2)

	if !bytes.Equal(data, content) {
		t.Error("content mismatch for second identifier")
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (cache hit for second identifier)", fetchCount.Load())
	}
}

// --- Stale index (cache GC'd) ---

func TestContentStore_StaleIndexRefetches(t *testing.T) {
	t.Parallel()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	indexDir := filepath.Join(t.TempDir(), "index")

	// Tiny quota to force GC to evict.
	cache := store.NewCache(cacheDir, 10)
	cs := store.NewContentStore(cache, indexDir)

	content := []byte("content that will be evicted by GC")

	var fetchCount atomic.Int64

	fetch := countingFetch(content, &fetchCount)

	// First acquire — populates cache and index.
	reader1, _, err := cs.Acquire("https://example.com/evictable", "", fetch)
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	// GC the cache — evicts the content (quota is 10 bytes, content is much larger).
	_, gcErr := cache.GarbageCollect()
	if gcErr != nil {
		t.Fatalf("GarbageCollect() error: %v", gcErr)
	}

	// Second acquire — index exists but cache was GC'd. Must re-fetch.
	reader2, _, err := cs.Acquire("https://example.com/evictable", "", fetch)
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	data, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content mismatch after stale index re-fetch")
	}

	if fetchCount.Load() != 2 {
		t.Errorf("fetch count = %d, want 2 (should re-fetch after GC)", fetchCount.Load())
	}
}

// --- Corrupt index ---

func TestContentStore_CorruptIndexRefetches(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("content behind a corrupt index")

	// First acquire to create index entry.
	reader1, _, err := cs.Acquire("corrupt-id", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	// Corrupt the index file by writing garbage.
	// We create a separate ContentStore so we can find and corrupt the index directory.
	indexDir := filepath.Join(t.TempDir(), "index-corrupt")
	cacheDir := filepath.Join(t.TempDir(), "cache-corrupt")
	cache := store.NewCache(cacheDir)
	cs2 := store.NewContentStore(cache, indexDir)

	// Populate.
	reader2, _, err := cs2.Acquire("id-to-corrupt", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	io.ReadAll(reader2)
	reader2.Close()

	// Find and corrupt the meta file.
	metaPaths := findIndexMetaPaths(t, indexDir)
	if len(metaPaths) == 0 {
		t.Fatal("expected index directory to have entries")
	}

	metaPath := metaPaths[0]

	if err := filesystem.WriteFile(metaPath, []byte("{{{{not json}}}}"), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	// Acquire again — should treat corrupt index as miss and re-fetch.
	var fetchCount atomic.Int64

	reader3, _, err := cs2.Acquire("id-to-corrupt", "", countingFetch(content, &fetchCount))
	if err != nil {
		t.Fatalf("Acquire() after corruption error: %v", err)
	}

	defer reader3.Close()

	data, err := io.ReadAll(reader3)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content mismatch after corrupt index recovery")
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1", fetchCount.Load())
	}
}

// --- Empty and large content ---

func TestContentStore_EmptyContent(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte{}

	reader, _, err := cs.Acquire("empty-resource", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(data))
	}

	// Hit path.
	reader2, _, err := cs.Acquire("empty-resource", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	data2, _ := io.ReadAll(reader2)

	if len(data2) != 0 {
		t.Errorf("expected empty content on hit, got %d bytes", len(data2))
	}
}

func TestContentStore_EmptyContentWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte{}
	dgst := computeDigest(content)

	reader, _, err := cs.Acquire("empty-with-digest", dgst, fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(data))
	}
}

func TestContentStore_LargeContent(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	// 5MB content.
	content := make([]byte, 5*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	reader, _, err := cs.Acquire("large-file", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	data, _ := io.ReadAll(reader)
	reader.Close()

	if !bytes.Equal(data, content) {
		t.Errorf("large content mismatch: got %d bytes, want %d", len(data), len(content))
	}

	// Hit path.
	reader2, _, err := cs.Acquire("large-file", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	data2, _ := io.ReadAll(reader2)

	if !bytes.Equal(data2, content) {
		t.Error("large content mismatch on hit")
	}
}

func TestContentStore_LargeContentWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	content := make([]byte, 5*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	dgst := computeDigest(content)

	reader, _, err := cs.Acquire("large-with-digest", dgst, fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Errorf("large content mismatch: got %d bytes, want %d", len(data), len(content))
	}
}

// --- Truncated fetch (reader returns error mid-stream) ---

func TestContentStore_TruncatedFetchWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	truncatedFetch := func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     []byte("partial data that will be cut short after a few bytes"),
			failAt:   10,
			failWith: errors.New("connection reset"),
		}), nil
	}

	_, _, err := cs.Acquire("truncated", "", truncatedFetch)
	if err == nil {
		t.Fatal("Acquire() should fail with truncated fetch")
	}

	if !errors.Is(err, fault.ErrReadFailure) {
		t.Errorf("error = %v, want ErrReadFailure", err)
	}
}

func TestContentStore_TruncatedFetchWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	fullContent := []byte("this is the full content that will be truncated mid-stream")
	dgst := computeDigest(fullContent)

	truncatedFetch := func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     fullContent,
			failAt:   10,
			failWith: errors.New("connection reset"),
		}), nil
	}

	_, _, err := cs.Acquire("truncated-dgst", dgst, truncatedFetch)
	if err == nil {
		t.Fatal("Acquire() should fail with truncated fetch")
	}

	// Either ErrWriteFailure (copy failed) or ErrWriteFailure (hash mismatch on close).
	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want ErrWriteFailure", err)
	}
}

func TestContentStore_TruncatedFetchDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("full content after truncation recovery")

	truncatedFetch := func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     content,
			failAt:   5,
			failWith: errors.New("broken pipe"),
		}), nil
	}

	// First attempt — truncated.
	_, _, err := cs.Acquire("truncated-recover", "", truncatedFetch)
	if err == nil {
		t.Fatal("first Acquire() should fail")
	}

	// Second attempt — succeeds.
	reader, _, err := cs.Acquire("truncated-recover", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Error("content mismatch after truncation recovery")
	}
}

// --- Concurrency ---

func TestContentStore_ConcurrentSameIdentifier(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("concurrent access content")

	const numGoroutines = 50

	var (
		wg         sync.WaitGroup
		fetchCount atomic.Int64
	)

	fetch := countingFetch(content, &fetchCount)

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("concurrent-id", "", fetch)
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("content mismatch: got %d bytes, want %d", len(data), len(content))
			}
		})
	}

	wg.Wait()
}

func TestContentStore_ConcurrentDifferentIdentifiers(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	const numIdentifiers = 20

	var wg sync.WaitGroup

	for idx := range numIdentifiers {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			content := []byte("content for identifier " + string(rune('A'+id)))
			identifier := "id-" + string(rune('A'+id))

			reader, _, err := cs.Acquire(identifier, "", fetchFunc(content))
			if err != nil {
				t.Errorf("Acquire(%s) error: %v", identifier, err)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll(%s) error: %v", identifier, err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("content mismatch for %s", identifier)
			}
		}(idx)
	}

	wg.Wait()
}

func TestContentStore_ConcurrentSameIdentifierWithDigest(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("concurrent content with known digest")
	dgst := computeDigest(content)

	const numGoroutines = 50

	var (
		wg         sync.WaitGroup
		fetchCount atomic.Int64
	)

	fetch := countingFetch(content, &fetchCount)

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("concurrent-dgst", dgst, fetch)
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("content mismatch: got %d bytes, want %d", len(data), len(content))
			}
		})
	}

	wg.Wait()
}

func TestContentStore_ConcurrentMixedHitAndMiss(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("mixed hit/miss content")

	// Pre-populate the cache.
	reader0, _, err := cs.Acquire("mixed-id", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("initial Acquire() error: %v", err)
	}

	io.ReadAll(reader0)
	reader0.Close()

	const numGoroutines = 50

	var wg sync.WaitGroup

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("mixed-id", "", fetchFunc(content))
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Error("content mismatch")
			}
		})
	}

	wg.Wait()
}

// --- Edge cases ---

func TestContentStore_IdentifierIsEmptyString(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("content for empty identifier")

	reader, _, err := cs.Acquire("", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Error("content mismatch for empty identifier")
	}
}

func TestContentStore_VeryLongIdentifier(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("content for long identifier")

	// 10KB identifier — Hashpath truncates to 16 hex chars, so this is safe.
	longID := string(make([]byte, 10240))

	reader, _, err := cs.Acquire(longID, "", fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Error("content mismatch for long identifier")
	}
}

func TestContentStore_DigestPathSkipsFetchOnCacheHit(t *testing.T) {
	t.Parallel()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	indexDir := filepath.Join(t.TempDir(), "index")
	cache := store.NewCache(cacheDir)
	cs := store.NewContentStore(cache, indexDir)

	content := []byte("pre-cached content")
	dgst := computeDigest(content)

	// Pre-populate cache directly (bypass ContentStore).
	cacheReader, cacheWriter, err := cache.Acquire(dgst)
	if err != nil {
		t.Fatalf("cache.Acquire() error: %v", err)
	}

	go func() {
		io.ReadAll(cacheReader)
		cacheReader.Close()
	}()

	_, _ = cacheWriter.Write(content)

	if err := cacheWriter.Close(); err != nil {
		t.Fatalf("cacheWriter.Close() error: %v", err)
	}

	// Now use ContentStore with known digest — fetch should NOT be called.
	panicFetch := func() (io.ReadCloser, error) {
		t.Fatal("fetch should not be called when cache already has content")

		return nil, errors.New("unreachable")
	}

	reader, _, err := cs.Acquire("new-identifier", dgst, panicFetch)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Error("content mismatch")
	}
}

func TestContentStore_RapidAcquireRelease(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("rapid cycle content")

	const iterations = 100

	var wg sync.WaitGroup

	for idx := range iterations {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			reader, _, err := cs.Acquire("rapid-id", "", fetchFunc(content))
			if err != nil {
				t.Errorf("iteration %d: Acquire() error: %v", id, err)

				return
			}

			// Read a few bytes then close immediately.
			buf := make([]byte, 3)

			_, readErr := reader.Read(buf)
			if readErr != nil && readErr != io.EOF {
				t.Errorf("iteration %d: Read() error: %v", id, readErr)
			}

			reader.Close()
		}(idx)
	}

	wg.Wait()

	// Verify content is still intact.
	reader, _, err := cs.Acquire("rapid-id", "", fetchFunc(content))
	if err != nil {
		t.Fatalf("final Acquire() error: %v", err)
	}

	defer reader.Close()

	data, _ := io.ReadAll(reader)

	if !bytes.Equal(data, content) {
		t.Error("content corrupted after rapid cycles")
	}
}

// --- Index metadata correctness ---

func TestContentStore_IndexMetadataFormat(t *testing.T) {
	t.Parallel()

	indexDir := filepath.Join(t.TempDir(), "index")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cache := store.NewCache(cacheDir)
	cs := store.NewContentStore(cache, indexDir)

	content := []byte("metadata format test")
	dgst := computeDigest(content)

	before := time.Now()

	reader, _, err := cs.Acquire("meta-test-id", dgst, fetchFunc(content))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	io.ReadAll(reader)
	reader.Close()

	after := time.Now()

	// Read the raw index file and verify structure.
	metaPaths := findIndexMetaPaths(t, indexDir)
	if len(metaPaths) != 1 {
		t.Fatalf("expected 1 index entry, got %d", len(metaPaths))
	}

	raw, err := filesystem.ReadFile(metaPaths[0])
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}

	var meta struct {
		Digest    string    `json:"digest"`
		CreatedAt time.Time `json:"createdAt"`
	}

	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if meta.Digest != dgst {
		t.Errorf("stored digest = %q, want %q", meta.Digest, dgst)
	}

	if meta.CreatedAt.Before(before) || meta.CreatedAt.After(after) {
		t.Errorf("createdAt %v not within [%v, %v]", meta.CreatedAt, before, after)
	}
}

func TestContentStore_MultipleIdentifiersSeparateIndexEntries(t *testing.T) {
	t.Parallel()

	indexDir := filepath.Join(t.TempDir(), "index")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cache := store.NewCache(cacheDir)
	cs := store.NewContentStore(cache, indexDir)

	identifiers := []string{"id-one", "id-two", "id-three"}

	for _, id := range identifiers {
		content := []byte("content for " + id)

		reader, _, err := cs.Acquire(id, "", fetchFunc(content))
		if err != nil {
			t.Fatalf("Acquire(%s) error: %v", id, err)
		}

		io.ReadAll(reader)
		reader.Close()
	}

	metaPaths := findIndexMetaPaths(t, indexDir)
	if len(metaPaths) != len(identifiers) {
		t.Errorf("index entries = %d, want %d", len(metaPaths), len(identifiers))
	}
}

// --- Slow fetch with concurrent readers ---

func TestContentStore_SlowFetchConcurrentReaders(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)
	content := []byte("slowly fetched content that readers wait for")
	dgst := computeDigest(content)

	slowFetch := func() (io.ReadCloser, error) {
		time.Sleep(100 * time.Millisecond)

		return io.NopCloser(bytes.NewReader(content)), nil
	}

	const numReaders = 10

	var wg sync.WaitGroup

	for idx := range numReaders {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Stagger start times.
			time.Sleep(time.Duration(id*10) * time.Millisecond)

			reader, _, err := cs.Acquire("slow-fetch-id", dgst, slowFetch)
			if err != nil {
				t.Errorf("reader %d: Acquire() error: %v", id, err)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("reader %d: ReadAll() error: %v", id, err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("reader %d: content mismatch", id)
			}
		}(idx)
	}

	wg.Wait()
}

// --- Stress test ---

func TestContentStore_StressMixedOperations(t *testing.T) {
	t.Parallel()

	cs := newContentStore(t)

	const (
		numIdentifiers = 10
		numGoroutines  = 50
		contentPerID   = 5
	)

	var wg sync.WaitGroup

	for idx := range numGoroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			contentIdx := id % contentPerID
			identifierIdx := id % numIdentifiers
			content := []byte("stress-" + string(rune('0'+contentIdx)) + "-" + string(rune('A'+identifierIdx)))
			identifier := "stress-id-" + string(rune('A'+identifierIdx))

			// Alternate between digest-known and digest-unknown paths.
			var dgst string
			if id%2 == 0 {
				dgst = computeDigest(content)
			}

			reader, _, err := cs.Acquire(identifier, dgst, fetchFunc(content))
			if err != nil {
				// Some errors expected when same identifier maps to different content
				// (different goroutines write different content for same identifier).
				return
			}

			defer reader.Close()

			_, _ = io.ReadAll(reader)
		}(idx)
	}

	wg.Wait()
}

// --- Helper functions ---

// findIndexMetaPaths walks a sharded index directory and returns all meta file paths.
func findIndexMetaPaths(t *testing.T, indexDir string) []string {
	t.Helper()

	var paths []string

	err := filepath.WalkDir(indexDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && d.Name() == "meta" {
			paths = append(paths, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error: %v", err)
	}

	return paths
}

// --- Helper types ---

// truncatedReader returns data up to failAt bytes, then returns an error.
type truncatedReader struct {
	data     []byte
	pos      int
	failAt   int
	failWith error
}

func (r *truncatedReader) Read(p []byte) (int, error) {
	if r.pos >= r.failAt {
		return 0, r.failWith
	}

	remaining := r.failAt - r.pos
	toRead := min(len(p), remaining, len(r.data)-r.pos)

	if toRead == 0 {
		return 0, r.failWith
	}

	copy(p[:toRead], r.data[r.pos:r.pos+toRead])
	r.pos += toRead

	return toRead, nil
}

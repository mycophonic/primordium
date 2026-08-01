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

package content_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/content"
)

// computeDigest returns a digest for the given content (SHA256).
func computeDigest(data []byte) digest.Digest {
	hash := sha256.Sum256(data)
	dgst, _ := digest.New(digest.SHA256, hash[:])

	return dgst
}

// fetchFunc returns a FetchFunc that serves the given content.
func fetchFunc(data []byte) content.FetchFunc {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
}

// failingFetch returns a FetchFunc that always fails.
func failingFetch(err error) content.FetchFunc {
	return func() (io.ReadCloser, error) {
		return nil, err
	}
}

// countingFetch returns a FetchFunc that tracks how many times it was called.
func countingFetch(data []byte, count *atomic.Int64) content.FetchFunc {
	return func() (io.ReadCloser, error) {
		count.Add(1)

		return io.NopCloser(bytes.NewReader(data)), nil
	}
}

func newStore(t *testing.T) *content.Store {
	t.Helper()

	cs, err := content.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("content.New() error: %v", err)
	}

	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

// --- Basic flows ---

func TestStore_MissWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("hello from the fetch function")

	reader, createdAt, err := cs.Acquire("https://example.com/file.bin", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(data))
	}

	if createdAt.IsZero() {
		t.Error("createdAt is zero")
	}

	if time.Since(createdAt) > 5*time.Second {
		t.Errorf("createdAt too old: %v", createdAt)
	}
}

func TestStore_MissWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content with known digest")
	dgst := computeDigest(data)

	reader, createdAt, err := cs.Acquire("https://example.com/known.bin", dgst, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(data))
	}

	if createdAt.IsZero() {
		t.Error("createdAt is zero")
	}
}

func TestStore_HitReturnsCache(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("cacheable content")

	var fetchCount atomic.Int64

	fetch := countingFetch(data, &fetchCount)

	// First acquire — miss, calls fetch.
	reader1, createdAt1, err := cs.Acquire("https://example.com/data", nil, fetch)
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
	reader2, createdAt2, err := cs.Acquire("https://example.com/data", nil, fetch)
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

func TestStore_HitPreservesCreatedAt(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("time-sensitive content")

	reader1, createdAt1, err := cs.Acquire("id-1", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	// Small delay to ensure timestamps differ if re-created.
	time.Sleep(10 * time.Millisecond)

	reader2, createdAt2, err := cs.Acquire("id-1", nil, fetchFunc(data))
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

func TestStore_WrongDigestReturnsError(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("actual content")
	wrongDigest := computeDigest([]byte("different content entirely"))

	reader, _, err := cs.Acquire("https://example.com/wrong", wrongDigest, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	_, err = io.ReadAll(reader)
	_ = reader.Close()

	if err == nil {
		t.Fatal("ReadAll() should fail when content doesn't match provided digest")
	}

	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want ErrWriteFailure (hash mismatch)", err)
	}
}

func TestStore_WrongDigestDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("the real content")
	correctDigest := computeDigest(data)
	wrongDigest := computeDigest([]byte("bogus"))

	// First attempt with wrong digest — must fail on read.
	reader, _, err := cs.Acquire("https://example.com/resource", wrongDigest, fetchFunc(data))
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	_, err = io.ReadAll(reader)
	_ = reader.Close()

	if err == nil {
		t.Fatal("ReadAll() with wrong digest should fail")
	}

	// Second attempt with correct digest — must succeed.
	reader, _, err = cs.Acquire("https://example.com/resource", correctDigest, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() with correct digest error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after recovery from wrong digest")
	}
}

// --- Fetch failures ---

func TestStore_FetchFailsWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	fetchErr := errors.New("network timeout")

	_, _, err := cs.Acquire("https://example.com/down", nil, failingFetch(fetchErr))
	if err == nil {
		t.Fatal("Acquire() should fail when fetch fails")
	}

	if !errors.Is(err, fault.ErrReadFailure) {
		t.Errorf("error = %v, want ErrReadFailure", err)
	}
}

func TestStore_FetchFailsWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
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

func TestStore_FetchFailureDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("eventually available content")
	fetchErr := errors.New("temporary failure")

	// First attempt — fetch fails.
	_, _, err := cs.Acquire("https://example.com/flaky", nil, failingFetch(fetchErr))
	if err == nil {
		t.Fatal("first Acquire() should fail")
	}

	// Second attempt — fetch succeeds.
	reader, _, err := cs.Acquire("https://example.com/flaky", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after fetch recovery")
	}
}

// --- Content deduplication ---

func TestStore_DifferentIdentifiersSameContent(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("shared content across identifiers")

	var fetchCount atomic.Int64

	fetch := countingFetch(data, &fetchCount)

	// Two different identifiers, same content.
	reader1, _, err := cs.Acquire("https://mirror1.example.com/file", nil, fetch)
	if err != nil {
		t.Fatalf("Acquire(mirror1) error: %v", err)
	}

	data1, _ := io.ReadAll(reader1)
	reader1.Close()

	reader2, _, err := cs.Acquire("https://mirror2.example.com/file", nil, fetch)
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

func TestStore_DifferentIdentifiersSameDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content known by digest")
	dgst := computeDigest(data)

	var fetchCount atomic.Int64

	fetch := countingFetch(data, &fetchCount)

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

	got, _ := io.ReadAll(reader2)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch for second identifier")
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (cache hit for second identifier)", fetchCount.Load())
	}
}

// --- Empty and large content ---

func TestStore_EmptyContent(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte{}

	reader, _, err := cs.Acquire("empty-resource", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(got))
	}

	// Hit path.
	reader2, _, err := cs.Acquire("empty-resource", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	got2, _ := io.ReadAll(reader2)

	if len(got2) != 0 {
		t.Errorf("expected empty content on hit, got %d bytes", len(got2))
	}
}

func TestStore_EmptyContentWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte{}
	dgst := computeDigest(data)

	reader, _, err := cs.Acquire("empty-with-digest", dgst, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(got))
	}
}

func TestStore_LargeContent(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

	// 5MB content.
	data := make([]byte, 5*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	reader, _, err := cs.Acquire("large-file", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	got, _ := io.ReadAll(reader)
	reader.Close()

	if !bytes.Equal(got, data) {
		t.Errorf("large content mismatch: got %d bytes, want %d", len(got), len(data))
	}

	// Hit path.
	reader2, _, err := cs.Acquire("large-file", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	got2, _ := io.ReadAll(reader2)

	if !bytes.Equal(got2, data) {
		t.Error("large content mismatch on hit")
	}
}

func TestStore_LargeContentWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

	data := make([]byte, 5*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	dgst := computeDigest(data)

	reader, _, err := cs.Acquire("large-with-digest", dgst, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, _ := io.ReadAll(reader)

	if !bytes.Equal(got, data) {
		t.Errorf("large content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

// --- Truncated fetch (reader returns error mid-stream) ---

func TestStore_TruncatedFetchWithoutDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

	truncatedFetch := content.FetchFunc(func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     []byte("partial data that will be cut short after a few bytes"),
			failAt:   10,
			failWith: errors.New("connection reset"),
		}), nil
	})

	_, _, err := cs.Acquire("truncated", nil, truncatedFetch)
	if err == nil {
		t.Fatal("Acquire() should fail with truncated fetch")
	}

	if !errors.Is(err, fault.ErrReadFailure) {
		t.Errorf("error = %v, want ErrReadFailure", err)
	}
}

func TestStore_TruncatedFetchWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	fullContent := []byte("this is the full content that will be truncated mid-stream")
	dgst := computeDigest(fullContent)

	truncatedFetch := content.FetchFunc(func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     fullContent,
			failAt:   10,
			failWith: errors.New("connection reset"),
		}), nil
	})

	reader, _, err := cs.Acquire("truncated-dgst", dgst, truncatedFetch)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	_, err = io.ReadAll(reader)
	_ = reader.Close()

	if err == nil {
		t.Fatal("ReadAll() should fail with truncated fetch")
	}

	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want ErrWriteFailure", err)
	}
}

func TestStore_TruncatedFetchDoesNotPoison(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("full content after truncation recovery")

	truncatedFetch := content.FetchFunc(func() (io.ReadCloser, error) {
		return io.NopCloser(&truncatedReader{
			data:     data,
			failAt:   5,
			failWith: errors.New("broken pipe"),
		}), nil
	})

	// First attempt — truncated.
	_, _, err := cs.Acquire("truncated-recover", nil, truncatedFetch)
	if err == nil {
		t.Fatal("first Acquire() should fail")
	}

	// Second attempt — succeeds.
	reader, _, err := cs.Acquire("truncated-recover", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader.Close()

	got, _ := io.ReadAll(reader)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after truncation recovery")
	}
}

// --- Concurrency ---

func TestStore_ConcurrentSameIdentifier(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("concurrent access content")

	const numGoroutines = 50

	var (
		wg         sync.WaitGroup
		fetchCount atomic.Int64
	)

	fetch := countingFetch(data, &fetchCount)

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("concurrent-id", nil, fetch)
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(got, data) {
				t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(data))
			}
		})
	}

	wg.Wait()
}

func TestStore_ConcurrentDifferentIdentifiers(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

	const numIdentifiers = 20

	var wg sync.WaitGroup

	for idx := range numIdentifiers {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			data := []byte("content for identifier " + string(rune('A'+id)))
			identifier := "id-" + string(rune('A'+id))

			reader, _, err := cs.Acquire(identifier, nil, fetchFunc(data))
			if err != nil {
				t.Errorf("Acquire(%s) error: %v", identifier, err)

				return
			}

			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll(%s) error: %v", identifier, err)

				return
			}

			if !bytes.Equal(got, data) {
				t.Errorf("content mismatch for %s", identifier)
			}
		}(idx)
	}

	wg.Wait()
}

func TestStore_ConcurrentSameIdentifierWithDigest(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("concurrent content with known digest")
	dgst := computeDigest(data)

	const numGoroutines = 50

	var (
		wg         sync.WaitGroup
		fetchCount atomic.Int64
	)

	fetch := countingFetch(data, &fetchCount)

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("concurrent-dgst", dgst, fetch)
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(got, data) {
				t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(data))
			}
		})
	}

	wg.Wait()
}

func TestStore_ConcurrentMixedHitAndMiss(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("mixed hit/miss content")

	// Pre-populate the cache.
	reader0, _, err := cs.Acquire("mixed-id", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("initial Acquire() error: %v", err)
	}

	io.ReadAll(reader0)
	reader0.Close()

	const numGoroutines = 50

	var wg sync.WaitGroup

	for range numGoroutines {
		wg.Go(func() {
			reader, _, err := cs.Acquire("mixed-id", nil, fetchFunc(data))
			if err != nil {
				t.Errorf("Acquire() error: %v", err)

				return
			}

			defer reader.Close()

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("ReadAll() error: %v", err)

				return
			}

			if !bytes.Equal(got, data) {
				t.Error("content mismatch")
			}
		})
	}

	wg.Wait()
}

// --- Edge cases ---

func TestStore_IdentifierIsEmptyString(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content for empty identifier")

	reader, _, err := cs.Acquire("", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, _ := io.ReadAll(reader)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch for empty identifier")
	}
}

func TestStore_VeryLongIdentifier(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content for long identifier")

	// 10KB identifier — hashIdentifier hashes to uint64, so this is safe.
	longID := string(make([]byte, 10240))

	reader, _, err := cs.Acquire(longID, nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	defer reader.Close()

	got, _ := io.ReadAll(reader)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch for long identifier")
	}
}

func TestStore_DigestPathSkipsFetchOnCacheHit(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("pre-cached content")
	dgst := computeDigest(data)

	var fetchCount atomic.Int64

	// First identifier — populates cache with this digest.
	reader1, _, err := cs.Acquire("id-first", dgst, countingFetch(data, &fetchCount))
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	if fetchCount.Load() != 1 {
		t.Fatalf("fetch count = %d, want 1", fetchCount.Load())
	}

	// Second identifier, same digest — cache already has blob, fetch NOT called.
	reader2, _, err := cs.Acquire("id-second", dgst, countingFetch(data, &fetchCount))
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	got, _ := io.ReadAll(reader2)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch")
	}

	if fetchCount.Load() != 1 {
		t.Errorf("fetch count = %d, want 1 (fetch should not be called on cache hit)", fetchCount.Load())
	}
}

func TestStore_RapidAcquireRelease(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("rapid cycle content")

	const iterations = 100

	var wg sync.WaitGroup

	for idx := range iterations {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			reader, _, err := cs.Acquire("rapid-id", nil, fetchFunc(data))
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
	reader, _, err := cs.Acquire("rapid-id", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("final Acquire() error: %v", err)
	}

	defer reader.Close()

	got, _ := io.ReadAll(reader)

	if !bytes.Equal(got, data) {
		t.Error("content corrupted after rapid cycles")
	}
}

// --- Invalidate ---

func TestStore_InvalidateTriggersRefetch(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content to invalidate")

	var fetchCount atomic.Int64

	fetch := countingFetch(data, &fetchCount)

	// Populate.
	reader1, _, err := cs.Acquire("invalidate-id", nil, fetch)
	if err != nil {
		t.Fatalf("first Acquire() error: %v", err)
	}

	io.ReadAll(reader1)
	reader1.Close()

	if fetchCount.Load() != 1 {
		t.Fatalf("fetch count = %d, want 1", fetchCount.Load())
	}

	// Invalidate.
	if err := cs.Invalidate("invalidate-id"); err != nil {
		t.Fatalf("Invalidate() error: %v", err)
	}

	// Re-acquire — should call fetch again.
	reader2, _, err := cs.Acquire("invalidate-id", nil, fetch)
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	defer reader2.Close()

	got, _ := io.ReadAll(reader2)

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after invalidate")
	}

	if fetchCount.Load() != 2 {
		t.Errorf("fetch count = %d, want 2 (should re-fetch after invalidate)", fetchCount.Load())
	}
}

func TestStore_InvalidateNonexistentIdentifier(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

	// Should not error when invalidating an identifier that was never acquired.
	if err := cs.Invalidate("never-existed"); err != nil {
		t.Fatalf("Invalidate() error: %v", err)
	}
}

// --- Slow fetch with concurrent readers ---

func TestStore_SlowFetchConcurrentReaders(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("slowly fetched content that readers wait for")
	dgst := computeDigest(data)

	slowFetch := content.FetchFunc(func() (io.ReadCloser, error) {
		time.Sleep(100 * time.Millisecond)

		return io.NopCloser(bytes.NewReader(data)), nil
	})

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

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("reader %d: ReadAll() error: %v", id, err)

				return
			}

			if !bytes.Equal(got, data) {
				t.Errorf("reader %d: content mismatch", id)
			}
		}(idx)
	}

	wg.Wait()
}

// --- Stress test ---

func TestStore_StressMixedOperations(t *testing.T) {
	t.Parallel()

	cs := newStore(t)

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
			data := []byte("stress-" + string(rune('0'+contentIdx)) + "-" + string(rune('A'+identifierIdx)))
			identifier := "stress-id-" + string(rune('A'+identifierIdx))

			// Alternate between digest-known and digest-unknown paths.
			var dgst digest.Digest
			if id%2 == 0 {
				dgst = computeDigest(data)
			}

			reader, _, err := cs.Acquire(identifier, dgst, fetchFunc(data))
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

func TestStore_AcquireFileColdAndWarm(t *testing.T) {
	t.Parallel()

	cs, err := content.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("content.New() error: %v", err)
	}
	defer func() { _ = cs.Close() }()

	payload := []byte("acquire-file payload bytes")
	fetches := 0
	fetch := func() (io.ReadCloser, error) {
		fetches++

		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	// Cold: fetch runs to completion synchronously; the pinned file holds
	// exactly the payload.
	pin, err := cs.AcquireFile("acquire-file-id", nil, fetch)
	if err != nil {
		t.Fatalf("AcquireFile() cold error: %v", err)
	}

	if fetches != 1 {
		t.Errorf("cold fetches = %d, want 1", fetches)
	}

	if pin.Size != int64(len(payload)) {
		t.Errorf("size = %d, want %d", pin.Size, len(payload))
	}

	got, err := xos.ReadFile(pin.Path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("file content = %q (%v), want %q", got, err, payload)
	}

	if err := pin.Release(); err != nil {
		t.Errorf("Release() error: %v", err)
	}

	// Warm: no fetch, same content.
	warm, err := cs.AcquireFile("acquire-file-id", nil, fetch)
	if err != nil {
		t.Fatalf("AcquireFile() warm error: %v", err)
	}
	defer func() { _ = warm.Release() }()

	if fetches != 1 {
		t.Errorf("warm fetches = %d, want 1 (no refetch)", fetches)
	}

	if warm.Path != pin.Path || warm.Size != pin.Size {
		t.Errorf("warm (path,size) = (%q,%d), want (%q,%d)", warm.Path, warm.Size, pin.Path, pin.Size)
	}
}

func TestStore_AcquireFileSharesBlobWithAcquire(t *testing.T) {
	t.Parallel()

	cs, err := content.New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("content.New() error: %v", err)
	}
	defer func() { _ = cs.Close() }()

	payload := []byte("shared between stream and file consumers")
	fetches := 0
	fetch := func() (io.ReadCloser, error) {
		fetches++

		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	// Populate via the streaming API...
	reader, _, err := cs.Acquire("shared-id", nil, fetch)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("drain error: %v", err)
	}

	_ = reader.Close()

	// ...then pin the same blob as a file without refetching.
	pin, err := cs.AcquireFile("shared-id", nil, fetch)
	if err != nil {
		t.Fatalf("AcquireFile() error: %v", err)
	}
	defer func() { _ = pin.Release() }()

	if fetches != 1 {
		t.Errorf("fetches = %d, want 1", fetches)
	}

	got, err := xos.ReadFile(pin.Path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Errorf("file content = %q (%v), want %q", got, err, payload)
	}
}

// TestStore_DigestlessStagingUsesBLAKE3 pins the algorithm that digest-less
// staging assigns. That digest names the blob in the cache and is persisted in
// the index, so it is on-disk layout rather than an implementation detail.
//
// The check is indirect on purpose: it stages content without a digest, then
// re-acquires the same bytes under a different identifier using an
// independently computed BLAKE3 digest and a fetch that fails if called. That
// only succeeds if staging stored the blob under its BLAKE3 name — and it
// catches the case where content is hashed with one algorithm but labelled
// with another, which a digest-value comparison alone would miss.
func TestStore_DigestlessStagingUsesBLAKE3(t *testing.T) {
	t.Parallel()

	cs := newStore(t)
	data := []byte("content addressed by whatever stage() decided to use")

	reader, _, err := cs.Acquire("https://example.com/staged.bin", nil, fetchFunc(data))
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	_ = reader.Close()

	h := digest.BLAKE3256.Hash()
	h.Write(data)

	want, err := digest.New(digest.BLAKE3256, h.Sum(nil))
	if err != nil {
		t.Fatalf("digest.New() error: %v", err)
	}

	sentinel := errors.New("fetch called: blob was not stored under its BLAKE3 digest")

	second, _, err := cs.Acquire("https://example.com/other.bin", want, failingFetch(sentinel))
	if err != nil {
		t.Fatalf("re-acquire by BLAKE3 digest: %v", err)
	}

	defer second.Close()

	got, err := io.ReadAll(second)
	if err != nil {
		t.Fatalf("ReadAll() on re-acquire: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

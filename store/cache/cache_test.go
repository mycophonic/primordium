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

package cache_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	dgst "github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/cache"
)

func computeDigest(content []byte) dgst.Digest {
	hash := sha256.Sum256(content)

	d, err := dgst.New(dgst.SHA256, hash[:])
	if err != nil {
		panic(err)
	}

	return d
}

func TestCache_WriteAndRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("test content for caching")
	digest := computeDigest(content)

	// First acquire - should get both reader and writer (cache miss)
	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer == nil {
		t.Fatal("expected writer for new content")
	}

	if reader == nil {
		t.Fatal("expected reader for new content (connected to writer)")
	}

	// Write content and read simultaneously
	var wg sync.WaitGroup

	var readData []byte

	var readErr error

	wg.Add(1)

	go func() {
		defer wg.Done()

		readData, readErr = io.ReadAll(reader)
		reader.Close()
	}()

	// Write content
	n, err := writer.Write(content)
	if err != nil {
		t.Fatalf("writer.Write() error: %v", err)
	}

	if n != len(content) {
		t.Errorf("writer.Write() = %d, want %d", n, len(content))
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	wg.Wait()

	if readErr != nil {
		t.Fatalf("ReadAll() error: %v", readErr)
	}

	if !bytes.Equal(readData, content) {
		t.Errorf("Read content = %q, want %q", readData, content)
	}

	// Second acquire - should get reader only (cache hit)
	reader, writer, err = blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer != nil {
		t.Error("expected nil writer for existing content")
		writer.Close()
	}

	if reader == nil {
		t.Fatal("expected reader for existing content")
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("Read content = %q, want %q", data, content)
	}
}

func TestCache_ReadNotExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// Use a valid digest format for non-existent content
	nonexistentDigest := computeDigest([]byte("content that doesn't exist yet"))

	// Acquire non-existent content - should get both reader and writer
	reader, writer, err := blobCache.Acquire(nonexistentDigest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer == nil {
		t.Fatal("expected writer for non-existent content")
	}

	if reader == nil {
		t.Fatal("expected reader for non-existent content (connected to writer)")
	}

	reader.Close()
	writer.Close()
}

func TestCache_WriteDigestMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("actual content")
	wrongDigest := computeDigest([]byte("different content"))

	reader, writer, err := blobCache.Acquire(wrongDigest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer == nil {
		t.Fatal("expected writer")
	}

	// Read in background
	var wg sync.WaitGroup

	var readErr error

	wg.Add(1)

	go func() {
		defer wg.Done()

		_, readErr = io.ReadAll(reader)
		reader.Close()
	}()

	_, _ = writer.Write(content)

	err = writer.Close()
	if !errors.Is(err, fault.ErrHashMismatch) {
		t.Errorf("writer.Close() error = %v, want ErrHashMismatch", err)
	}

	wg.Wait()

	// Reader should get write failure error
	if !errors.Is(readErr, fault.ErrWriteFailure) {
		t.Errorf("ReadAll() error = %v, want ErrWriteFailure", readErr)
	}

	// Verify content is not readable - should get writer again
	reader, writer, err = blobCache.Acquire(wrongDigest)
	if err != nil {
		t.Fatalf("Acquire() after failed write error: %v", err)
	}

	if writer == nil {
		t.Error("expected writer after failed write")
	}

	if reader != nil {
		reader.Close()
	}

	if writer != nil {
		writer.Close()
	}
}

func TestCache_WriteAlreadyExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("existing content")
	digest := computeDigest(content)

	// First acquire and write
	reader1, writer1, _ := blobCache.Acquire(digest)

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(reader1)
		reader1.Close()
	}()

	_, _ = writer1.Write(content)
	_ = writer1.Close()

	wg.Wait()

	// Second acquire should return reader only
	reader2, writer2, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("second Acquire() error: %v", err)
	}

	if writer2 != nil {
		t.Error("expected nil writer for existing content")
		writer2.Close()
	}

	if reader2 == nil {
		t.Fatal("expected reader for existing content")
	}

	reader2.Close()
}

func TestCache_ConcurrentReadWhileWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// Large content to ensure write takes time
	content := make([]byte, 1024*1024) // 1MB
	for i := range content {
		content[i] = byte(i % 256)
	}

	digest := computeDigest(content)

	var wg sync.WaitGroup

	writerStarted := make(chan struct{})

	// Writer goroutine
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader, writer, err := blobCache.Acquire(digest)
		if err != nil {
			t.Errorf("Acquire() error: %v", err)

			return
		}

		close(writerStarted)

		// Read from pipe in background
		wg.Add(1)

		go func() {
			defer wg.Done()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("writer's reader ReadAll() error: %v", err)
			}

			if !bytes.Equal(data, content) {
				t.Errorf("writer's reader content length = %d, want %d", len(data), len(content))
			}

			reader.Close()
		}()

		// Write in chunks to simulate slow transfer
		chunkSize := 64 * 1024

		for i := 0; i < len(content); i += chunkSize {
			end := min(i+chunkSize, len(content))

			_, err := writer.Write(content[i:end])
			if err != nil {
				t.Errorf("writer.Write() error: %v", err)

				return
			}

			time.Sleep(10 * time.Millisecond) // Simulate network delay
		}

		if err := writer.Close(); err != nil {
			t.Errorf("writer.Close() error: %v", err)
		}
	}()

	// Wait for writer to start
	<-writerStarted
	time.Sleep(50 * time.Millisecond) // Give writer time to create file

	// Reader goroutine - reads while write is in progress (from another process perspective)
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader, writer, err := blobCache.Acquire(digest)
		if err != nil {
			t.Errorf("Acquire() error: %v", err)

			return
		}

		// Should get reader for in-progress write, no writer
		if writer != nil {
			t.Error("expected nil writer for in-progress content")
			writer.Close()
		}

		if reader == nil {
			t.Error("expected reader for in-progress write")

			return
		}

		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			t.Errorf("ReadAll() error: %v", err)

			return
		}

		if !bytes.Equal(data, content) {
			t.Errorf("Read content length = %d, want %d", len(data), len(content))
		}
	}()

	wg.Wait()
}

func TestCache_ConcurrentReadWhileWriteFails(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	content := []byte("content that will have wrong digest")
	wrongDigest := computeDigest([]byte("different"))

	var wg sync.WaitGroup

	writerStarted := make(chan struct{})

	// Writer goroutine - will fail due to digest mismatch
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader, writer, err := blobCache.Acquire(wrongDigest)
		if err != nil {
			t.Errorf("Acquire() error: %v", err)

			return
		}

		close(writerStarted)

		// Read in background
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, _ = io.ReadAll(reader)
			reader.Close()
		}()

		time.Sleep(50 * time.Millisecond) // Give reader time to start

		_, _ = writer.Write(content)

		err = writer.Close()
		if !errors.Is(err, fault.ErrHashMismatch) {
			t.Errorf("writer.Close() error = %v, want ErrHashMismatch", err)
		}
	}()

	// Wait for writer to start
	<-writerStarted
	time.Sleep(20 * time.Millisecond)

	// Reader goroutine - should get error when write fails
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader, writer, err := blobCache.Acquire(wrongDigest)
		if err != nil {
			// Might not exist yet, that's ok
			return
		}

		if writer != nil {
			writer.Close()
		}

		if reader == nil {
			return
		}

		defer reader.Close()

		_, err = io.ReadAll(reader)
		if err == nil {
			t.Error("ReadAll() should have failed due to write failure")
		} else if !errors.Is(err, fault.ErrWriteFailure) {
			t.Errorf("ReadAll() error = %v, want ErrWriteFailure", err)
		}
	}()

	wg.Wait()
}

func TestCache_MultipleReadersComplete(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("content for multiple readers")
	digest := computeDigest(content)

	// Write content first
	reader1, writer1, _ := blobCache.Acquire(digest)

	var setupWg sync.WaitGroup

	setupWg.Add(1)

	go func() {
		defer setupWg.Done()

		io.ReadAll(reader1)
		reader1.Close()
	}()

	_, _ = writer1.Write(content)
	_ = writer1.Close()

	setupWg.Wait()

	// Multiple concurrent readers
	const numReaders = 10

	var wg sync.WaitGroup

	for i := range numReaders {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			r, w, err := blobCache.Acquire(digest)
			if err != nil {
				t.Errorf("reader %d: Acquire() error: %v", id, err)

				return
			}

			if w != nil {
				t.Errorf("reader %d: expected nil writer", id)
				w.Close()
			}

			if r == nil {
				t.Errorf("reader %d: expected reader", id)

				return
			}

			defer r.Close()

			data, err := io.ReadAll(r)
			if err != nil {
				t.Errorf("reader %d: ReadAll() error: %v", id, err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("reader %d: content mismatch", id)
			}
		}(i)
	}

	wg.Wait()
}

func TestCache_LargeContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// 10MB content
	content := make([]byte, 10*1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}

	digest := computeDigest(content)

	// Write
	reader1, writer1, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	var writeData []byte

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		writeData, _ = io.ReadAll(reader1)
		reader1.Close()
	}()

	_, err = writer1.Write(content)
	if err != nil {
		t.Fatalf("writer.Write() error: %v", err)
	}

	if err := writer1.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	wg.Wait()

	if !bytes.Equal(writeData, content) {
		t.Error("content mismatch during write")
	}

	// Read
	reader2, writer2, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer2 != nil {
		t.Error("expected nil writer for cached content")
		writer2.Close()
	}

	defer reader2.Close()

	data, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content mismatch")
	}
}

func TestCache_EmptyContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte{}
	digest := computeDigest(content)

	// Write
	reader1, writer1, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(reader1)
		reader1.Close()
	}()

	if err := writer1.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	wg.Wait()

	// Read
	reader2, writer2, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer2 != nil {
		t.Error("expected nil writer for cached content")
		writer2.Close()
	}

	defer reader2.Close()

	data, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty content, got %d bytes", len(data))
	}
}

func TestCache_SequentialWriteThenRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("content for sequential write-then-read pattern")
	digest := computeDigest(content)

	// Acquire - get reader and writer
	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer == nil {
		t.Fatal("expected writer for new content")
	}

	if reader == nil {
		t.Fatal("expected reader")
	}

	// Write ALL content first (no concurrent reader goroutine)
	n, err := writer.Write(content)
	if err != nil {
		t.Fatalf("writer.Write() error: %v", err)
	}

	if n != len(content) {
		t.Errorf("writer.Write() = %d, want %d", n, len(content))
	}

	// Close writer (finalizes cache entry)
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	// NOW read - should work without blocking
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}

	reader.Close()

	if !bytes.Equal(data, content) {
		t.Errorf("Read content = %q, want %q", data, content)
	}
}

func TestCache_SequentialWriteThenReadMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("actual content")
	wrongDigest := computeDigest([]byte("different content"))

	// Acquire with wrong digest
	reader, writer, err := blobCache.Acquire(wrongDigest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if writer == nil {
		t.Fatal("expected writer")
	}

	// Write content
	_, _ = writer.Write(content)

	// Close should fail with hash mismatch
	err = writer.Close()
	if !errors.Is(err, fault.ErrHashMismatch) {
		t.Errorf("writer.Close() error = %v, want ErrHashMismatch", err)
	}

	// Reader should get write failure error
	_, err = io.ReadAll(reader)
	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("ReadAll() error = %v, want ErrWriteFailure", err)
	}

	reader.Close()
}

// TestCache_ConcurrentWritersRace tests multiple goroutines racing to write the same digest.
// Only ONE should get a writer, others should get reader-only (for in-progress or complete).
func TestCache_ConcurrentWritersRace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("content for concurrent writer race")
	digest := computeDigest(content)

	const numGoroutines = 20

	var wg sync.WaitGroup

	writerCount := 0
	readerOnlyCount := 0

	var mu sync.Mutex

	results := make(chan struct {
		gotWriter bool
		readData  []byte
		err       error
	}, numGoroutines)

	// Launch all goroutines simultaneously
	start := make(chan struct{})

	for i := range numGoroutines {
		wg.Add(1)

		go func(_ int) {
			defer wg.Done()

			<-start // Wait for signal

			reader, writer, err := blobCache.Acquire(digest)
			if err != nil {
				results <- struct {
					gotWriter bool
					readData  []byte
					err       error
				}{false, nil, err}

				return
			}

			gotWriter := writer != nil

			if gotWriter {
				// I'm the writer - write content
				wg.Add(1)

				go func() {
					defer wg.Done()
					// Read from my connected reader
					data, _ := io.ReadAll(reader)
					reader.Close()

					results <- struct {
						gotWriter bool
						readData  []byte
						err       error
					}{true, data, nil}
				}()

				_, _ = writer.Write(content)
				_ = writer.Close()
			} else {
				// I'm a reader only
				data, err := io.ReadAll(reader)
				reader.Close()

				results <- struct {
					gotWriter bool
					readData  []byte
					err       error
				}{false, data, err}
			}
		}(i)
	}

	// Start all goroutines at once
	close(start)
	wg.Wait()
	close(results)

	// Analyze results
	for result := range results {
		mu.Lock()

		if result.gotWriter {
			writerCount++
		} else {
			readerOnlyCount++
		}

		mu.Unlock()

		if result.err != nil {
			t.Errorf("goroutine error: %v", result.err)

			continue
		}

		if !bytes.Equal(result.readData, content) {
			t.Errorf("data mismatch: got %d bytes, want %d", len(result.readData), len(content))
		}
	}

	// Exactly ONE goroutine should have gotten a writer
	if writerCount != 1 {
		t.Errorf("writer count = %d, want exactly 1", writerCount)
	}

	if readerOnlyCount != numGoroutines-1 {
		t.Errorf("reader-only count = %d, want %d", readerOnlyCount, numGoroutines-1)
	}
}

// TestCache_ReaderAttachesMidWrite tests a reader attaching at various points during a write.
func TestCache_ReaderAttachesMidWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// Use large content to ensure write takes time
	content := make([]byte, 512*1024) // 512KB
	for i := range content {
		content[i] = byte(i % 256)
	}

	digest := computeDigest(content)

	var wg sync.WaitGroup

	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})

	// Writer goroutine
	wg.Add(1)

	go func() {
		defer wg.Done()
		defer close(writerDone)

		reader, writer, err := blobCache.Acquire(digest)
		if err != nil {
			t.Errorf("writer Acquire() error: %v", err)

			return
		}

		close(writerStarted)

		// Reader in background
		wg.Add(1)

		go func() {
			defer wg.Done()

			io.ReadAll(reader)
			reader.Close()
		}()

		// Write in small chunks
		chunkSize := 4096

		for i := 0; i < len(content); i += chunkSize {
			end := min(i+chunkSize, len(content))

			_, err := writer.Write(content[i:end])
			if err != nil {
				t.Errorf("writer.Write() error: %v", err)

				return
			}

			time.Sleep(1 * time.Millisecond) // Small delay between chunks
		}

		if err := writer.Close(); err != nil {
			t.Errorf("writer.Close() error: %v", err)
		}
	}()

	// Wait for writer to start
	<-writerStarted

	// Launch readers at different times during the write
	delays := []time.Duration{
		0,                      // Immediately
		10 * time.Millisecond,  // Early
		50 * time.Millisecond,  // Mid
		100 * time.Millisecond, // Late
	}

	for _, delay := range delays {
		wg.Add(1)

		go func(d time.Duration) {
			defer wg.Done()

			time.Sleep(d)

			reader, writer, err := blobCache.Acquire(digest)
			if err != nil {
				t.Errorf("reader (delay=%v) Acquire() error: %v", d, err)

				return
			}

			if writer != nil {
				// This can happen if we're very fast - that's ok, just close it
				writer.Close()
			}

			if reader == nil {
				t.Errorf("reader (delay=%v): expected reader", d)

				return
			}

			defer reader.Close()

			data, err := io.ReadAll(reader)
			if err != nil {
				t.Errorf("reader (delay=%v) ReadAll() error: %v", d, err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("reader (delay=%v): data mismatch, got %d bytes, want %d", d, len(data), len(content))
			}
		}(delay)
	}

	wg.Wait()
}

// TestCache_RapidAcquireClose tests rapid acquire/close cycles don't corrupt state.
func TestCache_RapidAcquireClose(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("content for rapid cycles")
	digest := computeDigest(content)

	// First, write the content
	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("initial Acquire() error: %v", err)
	}

	var wg sync.WaitGroup

	initReader := reader

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(initReader)
		initReader.Close()
	}()

	_, _ = writer.Write(content)

	if err := writer.Close(); err != nil {
		t.Fatalf("initial writer.Close() error: %v", err)
	}

	// Now rapidly acquire and close
	const iterations = 100

	for i := range iterations {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			reader, writer, err := blobCache.Acquire(digest)
			if err != nil {
				t.Errorf("iteration %d: Acquire() error: %v", id, err)

				return
			}

			if writer != nil {
				t.Errorf("iteration %d: unexpected writer for cached content", id)
				writer.Close()
			}

			if reader == nil {
				t.Errorf("iteration %d: expected reader", id)

				return
			}

			// Read just a few bytes, then close
			buf := make([]byte, 5)

			_, err = reader.Read(buf)
			if err != nil && !errors.Is(err, io.EOF) {
				t.Errorf("iteration %d: Read() error: %v", id, err)
			}

			reader.Close()
		}(i)
	}

	wg.Wait()

	// Verify content is still intact
	reader, writer, err = blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("final Acquire() error: %v", err)
	}

	if writer != nil {
		t.Error("final: unexpected writer")
		writer.Close()
	}

	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("final ReadAll() error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("final: content corrupted")
	}
}

// TestCache_WriterAbandonmentNoWrite tests closing writer without writing anything.
func TestCache_WriterAbandonmentNoWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// Empty content has a specific digest
	emptyDigest := computeDigest([]byte{})
	nonEmptyDigest := computeDigest([]byte("some content"))

	// Case 1: Acquire for empty content, close without writing
	reader, writer, err := blobCache.Acquire(emptyDigest)
	if err != nil {
		t.Fatalf("Acquire(empty) error: %v", err)
	}

	// Close writer immediately (valid for empty content)
	if err := writer.Close(); err != nil {
		t.Errorf("writer.Close() for empty error: %v", err)
	}

	// Reader should get empty content
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Errorf("ReadAll() for empty error: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}

	reader.Close()

	// Case 2: Acquire for non-empty content, close without writing (hash mismatch)
	reader, writer, err = blobCache.Acquire(nonEmptyDigest)
	if err != nil {
		t.Fatalf("Acquire(non-empty) error: %v", err)
	}

	// Close without writing - should fail because hash won't match
	err = writer.Close()
	if !errors.Is(err, fault.ErrHashMismatch) {
		t.Errorf("writer.Close() without write: error = %v, want ErrHashMismatch", err)
	}

	// Reader should also fail
	_, err = io.ReadAll(reader)
	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("ReadAll() without write: error = %v, want ErrWriteFailure", err)
	}

	reader.Close()

	// Verify cache is not corrupted - should get writer again
	reader, writer, err = blobCache.Acquire(nonEmptyDigest)
	if err != nil {
		t.Fatalf("re-Acquire() error: %v", err)
	}

	if writer == nil {
		t.Error("expected writer after failed write")
	}

	reader.Close()

	if writer != nil {
		writer.Close()
	}
}

// TestCache_PartialWriteAbandon tests writer closing after partial write.
func TestCache_PartialWriteAbandon(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	fullContent := []byte("this is the full content that should be written")
	digest := computeDigest(fullContent)

	// Acquire
	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	// Write only half the content
	half := len(fullContent) / 2

	_, err = writer.Write(fullContent[:half])
	if err != nil {
		t.Fatalf("writer.Write() error: %v", err)
	}

	// Close without completing - hash won't match
	err = writer.Close()
	if !errors.Is(err, fault.ErrHashMismatch) {
		t.Errorf("writer.Close() partial: error = %v, want ErrHashMismatch", err)
	}

	// Reader should fail
	_, err = io.ReadAll(reader)
	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("ReadAll() partial: error = %v, want ErrWriteFailure", err)
	}

	reader.Close()

	// Cache should allow retry
	reader, writer, err = blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("re-Acquire() error: %v", err)
	}

	if writer == nil {
		t.Error("expected writer after partial write failure")
	}

	reader.Close()

	if writer != nil {
		writer.Close()
	}
}

// TestCache_MultipleConcurrentDigests tests concurrent operations on different digests.
func TestCache_MultipleConcurrentDigests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	const numDigests = 10

	var wg sync.WaitGroup

	// Generate different content for each digest
	contents := make([][]byte, numDigests)
	digests := make([]dgst.Digest, numDigests)

	for i := range numDigests {
		contents[i] = []byte("content number " + string(rune('A'+i)) + " with some padding")
		digests[i] = computeDigest(contents[i])
	}

	// Write all digests concurrently
	for i := range numDigests {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			reader, writer, err := blobCache.Acquire(digests[idx])
			if err != nil {
				t.Errorf("digest %d: Acquire() error: %v", idx, err)

				return
			}

			if writer == nil {
				t.Errorf("digest %d: expected writer", idx)
				reader.Close()

				return
			}

			// Read in background
			var readData []byte

			var readErr error

			done := make(chan struct{})

			go func() {
				readData, readErr = io.ReadAll(reader)
				reader.Close()
				close(done)
			}()

			_, _ = writer.Write(contents[idx])

			if err := writer.Close(); err != nil {
				t.Errorf("digest %d: writer.Close() error: %v", idx, err)

				return
			}

			<-done

			if readErr != nil {
				t.Errorf("digest %d: ReadAll() error: %v", idx, readErr)

				return
			}

			if !bytes.Equal(readData, contents[idx]) {
				t.Errorf("digest %d: content mismatch", idx)
			}
		}(i)
	}

	wg.Wait()

	// Verify all cached correctly
	for i := range numDigests {
		reader, writer, err := blobCache.Acquire(digests[i])
		if err != nil {
			t.Errorf("verify digest %d: Acquire() error: %v", i, err)

			continue
		}

		if writer != nil {
			t.Errorf("verify digest %d: unexpected writer", i)
			writer.Close()
		}

		data, err := io.ReadAll(reader)
		reader.Close()

		if err != nil {
			t.Errorf("verify digest %d: ReadAll() error: %v", i, err)

			continue
		}

		if !bytes.Equal(data, contents[i]) {
			t.Errorf("verify digest %d: content mismatch", i)
		}
	}
}

// TestCache_StressReadersWhileWriting stress tests many readers attaching during write.
func TestCache_StressReadersWhileWriting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)

	// Large content
	content := make([]byte, 1024*1024) // 1MB
	for i := range content {
		content[i] = byte(i % 256)
	}

	digest := computeDigest(content)

	const numReaders = 50

	var wg sync.WaitGroup

	writerStarted := make(chan struct{})

	// Writer
	wg.Add(1)

	go func() {
		defer wg.Done()

		reader, writer, err := blobCache.Acquire(digest)
		if err != nil {
			t.Errorf("writer Acquire() error: %v", err)

			return
		}

		close(writerStarted)

		// Drain reader
		wg.Add(1)

		go func() {
			defer wg.Done()

			io.ReadAll(reader)
			reader.Close()
		}()

		// Write slowly
		chunkSize := 16 * 1024

		for i := 0; i < len(content); i += chunkSize {
			end := min(i+chunkSize, len(content))

			_, err := writer.Write(content[i:end])
			if err != nil {
				t.Errorf("writer.Write() error: %v", err)

				return
			}

			time.Sleep(500 * time.Microsecond)
		}

		if err := writer.Close(); err != nil {
			t.Errorf("writer.Close() error: %v", err)
		}
	}()

	<-writerStarted

	// Spawn many readers with random delays
	for i := range numReaders {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Random-ish delay
			time.Sleep(time.Duration(id%10) * time.Millisecond)

			reader, writer, err := blobCache.Acquire(digest)
			if err != nil {
				t.Errorf("reader %d: Acquire() error: %v", id, err)

				return
			}

			if writer != nil {
				writer.Close()
			}

			if reader == nil {
				t.Errorf("reader %d: nil reader", id)

				return
			}

			data, err := io.ReadAll(reader)
			reader.Close()

			if err != nil {
				t.Errorf("reader %d: ReadAll() error: %v", id, err)

				return
			}

			if !bytes.Equal(data, content) {
				t.Errorf("reader %d: data mismatch, got %d bytes", id, len(data))
			}
		}(i)
	}

	wg.Wait()
}

// TestCache_GC_UnderQuota tests GC does nothing when under quota.
func TestCache_GC_UnderQuota(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// 1MB quota
	blobCache := cache.New(root, 1024*1024)

	// Write small content - well under quota
	content := []byte("small content")
	digest := computeDigest(content)

	reader1, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(reader1)
		reader1.Close()
	}()

	_, _ = writer.Write(content)

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	// Wait for reader goroutine to complete
	wg.Wait()

	// Run GC
	stats, err := blobCache.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect() error: %v", err)
	}

	// Nothing should be freed - we're under quota
	if stats.EntriesFreed != 0 {
		t.Errorf("EntriesFreed = %d, want 0 (under quota)", stats.EntriesFreed)
	}

	if stats.BytesFreed != 0 {
		t.Errorf("BytesFreed = %d, want 0 (under quota)", stats.BytesFreed)
	}

	// Content should still be accessible
	reader2, _, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() after GC error: %v", err)
	}

	defer reader2.Close()

	data, err := io.ReadAll(reader2)
	if err != nil {
		t.Fatalf("ReadAll() after GC error: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("content corrupted after GC")
	}
}

// TestCache_GC_OverQuota tests GC removes entries when over quota.
func TestCache_GC_OverQuota(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Tiny quota - 100 bytes
	blobCache := cache.New(root, 100)

	// Write content that exceeds quota
	content1 := make([]byte, 200)
	for i := range content1 {
		content1[i] = 'a'
	}

	digest1 := computeDigest(content1)

	reader1, writer1, err := blobCache.Acquire(digest1)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(reader1)
		reader1.Close()
	}()

	_, _ = writer1.Write(content1)

	if err := writer1.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	// Wait for reader to release lock
	wg.Wait()

	// Run GC - should free the entry since it exceeds quota
	stats, err := blobCache.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect() error: %v", err)
	}

	// Entry should have been freed
	if stats.EntriesFreed == 0 {
		t.Error("EntriesFreed = 0, expected entry to be removed (over quota)")
	}

	if stats.BytesFreed == 0 {
		t.Error("BytesFreed = 0, expected entry to be removed (over quota)")
	}
}

// TestCache_GC_PreservesInUseEntries tests GC doesn't remove entries with active readers.
func TestCache_GC_PreservesInUseEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Tiny quota to force GC to want to clean
	blobCache := cache.New(root, 10)

	// Write content
	content := make([]byte, 500)
	for i := range content {
		content[i] = 'x'
	}

	digest := computeDigest(content)

	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	// Read in background but DON'T close the reader yet
	var readData []byte

	var readWg sync.WaitGroup

	readWg.Add(1)

	go func() {
		defer readWg.Done()

		readData, _ = io.ReadAll(reader)
		// Don't close reader - keep it open
	}()

	_, _ = writer.Write(content)

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	readWg.Wait()

	// Reader is still open (holding lock)
	// GC should NOT be able to remove this entry

	stats, err := blobCache.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect() error: %v", err)
	}

	// Entry should NOT be freed - it's in use
	if stats.EntriesFreed != 0 {
		t.Errorf("EntriesFreed = %d, want 0 (entry in use)", stats.EntriesFreed)
	}

	if stats.BytesFreed != 0 {
		t.Errorf("BytesFreed = %d, want 0 (entry in use)", stats.BytesFreed)
	}

	// Now close the reader
	reader.Close()

	// Verify data was correct
	if !bytes.Equal(readData, content) {
		t.Error("data mismatch")
	}
}

// TestCache_GC_StatsAccuracy tests GC stats are accurate.
func TestCache_GC_StatsAccuracy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// 50 byte quota
	blobCache := cache.New(root, 50)

	var wg sync.WaitGroup

	// Write two entries, each 100 bytes
	for i := range 2 {
		content := make([]byte, 100)
		for j := range content {
			content[j] = byte('a' + i)
		}

		digest := computeDigest(content)

		reader, writer, err := blobCache.Acquire(digest)
		if err != nil {
			t.Fatalf("Acquire() error: %v", err)
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			io.ReadAll(reader)
			reader.Close()
		}()

		_, _ = writer.Write(content)

		if err := writer.Close(); err != nil {
			t.Fatalf("writer.Close() error: %v", err)
		}
	}

	// Wait for all readers to complete and release locks
	wg.Wait()

	// Run GC - should free entries to get under 50 byte quota
	stats, err := blobCache.GarbageCollect()
	if err != nil {
		t.Fatalf("GarbageCollect() error: %v", err)
	}

	if stats.Quota != 50 {
		t.Errorf("Quota = %d, want 50", stats.Quota)
	}

	// Should have freed at least 150 bytes (200 total - 50 quota)
	// to get to 50 or under
	if stats.EntriesFreed == 0 {
		t.Error("EntriesFreed = 0, expected at least one entry freed")
	}

	if stats.BytesFreed < 150 {
		t.Errorf("BytesFreed = %d, expected >= 150", stats.BytesFreed)
	}

	if stats.Remaining > 50 {
		t.Errorf("Remaining = %d, should be <= quota (50)", stats.Remaining)
	}
}

// TestCache_GC_ConcurrentWithAcquire tests GC doesn't interfere with concurrent Acquire.
func TestCache_GC_ConcurrentWithAcquire(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 1024*1024) // 1MB quota

	var wg sync.WaitGroup

	// Run multiple acquire/release cycles concurrently with GC
	for i := range 10 {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			content := make([]byte, 1000+idx*100)
			for j := range content {
				content[j] = byte(idx)
			}

			digest := computeDigest(content)

			reader, writer, err := blobCache.Acquire(digest)
			if err != nil {
				t.Errorf("Acquire(%d) error: %v", idx, err)

				return
			}

			wg.Add(1)

			go func() {
				defer wg.Done()

				io.ReadAll(reader)
				reader.Close()
			}()

			if writer != nil {
				_, _ = writer.Write(content)
				writer.Close()
			}
		}(i)
	}

	// Run GC concurrently
	for range 3 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := blobCache.GarbageCollect()
			if err != nil {
				t.Errorf("GarbageCollect() error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestCache_Exists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	blobCache := cache.New(root, 0)
	content := []byte("exists test content")
	digest := computeDigest(content)

	// Before writing — should not exist
	if blobCache.Exists(digest) {
		t.Error("Exists() = true before content written")
	}

	// Write content
	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		io.ReadAll(reader)
		reader.Close()
	}()

	_, _ = writer.Write(content)

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error: %v", err)
	}

	wg.Wait()

	// After writing — should exist
	if !blobCache.Exists(digest) {
		t.Error("Exists() = false after content written")
	}

	// Different digest — should not exist
	otherDigest := computeDigest([]byte("other content"))
	if blobCache.Exists(otherDigest) {
		t.Error("Exists() = true for unwritten digest")
	}
}

func TestCache_AcquireFileMissing(t *testing.T) {
	t.Parallel()

	blobCache := cache.New(t.TempDir(), 0)

	_, err := blobCache.AcquireFile(computeDigest([]byte("never written")))
	if !errors.Is(err, fault.ErrNotFound) {
		t.Fatalf("AcquireFile() on absent content: err = %v, want fault.ErrNotFound", err)
	}
}

func TestCache_AcquireFilePinsCommittedContent(t *testing.T) {
	t.Parallel()

	blobCache := cache.New(t.TempDir(), 0)
	content := []byte("committed file content")
	digest := computeDigest(content)

	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	if _, err := writer.Write(content); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("writer Close() error: %v", err)
	}

	_ = reader.Close()

	pin, err := blobCache.AcquireFile(digest)
	if err != nil {
		t.Fatalf("AcquireFile() error: %v", err)
	}

	if pin.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", pin.Size, len(content))
	}

	got, err := xos.ReadFile(pin.Path)
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("file at path = %q (%v), want %q", got, err, content)
	}

	if err := pin.Release(); err != nil {
		t.Errorf("Release() error: %v", err)
	}
}

func TestCache_AcquireFileRefusesActiveWriter(t *testing.T) {
	t.Parallel()

	blobCache := cache.New(t.TempDir(), 0)
	content := []byte("still streaming")
	digest := computeDigest(content)

	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	// Writer still open: the data is in flight and must not be exposed.
	if _, err := blobCache.AcquireFile(digest); !errors.Is(err, fault.ErrNotFound) {
		t.Fatalf("AcquireFile() with active writer: err = %v, want fault.ErrNotFound", err)
	}

	_, _ = writer.Write(content)
	_ = writer.Close()
	_ = reader.Close()
}

func TestCache_AcquireFilePinSurvivesGC(t *testing.T) {
	t.Parallel()

	// Quota of one byte: any committed blob is immediately over quota.
	blobCache := cache.New(t.TempDir(), 1)
	content := []byte("pinned against garbage collection")
	digest := computeDigest(content)

	reader, writer, err := blobCache.Acquire(digest)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}

	_, _ = writer.Write(content)
	_ = writer.Close()
	_ = reader.Close()

	pin, err := blobCache.AcquireFile(digest)
	if err != nil {
		t.Fatalf("AcquireFile() error: %v", err)
	}

	if _, err := blobCache.GarbageCollect(); err != nil {
		t.Fatalf("GarbageCollect() error: %v", err)
	}

	if _, err := xos.Stat(pin.Path); err != nil {
		t.Fatalf("pinned blob was reclaimed by GC: %v", err)
	}

	if err := pin.Release(); err != nil {
		t.Fatalf("Release() error: %v", err)
	}

	if _, err := blobCache.GarbageCollect(); err != nil {
		t.Fatalf("GarbageCollect() after release error: %v", err)
	}

	if _, err := xos.Stat(pin.Path); err == nil {
		t.Fatal("released blob survived GC despite being over quota")
	}
}

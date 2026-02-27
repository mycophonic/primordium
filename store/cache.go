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

package store

import (
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
)

const (
	cacheDataFile     = "data"
	cacheDataFileTemp = "_data"
	cacheLockFile     = "lock.read"
	cacheWriteLock    = "lock.write"
	cachePollInterval = 10 * time.Millisecond

	// DefaultCacheQuota is the default disk space quota for the cache (50GB).
	DefaultCacheQuota = 50 << 30
)

// Cache provides content-addressed persistent storage with read-while-write support.
// Safe for concurrent access across multiple processes.
// Readers can stream content as it's being written by another process.
type Cache struct {
	rootDir string
	quota   int64
}

// NewCache creates a new cache at the given root directory.
// Optional quota parameter overrides the default disk space quota.
func NewCache(root string, quota ...int64) *Cache {
	q := int64(DefaultCacheQuota)
	if len(quota) > 0 && quota[0] > 0 {
		q = quota[0]
	}

	return &Cache{rootDir: root, quota: q}
}

// Acquire atomically checks for content and returns a reader, and optionally a writer.
// If content exists (complete or in-progress): returns (reader, nil, nil) - read from cache.
// If content doesn't exist: returns (reader, writer, nil) - write to writer, read from reader.
// On cache miss, the reader and writer are connected via a pipe: data written to writer
// is teed to both the cache file and the reader.
// The caller must Close() the returned reader and writer to release locks.
func (c *Cache) Acquire(dgst string) (io.ReadCloser, io.WriteCloser, error) {
	cacheKey, err := digest.FromString(dgst)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	}

	// Use algorithm-encoded format for directory name (safe: validated hex + known algorithm).
	// Shard into 256 prefix buckets by first 2 hex chars of the encoded hash.
	name := strings.Replace(cacheKey.String(), ":", "-", 1)
	prefix := cacheKey.Encoded()[:2]
	resourceDir := filepath.Join(c.rootDir, prefix, name)
	dataPath := filepath.Join(resourceDir, cacheDataFile)
	tempPath := filepath.Join(resourceDir, cacheDataFileTemp)
	readLockPath := filepath.Join(resourceDir, cacheLockFile)
	writeLockPath := filepath.Join(resourceDir, cacheWriteLock)

	// Step 1: Acquire EXCLUSIVE global lock
	if err := os.MkdirAll(c.rootDir, filesystem.DirPermissionsPrivate); err != nil {
		return nil, nil, fmt.Errorf("%w: cache rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	globalLock, err := filesystem.Lock(c.rootDir)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Create directory XYZ
	if err := os.MkdirAll(resourceDir, filesystem.DirPermissionsPrivate); err != nil {
		_ = filesystem.Unlock(globalLock)

		return nil, nil, fmt.Errorf("%w: entry directory: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 3: Acquire EXCLUSIVE lock on XYZ
	dirLock, err := filesystem.Lock(resourceDir)
	_ = filesystem.Unlock(globalLock)

	if err != nil {
		return nil, nil, fmt.Errorf("%w: entry directory: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 4: Touch XYZ/lock, acquire READ lock on it (for GC protection)
	if err := touchFile(readLockPath); err != nil {
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: lock file: %w", fault.ErrFilesystemFailure, err)
	}

	lockFile, err := filesystem.ReadOnlyLock(readLockPath)
	if err != nil {
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 5: TryLock on writeLock to determine if a writer is active
	if err := touchFile(writeLockPath); err != nil {
		_ = filesystem.Unlock(lockFile)
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: write lock file: %w", fault.ErrFilesystemFailure, err)
	}

	writeLock, tryLockErr := filesystem.TryLock(writeLockPath)

	// Case 1: No active writer (we got the writeLock)
	if tryLockErr == nil {
		return c.acquireNoActiveWriter(
			cacheKey,
			resourceDir,
			dataPath,
			tempPath,
			readLockPath,
			writeLockPath,
			dirLock,
			lockFile,
			writeLock,
		)
	}

	// Case 2: Active writer exists (couldn't get writeLock)
	return c.acquireActiveWriter(
		cacheKey, resourceDir, dataPath, tempPath, readLockPath, writeLockPath, dirLock, lockFile,
	)
}

// GarbageCollect removes unused cache entries to stay within quota.
// Returns statistics about the cleanup operation.
func (c *Cache) GarbageCollect() (GCStats, error) {
	stats := GCStats{Quota: c.quota}

	// Acquire exclusive global lock
	globalLock, err := filesystem.Lock(c.rootDir)
	if err != nil {
		return stats, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	// Enumerate all prefix buckets, then entry folders within each.
	prefixes, err := filesystem.ReadDir(c.rootDir)
	if err != nil {
		_ = filesystem.Unlock(globalLock)

		return stats, fmt.Errorf("%w: cache directory: %w", fault.ErrReadFailure, err)
	}

	var total int64

	var candidates []gcCandidate

	for _, pfx := range prefixes {
		if !pfx.IsDir() {
			continue
		}

		prefixDir := filepath.Join(c.rootDir, pfx.Name())

		bucketTotal, bucketCandidates := gcScanBucket(prefixDir)
		total += bucketTotal

		candidates = append(candidates, bucketCandidates...)
	}

	// Release global lock - we now have exclusive locks on candidate directories
	_ = filesystem.Unlock(globalLock)

	stats.Remaining = total

	// If under quota, release all locks and return
	if total <= c.quota {
		for _, candidate := range candidates {
			_ = filesystem.Unlock(candidate.lock)
		}

		return stats, nil
	}

	// Delete entries until under quota
	for _, candidate := range candidates {
		if stats.Remaining <= c.quota {
			// Under quota, release remaining locks
			_ = filesystem.Unlock(candidate.lock)

			continue
		}

		// Delete this entry
		if err := os.RemoveAll(candidate.path); err != nil {
			_ = filesystem.Unlock(candidate.lock)

			continue
		}

		_ = filesystem.Unlock(candidate.lock)

		stats.BytesFreed += candidate.size
		stats.Remaining -= candidate.size
	}

	return stats, nil
}

// gcScanBucket scans a single prefix bucket directory for GC candidates.
// Returns the total size of data files and any candidates eligible for eviction.
func gcScanBucket(prefixDir string) (int64, []gcCandidate) {
	entries, err := filesystem.ReadDir(prefixDir)
	if err != nil {
		return 0, nil
	}

	var total int64

	var candidates []gcCandidate

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(prefixDir, entry.Name())
		dataPath := filepath.Join(dir, cacheDataFile)
		lockPath := filepath.Join(dir, cacheLockFile)
		writeLockPath := filepath.Join(dir, cacheWriteLock)

		// Acquire exclusive lock on directory
		dirLock, err := filesystem.TryLock(dir)
		if err != nil {
			// Directory in use, skip
			continue
		}

		// Read size of data file
		info, err := filesystem.Stat(dataPath)
		if err != nil {
			// No data file, release dir lock and continue
			_ = filesystem.Unlock(dirLock)

			continue
		}

		size := info.Size()
		total += size

		// Try to acquire exclusive lock on XYZ/lock
		readLock, err := filesystem.TryLock(lockPath)
		if err != nil {
			// Someone has a read lock, skip this entry
			_ = filesystem.Unlock(dirLock)

			continue
		}

		// Try to acquire exclusive lock on XYZ/lock.write
		writeLock, err := filesystem.TryLock(writeLockPath)
		if err != nil {
			// Write in progress, skip this entry
			_ = filesystem.Unlock(readLock)
			_ = filesystem.Unlock(dirLock)

			continue
		}

		// Both locks acquired - entry is unused
		// Release the file locks but keep directory lock
		_ = filesystem.Unlock(writeLock)
		_ = filesystem.Unlock(readLock)

		candidates = append(candidates, gcCandidate{
			path: dir,
			size: size,
			lock: dirLock,
		})
	}

	return total, candidates
}

// acquireNoActiveWriter handles the case where no writer is active.
// Either returns a reader for existing data, or becomes the writer.
//
//revive:disable:argument-limit // Argument count justified by file lock management complexity
func (*Cache) acquireNoActiveWriter(
	cacheKey digest.Digest,
	dir, dataPath, tempPath, lockPath, writeLockPath string,
	dirLock, lockFile, writeLock *os.File,
) (io.ReadCloser, io.WriteCloser, error) {
	// Case 1a: Check if complete data exists
	if file, err := filesystem.Open(dataPath); err == nil {
		// Complete data exists - return reader only
		_ = filesystem.Unlock(writeLock)
		_ = filesystem.Unlock(dirLock)

		return &cacheReader{
			file:     file,
			lockFile: lockFile,
		}, nil, nil
	}

	// Case 1b: No data exists - become the writer
	// Clean up any stale temp file from a previous failed write
	_ = os.Remove(tempPath)

	file, err := filesystem.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filesystem.FilePermissionsPrivate)
	if err != nil {
		_ = filesystem.Unlock(writeLock)
		_ = filesystem.Unlock(lockFile)
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: temp file: %w", fault.ErrFilesystemFailure, err)
	}

	// Open temp file for reading (reader will tail this file)
	readFile, err := filesystem.Open(tempPath)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = filesystem.Unlock(writeLock)
		_ = filesystem.Unlock(lockFile)
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: open temp for read: %w", fault.ErrFilesystemFailure, err)
	}

	// Acquire second read lock for the reader
	readerLockFile, err := filesystem.ReadOnlyLock(lockPath)
	if err != nil {
		_ = readFile.Close()
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = filesystem.Unlock(writeLock)
		_ = filesystem.Unlock(lockFile)
		_ = filesystem.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: reader lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Release dirLock - we have writeLock which protects our write operation
	_ = filesystem.Unlock(dirLock)

	writer := &cacheWriter{
		file:      file,
		hasher:    cacheKey.Algorithm().Hash(),
		encoded:   cacheKey.Encoded(),
		dir:       dir,
		dataPath:  dataPath,
		tempPath:  tempPath,
		lockFile:  lockFile,
		writeLock: writeLock,
	}

	reader := &inProgressReader{
		file:          readFile,
		lockFile:      readerLockFile,
		dataPath:      dataPath,
		writeLockPath: writeLockPath,
	}

	return reader, writer, nil
}

// acquireActiveWriter handles the case where another writer is active.
// Returns a reader for in-progress or completed data.
func (*Cache) acquireActiveWriter(
	cacheKey digest.Digest,
	dir, dataPath, tempPath, lockPath, writeLockPath string,
	dirLock, lockFile *os.File,
) (io.ReadCloser, io.WriteCloser, error) {
	// Case 2a: Try to open the temp file (writer is actively writing)
	if file, err := filesystem.Open(tempPath); err == nil {
		_ = filesystem.Unlock(dirLock)

		return &inProgressReader{
			file:          file,
			lockFile:      lockFile,
			dataPath:      dataPath,
			writeLockPath: writeLockPath,
		}, nil, nil
	}

	// Case 2b: Temp file doesn't exist - writer may have finished between our check and open
	// Try to read the completed data file
	if file, err := filesystem.Open(dataPath); err == nil {
		_ = filesystem.Unlock(dirLock)

		return &cacheReader{
			file:     file,
			lockFile: lockFile,
		}, nil, nil
	}

	// Case 2b.ii: Neither temp nor data file exists, but we thought there was a writer
	// Try to acquire writeLock again - maybe writer finished and cleaned up
	writeLock, tryLockErr := filesystem.TryLock(writeLockPath)
	if tryLockErr == nil {
		// We got the lock - writer is gone, we should become the writer
		_ = os.Remove(tempPath) // Clean up any stale temp file

		file, err := filesystem.OpenFile(
			tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filesystem.FilePermissionsPrivate,
		)
		if err != nil {
			_ = filesystem.Unlock(writeLock)
			_ = filesystem.Unlock(lockFile)
			_ = filesystem.Unlock(dirLock)

			return nil, nil, fmt.Errorf("%w: temp file: %w", fault.ErrFilesystemFailure, err)
		}

		// Open temp file for reading
		readFile, err := filesystem.Open(tempPath)
		if err != nil {
			_ = file.Close()
			_ = os.Remove(tempPath)
			_ = filesystem.Unlock(writeLock)
			_ = filesystem.Unlock(lockFile)
			_ = filesystem.Unlock(dirLock)

			return nil, nil, fmt.Errorf("%w: open temp for read: %w", fault.ErrFilesystemFailure, err)
		}

		// Acquire second read lock for the reader
		readerLockFile, err := filesystem.ReadOnlyLock(lockPath)
		if err != nil {
			_ = readFile.Close()
			_ = file.Close()
			_ = os.Remove(tempPath)
			_ = filesystem.Unlock(writeLock)
			_ = filesystem.Unlock(lockFile)
			_ = filesystem.Unlock(dirLock)

			return nil, nil, fmt.Errorf("%w: reader lock: %w", fault.ErrFilesystemFailure, err)
		}

		_ = filesystem.Unlock(dirLock)

		writer := &cacheWriter{
			file:      file,
			hasher:    cacheKey.Algorithm().Hash(),
			encoded:   cacheKey.Encoded(),
			dir:       dir,
			dataPath:  dataPath,
			tempPath:  tempPath,
			lockFile:  lockFile,
			writeLock: writeLock,
		}

		reader := &inProgressReader{
			file:          readFile,
			lockFile:      readerLockFile,
			dataPath:      dataPath,
			writeLockPath: writeLockPath,
		}

		return reader, writer, nil
	}

	// Catastrophic: writeLock is held but no files are readable
	// This should never happen - invariant violation
	_ = filesystem.Unlock(lockFile)
	_ = filesystem.Unlock(dirLock)

	panic("cache: write lock held but no data accessible - invariant violation")
}

func touchFile(path string) error {
	file, err := filesystem.OpenFile(path, os.O_CREATE|os.O_RDONLY, filesystem.FilePermissionsPrivate)
	//nolint:wrapcheck
	if err != nil {
		return err
	}

	//nolint:wrapcheck
	return file.Close()
}

// cacheReader reads complete content with read lock held.
type cacheReader struct {
	file     *os.File
	lockFile *os.File
}

func (r *cacheReader) Read(p []byte) (int, error) {
	//nolint:wrapcheck // io.Reader interface, thin wrapper
	return r.file.Read(p)
}

func (r *cacheReader) Close() error {
	fileErr := r.file.Close()
	lockErr := filesystem.Unlock(r.lockFile)

	//nolint:wrapcheck // io.Closer interface, thin wrapper
	if fileErr != nil {
		return fileErr
	}

	//nolint:wrapcheck // io.Closer interface, thin wrapper
	return lockErr
}

// cacheWriter writes content to the cache file.
// Reader tails the same file to stream data as it's written.
type cacheWriter struct {
	file      *os.File
	hasher    hash.Hash
	encoded   string // hex-encoded expected hash for verification
	dir       string
	dataPath  string
	tempPath  string
	lockFile  *os.File
	writeLock *os.File
}

// Write writes data to the cache file.
func (w *cacheWriter) Write(p []byte) (int, error) {
	written, err := w.file.Write(p)
	if written > 0 {
		_, _ = w.hasher.Write(p[:written])
	}

	if err != nil {
		err = fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	return written, err
}

func (w *cacheWriter) Close() error {
	// Close the data file first
	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tempPath)
		_ = filesystem.Unlock(w.writeLock)
		_ = filesystem.Unlock(w.lockFile)

		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 1: Acquire EXCLUSIVE lock on XYZ
	dirLock, err := filesystem.Lock(w.dir)
	if err != nil {
		_ = os.Remove(w.tempPath)
		_ = filesystem.Unlock(w.writeLock)
		_ = filesystem.Unlock(w.lockFile)

		return fmt.Errorf("%w: finalize: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Release lock on XYZ/lock (GC protection no longer needed)
	_ = filesystem.Unlock(w.lockFile)

	// Step 3: Compare hash with expected encoded value
	actual := hex.EncodeToString(w.hasher.Sum(nil))

	// Step 4: Rename or delete based on hash match
	if actual == w.encoded {
		if err := os.Rename(w.tempPath, w.dataPath); err != nil {
			_ = os.Remove(w.tempPath)
			_ = filesystem.Unlock(w.writeLock)
			_ = filesystem.Unlock(dirLock)

			return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
		}

		// Step 5: Release lock.write - now readers can detect completion
		_ = filesystem.Unlock(w.writeLock)
		_ = filesystem.Unlock(dirLock)

		return nil
	}

	// Hash mismatch - delete temp file
	_ = os.Remove(w.tempPath)

	// Release lock.write - now readers can detect failure
	_ = filesystem.Unlock(w.writeLock)
	_ = filesystem.Unlock(dirLock)

	return fmt.Errorf("%w: expected %s, got %s", fault.ErrHashMismatch, w.encoded, actual)
}

// inProgressReader reads from a file being written by another process.
// Holds read lock on lock file for GC protection.
type inProgressReader struct {
	file          *os.File
	lockFile      *os.File
	dataPath      string
	writeLockPath string
}

func (r *inProgressReader) Read(p []byte) (int, error) {
	writeComplete := false

	for {
		n, err := r.file.Read(p)
		if n > 0 {
			return n, nil
		}

		//nolint:wrapcheck // io.Reader interface
		if err != nil && err != io.EOF {
			return 0, err
		}

		// Hit EOF - if we already confirmed write complete, we're truly done
		if writeComplete {
			return 0, io.EOF
		}

		// Hit EOF - check if write is complete
		writeLock, lockErr := filesystem.TryLock(r.writeLockPath)
		if lockErr != nil {
			// Writer still active, wait and retry
			time.Sleep(cachePollInterval)

			continue
		}

		// Writer done - check outcome
		_ = filesystem.Unlock(writeLock)

		if _, err := filesystem.Stat(r.dataPath); err != nil {
			// Write failed - data file doesn't exist
			return 0, fault.ErrWriteFailure
		}

		// Write succeeded - mark complete and loop to drain any remaining data
		writeComplete = true
	}
}

func (r *inProgressReader) Close() error {
	fileErr := r.file.Close()
	lockErr := filesystem.Unlock(r.lockFile)

	//nolint:wrapcheck // io.Closer interface
	if fileErr != nil {
		return fileErr
	}

	//nolint:wrapcheck // io.Closer interface
	return lockErr
}

type gcCandidate struct {
	path string
	size int64
	lock *os.File
}

// GCStats contains statistics from a garbage collection run.
type GCStats struct {
	BytesFreed int64
	Remaining  int64
	Quota      int64
}

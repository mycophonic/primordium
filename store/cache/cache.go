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

package cache

import (
	"encoding/hex"
	"errors"
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
	"github.com/mycophonic/primordium/filesystem/flock"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Cache provides content-addressed persistent storage with read-while-write support.
// Safe for concurrent access across multiple processes.
// Readers can stream content as it's being written by another process.
type Cache struct {
	rootDir string
	quota   int64
}

// New creates a new cache at the given root directory.
// quota is the disk space quota in bytes; zero means DefaultCacheQuota.
func New(root string, quota int64) *Cache {
	if quota <= 0 {
		quota = DefaultCacheQuota
	}

	return &Cache{rootDir: root, quota: quota}
}

// Acquire atomically checks for content and returns a reader, and optionally a writer.
// If content exists (complete or in-progress): returns (reader, nil, nil) - read from cache.
// If content doesn't exist: returns (reader, writer, nil) - write to writer, read from reader.
// On cache miss, the reader and writer are connected via a pipe: data written to writer
// is teed to both the cache file and the reader.
// The caller must Close() the returned reader and writer to release locks.
func (c *Cache) Acquire(dgst digest.Digest) (io.ReadCloser, io.WriteCloser, error) {
	// Use algorithm-encoded format for directory name (safe: validated hex + known algorithm).
	// Shard into 256 prefix buckets by first 2 hex chars of the encoded hash.
	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	resourceDir := filepath.Join(c.rootDir, prefix, name)
	dataPath := filepath.Join(resourceDir, cacheDataFile)
	tempPath := filepath.Join(resourceDir, cacheDataFileTemp)
	readLockPath := filepath.Join(resourceDir, cacheLockFile)
	writeLockPath := filepath.Join(resourceDir, cacheWriteLock)

	// Step 1: Acquire EXCLUSIVE global lock
	if err := os.MkdirAll(filepath.Join(c.rootDir, prefix), filesystem.DirPermissionsPrivate); err != nil {
		return nil, nil, fmt.Errorf("%w: cache rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	globalLock, err := flock.Lock(filepath.Join(c.rootDir, prefix))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Create directory XYZ
	if err := os.MkdirAll(resourceDir, filesystem.DirPermissionsPrivate); err != nil {
		_ = flock.Unlock(globalLock)

		return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
	}

	// Step 3: Acquire EXCLUSIVE lock on XYZ
	dirLock, err := flock.Lock(resourceDir)
	_ = flock.Unlock(globalLock)

	if err != nil {
		return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
	}

	// Guard: concurrent GC may have deleted resourceDir between MkdirAll
	// (step 2) and flock.Lock (step 3). On Unix the lock succeeds on the
	// deleted inode, but path-based operations inside the directory fail.
	// Detect and recover by re-creating the directory and re-locking.
	if _, statErr := xos.Stat(resourceDir); statErr != nil {
		_ = flock.Unlock(dirLock)

		if mkErr := os.MkdirAll(resourceDir, filesystem.DirPermissionsPrivate); mkErr != nil {
			return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, mkErr)
		}

		dirLock, err = flock.Lock(resourceDir)
		if err != nil {
			return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
		}
	}

	// Step 4: Touch XYZ/lock, acquire READ lock on it (for GC protection)
	if err := touchFile(readLockPath); err != nil {
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: lock file: %w", fault.ErrFilesystemFailure, err)
	}

	lockFile, err := flock.ReadOnlyLock(readLockPath)
	if err != nil {
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 5: TryLock on writeLock to determine if a writer is active
	if err := touchFile(writeLockPath); err != nil {
		_ = flock.Unlock(lockFile)
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: write lock file: %w", fault.ErrFilesystemFailure, err)
	}

	writeLock, tryLockErr := flock.TryLock(writeLockPath)

	// Case 1: No active writer (we got the writeLock)
	if tryLockErr == nil {
		return c.acquireNoActiveWriter(
			dgst,
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
		dgst, resourceDir, dataPath, tempPath, readLockPath, writeLockPath, dirLock, lockFile,
	)
}

// PinnedFile is a complete cached blob exposed as an immutable on-disk file,
// pinned against garbage collection until Release is called.
type PinnedFile struct {
	// Path locates the blob's data file. The bytes there are complete and
	// immutable (rename-committed, content-addressed) for as long as the
	// pin is held.
	Path string
	// Size is the byte length of the file at Path.
	Size int64

	lockFile *os.File
}

// Release drops the pin. The path must not be used afterwards.
func (p *PinnedFile) Release() error {
	//nolint:wrapcheck // single flock call; context adds nothing
	return flock.Unlock(p.lockFile)
}

// AcquireFile returns COMPLETE cached content as a GC-pinned on-disk file.
// It exists for consumers that need the blob as an immutable file rather
// than a stream — attaching it to a virtual machine as a read-only block
// device, say — where a reader cannot serve.
//
// It never exposes in-progress data: if the content is absent, or a writer
// is still streaming it, it fails with fault.ErrNotFound, and the caller is
// expected to complete a regular Acquire first and try again. The pin is the
// same shared read flock a reader holds, so GarbageCollect skips the entry
// while the pin is outstanding.
func (c *Cache) AcquireFile(dgst digest.Digest) (*PinnedFile, error) {
	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	resourceDir := filepath.Join(c.rootDir, prefix, name)
	dataPath := filepath.Join(resourceDir, cacheDataFile)
	readLockPath := filepath.Join(resourceDir, cacheLockFile)
	writeLockPath := filepath.Join(resourceDir, cacheWriteLock)

	// Fast miss, decided WITHOUT locks: this function never creates entries,
	// so an absent entry directory is a definitive miss. It must not be
	// derived from the lock ladder failing — flock.Lock on a missing path
	// fails on Unix (it opens the directory itself) but SUCCEEDS on Windows
	// (it locks a sibling file it creates on demand), which turned this miss
	// into a mid-ladder filesystem error there. Locklessness is sound in
	// both directions: a stale positive is re-verified by the data-file stat
	// under the locks below, and a stale negative is the same answer a
	// locked probe would have returned a moment earlier.
	if _, err := xos.Stat(resourceDir); err != nil {
		return nil, fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
	}

	// Same lock ladder as Acquire (global → entry dir → read lock → write
	// probe); see Acquire for the step-by-step rationale.
	if err := os.MkdirAll(filepath.Join(c.rootDir, prefix), filesystem.DirPermissionsPrivate); err != nil {
		return nil, fmt.Errorf("%w: cache rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	globalLock, err := flock.Lock(filepath.Join(c.rootDir, prefix))
	if err != nil {
		return nil, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	dirLock, err := flock.Lock(resourceDir)
	_ = flock.Unlock(globalLock)

	if err != nil {
		// Concurrent GC may have deleted the entry between the stat above
		// and here (Unix surfaces that as a failed lock).
		if _, statErr := xos.Stat(resourceDir); statErr != nil {
			return nil, fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
		}

		return nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
	}

	if err := touchFile(readLockPath); err != nil {
		_ = flock.Unlock(dirLock)

		// Same GC race, as Windows surfaces it: the sibling-file lock above
		// succeeds even when the entry directory is gone, so the deletion is
		// only discovered here, creating the read-lock file inside it.
		if _, statErr := xos.Stat(resourceDir); statErr != nil {
			return nil, fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
		}

		return nil, fmt.Errorf("%w: lock file: %w", fault.ErrFilesystemFailure, err)
	}

	lockFile, err := flock.ReadOnlyLock(readLockPath)
	if err != nil {
		_ = flock.Unlock(dirLock)

		return nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	pin := &PinnedFile{lockFile: lockFile}

	// An active writer means the data file (if present at all) is a stale
	// name for in-flight bytes: refuse rather than expose it. Touch first,
	// like Acquire's step 5 — on Unix TryLock opens the path itself, so a
	// missing lock file would read as a probe failure rather than a free lock.
	if err := touchFile(writeLockPath); err != nil {
		_ = pin.Release()
		_ = flock.Unlock(dirLock)

		return nil, fmt.Errorf("%w: write lock file: %w", fault.ErrFilesystemFailure, err)
	}

	writeLock, tryLockErr := flock.TryLock(writeLockPath)
	if tryLockErr != nil {
		_ = pin.Release()
		_ = flock.Unlock(dirLock)

		// Only a held lock means an active writer. Anything else (EACCES,
		// EMFILE, …) is a filesystem fault, not a miss — reporting it as
		// ErrNotFound would send the caller into a pointless refetch.
		if errors.Is(tryLockErr, flock.ErrLockWouldBlock) {
			return nil, fmt.Errorf("%w: content for %s is still being written", fault.ErrNotFound, dgst.String())
		}

		return nil, fmt.Errorf("%w: write lock: %w", fault.ErrFilesystemFailure, tryLockErr)
	}

	_ = flock.Unlock(writeLock)

	info, statErr := xos.Stat(dataPath)

	_ = flock.Unlock(dirLock)

	if statErr != nil {
		_ = pin.Release()

		return nil, fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
	}

	pin.Path = dataPath
	pin.Size = info.Size()

	return pin, nil
}

// Exists reports whether cached content for the given digest is present on disk.
// This is a lockless, best-effort check (subject to TOCTOU races) intended for
// fast-path skipping in bulk operations where a subsequent Acquire handles correctness.
func (c *Cache) Exists(dgst digest.Digest) bool {
	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	dataPath := filepath.Join(c.rootDir, prefix, name, cacheDataFile)

	_, err := xos.Stat(dataPath)

	return err == nil
}

// GarbageCollect removes unused cache entries to stay within quota.
// Returns statistics about the cleanup operation.
// Locking is per-shard (prefix bucket), so Acquire operations in other shards
// can proceed concurrently.
func (c *Cache) GarbageCollect() (GCStats, error) {
	stats := GCStats{Quota: c.quota}

	// Enumerate all prefix buckets.
	prefixes, err := xos.ReadDir(c.rootDir)
	if err != nil {
		return stats, fmt.Errorf("%w: cache directory: %w", fault.ErrReadFailure, err)
	}

	var total int64

	var candidates []gcCandidate

	for _, pfx := range prefixes {
		if !pfx.IsDir() {
			continue
		}

		prefixDir := filepath.Join(c.rootDir, pfx.Name())

		// Lock this shard while scanning its entries.
		shardLock, err := flock.Lock(prefixDir)
		if err != nil {
			continue
		}

		bucketTotal, bucketCandidates := gcScanBucket(prefixDir)
		total += bucketTotal

		candidates = append(candidates, bucketCandidates...)

		// Release shard lock — candidates retain their own entry dir locks.
		_ = flock.Unlock(shardLock)
	}

	stats.Remaining = total

	// If under quota, release all locks and return
	if total <= c.quota {
		for _, candidate := range candidates {
			_ = flock.Unlock(candidate.lock)
		}

		return stats, nil
	}

	// Delete entries until under quota
	for _, candidate := range candidates {
		if stats.Remaining <= c.quota {
			// Under quota, release remaining locks
			_ = flock.Unlock(candidate.lock)

			continue
		}

		// Delete this entry
		if err := os.RemoveAll(candidate.path); err != nil {
			_ = flock.Unlock(candidate.lock)

			continue
		}

		_ = flock.Unlock(candidate.lock)

		stats.EntriesFreed++
		stats.BytesFreed += candidate.size
		stats.Remaining -= candidate.size
	}

	return stats, nil
}

// gcScanBucket scans a single prefix bucket directory for GC candidates.
// Returns the total size of data files and any candidates eligible for eviction.
func gcScanBucket(prefixDir string) (int64, []gcCandidate) {
	entries, err := xos.ReadDir(prefixDir)
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
		dirLock, err := flock.TryLock(dir)
		if err != nil {
			// Directory in use, skip
			continue
		}

		// Read size of data file
		info, err := xos.Stat(dataPath)
		if err != nil {
			// No data file, release dir lock and continue
			_ = flock.Unlock(dirLock)

			continue
		}

		size := info.Size()
		total += size

		// Try to acquire exclusive lock on XYZ/lock
		readLock, err := flock.TryLock(lockPath)
		if err != nil {
			// Someone has a read lock, skip this entry
			_ = flock.Unlock(dirLock)

			continue
		}

		// Try to acquire exclusive lock on XYZ/lock.write
		writeLock, err := flock.TryLock(writeLockPath)
		if err != nil {
			// Write in progress, skip this entry
			_ = flock.Unlock(readLock)
			_ = flock.Unlock(dirLock)

			continue
		}

		// Both locks acquired - entry is unused
		// Release the file locks but keep directory lock
		_ = flock.Unlock(writeLock)
		_ = flock.Unlock(readLock)

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
	if file, err := xos.Open(dataPath); err == nil {
		// Complete data exists - return reader only
		_ = flock.Unlock(writeLock)
		_ = flock.Unlock(dirLock)

		return &cacheReader{
			file:     file,
			lockFile: lockFile,
		}, nil, nil
	}

	// Case 1b: No data exists - become the writer
	return becomeWriter(cacheKey, dir, dataPath, tempPath, lockPath, writeLockPath, dirLock, lockFile, writeLock)
}

// acquireActiveWriter handles the case where another writer is active.
// Returns a reader for in-progress or completed data.
func (*Cache) acquireActiveWriter(
	cacheKey digest.Digest,
	dir, dataPath, tempPath, lockPath, writeLockPath string,
	dirLock, lockFile *os.File,
) (io.ReadCloser, io.WriteCloser, error) {
	// Case 2a: Try to open the temp file (writer is actively writing)
	if file, err := xos.Open(tempPath); err == nil {
		_ = flock.Unlock(dirLock)

		return &inProgressReader{
			file:          file,
			lockFile:      lockFile,
			dataPath:      dataPath,
			writeLockPath: writeLockPath,
		}, nil, nil
	}

	// Case 2b: Temp file doesn't exist - writer may have finished between our check and open
	// Try to read the completed data file
	if file, err := xos.Open(dataPath); err == nil {
		_ = flock.Unlock(dirLock)

		return &cacheReader{
			file:     file,
			lockFile: lockFile,
		}, nil, nil
	}

	// Case 2b.ii: Neither temp nor data file exists, but we thought there was a writer
	// Try to acquire writeLock again - maybe writer finished and cleaned up
	writeLock, tryLockErr := flock.TryLock(writeLockPath)
	if tryLockErr == nil {
		// We got the lock - writer is gone, we should become the writer
		return becomeWriter(cacheKey, dir, dataPath, tempPath, lockPath, writeLockPath, dirLock, lockFile, writeLock)
	}

	// Catastrophic: writeLock is held but no files are readable
	// This should never happen - invariant violation
	_ = flock.Unlock(lockFile)
	_ = flock.Unlock(dirLock)

	panic("cache: write lock held but no data accessible - invariant violation")
}

// becomeWriter sets up this caller as the writer for a cache entry.
// Creates temp file, opens it for tailing, acquires a second read lock for the reader,
// and returns the connected writer/reader pair.
// On error, releases all locks (writeLock, lockFile, dirLock).
//
//revive:disable-next-line:argument-limit // Argument count justified by file lock management complexity
func becomeWriter(
	cacheKey digest.Digest,
	dir, dataPath, tempPath, lockPath, writeLockPath string,
	dirLock, lockFile, writeLock *os.File,
) (io.ReadCloser, io.WriteCloser, error) {
	// Clean up any stale temp file from a previous failed write
	_ = os.Remove(tempPath)

	file, err := xos.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filesystem.FilePermissionsPrivate)
	if err != nil {
		_ = flock.Unlock(writeLock)
		_ = flock.Unlock(lockFile)
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: temp file: %w", fault.ErrFilesystemFailure, err)
	}

	// Open temp file for reading (reader will tail this file)
	readFile, err := xos.Open(tempPath)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = flock.Unlock(writeLock)
		_ = flock.Unlock(lockFile)
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: open temp for read: %w", fault.ErrFilesystemFailure, err)
	}

	// Acquire second read lock for the reader
	readerLockFile, err := flock.ReadOnlyLock(lockPath)
	if err != nil {
		_ = readFile.Close()
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = flock.Unlock(writeLock)
		_ = flock.Unlock(lockFile)
		_ = flock.Unlock(dirLock)

		return nil, nil, fmt.Errorf("%w: reader lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Release dirLock - we have writeLock which protects our write operation
	_ = flock.Unlock(dirLock)

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

func touchFile(path string) error {
	file, err := xos.OpenFile(path, os.O_CREATE|os.O_RDONLY, filesystem.FilePermissionsPrivate)
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

// Close releases the data file and the shared read lock.
// Not safe for concurrent or repeated calls.
func (r *cacheReader) Close() error {
	fileErr := r.file.Close()
	lockErr := flock.Unlock(r.lockFile)

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

// Close finalizes the write: verifies the content hash, renames (or deletes)
// the temp file, and releases all locks. Not safe for concurrent or repeated
// calls — like os.File, callers must not call Close more than once.
func (w *cacheWriter) Close() error {
	// Close the data file first
	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tempPath)
		_ = flock.Unlock(w.writeLock)
		_ = flock.Unlock(w.lockFile)

		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 1: Acquire EXCLUSIVE lock on XYZ
	dirLock, err := flock.Lock(w.dir)
	if err != nil {
		_ = os.Remove(w.tempPath)
		_ = flock.Unlock(w.writeLock)
		_ = flock.Unlock(w.lockFile)

		return fmt.Errorf("%w: finalize: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Compare hash with expected encoded value
	actual := hex.EncodeToString(w.hasher.Sum(nil))

	// Step 3: Rename or delete based on hash match
	if actual == w.encoded {
		if err := os.Rename(w.tempPath, w.dataPath); err != nil {
			_ = os.Remove(w.tempPath)
			_ = flock.Unlock(w.writeLock)
			_ = flock.Unlock(dirLock)
			_ = flock.Unlock(w.lockFile)

			return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
		}

		// Step 4: Release locks — write lock first (signals readers), then dir, then read lock.
		_ = flock.Unlock(w.writeLock)
		_ = flock.Unlock(dirLock)
		_ = flock.Unlock(w.lockFile)

		return nil
	}

	// Hash mismatch - delete temp file
	_ = os.Remove(w.tempPath)

	// Release locks — write lock first (signals readers), then dir, then read lock.
	_ = flock.Unlock(w.writeLock)
	_ = flock.Unlock(dirLock)
	_ = flock.Unlock(w.lockFile)

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
		writeLock, lockErr := flock.TryLock(r.writeLockPath)
		if lockErr != nil {
			// Writer still active, wait and retry
			time.Sleep(cachePollInterval)

			continue
		}

		// Writer done - check outcome
		_ = flock.Unlock(writeLock)

		if _, err := xos.Stat(r.dataPath); err != nil {
			// Write failed - data file doesn't exist
			return 0, fault.ErrWriteFailure
		}

		// Write succeeded - mark complete and loop to drain any remaining data
		writeComplete = true
	}
}

// Close releases the temp file and the shared read lock.
// Not safe for concurrent or repeated calls.
func (r *inProgressReader) Close() error {
	fileErr := r.file.Close()
	lockErr := flock.Unlock(r.lockFile)

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
	EntriesFreed int
	BytesFreed   int64
	Remaining    int64
	Quota        int64
}

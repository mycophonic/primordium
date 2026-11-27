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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/forkcloser/xos"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/flock"
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
// A zero digest is ErrInvalidArgument.
func (c *Cache) Acquire(dgst digest.Digest) (io.ReadCloser, io.WriteCloser, error) {
	if dgst.IsZero() {
		return nil, nil, fmt.Errorf("%w: zero digest", fault.ErrInvalidArgument)
	}

	// Use algorithm-encoded format for directory name (safe: validated hex + known algorithm).
	// Shard into 256 prefix buckets by first 2 hex chars of the encoded hash.
	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	resourceDir := filepath.Join(c.rootDir, prefix, name)
	dataPath := filepath.Join(resourceDir, cacheDataFile)
	tempPath := filepath.Join(resourceDir, cacheDataFileTemp)
	readLockPath := filepath.Join(resourceDir, cacheLockFile)
	writeLockPath := filepath.Join(resourceDir, cacheWriteLock)

	// Step 1: Acquire EXCLUSIVE global lock, on the shard made with its lock
	// file.
	shardDir := filepath.Join(c.rootDir, prefix)
	if err := os.MkdirAll(shardDir, filesystem.DirPermissionsPrivate); err != nil {
		return nil, nil, fmt.Errorf("%w: cache rootDir: %w", fault.ErrFilesystemFailure, err)
	}

	if err := makeLockFiles(shardLockPath(shardDir)); err != nil {
		return nil, nil, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	globalLock, err := flock.Exclusive(shardLockPath(shardDir))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: global: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Create directory XYZ, with its lock files
	if err = os.MkdirAll(resourceDir, filesystem.DirPermissionsPrivate); err == nil {
		err = makeLockFiles(entryLockPath(resourceDir), readLockPath, writeLockPath)
	}

	if err != nil {
		_ = globalLock.Unlock()

		return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
	}

	// Step 3: Acquire EXCLUSIVE lock on XYZ. The global lock is held until
	// then: an eviction takes it too, so the directory just created is still
	// there once its lock is held.
	dirLock, err := flock.Exclusive(entryLockPath(resourceDir))
	_ = globalLock.Unlock()

	if err != nil {
		return nil, nil, fmt.Errorf(errFmtEntryDir, fault.ErrFilesystemFailure, err)
	}

	// Step 4: Acquire READ lock on XYZ/lock (for GC protection)
	lockFile, err := flock.Shared(readLockPath)
	if err != nil {
		_ = dirLock.Unlock()

		return nil, nil, fmt.Errorf("%w: read lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 5: TryExclusive on writeLock to determine if a writer is active.
	// Only a held lock is a writer; anything else is a fault.
	writeLock, tryLockErr := flock.TryExclusive(writeLockPath)
	if tryLockErr != nil && !errors.Is(tryLockErr, flock.ErrLockWouldBlock) {
		_ = lockFile.Unlock()
		_ = dirLock.Unlock()

		return nil, nil, fmt.Errorf("%w: write lock: %w", fault.ErrFilesystemFailure, tryLockErr)
	}

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

	lockFile *flock.Lock
}

// Release drops the pin. The path must not be used afterwards.
func (p *PinnedFile) Release() error {
	//nolint:wrapcheck // single flock call; context adds nothing
	return p.lockFile.Unlock()
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
// A zero digest is ErrInvalidArgument.
func (c *Cache) AcquireFile(dgst digest.Digest) (*PinnedFile, error) {
	if dgst.IsZero() {
		return nil, fmt.Errorf("%w: zero digest", fault.ErrInvalidArgument)
	}

	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	resourceDir := filepath.Join(c.rootDir, prefix, name)
	dataPath := filepath.Join(resourceDir, cacheDataFile)
	readLockPath := filepath.Join(resourceDir, cacheLockFile)
	writeLockPath := filepath.Join(resourceDir, cacheWriteLock)

	// Fast miss, decided WITHOUT locks: this function never creates entries,
	// so an absent entry directory is a definitive miss. Locklessness is
	// sound in both directions: a stale positive meets a lock file that is
	// gone below, and a stale negative is the same answer a locked probe
	// would have returned a moment earlier.
	if _, err := xos.Stat(resourceDir); err != nil {
		return nil, fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
	}

	// Same lock ladder as Acquire (global → entry dir → read lock → write
	// probe); see Acquire for the step-by-step rationale. This function
	// makes no lock file: one that is missing is an entry evicted since the
	// stat above, or never made.
	globalLock, err := flock.Exclusive(shardLockPath(filepath.Join(c.rootDir, prefix)))
	if err != nil {
		return nil, absent(err, "global", dgst)
	}

	dirLock, err := flock.Exclusive(entryLockPath(resourceDir))
	_ = globalLock.Unlock()

	if err != nil {
		return nil, absent(err, "entry directory", dgst)
	}

	lockFile, err := flock.Shared(readLockPath)
	if err != nil {
		_ = dirLock.Unlock()

		return nil, absent(err, "read lock", dgst)
	}

	pin := &PinnedFile{lockFile: lockFile}

	// An active writer means the data file (if present at all) is a stale
	// name for in-flight bytes: refuse rather than expose it.
	writeLock, tryLockErr := flock.TryExclusive(writeLockPath)
	if tryLockErr != nil {
		_ = pin.Release()
		_ = dirLock.Unlock()

		// Only a held lock means an active writer. Anything else (EACCES,
		// EMFILE, …) is a filesystem fault, not a miss — reporting it as
		// ErrNotFound would send the caller into a pointless refetch.
		if errors.Is(tryLockErr, flock.ErrLockWouldBlock) {
			return nil, fmt.Errorf("%w: content for %s is still being written", fault.ErrNotFound, dgst.String())
		}

		return nil, absent(tryLockErr, "write lock", dgst)
	}

	_ = writeLock.Unlock()

	info, statErr := xos.Stat(dataPath)

	_ = dirLock.Unlock()

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
// Nothing is cached under the zero digest.
func (c *Cache) Exists(dgst digest.Digest) bool {
	if dgst.IsZero() {
		return false
	}

	name := strings.Replace(dgst.String(), ":", "-", 1)
	prefix := dgst.Encoded()[:2]
	dataPath := filepath.Join(c.rootDir, prefix, name, cacheDataFile)

	_, err := xos.Stat(dataPath)

	return err == nil
}

// GarbageCollect brings the cache under its quota, evicting the oldest
// committed entries first among those no reader, writer or pin holds. The
// measure takes no lock; each eviction takes the entry's locks for itself,
// so the walk holds no descriptor however many entries there are, and an
// entry in use at that moment is skipped.
func (c *Cache) GarbageCollect() (GCStats, error) {
	stats := GCStats{Quota: c.quota}

	entries, err := c.measure()
	if err != nil {
		return stats, err
	}

	for _, entry := range entries {
		stats.Remaining += entry.size
	}

	if stats.Remaining <= c.quota {
		return stats, nil
	}

	slices.SortFunc(entries, func(a, b gcEntry) int { return a.modTime.Compare(b.modTime) })

	for _, entry := range entries {
		if stats.Remaining <= c.quota {
			break
		}

		if !c.evict(entry.path) {
			continue
		}

		stats.EntriesFreed++
		stats.BytesFreed += entry.size
		stats.Remaining -= entry.size
	}

	return stats, nil
}

// acquireNoActiveWriter handles the case where no writer is active.
// Either returns a reader for existing data, or becomes the writer.
//
//revive:disable:argument-limit // Argument count justified by file lock management complexity
func (*Cache) acquireNoActiveWriter(
	cacheKey digest.Digest,
	dir, dataPath, tempPath, lockPath, writeLockPath string,
	dirLock, lockFile, writeLock *flock.Lock,
) (io.ReadCloser, io.WriteCloser, error) {
	// Case 1a: Check if complete data exists
	if file, err := xos.Open(dataPath); err == nil {
		// Complete data exists - return reader only
		_ = writeLock.Unlock()
		_ = dirLock.Unlock()

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
	dirLock, lockFile *flock.Lock,
) (io.ReadCloser, io.WriteCloser, error) {
	// Case 2a: Try to open the temp file (writer is actively writing)
	if file, err := xos.Open(tempPath); err == nil {
		_ = dirLock.Unlock()

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
		_ = dirLock.Unlock()

		return &cacheReader{
			file:     file,
			lockFile: lockFile,
		}, nil, nil
	}

	// Case 2b.ii: Neither temp nor data file exists, but we thought there was a writer
	// Try to acquire writeLock again - maybe writer finished and cleaned up
	writeLock, tryLockErr := flock.TryExclusive(writeLockPath)
	if tryLockErr == nil {
		// We got the lock - writer is gone, we should become the writer
		return becomeWriter(cacheKey, dir, dataPath, tempPath, lockPath, writeLockPath, dirLock, lockFile, writeLock)
	}

	// Catastrophic: writeLock is held but no files are readable
	// This should never happen - invariant violation
	_ = lockFile.Unlock()
	_ = dirLock.Unlock()

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
	dirLock, lockFile, writeLock *flock.Lock,
) (io.ReadCloser, io.WriteCloser, error) {
	// Clean up any stale temp file from a previous failed write
	_ = os.Remove(tempPath)

	file, err := xos.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filesystem.FilePermissionsPrivate)
	if err != nil {
		_ = writeLock.Unlock()
		_ = lockFile.Unlock()
		_ = dirLock.Unlock()

		return nil, nil, fmt.Errorf("%w: temp file: %w", fault.ErrFilesystemFailure, err)
	}

	// Open temp file for reading (reader will tail this file)
	readFile, err := xos.Open(tempPath)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = writeLock.Unlock()
		_ = lockFile.Unlock()
		_ = dirLock.Unlock()

		return nil, nil, fmt.Errorf("%w: open temp for read: %w", fault.ErrFilesystemFailure, err)
	}

	// Acquire second read lock for the reader
	readerLockFile, err := flock.Shared(lockPath)
	if err != nil {
		_ = readFile.Close()
		_ = file.Close()
		_ = os.Remove(tempPath)
		_ = writeLock.Unlock()
		_ = lockFile.Unlock()
		_ = dirLock.Unlock()

		return nil, nil, fmt.Errorf("%w: reader lock: %w", fault.ErrFilesystemFailure, err)
	}

	// Release dirLock - we have writeLock which protects our write operation
	_ = dirLock.Unlock()

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

// cacheReader reads complete content with read lock held.
type cacheReader struct {
	file     *os.File
	lockFile *flock.Lock
}

func (r *cacheReader) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
	}

	return n, err
}

// Close releases the data file and the shared read lock.
// Not safe for concurrent or repeated calls.
func (r *cacheReader) Close() error {
	return closeReader(r.file, r.lockFile)
}

// closeReader closes a reader's data file and releases its read lock; the
// lock is released whatever the close said.
func closeReader(file *os.File, lockFile *flock.Lock) error {
	fileErr := file.Close()
	lockErr := lockFile.Unlock()

	if fileErr != nil {
		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, fileErr)
	}

	//nolint:wrapcheck // flock's own sentinel, ErrUnlockFail
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
	lockFile  *flock.Lock
	writeLock *flock.Lock
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

// Close finalizes the write: flushes the temp file to stable storage, verifies
// the content hash, renames (or deletes) the temp file, and releases all locks.
// Not safe for concurrent or repeated calls — like os.File, callers must not
// call Close more than once.
func (w *cacheWriter) Close() error {
	// The bytes reach stable storage before the name does: a rename can survive
	// a power loss that the unflushed data behind it does not.
	if err := w.file.Sync(); err != nil {
		_ = w.file.Close()
		_ = os.Remove(w.tempPath)
		_ = w.writeLock.Unlock()
		_ = w.lockFile.Unlock()

		return fmt.Errorf("%w: sync: %w", fault.ErrFilesystemFailure, err)
	}

	if err := w.file.Close(); err != nil {
		_ = os.Remove(w.tempPath)
		_ = w.writeLock.Unlock()
		_ = w.lockFile.Unlock()

		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 1: Acquire EXCLUSIVE lock on XYZ
	dirLock, err := flock.Exclusive(entryLockPath(w.dir))
	if err != nil {
		_ = os.Remove(w.tempPath)
		_ = w.writeLock.Unlock()
		_ = w.lockFile.Unlock()

		return fmt.Errorf("%w: finalize: %w", fault.ErrFilesystemFailure, err)
	}

	// Step 2: Compare hash with expected encoded value
	actual := hex.EncodeToString(w.hasher.Sum(nil))

	// Step 3: Rename or delete based on hash match
	if actual == w.encoded {
		if err := os.Rename(w.tempPath, w.dataPath); err != nil {
			_ = os.Remove(w.tempPath)
			_ = w.writeLock.Unlock()
			_ = dirLock.Unlock()
			_ = w.lockFile.Unlock()

			return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
		}

		// Step 4: Release locks — write lock first (signals readers), then dir, then read lock.
		_ = w.writeLock.Unlock()
		_ = dirLock.Unlock()
		_ = w.lockFile.Unlock()

		return nil
	}

	// Hash mismatch - delete temp file
	_ = os.Remove(w.tempPath)

	// Release locks — write lock first (signals readers), then dir, then read lock.
	_ = w.writeLock.Unlock()
	_ = dirLock.Unlock()
	_ = w.lockFile.Unlock()

	return fmt.Errorf("%w: expected %s, got %s", fault.ErrHashMismatch, w.encoded, actual)
}

// inProgressReader reads from a file being written by another process.
// Holds read lock on lock file for GC protection.
type inProgressReader struct {
	file          *os.File
	lockFile      *flock.Lock
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

		if err != nil && !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
		}

		// Hit EOF - if we already confirmed write complete, we're truly done
		if writeComplete {
			return 0, io.EOF
		}

		// Hit EOF - check if write is complete
		writeLock, lockErr := flock.TryExclusive(r.writeLockPath)
		if lockErr != nil {
			// Writer still active, wait and retry
			time.Sleep(cachePollInterval)

			continue
		}

		// Writer done - check outcome
		_ = writeLock.Unlock()

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
	return closeReader(r.file, r.lockFile)
}

// measure lists every committed entry with its size and commit time, locking
// nothing: a shard or an entry that cannot be read is simply not there.
func (c *Cache) measure() ([]gcEntry, error) {
	prefixes, err := xos.ReadDir(c.rootDir)
	if err != nil {
		return nil, fmt.Errorf("%w: cache directory: %w", fault.ErrReadFailure, err)
	}

	var entries []gcEntry

	for _, pfx := range prefixes {
		if !pfx.IsDir() {
			continue
		}

		prefixDir := filepath.Join(c.rootDir, pfx.Name())

		shard, err := xos.ReadDir(prefixDir)
		if err != nil {
			continue
		}

		for _, entry := range shard {
			if !entry.IsDir() {
				continue
			}

			dir := filepath.Join(prefixDir, entry.Name())

			info, err := xos.Stat(filepath.Join(dir, cacheDataFile))
			if err != nil {
				continue
			}

			entries = append(entries, gcEntry{path: dir, size: info.Size(), modTime: info.ModTime()})
		}
	}

	return entries, nil
}

// evict removes the entry at dir when nobody holds it. The shard lock keeps
// an Acquire out of its create-then-lock window; the entry's lock stands for
// the entry, and the two probes, released at once, for a reader or a pin and
// for a writer. False when any is held, or the entry is already gone.
func (*Cache) evict(dir string) bool {
	shard := filepath.Dir(dir)

	// A cache laid out without lock files has none in its shards and none
	// beside its entries. The shard's is made here, as Acquire makes it, so
	// that such a cache is collected like any other.
	if makeLockFiles(shardLockPath(shard)) != nil {
		return false
	}

	shardLock, err := flock.Exclusive(shardLockPath(shard))
	if err != nil {
		return false
	}

	defer func() { _ = shardLock.Unlock() }()

	// An entry with no lock file beside it is one no Acquire has taken:
	// Acquire makes that file under the shard lock, held here. Nobody is
	// taking the entry, and the two probes below stand for whoever holds it.
	dirLock, err := flock.TryExclusive(entryLockPath(dir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false
	}

	// Probed, not held: the two lock files are inside the directory, and
	// Windows removes no directory with an open file in it. Nothing takes
	// either meanwhile; Acquire and AcquireFile take the entry's lock first.
	free := unheld(filepath.Join(dir, cacheLockFile)) && unheld(filepath.Join(dir, cacheWriteLock))

	removed := false

	if free {
		if _, err := xos.Stat(filepath.Join(dir, cacheDataFile)); err == nil {
			removed = os.RemoveAll(dir) == nil
		}
	}

	if dirLock != nil {
		_ = dirLock.Unlock()
	}

	// The entry's lock file sits beside the directory and goes with it.
	// Under the shard lock nobody waits on it or is about to open it.
	if removed {
		_ = os.Remove(entryLockPath(dir))
	}

	return removed
}

// absent is the error of a lock AcquireFile could not take: a miss when the
// lock file is not there, a filesystem failure otherwise.
func absent(err error, what string, dgst digest.Digest) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf(errFmtNoContent, fault.ErrNotFound, dgst.String())
	}

	return fmt.Errorf("%w: %s: %w", fault.ErrFilesystemFailure, what, err)
}

// makeLockFiles creates each lock file that is not there yet. flock locks
// only a file that exists, so whoever makes a directory makes its lock files.
func makeLockFiles(paths ...string) error {
	for _, path := range paths {
		if _, err := xos.Stat(path); err == nil {
			continue
		}

		file, err := xos.OpenFile(path, os.O_CREATE|os.O_RDONLY, filesystem.FilePermissionsPrivate)
		if err != nil {
			return fmt.Errorf("lock file: %w", err)
		}

		if err = file.Close(); err != nil {
			return fmt.Errorf("lock file: %w", err)
		}
	}

	return nil
}

// shardLockPath is the lock file of the shard at dir: inside it, since a
// shard is never removed.
func shardLockPath(dir string) string {
	return filepath.Join(dir, cacheShardLock)
}

// entryLockPath is the lock file of the entry at dir: beside it, since an
// entry is removed under its lock.
func entryLockPath(dir string) string {
	return dir + cacheEntryLockSuffix
}

// unheld reports whether nobody holds the lock at path: a TryExclusive that
// succeeds, released at once.
func unheld(path string) bool {
	lock, err := flock.TryExclusive(path)
	if err != nil {
		return false
	}

	_ = lock.Unlock()

	return true
}

type gcEntry struct {
	path    string
	size    int64
	modTime time.Time
}

// GCStats contains statistics from a garbage collection run. Sizes are in
// bytes.
type GCStats struct {
	EntriesFreed int
	BytesFreed   int64
	Remaining    int64
	Quota        int64
}

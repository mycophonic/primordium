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

package content

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/cache"
	"github.com/mycophonic/primordium/store/index"
)

// stagingDir holds in-flight digest-less fetches. It sits beside the cache
// rather than inside it: the GC walks the cache root treating every child as a
// shard bucket, and a staging directory there would be walked as one.
const stagingDir = "staging"

// errFmtFetch wraps a failed FetchFunc invocation.
const errFmtFetch = "%w: fetch: %w"

// algorithmToID maps digest algorithms to stable numeric identifiers for
// on-disk index records. These IDs are a persistence contract owned by this
// package — existing values must never be changed or reassigned.
//
//nolint:gochecknoglobals // Package-level persistence registry.
var algorithmToID = map[digest.Algorithm]uint8{
	digest.SHA1:       1,
	digest.SHA256:     2,
	digest.SHA384:     3,
	digest.SHA512:     4,
	digest.BLAKE2b256: 5,
	digest.BLAKE2b512: 6,
}

// algorithmFromID is the reverse of algorithmToID.
//
//nolint:gochecknoglobals // Package-level persistence registry.
var algorithmFromID = map[uint8]digest.Algorithm{
	1: digest.SHA1,
	2: digest.SHA256,
	3: digest.SHA384,
	4: digest.SHA512,
	5: digest.BLAKE2b256,
	6: digest.BLAKE2b512,
}

// FetchFunc retrieves content from an external source.
// Called only on cache miss.
type FetchFunc func() (io.ReadCloser, error)

// Options configures a content store.
type Options struct {
	// InitialCap is the initial bucket count for the index.
	// Zero means default (1024).
	InitialCap uint64
	// MaxCap is the maximum bucket count the index may grow to.
	// Zero means unlimited.
	MaxCap uint64
	// Quota is the disk space quota for the cache in bytes.
	// Zero means default (50GB).
	Quota int64
}

// Store provides identifier-based lookup over a content-addressed Cache.
// It maps arbitrary identifiers (URLs, keys) to cached content via an
// mmap'd flat-file index.
type Store struct {
	cache *cache.Cache
	idx   *index.Index
	// root is the store directory, used to stage digest-less fetches on the
	// same filesystem as the cache they are about to be written into.
	root string
	wg   sync.WaitGroup
}

// New creates a content store under root, using root/cache for the
// content-addressed blob cache and root/index.dat for the flat-file index.
// Close must be called when the store is no longer needed.
func New(root string, opts *Options) (*Store, error) {
	idxOpts := &index.Options{
		ValSize: 1 + digest.MaxDigestSize,
	}

	var quota int64

	if opts != nil {
		idxOpts.InitialCap = opts.InitialCap
		idxOpts.MaxCap = opts.MaxCap
		quota = opts.Quota
	}

	idx, err := index.New(filepath.Join(root, "index.dat"), idxOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return &Store{
		cache: cache.New(filepath.Join(root, "cache"), quota),
		idx:   idx,
		root:  root,
	}, nil
}

// Close waits for in-flight background writes to complete, then releases
// resources held by the store.
func (s *Store) Close() error {
	s.wg.Wait()

	//nolint:wrapcheck
	return s.idx.Close()
}

// GarbageCollect reclaims disk space from the cache by removing blobs
// that exceed the quota.
func (s *Store) GarbageCollect() (cache.GCStats, error) {
	//nolint:wrapcheck
	return s.cache.GarbageCollect()
}

// Acquire returns cached content for the given identifier, or fetches it on cache miss.
//
// When dgst is non-nil, content streams directly into cache and the reader is returned
// immediately — the caller can read while the fetch is still in progress.
// When dgst is nil, the index is consulted for a previously stored digest. If neither
// is available, content is buffered to compute the digest before caching.
//
// The returned reader must be closed by the caller.
func (s *Store) Acquire(identifier string, dgst digest.Digest, fetch FetchFunc) (io.ReadCloser, time.Time, error) {
	key := hashKey(identifier)

	// Look up the index once — needed to preserve timestamps and avoid redundant writes.
	rec, found, err := s.idx.Get(key)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
	}

	var indexedDgst digest.Digest

	if found {
		indexedDgst, _ = decodeValue(rec.Value)
	}

	if indexedDgst != nil {
		slog.Debug("index hit", "identifier", identifier, "digest", indexedDgst.String())
	} else {
		slog.Debug("index miss", "identifier", identifier)
	}

	// (a) Caller provided a digest — go straight to cache.
	if dgst != nil {
		slog.Debug("using caller digest", "identifier", identifier, "digest", dgst.String())

		return s.fetchWithDigest(key, dgst, indexedDgst, rec.Timestamp, fetch)
	}

	// (b) No caller digest — use the indexed one if available.
	if indexedDgst != nil {
		slog.Debug("using indexed digest", "identifier", identifier, "digest", indexedDgst.String())

		return s.fetchWithDigest(key, indexedDgst, indexedDgst, rec.Timestamp, fetch)
	}

	// (c) No digest, no index entry — fetch, hash, cache.
	slog.Debug("no digest available, will fetch and hash", "identifier", identifier)

	return s.fetchAndHash(key, fetch)
}

// AcquireFile is Acquire for consumers that need the blob as a complete,
// immutable, GC-pinned file on disk rather than a stream — attaching a
// cached blob to a virtual machine as a read-only block device is the
// motivating case. The returned pin must be held (and then Released) for as
// long as the file is in use.
//
// On a hit this costs an index lookup and a lock acquisition; no content is
// read. On a miss it drives fetch to COMPLETION first — a file consumer
// cannot tail an in-flight fetch, so the digest-less path stages, hashes and
// commits synchronously — and then pins the committed blob.
func (s *Store) AcquireFile(
	identifier string, dgst digest.Digest, fetch FetchFunc,
) (*cache.PinnedFile, error) {
	key := hashKey(identifier)

	resolved := dgst
	if resolved == nil {
		rec, found, err := s.idx.Get(key)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
		}

		if found {
			resolved, _ = decodeValue(rec.Value)
		}
	}

	// Warm path: the blob is committed and merely needs pinning.
	if resolved != nil {
		pin, err := s.cache.AcquireFile(resolved)
		if err == nil {
			return pin, nil
		}

		if !errors.Is(err, fault.ErrNotFound) {
			//nolint:wrapcheck // cache errors already carry fault sentinels
			return nil, err
		}
	}

	// Miss. With a digest in hand (caller-provided, or indexed but its blob
	// reclaimed), the regular Acquire fetches and verifies through the cache
	// pipe; draining the reader waits for the rename-commit.
	if resolved != nil {
		reader, _, err := s.Acquire(identifier, dgst, fetch)
		if err != nil {
			return nil, err
		}

		_, copyErr := io.Copy(io.Discard, reader)
		_ = reader.Close()

		if copyErr != nil {
			return nil, fmt.Errorf("%w: completing fetch: %w", fault.ErrWriteFailure, copyErr)
		}

		//nolint:wrapcheck // cache errors already carry fault sentinels
		return s.cache.AcquireFile(resolved)
	}

	// Digest-less miss: stage/hash/commit synchronously. Acquire's own
	// digest-less path commits in a background goroutine after its reader
	// drains, which would leave this function racing the index write.
	committed, err := s.commitDigestless(key, fetch)
	if err != nil {
		return nil, err
	}

	//nolint:wrapcheck // cache errors already carry fault sentinels
	return s.cache.AcquireFile(committed)
}

// Invalidate removes the index entry for the given identifier.
// The next Acquire for this identifier will be a cache miss, triggering a fresh fetch.
// Cached content blobs are not removed — they may be shared by other identifiers
// and will be cleaned up by Cache garbage collection.
func (s *Store) Invalidate(identifier string) error {
	key := hashKey(identifier)

	_, err := s.idx.Delete(key)
	if err != nil {
		return fmt.Errorf("%w: invalidate: %w", fault.ErrFilesystemFailure, err)
	}

	return nil
}

// commitDigestless fetches, stages, hashes, and commits a digest-less blob,
// returning its digest once the blob and its index entry are durable. It is
// fetchAndHash minus the streaming reader: everything happens synchronously.
func (s *Store) commitDigestless(key uint64, fetch FetchFunc) (digest.Digest, error) {
	source, err := fetch()
	if err != nil {
		return nil, fmt.Errorf(errFmtFetch, fault.ErrReadFailure, err)
	}

	staged, dgst, err := s.stage(source)
	_ = source.Close()

	if err != nil {
		return nil, err
	}

	defer func() { _ = staged.Close() }()

	reader, writer, err := s.cache.Acquire(dgst)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	if writer != nil {
		_, writeErr := io.Copy(writer, staged)
		closeErr := writer.Close()
		_ = reader.Close()

		if writeErr != nil || closeErr != nil {
			return nil, fmt.Errorf("%w: committing staged blob: %w", fault.ErrWriteFailure,
				errors.Join(writeErr, closeErr))
		}
	} else {
		// Another writer got there first (concurrent fetch of identical
		// content): draining the reader waits for its commit.
		_, copyErr := io.Copy(io.Discard, reader)
		_ = reader.Close()

		if copyErr != nil {
			return nil, fmt.Errorf("%w: waiting for concurrent commit: %w", fault.ErrWriteFailure, copyErr)
		}
	}

	_ = s.idx.Put(key, encodeDigest(dgst), time.Now().UnixNano())

	return dgst, nil
}

// fetchWithDigest acquires content from cache by digest.
// On cache hit (writer == nil), returns the reader immediately.
// On cache miss (writer != nil), starts a background fetch that streams
// through the cache pipe — the reader is returned immediately and receives
// data as the fetch progresses.
// The index is only written when the entry is missing or maps to a different digest.
// indexedDgst/indexedTS are the previously stored digest and timestamp (indexedDgst may be nil).
func (s *Store) fetchWithDigest(
	key uint64,
	dgst digest.Digest,
	indexedDgst digest.Digest,
	indexedTS int64,
	fetch FetchFunc,
) (io.ReadCloser, time.Time, error) {
	reader, writer, err := s.cache.Acquire(dgst)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	encodedDgst := encodeDigest(dgst)

	if writer != nil {
		slog.Debug("cache miss, fetching", "digest", dgst.String())

		now := time.Now()

		source, fetchErr := fetch()
		if fetchErr != nil {
			_ = reader.Close()
			_ = writer.Close()

			return nil, time.Time{}, fmt.Errorf("%w: fetch: %w", fault.ErrReadFailure, fetchErr)
		}

		s.wg.Go(func() {
			_, copyErr := io.Copy(writer, source)
			_ = source.Close()

			closeErr := writer.Close()

			if copyErr == nil && closeErr == nil {
				slog.Debug("background write complete", "digest", dgst.String())

				_ = s.idx.Put(key, encodedDgst, now.UnixNano())
			} else {
				slog.Warn("background write failed", "digest", dgst.String(),
					"copyErr", copyErr, "closeErr", closeErr)
			}
		})

		return reader, now, nil
	}

	slog.Debug("cache hit", "digest", dgst.String())

	// Cache hit — preserve existing timestamp if the index already matches.
	if indexedDgst != nil && dgst.String() == indexedDgst.String() {
		return reader, time.Unix(0, indexedTS), nil
	}

	// Index missing or maps to a different digest — update it.
	now := time.Now()
	_ = s.idx.Put(key, encodedDgst, now.UnixNano())

	return reader, now, nil
}

// fetchAndHash handles the miss path when the digest is unknown.
// Content is buffered to compute the digest, then written to cache.
func (s *Store) fetchAndHash(
	key uint64,
	fetch FetchFunc,
) (io.ReadCloser, time.Time, error) {
	slog.Debug("fetching without digest (will stage and hash)")

	source, err := fetch()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf(errFmtFetch, fault.ErrReadFailure, err)
	}

	staged, dgst, err := s.stage(source)
	_ = source.Close()

	if err != nil {
		return nil, time.Time{}, err
	}

	slog.Debug("computed digest", "digest", dgst.String())

	reader, writer, err := s.cache.Acquire(dgst)
	if err != nil {
		_ = staged.Close()

		return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	now := time.Now()
	encodedDgst := encodeDigest(dgst)

	if writer != nil {
		s.wg.Go(func() {
			// Closing the staged file drops the last reference to an
			// already-unlinked inode, so the space is returned here.
			defer func() { _ = staged.Close() }()

			_, writeErr := io.Copy(writer, staged)

			closeErr := writer.Close()

			if writeErr == nil && closeErr == nil {
				slog.Debug("staged write complete", "digest", dgst.String())

				_ = s.idx.Put(key, encodedDgst, now.UnixNano())
			} else {
				slog.Warn("staged write failed", "digest", dgst.String(),
					"writeErr", writeErr, "closeErr", closeErr)
			}
		})

		return reader, now, nil
	}

	_ = staged.Close()

	slog.Debug("blob already cached", "digest", dgst.String())

	// Blob exists but index is missing (e.g. prior incomplete run).
	_ = s.idx.Put(key, encodedDgst, now.UnixNano())

	return reader, now, nil
}

// stage writes source to a scratch file while hashing it, and returns that file
// rewound to the start together with the content digest.
//
// A digest-less fetch cannot be addressed until it has been read to the end, so
// the bytes have to live somewhere in the meantime. On disk rather than in
// memory: callers hand this path arbitrarily large blobs (a flattened container
// rootfs, for instance), and buffering those in RAM makes peak memory track
// content size — several times the blob once the growing []byte is counted.
// Staging keeps it flat regardless of size.
//
// The scratch file is unlinked immediately after creation and used only through
// the open descriptor. Nothing has to clean it up: the kernel frees the space
// when the descriptor closes, including when the process is killed mid-fetch,
// so there is no stale-file sweep and no interference between concurrent
// callers. It lives under the store root so it lands on the same filesystem as
// the cache — the space it needs is space the cache was about to need anyway.
func (s *Store) stage(source io.Reader) (*os.File, digest.Digest, error) {
	dir := filepath.Join(s.root, stagingDir)
	if err := os.MkdirAll(dir, filesystem.DirPermissionsPrivate); err != nil {
		return nil, nil, fmt.Errorf("%w: staging dir: %w", fault.ErrFilesystemFailure, err)
	}

	file, err := xos.CreateTemp(dir, "blob-*")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: staging file: %w", fault.ErrFilesystemFailure, err)
	}

	_ = os.Remove(file.Name())

	hasher := digest.BLAKE2b256.Hash()

	written, err := io.Copy(io.MultiWriter(file, hasher), source)
	if err != nil {
		_ = file.Close()

		return nil, nil, fmt.Errorf("%w: read: %w", fault.ErrReadFailure, err)
	}

	slog.Debug("content staged, computing digest", "size", written)

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()

		return nil, nil, fmt.Errorf("%w: rewind staging file: %w", fault.ErrFilesystemFailure, err)
	}

	dgst, err := digest.New(digest.BLAKE2b256, hasher.Sum(nil))
	if err != nil {
		_ = file.Close()

		return nil, nil, fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return file, dgst, nil
}

// encodeDigest encodes a digest as [algID:1][rawDigest:N] for index storage.
func encodeDigest(dgst digest.Digest) []byte {
	algID, ok := algorithmToID[dgst.Algorithm()]
	if !ok {
		panic(fmt.Sprintf("unknown algorithm: %s", dgst.Algorithm()))
	}

	raw, _ := hex.DecodeString(dgst.Encoded())
	val := make([]byte, 1+len(raw))
	val[0] = algID
	copy(val[1:], raw)

	return val
}

// decodeValue reconstructs a Digest from an index value encoded by encodeDigest.
func decodeValue(value []byte) (digest.Digest, error) {
	if len(value) == 0 {
		return nil, fmt.Errorf("%w: empty index value", fault.ErrInvalidArgument)
	}

	algID := value[0]
	if algID == 0 {
		return nil, fmt.Errorf("%w: zero algorithm ID", fault.ErrInvalidArgument)
	}

	alg, ok := algorithmFromID[algID]
	if !ok {
		return nil, fmt.Errorf("%w: unknown algorithm ID %d", fault.ErrInvalidArgument, algID)
	}

	size := alg.Hash().Size()
	if len(value) < 1+size {
		return nil, fmt.Errorf("%w: value too short for %s: need %d, have %d",
			fault.ErrInvalidArgument, alg, 1+size, len(value))
	}

	dgst, err := digest.New(alg, value[1:1+size])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	}

	return dgst, nil
}

// hashKey returns a uint64 hash of the given string using BLAKE2b-256.
// Same entropy as HashPath (64 bits) but as a numeric key suitable for index lookups.
func hashKey(s string) uint64 {
	h := digest.BLAKE2b256.Hash()
	_, _ = h.Write([]byte(s))

	return binary.BigEndian.Uint64(h.Sum(nil)[:8])
}

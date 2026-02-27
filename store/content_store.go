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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
)

const contentIndexFile = "meta"

// FetchFunc retrieves content from an external source.
// Called only on cache miss.
type FetchFunc func() (io.ReadCloser, error)

// ContentStore provides identifier-based lookup over a content-addressed Cache.
// It maps arbitrary identifiers (URLs, keys) to cached content via a persistent index.
type ContentStore struct {
	cache    *Cache
	indexDir string
}

// NewContentStore creates a content store backed by the given cache.
// indexDir is the directory for the identifier-to-digest index.
func NewContentStore(cache *Cache, indexDir string) *ContentStore {
	return &ContentStore{cache: cache, indexDir: indexDir}
}

// Acquire returns cached content for the given identifier, or fetches it on cache miss.
// dgst is optional — if non-empty, content streams directly into cache using that digest
// without buffering or hashing. If empty, content is buffered to compute the digest.
// The returned reader must be closed by the caller.
func (s *ContentStore) Acquire(identifier, dgst string, fetch FetchFunc) (io.ReadCloser, time.Time, error) {
	key := digest.Hashpath(identifier)
	entryDir := filepath.Join(s.indexDir, key)
	metaPath := filepath.Join(entryDir, contentIndexFile)

	// Index hit — try serving from cache.
	entry, indexErr := readIndex(metaPath)
	if indexErr == nil {
		reader, writer, cacheErr := s.cache.Acquire(entry.Digest)
		if cacheErr != nil {
			return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrReadFailure, cacheErr)
		}

		if writer == nil {
			// Cache hit.
			return reader, entry.CreatedAt, nil
		}

		// Stale index — cache was GC'd. Close and fall through to fetch.
		_ = reader.Close()
		_ = writer.Close()
	}

	// Cache miss — fetch content.
	if dgst != "" {
		return s.fetchWithDigest(dgst, fetch, entryDir, metaPath)
	}

	return s.fetchAndHash(fetch, entryDir, metaPath)
}

// Invalidate removes the index entry for the given identifier.
// The next Acquire for this identifier will be a cache miss, triggering a fresh fetch.
// Cached content blobs are not removed — they may be shared by other identifiers
// and will be cleaned up by Cache garbage collection.
func (s *ContentStore) Invalidate(identifier string) error {
	key := digest.Hashpath(identifier)
	entryDir := filepath.Join(s.indexDir, key)

	if err := os.RemoveAll(entryDir); err != nil {
		return fmt.Errorf("%w: invalidate: %w", fault.ErrFilesystemFailure, err)
	}

	return nil
}

// fetchWithDigest handles the miss path when the caller provides a known digest.
// Content streams directly from fetch into the cache writer without buffering.
func (s *ContentStore) fetchWithDigest(
	dgst string,
	fetch FetchFunc,
	entryDir, metaPath string,
) (io.ReadCloser, time.Time, error) {
	reader, writer, err := s.cache.Acquire(dgst)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	if writer != nil {
		source, fetchErr := fetch()
		if fetchErr != nil {
			_ = reader.Close()
			_ = writer.Close()

			return nil, time.Time{}, fmt.Errorf("%w: fetch: %w", fault.ErrReadFailure, fetchErr)
		}

		_, copyErr := io.Copy(writer, source)
		_ = source.Close()

		if copyErr != nil {
			_ = reader.Close()
			_ = writer.Close()

			return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrWriteFailure, copyErr)
		}

		if closeErr := writer.Close(); closeErr != nil {
			_ = reader.Close()

			return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrWriteFailure, closeErr)
		}

		now := time.Now()

		if err := writeIndex(entryDir, metaPath, dgst, now); err != nil {
			_ = reader.Close()

			return nil, time.Time{}, err
		}

		return reader, now, nil
	}

	return reader, time.Now(), nil
}

// fetchAndHash handles the miss path when the digest is unknown.
// Content is buffered to compute the digest, then written to cache.
func (s *ContentStore) fetchAndHash(
	fetch FetchFunc,
	entryDir, metaPath string,
) (io.ReadCloser, time.Time, error) {
	source, err := fetch()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: fetch: %w", fault.ErrReadFailure, err)
	}

	content, err := io.ReadAll(source)
	_ = source.Close()

	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: read: %w", fault.ErrReadFailure, err)
	}

	// Compute content digest.
	hasher := digest.SHA256.Hash()
	_, _ = hasher.Write(content)

	dgst := string(digest.SHA256) + ":" + hex.EncodeToString(hasher.Sum(nil))

	reader, writer, err := s.cache.Acquire(dgst)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	if writer != nil {
		if _, writeErr := writer.Write(content); writeErr != nil {
			_ = reader.Close()
			_ = writer.Close()

			return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrWriteFailure, writeErr)
		}

		if closeErr := writer.Close(); closeErr != nil {
			_ = reader.Close()

			return nil, time.Time{}, fmt.Errorf("%w: %w", fault.ErrWriteFailure, closeErr)
		}

		now := time.Now()

		if err := writeIndex(entryDir, metaPath, dgst, now); err != nil {
			_ = reader.Close()

			return nil, time.Time{}, err
		}

		return reader, now, nil
	}

	return reader, time.Now(), nil
}

type indexEntry struct {
	Digest    string    `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
}

func readIndex(metaPath string) (*indexEntry, error) {
	data, err := filesystem.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
	}

	var entry indexEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrInvalidJSON, err)
	}

	return &entry, nil
}

func writeIndex(entryDir, metaPath, dgst string, createdAt time.Time) error {
	if err := os.MkdirAll(entryDir, filesystem.DirPermissionsPrivate); err != nil {
		return fmt.Errorf("%w: index directory: %w", fault.ErrFilesystemFailure, err)
	}

	entry := indexEntry{
		Digest:    dgst,
		CreatedAt: createdAt,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	}

	if err := filesystem.WriteFile(metaPath, data, filesystem.FilePermissionsPrivate); err != nil {
		return fmt.Errorf("%w: index: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

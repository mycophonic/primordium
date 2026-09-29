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
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/store/content"
	"github.com/mycophonic/primordium/store/index"
)

var errDigestNotResolved = errors.New("persisted digest was not resolved")

// TestIndexRecordAlgorithmIDs pins the on-disk algorithm IDs. Index records
// persist a digest as [algID:1][raw digest], so changing or reassigning an ID
// silently corrupts every existing index. Do not update the expected values
// without a migration strategy.
func TestIndexRecordAlgorithmIDs(t *testing.T) {
	t.Parallel()

	pinned := []struct {
		alg digest.Algorithm
		id  byte
	}{
		{digest.SHA1, 1},
		{digest.SHA256, 2},
		{digest.SHA384, 3},
		{digest.SHA512, 4},
		{digest.BLAKE2b256, 5},
		{digest.BLAKE2b512, 6},
		{digest.BLAKE3256, 7},
	}

	data := []byte("pinned on-disk algorithm identifiers")

	for _, tt := range pinned {
		t.Run(string(tt.alg), func(t *testing.T) {
			t.Parallel()

			hasher := tt.alg.Hash()
			_, _ = hasher.Write(data)
			raw := hasher.Sum(nil)

			dgst, err := digest.New(tt.alg, raw)
			if err != nil {
				t.Fatalf("digest.New(%s) error: %v", tt.alg, err)
			}

			root := t.TempDir()

			acquire(t, root, dgst, fetchFunc(data))

			want := append([]byte{tt.id}, raw...)
			if got := persistedValue(t, root); !bytes.HasPrefix(got, want) {
				t.Fatalf("index record for %s = %x, want prefix %x (on-disk format changed!)", tt.alg, got, want)
			}

			// Reading the record back must resolve the persisted digest: a hit
			// without fetching proves the ID decodes to the same algorithm.
			acquire(t, root, nil, failingFetch(errDigestNotResolved))
		})
	}
}

// acquire opens the store at root, reads identifier's content through it,
// and closes the store so that every index write has landed on disk.
func acquire(t *testing.T, root string, dgst digest.Digest, fetch content.FetchFunc) {
	t.Helper()

	store, err := content.New(root, nil)
	if err != nil {
		t.Fatalf("content.New() error: %v", err)
	}

	reader, _, err := store.Acquire("identifier", dgst, fetch)
	if err != nil {
		_ = store.Close()

		t.Fatalf("Acquire() error: %v", err)
	}

	_, err = io.Copy(io.Discard, reader)

	_ = reader.Close()

	if closeErr := store.Close(); closeErr != nil {
		t.Fatalf("Close() error: %v", closeErr)
	}

	if err != nil {
		t.Fatalf("read error: %v", err)
	}
}

// persistedValue returns the value of the single record in root's index.
func persistedValue(t *testing.T, root string) []byte {
	t.Helper()

	idx, err := index.New(filepath.Join(root, "index.dat"), &index.Options{ValSize: 1 + digest.MaxDigestSize})
	if err != nil {
		t.Fatalf("index.New() error: %v", err)
	}

	defer func() { _ = idx.Close() }()

	var values [][]byte

	err = idx.ForEach(func(rec index.Record) bool {
		values = append(values, bytes.Clone(rec.Value))

		return true
	})
	if err != nil {
		t.Fatalf("ForEach() error: %v", err)
	}

	if len(values) != 1 {
		t.Fatalf("index holds %d records, want 1", len(values))
	}

	return values[0]
}

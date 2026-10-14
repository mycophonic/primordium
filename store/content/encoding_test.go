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
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/store/content"
	"github.com/mycophonic/primordium/store/index"
)

var errDigestNotResolved = errors.New("persisted digest was not resolved")

// TestIndexRecordAlgorithmIDs pins the on-disk record value: the identifier's
// confirmation, the next 64 bits of its BLAKE2b-256 after the key, then the
// algorithm ID and the raw digest. Changing or reassigning an ID silently
// corrupts every existing index; changing the layout makes New discard it.
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

			want := append(confirmationOf("identifier"), tt.id)
			want = append(want, raw...)

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

// valueWidth is the index value width the store sets: the confirmation, the
// algorithm ID and the widest digest.
const valueWidth = 8 + 1 + digest.MaxDigestSize

// confirmationOf is the confirmation the store writes for an identifier:
// bytes 8 to 16 of its BLAKE2b-256, the key being bytes 0 to 8.
func confirmationOf(identifier string) []byte {
	h := digest.BLAKE2b256.Hash()
	_, _ = h.Write([]byte(identifier))

	return h.Sum(nil)[8:16]
}

// persistedRecord returns the single record in root's index, opened with
// width; the store must be closed.
func persistedRecord(t *testing.T, root string, width int) index.Record {
	t.Helper()

	idx, err := index.New(filepath.Join(root, "index.dat"), &index.Options{ValSize: width})
	if err != nil {
		t.Fatalf("index.New() error: %v", err)
	}

	defer func() { _ = idx.Close() }()

	var records []index.Record

	err = idx.ForEach(func(rec index.Record) bool {
		rec.Value = bytes.Clone(rec.Value)
		records = append(records, rec)

		return true
	})
	if err != nil {
		t.Fatalf("ForEach() error: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("index holds %d records, want 1", len(records))
	}

	return records[0]
}

// persistedValue returns the value of the single record in root's index.
func persistedValue(t *testing.T, root string) []byte {
	t.Helper()

	return persistedRecord(t, root, valueWidth).Value
}

// TestAnotherIdentifiersRecordIsAMiss: a record under the identifier's key
// whose confirmation is another's, as a 64-bit collision leaves, is a miss,
// fetched again and written over, never the other identifier's content.
func TestAnotherIdentifiersRecordIsAMiss(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := []byte("content of the identifier")

	acquire(t, root, nil, fetchFunc(data))

	// The record as the store wrote it, then with another confirmation.
	rec := persistedRecord(t, root, valueWidth)
	rec.Value[0] ^= 0xff

	idx, err := index.New(filepath.Join(root, "index.dat"), &index.Options{ValSize: valueWidth})
	if err != nil {
		t.Fatal(err)
	}

	if err = idx.Put(rec.Key, rec.Value, rec.Timestamp); err != nil {
		t.Fatal(err)
	}

	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}

	var fetched atomic.Int64

	acquire(t, root, nil, countingFetch(data, &fetched))

	if fetched.Load() != 1 {
		t.Fatalf("fetched %d times, want once: the other identifier's record was served", fetched.Load())
	}

	if got := persistedValue(t, root); !bytes.HasPrefix(got, confirmationOf("identifier")) {
		t.Fatalf("the record was not written over: %x", got)
	}
}

// TestAnIndexOfAnotherFormatIsDiscarded: a store opens over an index of
// another value width, which it drops, and works as if the index were new.
func TestAnIndexOfAnotherFormatIsDiscarded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "index.dat")

	idx, err := index.New(path, &index.Options{ValSize: 1 + digest.MaxDigestSize})
	if err != nil {
		t.Fatal(err)
	}

	if err = idx.Put(1, make([]byte, 1+digest.MaxDigestSize), 1); err != nil {
		t.Fatal(err)
	}

	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}

	var fetched atomic.Int64

	data := []byte("content after the format change")
	acquire(t, root, nil, countingFetch(data, &fetched))
	acquire(t, root, nil, countingFetch(data, &fetched))

	if fetched.Load() != 1 {
		t.Fatalf("fetched %d times, want once then a hit", fetched.Load())
	}

	if _, err = os.Stat(path + ".journal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a journal of the discarded index remains: %v", err)
	}
}

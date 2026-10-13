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

package index_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/store/index"
)

// FuzzJournal: whatever bytes a grow journal holds, New either recovers or
// ignores it and opens the index; it never panics, and a record put before
// the journal appeared is readable or the open fails cleanly.
func FuzzJournal(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("short"))
	f.Add(make([]byte, 24))
	f.Add(append(bytes.Repeat([]byte{0xff}, 16), 0, 65, 0, 0, 0, 0, 0, 0))

	f.Fuzz(func(t *testing.T, journal []byte) {
		path := filepath.Join(t.TempDir(), "test.idx")

		idx, err := index.New(path, &index.Options{InitialCap: 16})
		if err != nil {
			t.Fatal(err)
		}

		if err = idx.Put(7, testValue(7), 70); err != nil {
			t.Fatal(err)
		}

		if err = idx.Close(); err != nil {
			t.Fatal(err)
		}

		if err = filesystem.WriteFile(path+".journal", journal, 0o600); err != nil {
			t.Fatal(err)
		}

		idx, err = index.New(path, &index.Options{InitialCap: 16})
		if err != nil {
			// A journal that parses but describes another index is refused;
			// that is a clean failure, not a corruption.
			return
		}

		defer func() { _ = idx.Close() }()

		rec, found, err := idx.Get(7)
		if err != nil {
			t.Fatal(err)
		}

		// A valid journal rebuilds the table from its own records, which may
		// not hold key 7; an ignored one leaves the table as it was.
		if found && (rec.Timestamp != 70 || !bytes.Equal(rec.Value, testValue(7))) {
			t.Fatalf("key 7 came back as %+v", rec)
		}

		if _, err := idx.Len(); err != nil {
			t.Fatal(err)
		}
	})
}

// FuzzPutGet: any key and any value up to the width round-trip, a value
// shorter than the width comes back zero-padded, and a longer one is refused.
func FuzzPutGet(f *testing.F) {
	f.Add(uint64(0), []byte{1}, int64(0))
	f.Add(^uint64(0), bytes.Repeat([]byte{0xab}, 65), int64(-1))
	f.Add(uint64(1024), []byte("x"), int64(1<<62))

	f.Fuzz(func(t *testing.T, key uint64, value []byte, timestamp int64) {
		idx := openTestIndex(t, &index.Options{InitialCap: 8})

		err := idx.Put(key, value, timestamp)

		switch {
		case len(value) == 0 || len(value) > 65:
			if err == nil {
				t.Fatalf("Put accepted a %d-byte value", len(value))
			}

			return
		case err != nil:
			t.Fatal(err)
		}

		rec, found, err := idx.Get(key)
		if err != nil || !found {
			t.Fatalf("Get(%d): found %v, %v", key, found, err)
		}

		want := make([]byte, 65)
		copy(want, value)

		if !bytes.Equal(rec.Value, want) || rec.Timestamp != timestamp || rec.Key != key {
			t.Fatalf("Get(%d) = %+v, want value %x at %d", key, rec, want, timestamp)
		}

		if deleted, err := idx.Delete(key); err != nil || !deleted {
			t.Fatalf("Delete(%d): %v %v", key, deleted, err)
		}

		if _, found, err := idx.Get(key); err != nil || found {
			t.Fatalf("Get after Delete: found %v, %v", found, err)
		}
	})
}

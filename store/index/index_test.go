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

//revive:disable:add-constant
package index_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/index"
)

// testValue returns a deterministic 65-byte value from a seed.
// Byte 0 is seed mod 5 + 1, bytes 1..8 hold the seed big-endian,
// remainder is zero-padded.
func testValue(seed uint64) []byte {
	val := make([]byte, 65)
	val[0] = uint8(seed%5 + 1)
	binary.BigEndian.PutUint64(val[1:9], seed)

	return val
}

func openTestIndex(t *testing.T, opts *index.Options) (*index.Index, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.idx")

	idx, err := index.New(path, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { idx.Close() })

	return idx, path
}

// seedIndex inserts n records with key=i, value derived from i*7, timestamp=i.
func seedIndex(t *testing.T, idx *index.Index, n uint64) {
	t.Helper()

	for i := range n {
		val := testValue(i * 7)
		if err := idx.Put(i, val, int64(i)); err != nil {
			t.Fatalf("seed put %d: %v", i, err)
		}
	}
}

// verifyRecords checks that keys 0..n-1 exist with the expected value and timestamp.
func verifyRecords(t *testing.T, idx *index.Index, n uint64) {
	t.Helper()

	for i := range n {
		rec, found, err := idx.Get(i)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}

		if !found {
			t.Fatalf("key %d not found", i)
		}

		wantVal := testValue(i * 7)

		if !bytes.Equal(rec.Value, wantVal) {
			t.Fatalf("key %d: value mismatch", i)
		}

		if rec.Timestamp != int64(i) {
			t.Fatalf("key %d: timestamp = %d, want %d", i, rec.Timestamp, int64(i))
		}
	}
}

// --- Grow correctness ---

// TestGrowPreservesRecords fills past the load factor threshold to trigger
// multiple doublings (64→128→256→512), then verifies every record survives.
func TestGrowPreservesRecords(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	const n = 200
	seedIndex(t, idx, n)
	verifyRecords(t, idx, n)

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != n {
		t.Fatalf("len = %d, want %d", count, n)
	}

	capVal, err := idx.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal <= 64 {
		t.Fatalf("cap = %d, expected growth beyond initial 64", capVal)
	}
}

// TestGrowReopenPreservesData verifies records persist across close/reopen
// after one or more grow operations.
func TestGrowReopenPreservesData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	idx, err := index.New(path, &index.Options{InitialCap: 64})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	const n = 200
	seedIndex(t, idx, n)

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	verifyRecords(t, idx2, n)

	count, err := idx2.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != n {
		t.Fatalf("len = %d, want %d", count, n)
	}
}

// TestGrowWithTombstones triggers a grow via tombstone pressure (deleted
// records still occupy slots), then verifies tombstones are eliminated
// and only live records survive.
func TestGrowWithTombstones(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	// Fill to 40 records (below 64*0.70 = 44.8 threshold).
	for i := range uint64(40) {
		val := testValue(i)
		if err := idx.Put(i, val, 1); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	// Delete all — creates 40 tombstones.
	for i := range uint64(40) {
		deleted, err := idx.Delete(i)
		if err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}

		if !deleted {
			t.Fatalf("key %d: expected deleted", i)
		}
	}

	// Insert keys whose hash buckets (key % 64) fall outside the tombstone
	// region (buckets 0-39). Keys 1000..1005 hash to buckets 40..45 — no
	// tombstone reuse, so Count + Tombstones grows with each insert.
	// Before the 5th insert: (4 + 40 + 1) / 64 = 0.703 > 0.70 → grow.
	const liveBase = uint64(1000)

	for i := range uint64(6) {
		key := liveBase + i
		val := testValue(key * 5)

		if err := idx.Put(key, val, int64(key)); err != nil {
			t.Fatalf("put %d: %v", key, err)
		}
	}

	capVal, err := idx.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal != 128 {
		t.Fatalf("cap = %d, want 128", capVal)
	}

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 6 {
		t.Fatalf("len = %d, want 6", count)
	}

	// Deleted keys must not be found.
	for i := range uint64(40) {
		_, found, err := idx.Get(i)
		if err != nil {
			t.Fatalf("get deleted %d: %v", i, err)
		}

		if found {
			t.Fatalf("deleted key %d found after grow", i)
		}
	}

	// Live keys must be found with correct values.
	for i := range uint64(6) {
		key := liveBase + i
		wantVal := testValue(key * 5)

		rec, found, err := idx.Get(key)
		if err != nil {
			t.Fatalf("get %d: %v", key, err)
		}

		if !found {
			t.Fatalf("key %d not found", key)
		}

		if !bytes.Equal(rec.Value, wantVal) {
			t.Fatalf("key %d: value mismatch", key)
		}
	}
}

// TestGrowMaxCapExceeded verifies that a Put which would trigger a grow
// beyond MaxCap returns index.ErrCapacityExceeded.
func TestGrowMaxCapExceeded(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64, MaxCap: 64})

	// Fill to threshold. 64 * 0.70 = 44.8, so the 45th insert triggers grow.
	for i := range uint64(44) {
		val := testValue(i)
		if err := idx.Put(i, val, 1); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	val := testValue(44)

	err := idx.Put(44, val, 1)
	if err == nil {
		t.Fatal("expected error from put that triggers grow beyond MaxCap")
	}

	if !errors.Is(err, index.ErrCapacityExceeded) {
		t.Fatalf("expected index.ErrCapacityExceeded, got: %v", err)
	}
}

// TestOpenMaxCapExceededByExistingFile verifies that New returns
// index.ErrCapacityExceeded when the existing file's capacity exceeds MaxCap.
func TestOpenMaxCapExceededByExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	// Create an index with capacity 256 (64 → grow → 128 → grow → 256).
	idx, err := index.New(path, &index.Options{InitialCap: 64})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	seedIndex(t, idx, 100)

	capVal, err := idx.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal <= 64 {
		t.Fatalf("expected capacity growth beyond 64, got %d", capVal)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen with MaxCap smaller than the existing capacity.
	_, err = index.New(path, &index.Options{MaxCap: 64})
	if err == nil {
		t.Fatal("expected error from New with MaxCap < existing capacity")
	}

	if !errors.Is(err, index.ErrCapacityExceeded) {
		t.Fatalf("expected index.ErrCapacityExceeded, got: %v", err)
	}
}

// TestGrowForEachConsistency verifies that ForEach visits exactly Len()
// records after grow, and that every visited record matches the expected data.
func TestGrowForEachConsistency(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	const n = 300
	seedIndex(t, idx, n)

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	// Build expected map from seed parameters.
	type expectation struct {
		Value     []byte
		Timestamp int64
	}

	expected := make(map[uint64]expectation, n)
	for i := range uint64(n) {
		val := testValue(i * 7)
		expected[i] = expectation{Value: val, Timestamp: int64(i)}
	}

	var visited uint64

	if err := idx.ForEach(func(rec index.Record) bool {
		visited++

		want, ok := expected[rec.Key]
		if !ok {
			t.Errorf("foreach returned unexpected key %d", rec.Key)

			return false
		}

		if !bytes.Equal(rec.Value, want.Value) {
			t.Errorf("key %d: value mismatch", rec.Key)

			return false
		}

		if rec.Timestamp != want.Timestamp {
			t.Errorf("key %d: timestamp = %d, want %d", rec.Key, rec.Timestamp, want.Timestamp)

			return false
		}

		delete(expected, rec.Key)

		return true
	}); err != nil {
		t.Fatalf("foreach: %v", err)
	}

	if visited != count {
		t.Fatalf("foreach visited %d, len = %d", visited, count)
	}

	if len(expected) != 0 {
		t.Fatalf("foreach missed %d records", len(expected))
	}
}

// --- Journal recovery ---

// writeTestJournal writes a journal file in the format expected by
// recoverFromJournal: [newCap:8][count:8][valSize:2][reserved:6][records: key:8 value:valSize ts:8 ...].
func writeTestJournal(t *testing.T, path string, newCap uint64, records []index.Record) {
	t.Helper()

	const (
		journalHeaderSize = 24
		valSize           = 65
	)

	journalRecordSize := 8 + valSize + 8

	buf := make([]byte, journalHeaderSize+len(records)*journalRecordSize)
	binary.BigEndian.PutUint64(buf[0:8], newCap)
	binary.BigEndian.PutUint64(buf[8:16], uint64(len(records)))
	binary.BigEndian.PutUint16(buf[16:18], valSize)

	for i, rec := range records {
		off := journalHeaderSize + i*journalRecordSize
		binary.BigEndian.PutUint64(buf[off:off+8], rec.Key)
		copy(buf[off+8:off+8+valSize], rec.Value)
		binary.BigEndian.PutUint64(buf[off+8+valSize:off+8+valSize+8], uint64(rec.Timestamp))
	}

	if err := filesystem.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
}

// TestJournalRecovery simulates a crash mid-grow by leaving a journal file
// and corrupting the data file, then verifies New recovers from the journal.
func TestJournalRecovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")
	journalPath := path + ".journal"

	// Create a valid index with known records.
	idx, err := index.New(path, &index.Options{InitialCap: 64})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	records := make([]index.Record, 30)
	for i := range records {
		val := testValue(uint64(i) * 11)
		records[i] = index.Record{Key: uint64(i), Value: val, Timestamp: int64(i)}

		rec := records[i]
		if err := idx.Put(rec.Key, rec.Value, rec.Timestamp); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Simulate crash: write journal (as growLocked would), then corrupt data file.
	writeTestJournal(t, journalPath, 128, records)

	if err := xos.Truncate(path, 0); err != nil {
		t.Fatalf("truncate data file: %v", err)
	}

	// Reopen — should recover from journal.
	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen with journal: %v", err)
	}
	defer idx2.Close()

	capVal, err := idx2.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal != 128 {
		t.Fatalf("cap = %d, want 128", capVal)
	}

	count, err := idx2.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != uint64(len(records)) {
		t.Fatalf("len = %d, want %d", count, len(records))
	}

	for _, rec := range records {
		got, found, err := idx2.Get(rec.Key)
		if err != nil {
			t.Fatalf("get %d: %v", rec.Key, err)
		}

		if !found {
			t.Fatalf("key %d not found after recovery", rec.Key)
		}

		if !bytes.Equal(got.Value, rec.Value) {
			t.Fatalf("key %d: value mismatch", rec.Key)
		}

		if got.Timestamp != rec.Timestamp {
			t.Fatalf("key %d: timestamp = %d, want %d", rec.Key, got.Timestamp, rec.Timestamp)
		}
	}

	// Journal should have been cleaned up.
	if _, err := xos.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("journal file should have been deleted after recovery")
	}
}

// TestJournalRecoveryEmptyTable verifies recovery from a journal that
// contains zero records (grow triggered by pure tombstone pressure).
func TestJournalRecoveryEmptyTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")
	journalPath := path + ".journal"

	// Create a minimal data file so the lock file sidecar exists.
	idx, err := index.New(path, &index.Options{InitialCap: 64})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Write a journal with zero records but a new capacity.
	writeTestJournal(t, journalPath, 128, nil)

	if err := xos.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	capVal, err := idx2.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal != 128 {
		t.Fatalf("cap = %d, want 128", capVal)
	}

	count, err := idx2.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 0 {
		t.Fatalf("len = %d, want 0", count)
	}
}

// TestJournalCorruptIgnored verifies that a corrupt (truncated) journal is
// deleted and the existing data file is used as-is.
func TestJournalCorruptIgnored(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")
	journalPath := path + ".journal"

	// Create a valid index with one record.
	idx, err := index.New(path, &index.Options{InitialCap: 64})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	val := testValue(100)
	if err := idx.Put(1, val, 1000); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Write a corrupt journal (too short to be valid).
	if err := filesystem.WriteFile(journalPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}

	// Reopen — should ignore corrupt journal, use data file.
	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	rec, found, err := idx2.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key 1 not found after ignoring corrupt journal")
	}

	if !bytes.Equal(rec.Value, val) {
		t.Fatal("value mismatch after ignoring corrupt journal")
	}

	// Corrupt journal should have been deleted.
	if _, err := xos.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("corrupt journal should have been deleted")
	}
}

// TestJournalSizeMismatch verifies that a journal with a valid header but
// wrong record count (header says 10, file has 5 records) is treated as
// corrupt and ignored.
func TestJournalSizeMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")
	journalPath := path + ".journal"

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	val := testValue(42)
	if err := idx.Put(42, val, 42); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Header claims 10 records, but only 5 records worth of data follow.
	const (
		journalHeaderSize = 24
		valSize           = 65
	)

	journalRecordSize := 8 + valSize + 8

	buf := make([]byte, journalHeaderSize+5*journalRecordSize)
	binary.BigEndian.PutUint64(buf[0:8], 128)
	binary.BigEndian.PutUint64(buf[8:16], 10) // lies: says 10, only 5 present
	binary.BigEndian.PutUint16(buf[16:18], valSize)

	if err := filesystem.WriteFile(journalPath, buf, 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}

	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	// Should have used the data file, not the corrupt journal.
	rec, found, err := idx2.Get(42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key 42 not found")
	}

	if !bytes.Equal(rec.Value, val) {
		t.Fatal("value mismatch")
	}
}

// --- Concurrent access ---

// TestConcurrentReadWriteGrow exercises concurrent Put (triggering multiple
// grows) and Get operations under the race detector.
func TestConcurrentReadWriteGrow(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	const (
		numWriters    = 4
		numReaders    = 4
		keysPerWriter = 2000 // 64→128→256→...→8192+
	)

	var writers sync.WaitGroup

	// Writers: each owns a disjoint key range to avoid update contention.
	for w := range numWriters {
		writers.Go(func() {
			base := uint64(w) * keysPerWriter
			for i := range uint64(keysPerWriter) {
				key := base + i
				val := testValue(key * 7)

				if err := idx.Put(key, val, int64(key)); err != nil {
					t.Errorf("writer %d put %d: %v", w, key, err)

					return
				}
			}
		})
	}

	// Readers: continuously Get while writers are active.
	stop := make(chan struct{})

	var readers sync.WaitGroup

	for r := range numReaders {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}

				key := uint64(r) * keysPerWriter

				_, _, err := idx.Get(key)
				if err != nil {
					t.Errorf("reader %d get %d: %v", r, key, err)

					return
				}
			}
		})
	}

	writers.Wait()
	close(stop)
	readers.Wait()

	// All writes landed — verify final state.
	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	expected := uint64(numWriters * keysPerWriter)
	if count != expected {
		t.Fatalf("len = %d, want %d", count, expected)
	}

	for w := range numWriters {
		base := uint64(w) * keysPerWriter
		for i := range uint64(keysPerWriter) {
			key := base + i
			wantVal := testValue(key * 7)

			rec, found, err := idx.Get(key)
			if err != nil {
				t.Fatalf("verify get %d: %v", key, err)
			}

			if !found {
				t.Fatalf("key %d not found", key)
			}

			if !bytes.Equal(rec.Value, wantVal) {
				t.Fatalf("key %d: value mismatch", key)
			}
		}
	}
}

// TestConcurrentDeleteDuringGrow exercises concurrent Delete, Put, and
// ForEach while grow operations are being triggered.
func TestConcurrentDeleteDuringGrow(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	// Pre-fill with keys 0..499.
	const prefill = 500
	for i := range uint64(prefill) {
		val := testValue(i)
		if err := idx.Put(i, val, int64(i)); err != nil {
			t.Fatalf("prefill %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup

	// Deleter: remove even keys.
	wg.Go(func() {
		for i := uint64(0); i < prefill; i += 2 {
			if _, err := idx.Delete(i); err != nil {
				t.Errorf("delete %d: %v", i, err)

				return
			}
		}
	})

	// Writer: insert new keys (triggers further grows).
	wg.Go(func() {
		for i := uint64(prefill); i < prefill+2000; i++ {
			val := testValue(i * 3)
			if err := idx.Put(i, val, int64(i)); err != nil {
				t.Errorf("put %d: %v", i, err)

				return
			}
		}
	})

	// Iterator: repeated ForEach.
	wg.Go(func() {
		for range 50 {
			if err := idx.ForEach(func(_ index.Record) bool {
				return true
			}); err != nil {
				t.Errorf("foreach: %v", err)

				return
			}
		}
	})

	wg.Wait()

	// Verify consistency: ForEach count must match Len.
	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	var forEachCount uint64

	if err := idx.ForEach(func(_ index.Record) bool {
		forEachCount++

		return true
	}); err != nil {
		t.Fatalf("final foreach: %v", err)
	}

	if forEachCount != count {
		t.Fatalf("ForEach count = %d, Len = %d", forEachCount, count)
	}

	// All new keys (prefill..prefill+2000) must exist.
	for i := uint64(prefill); i < prefill+2000; i++ {
		wantVal := testValue(i * 3)

		rec, found, err := idx.Get(i)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}

		if !found {
			t.Fatalf("key %d not found", i)
		}

		if !bytes.Equal(rec.Value, wantVal) {
			t.Fatalf("key %d: value mismatch", i)
		}
	}

	// Even keys 0..498 must have been deleted (odd keys survive).
	for i := uint64(1); i < prefill; i += 2 {
		_, found, err := idx.Get(i)
		if err != nil {
			t.Fatalf("get odd %d: %v", i, err)
		}

		if !found {
			t.Fatalf("odd key %d not found", i)
		}
	}
}

// TestConcurrentGrowContention has multiple goroutines all racing to insert
// into a tiny table, causing frequent grow contention on the write lock.
func TestConcurrentGrowContention(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 16})

	const (
		numGoroutines = 8
		keysEach      = 500
	)

	var wg sync.WaitGroup

	for g := range numGoroutines {
		wg.Go(func() {
			base := uint64(g) * keysEach
			for i := range uint64(keysEach) {
				key := base + i
				val := testValue(key)

				if err := idx.Put(key, val, int64(key)); err != nil {
					t.Errorf("goroutine %d put %d: %v", g, key, err)

					return
				}
			}
		})
	}

	wg.Wait()

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	expected := uint64(numGoroutines * keysEach)
	if count != expected {
		t.Fatalf("len = %d, want %d", count, expected)
	}

	// Spot-check records from each goroutine.
	for g := range numGoroutines {
		base := uint64(g) * keysEach
		key := base + keysEach/2
		wantVal := testValue(key)

		rec, found, err := idx.Get(key)
		if err != nil {
			t.Fatalf("get %d: %v", key, err)
		}

		if !found {
			t.Fatalf("key %d not found", key)
		}

		if !bytes.Equal(rec.Value, wantVal) {
			t.Fatalf("key %d: value mismatch", key)
		}
	}
}

// --- Basic CRUD ---

// TestPutGetRoundtrip verifies simple put/get without triggering a grow.
func TestPutGetRoundtrip(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	val := testValue(42)
	if err := idx.Put(1, val, 1000); err != nil {
		t.Fatalf("put: %v", err)
	}

	rec, found, err := idx.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key 1 not found")
	}

	if !bytes.Equal(rec.Value, val) {
		t.Fatal("value mismatch")
	}

	if rec.Key != 1 {
		t.Fatalf("key = %d, want 1", rec.Key)
	}

	if rec.Timestamp != 1000 {
		t.Fatalf("timestamp = %d, want 1000", rec.Timestamp)
	}
}

// TestPutUpdate verifies that putting the same key twice updates the value
// and timestamp.
func TestPutUpdate(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	val1 := testValue(1)
	if err := idx.Put(10, val1, 100); err != nil {
		t.Fatalf("put 1: %v", err)
	}

	val2 := testValue(2)
	if err := idx.Put(10, val2, 200); err != nil {
		t.Fatalf("put 2: %v", err)
	}

	rec, found, err := idx.Get(10)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key 10 not found")
	}

	if !bytes.Equal(rec.Value, val2) {
		t.Fatal("value not updated")
	}

	if rec.Timestamp != 200 {
		t.Fatalf("timestamp = %d, want 200", rec.Timestamp)
	}

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 1 {
		t.Fatalf("len = %d, want 1 (update should not create duplicate)", count)
	}
}

// TestDeleteThenGet verifies that a deleted key is no longer found.
func TestDeleteThenGet(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	val := testValue(7)
	if err := idx.Put(5, val, 50); err != nil {
		t.Fatalf("put: %v", err)
	}

	deleted, err := idx.Delete(5)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	if !deleted {
		t.Fatal("delete returned false for existing key")
	}

	_, found, err := idx.Get(5)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}

	if found {
		t.Fatal("key 5 found after delete")
	}

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 0 {
		t.Fatalf("len = %d, want 0", count)
	}
}

// TestDeleteNonExistent verifies that deleting a key that was never inserted
// returns false without error.
func TestDeleteNonExistent(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	deleted, err := idx.Delete(999)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	if deleted {
		t.Fatal("delete returned true for non-existent key")
	}
}

// TestGetNonExistent verifies that Get for a missing key returns false
// without error.
func TestGetNonExistent(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	_, found, err := idx.Get(12345)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if found {
		t.Fatal("found non-existent key")
	}
}

// TestKeyZero verifies that key 0 is a valid key (not confused with the
// empty status byte).
func TestKeyZero(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	val := testValue(0)
	if err := idx.Put(0, val, 999); err != nil {
		t.Fatalf("put key 0: %v", err)
	}

	rec, found, err := idx.Get(0)
	if err != nil {
		t.Fatalf("get key 0: %v", err)
	}

	if !found {
		t.Fatal("key 0 not found")
	}

	if !bytes.Equal(rec.Value, val) {
		t.Fatal("key 0: value mismatch")
	}

	if rec.Timestamp != 999 {
		t.Fatalf("key 0: timestamp = %d, want 999", rec.Timestamp)
	}
}

// TestTombstoneReuse verifies that a Put reuses a tombstone slot left by
// a prior Delete for the same hash bucket.
func TestTombstoneReuse(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 64})

	val1 := testValue(1)
	if err := idx.Put(7, val1, 10); err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, err := idx.Delete(7); err != nil {
		t.Fatalf("delete: %v", err)
	}

	val2 := testValue(2)
	if err := idx.Put(7, val2, 20); err != nil {
		t.Fatalf("re-put: %v", err)
	}

	rec, found, err := idx.Get(7)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key 7 not found after re-insert")
	}

	if !bytes.Equal(rec.Value, val2) {
		t.Fatal("value mismatch after re-insert")
	}

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 1 {
		t.Fatalf("len = %d, want 1", count)
	}
}

// --- Value boundary conditions ---

// TestPutEmptyValue verifies that an empty value is rejected.
func TestPutEmptyValue(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	err := idx.Put(1, nil, 1)
	if err == nil {
		t.Fatal("expected error for nil value")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}

	err = idx.Put(1, []byte{}, 1)
	if err == nil {
		t.Fatal("expected error for empty value")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestPutOversizedValue verifies that a value exceeding valSize is rejected.
func TestPutOversizedValue(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil) // default valSize = 65

	oversized := make([]byte, 66)
	oversized[0] = 1

	err := idx.Put(1, oversized, 1)
	if err == nil {
		t.Fatal("expected error for oversized value")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestPutExactSizeValue verifies that a value exactly valSize bytes is accepted.
func TestPutExactSizeValue(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil) // default valSize = 65

	exact := make([]byte, 65)
	exact[0] = 1

	if err := idx.Put(1, exact, 1); err != nil {
		t.Fatalf("put exact-size value: %v", err)
	}

	rec, found, err := idx.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("not found")
	}

	if !bytes.Equal(rec.Value, exact) {
		t.Fatal("value mismatch")
	}
}

// TestPutShortValueZeroPadded verifies that a value shorter than valSize
// is zero-padded and readable.
func TestPutShortValueZeroPadded(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil) // default valSize = 65

	short := []byte{0x02, 0xAA, 0xBB}
	if err := idx.Put(1, short, 1); err != nil {
		t.Fatalf("put short value: %v", err)
	}

	rec, found, err := idx.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("not found")
	}

	if len(rec.Value) != 65 {
		t.Fatalf("value length = %d, want 65", len(rec.Value))
	}

	// First 3 bytes match, rest is zero.
	if !bytes.Equal(rec.Value[:3], short) {
		t.Fatal("leading bytes mismatch")
	}

	for i := 3; i < 65; i++ {
		if rec.Value[i] != 0 {
			t.Fatalf("byte %d = %#x, want 0", i, rec.Value[i])
		}
	}
}

// --- Options validation ---

// TestOpenInvalidValSize verifies that New rejects out-of-range valSize.
func TestOpenInvalidValSize(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.idx")

	_, err := index.New(path, &index.Options{ValSize: -1})
	if err == nil {
		t.Fatal("expected error for negative valSize")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestOpenValSizeMismatch verifies that reopening an index with a different
// valSize returns an error.
func TestOpenValSizeMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	// Create with valSize=33.
	idx, err := index.New(path, &index.Options{ValSize: 33})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen with different valSize.
	_, err = index.New(path, &index.Options{ValSize: 65})
	if err == nil {
		t.Fatal("expected error for valSize mismatch")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestOpenInitialCapExceedsMaxCap verifies that New rejects InitialCap > MaxCap.
func TestOpenInitialCapExceedsMaxCap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.idx")

	_, err := index.New(path, &index.Options{InitialCap: 2048, MaxCap: 1024})
	if err == nil {
		t.Fatal("expected error for InitialCap > MaxCap")
	}

	if !errors.Is(err, index.ErrCapacityExceeded) {
		t.Fatalf("expected ErrCapacityExceeded, got: %v", err)
	}
}

// --- Error paths ---

// TestOpenCorruptMagic verifies that New rejects a data file with wrong magic.
func TestOpenCorruptMagic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	// Create a valid index, close it, then corrupt the magic.
	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := xos.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}

	// Write garbage magic at offset 0.
	if _, err := f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 0); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	_, err = index.New(path, nil)
	if err == nil {
		t.Fatal("expected error for corrupt magic")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestOpenCorruptVersion verifies that New rejects a data file with wrong version.
func TestOpenCorruptVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	f, err := xos.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}

	// Write version 99 at offset 4.
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], 99)

	if _, err := f.WriteAt(buf[:], 4); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	_, err = index.New(path, nil)
	if err == nil {
		t.Fatal("expected error for wrong version")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// TestOpenFileTooSmall verifies that New rejects a data file smaller than
// the header.
func TestOpenFileTooSmall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	// Create a valid index so the lock file exists.
	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Truncate data file to less than header size (48 bytes).
	if err := xos.Truncate(path, 16); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	_, err = index.New(path, nil)
	if err == nil {
		t.Fatal("expected error for file too small")
	}
}

// --- ForEach edge cases ---

// TestForEachEmptyIndex verifies ForEach on an empty index visits no records.
func TestForEachEmptyIndex(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	var visited int

	err := idx.ForEach(func(_ index.Record) bool {
		visited++

		return true
	})
	if err != nil {
		t.Fatalf("foreach: %v", err)
	}

	if visited != 0 {
		t.Fatalf("visited %d records on empty index", visited)
	}
}

// TestForEachEarlyStop verifies that ForEach stops when the callback
// returns false.
func TestForEachEarlyStop(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	for i := range uint64(10) {
		val := testValue(i)
		if err := idx.Put(i, val, int64(i)); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	var visited int

	err := idx.ForEach(func(_ index.Record) bool {
		visited++

		return visited < 3
	})
	if err != nil {
		t.Fatalf("foreach: %v", err)
	}

	if visited != 3 {
		t.Fatalf("visited = %d, want 3", visited)
	}
}

// --- Len/Cap on fresh index ---

// TestLenCapFreshIndex verifies Len and Cap on a newly created index.
func TestLenCapFreshIndex(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, &index.Options{InitialCap: 256})

	count, err := idx.Len()
	if err != nil {
		t.Fatalf("len: %v", err)
	}

	if count != 0 {
		t.Fatalf("len = %d, want 0", count)
	}

	capVal, err := idx.Cap()
	if err != nil {
		t.Fatalf("cap: %v", err)
	}

	if capVal != 256 {
		t.Fatalf("cap = %d, want 256", capVal)
	}
}

// --- Sync ---

// TestSync verifies that Sync completes without error.
func TestSync(t *testing.T) {
	t.Parallel()

	idx, _ := openTestIndex(t, nil)

	val := testValue(1)
	if err := idx.Put(1, val, 1); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := idx.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// --- Close idempotency ---

// TestDoubleClose verifies that calling Close twice does not panic.
func TestDoubleClose(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Second close — should not panic. May return error for already-closed
	// file handles, which is acceptable.
	_ = idx.Close()
}

// --- Stale writer recovery ---

// TestStaleWriterRecovery simulates a crashed writer process that left the
// write flag set in the lock word. A reader must detect the stale PID and
// CAS-reset the lock word to acquire a read lock.
func TestStaleWriterRecovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	val := testValue(1)
	if err := idx.Put(1, val, 100); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Simulate a stale writer: set write flag + a dead PID in the lock word.
	// PID 2 is typically a kernel thread (not a user process), so
	// isProcessAlive(2) returns false for non-root users on most systems.
	// Use a high PID that is almost certainly not alive.
	deadPID := uint64(4_000_000)
	lockWord := (uint64(1) << 63) | (deadPID << 32)

	dataFile, err := xos.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open data file: %v", err)
	}

	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], lockWord)

	if _, err := dataFile.WriteAt(buf[:], 40); err != nil {
		t.Fatalf("write lock word: %v", err)
	}

	if err := dataFile.Close(); err != nil {
		t.Fatalf("close data file: %v", err)
	}

	// Reopen.
	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	// Get from a goroutine with timeout. The reader will spin, detect the
	// stale writer PID, CAS-reset, and acquire the read lock.
	type result struct {
		rec   index.Record
		found bool
		err   error
	}

	done := make(chan result, 1)

	go func() {
		rec, found, err := idx2.Get(1)
		done <- result{rec, found, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("get after stale writer: %v", r.err)
		}

		if !r.found {
			t.Fatal("key 1 not found")
		}

		if r.rec.Timestamp != 100 {
			t.Fatalf("timestamp = %d, want 100", r.rec.Timestamp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("get timed out — stale writer recovery did not trigger")
	}
}

// --- Stale reader recovery ---

// TestStaleReaderRecovery simulates a crashed reader process that left a
// stale reader count in the cross-process lock word. The writer must detect
// that no alive PIDs are registered in the lock file and CAS-reset the
// stale count to acquire the write lock.
func TestStaleReaderRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short")
	}

	// The race detector's shadow memory does not track munmap/mmap cycles.
	// This test remaps the index file during stale lock recovery, which can
	// reuse the same virtual address — causing the race detector to SIGSEGV
	// on atomic operations against the new mapping.
	if raceEnabled {
		t.Skip("skipping under -race: mmap remap triggers race detector SIGSEGV")
	}

	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx")

	// Create a valid index with one record.
	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	val := testValue(1)
	if err := idx.Put(1, val, 100); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := idx.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Simulate a stale reader: write a reader count of 1 into the lock
	// word at offset 40 of the data file. The lock file PID slots are all
	// zero (cleared by Close), so allReaderPIDsDead will return true.
	dataFile, err := xos.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open data file: %v", err)
	}

	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], 1) // reader count = 1, no write flag

	if _, err := dataFile.WriteAt(buf[:], 40); err != nil {
		t.Fatalf("write lock word: %v", err)
	}

	if err := dataFile.Close(); err != nil {
		t.Fatalf("close data file: %v", err)
	}

	// Reopen the index.
	idx2, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()

	// Put from a goroutine with timeout. The writer will spin until it
	// hits stalePIDThreshold, detect no alive reader PIDs, and CAS-reset
	// the stale count to acquire the write lock.
	done := make(chan error, 1)

	go func() {
		val2 := testValue(2)
		done <- idx2.Put(2, val2, 200)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("put after stale reader: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("put timed out — stale reader recovery did not trigger")
	}

	// Verify both records are readable.
	rec, found, err := idx2.Get(1)
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}

	if !found {
		t.Fatal("key 1 not found")
	}

	if rec.Timestamp != 100 {
		t.Fatalf("key 1: timestamp = %d, want 100", rec.Timestamp)
	}

	rec, found, err = idx2.Get(2)
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}

	if !found {
		t.Fatal("key 2 not found")
	}

	if rec.Timestamp != 200 {
		t.Fatalf("key 2: timestamp = %d, want 200", rec.Timestamp)
	}
}

func TestNewCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "dirs", "test.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatalf("New with non-existent parent dirs: %v", err)
	}

	defer idx.Close()

	// Verify the index is functional.
	val := testValue(42)
	if err := idx.Put(42, val, 100); err != nil {
		t.Fatalf("put: %v", err)
	}

	rec, found, err := idx.Get(42)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !found {
		t.Fatal("key not found after put")
	}

	if rec.Timestamp != 100 {
		t.Fatalf("timestamp = %d, want 100", rec.Timestamp)
	}
}

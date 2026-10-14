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

// The cross-process contract, from index's docs: the index is safe across
// processes, and a writer or a reader that dies is detected by its PID's
// liveness. A PID the kernel has handed to another process since the holder
// died is not the holder: a live PID with another start time is taken over,
// as a writer and as a reader.

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/index"
)

var errNotReadBack = errors.New("the record was not read back")

// livePID is a PID that is alive for the test's duration, is not this
// process, and whose start time this process may read on every platform:
// its parent's. PID 1's start is hidden from other users on some Linux
// hosts, and a start that cannot be read counts as the holder's own.
func livePID() uint64 {
	return uint64(os.Getppid())
}

// writeAt writes a native-endian word into a file, as the index reads it
// through atomics.
func writeAt(t *testing.T, path string, offset int64, word any) {
	t.Helper()

	file, err := xos.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}

	var buf []byte

	switch w := word.(type) {
	case uint64:
		buf = binary.NativeEndian.AppendUint64(nil, w)
	case uint32:
		buf = binary.NativeEndian.AppendUint32(nil, w)
	default:
		t.Fatalf("word of type %T", word)
	}

	if _, err = file.WriteAt(buf, offset); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

// closedIndex is a new index at path with one record, closed.
func closedIndex(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.idx")

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err = idx.Put(1, testValue(1), 100); err != nil {
		t.Fatal(err)
	}

	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}

	return path
}

// within runs operation and fails unless it returns inside the limit.
func within(t *testing.T, limit time.Duration, what string, operation func() error) {
	t.Helper()

	done := make(chan error, 1)

	go func() { done <- operation() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	case <-time.After(limit):
		t.Fatalf("%s did not return within %v", what, limit)
	}
}

// TestAReusedPIDIsNotTheWriter: a lock word naming a live PID with a start
// time that is not that process's is a dead writer's, and is taken over.
// TestLiveWriterIsNotTakenOver holds the other half: the same PID with no
// start recorded keeps the lock. The lock file holds the writer's start at
// 256, past the 64 four-byte reader slots, and the readers' at 264 on.
func TestAReusedPIDIsNotTheWriter(t *testing.T) {
	t.Parallel()

	path := closedIndex(t)

	writeAt(t, path, 40, (uint64(1)<<63)|(livePID()<<32))
	writeAt(t, path+".lock", 256, uint64(1))

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Closed only once the operation has returned: a Close under a spinning
	// Get unmaps the word it spins on.
	within(t, 30*time.Second, "Get past a reused writer PID", func() error {
		rec, found, getErr := idx.Get(1)
		if getErr == nil && (!found || rec.Timestamp != 100) {
			return errNotReadBack
		}

		return getErr
	})

	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestAReusedPIDIsNotAReader: a reader slot naming a live PID with another
// start time, and a reader count of one, is a dead reader's; a writer takes
// the lock.
func TestAReusedPIDIsNotAReader(t *testing.T) {
	t.Parallel()

	path := closedIndex(t)

	writeAt(t, path, 40, uint64(1))
	writeAt(t, path+".lock", 0, uint32(livePID()))
	writeAt(t, path+".lock", 264, uint64(1))

	idx, err := index.New(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	within(t, 30*time.Second, "Put past a reused reader PID", func() error {
		return idx.Put(2, testValue(2), 200)
	})

	if err = idx.Close(); err != nil {
		t.Fatal(err)
	}
}

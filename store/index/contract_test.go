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
// processes, a writer that dies is detected by its PID's liveness and its
// lock taken over, and a grow it died in is recovered from its journal:
//   - a peer holding the index open when the writer dies mid-grow reads
//     every record the writer had, through its own handle and through
//     another it holds, one of them waiting on the lock through the
//     rebuild, and the journal is gone after;
//   - a PID the kernel has handed to another process since the holder died
//     is not the holder: a live PID with another start time is taken over,
//     as a writer and as a reader.
//
// The writer that dies is this test binary, told by PRIMORDIUM_INDEX_CHILD
// to grow the index at PRIMORDIUM_INDEX_PATH and killed by the parent once
// the journal exists.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/index"
)

var errNotReadBack = errors.New("the record was not read back")

const (
	childEnv = "PRIMORDIUM_INDEX_CHILD"
	pathEnv  = "PRIMORDIUM_INDEX_PATH"

	// growCap is the table the child fills: large enough that the rebuild
	// takes long enough for the parent to kill the child inside it.
	growCap = 1 << 18
	// growAt is the put that crosses the load factor and starts the grow.
	growAt = growCap*7/10 + 1
)

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "grow":
		os.Exit(grow())
	default:
		os.Exit(2)
	}
}

// grow fills the index to one short of the grow, says "growing", and puts
// the record that grows it; the parent kills it inside the grow.
func grow() int {
	idx, err := index.New(os.Getenv(pathEnv), &index.Options{InitialCap: growCap})
	if err != nil {
		return 3
	}

	for key := uint64(1); key < growAt; key++ {
		if err := idx.Put(key, testValue(key), int64(key)); err != nil {
			return 4
		}
	}

	_, _ = os.Stdout.WriteString("growing\n")

	if err := idx.Put(growAt, testValue(growAt), growAt); err != nil {
		return 5
	}

	_, _ = os.Stdout.WriteString("grown\n")

	return 0
}

// TestADeadWritersGrowIsRecoveredByItsPeer: the parent holds the index open
// twice while a child grows it and is killed inside the grow; both handles
// then read every record the child journaled, and the journal is gone.
func TestADeadWritersGrowIsRecoveredByItsPeer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.idx")

	first, err := index.New(path, &index.Options{InitialCap: growCap})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = first.Close() }()

	second, err := index.New(path, &index.Options{InitialCap: growCap})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = second.Close() }()

	// Both handles have read the table before the child touches it.
	for _, idx := range []*index.Index{first, second} {
		if _, _, err = idx.Get(1); err != nil {
			t.Fatal(err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"=grow", pathEnv+"="+path)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "growing\n" {
		t.Fatalf("child said %q, %v", line, err)
	}

	journal := path + ".journal"
	// The journal appears when the grow starts and goes when it ends: the
	// kill lands between the two, or the test cannot show what it is for.
	deadline := time.Now().Add(10 * time.Second)

	for {
		if _, err = os.Stat(journal); err == nil {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("the child never started its grow")
		}

		time.Sleep(100 * time.Microsecond)
	}

	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	_ = cmd.Wait()

	if _, err = os.Stat(journal); err != nil {
		t.Skip("the grow finished before the kill landed")
	}

	// A peer waiting on the dead writer's word while another takes over:
	// it must wait through the rebuild and read the recovered table.
	waited := make(chan error, 1)

	go func() {
		_, found, getErr := second.Get(growAt - 1)
		if getErr == nil && !found {
			getErr = errNotReadBack
		}

		waited <- getErr
	}()

	for _, idx := range []*index.Index{first, second} {
		for key := uint64(1); key < growAt; key++ {
			var (
				rec   index.Record
				found bool
			)

			rec, found, err = idx.Get(key)
			if err != nil {
				t.Fatalf("Get(%d) after the writer died: %v", key, err)
			}

			if !found || rec.Timestamp != int64(key) {
				t.Fatalf("Get(%d) after the writer died = %v, %t: the journal was not replayed", key, rec, found)
			}
		}

		var n uint64

		if n, err = idx.Len(); err != nil || n != growAt-1 {
			t.Fatalf("Len = %d, %v, want %d", n, err, growAt-1)
		}
	}

	if err = <-waited; err != nil {
		t.Fatalf("the waiting peer: %v", err)
	}

	if _, err = os.Stat(journal); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the journal remains after the recovery: %v", err)
	}
}

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

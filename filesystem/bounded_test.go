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

package filesystem_test

// The bounded check: every starting state, data size and mode, held to the
// contract; then readers racing writers, which may only see whole contents.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// start is a state a write starts from.
type start struct {
	name    string
	succeed *bool // whether the contract says it succeeds; nil when the platform decides
	setup   func(t *testing.T, path string)
}

func starts() []start {
	yes, no := true, false

	file := func(data string, mode os.FileMode) func(*testing.T, string) {
		return func(t *testing.T, path string) {
			t.Helper()

			if err := os.WriteFile(path, []byte(data), mode); err != nil {
				t.Fatal(err)
			}

			// Windows cannot remove a read-only file: give it back write
			// before the temporary directory goes.
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		}
	}

	return []start{
		{"missing", &yes, func(*testing.T, string) {}},
		{"empty", &yes, file("", 0o644)},
		{"holding data", &yes, file("old content", 0o600)},
		{"read-only", nil, file("old content", 0o444)},
		{"a directory", &no, func(t *testing.T, path string) {
			t.Helper()

			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"a directory with an entry", &no, func(t *testing.T, path string) {
			t.Helper()

			if err := os.MkdirAll(filepath.Join(path, "entry"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"in a missing directory", &no, func(*testing.T, string) {}},
	}
}

func TestBoundedWriteFile(t *testing.T) {
	t.Parallel()

	for _, from := range starts() {
		for _, size := range []int{0, 1, 4096, 1 << 20} {
			for _, perm := range []os.FileMode{0o600, 0o644, 0o444, 0o755} {
				dir := t.TempDir()
				if from.name == "in a missing directory" {
					dir = filepath.Join(dir, "missing")
				}

				path := filepath.Join(dir, "target")
				from.setup(t, path)

				before := take(path)
				listed := entries(dir)
				data := bytes.Repeat([]byte{'n'}, size)

				err := filesystem.WriteFile(path, data, perm)
				after := take(path)

				if after.kind == "file" {
					t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
				}

				switch {
				case err == nil && !after.equal(snapshot{kind: "file", data: data, mode: expectedMode(perm)}):
					t.Fatalf("%s, %d bytes, %v: succeeded, but the path holds %s %v %d bytes",
						from.name, size, perm, after.kind, after.mode, len(after.data))
				case err != nil && !errors.Is(err, fault.ErrWriteFailure):
					t.Fatalf("%s, %d bytes, %v: error %v is not ErrWriteFailure", from.name, size, perm, err)
				case err != nil && !after.equal(before):
					t.Fatalf("%s, %d bytes, %v: failed (%v), and the path changed", from.name, size, perm, err)
				case from.succeed != nil && (err == nil) != *from.succeed:
					t.Fatalf(
						"%s, %d bytes, %v: error %v, the contract says succeed=%v",
						from.name,
						size,
						perm,
						err,
						*from.succeed,
					)
				}

				if left := strays(dir, "target", listed); len(left) != 0 {
					t.Fatalf("%s, %d bytes, %v: left %v behind", from.name, size, perm, left)
				}
			}
		}
	}
}

// TestWholeContents: readers racing writes that alternate two contents of
// different lengths see one or the other, whole, every time, and every write
// lands, on Windows too, the readers holding the file through xos.
func TestWholeContents(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "target")
	contents := [][]byte{bytes.Repeat([]byte{'a'}, 10), bytes.Repeat([]byte{'b'}, 70000)}

	if err := filesystem.WriteFile(path, contents[0], 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		wg   sync.WaitGroup
		stop = make(chan struct{})
	)

	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}

				// xos opens with FILE_SHARE_DELETE on Windows, as primordium reads.
				data, err := xos.ReadFile(path)
				if err != nil {
					continue // Windows may refuse a read while the rename lands
				}

				if !bytes.Equal(data, contents[0]) && !bytes.Equal(data, contents[1]) {
					t.Errorf("a reader saw %d bytes, neither content whole", len(data))

					return
				}
			}
		})
	}

	const writes = 200

	landed := 0

	for i := range writes {
		err := filesystem.WriteFile(path, contents[i%2], 0o644)

		switch {
		case err == nil:
			landed++
		case !errors.Is(err, fault.ErrWriteFailure):
			t.Errorf("write %d: error %v is not ErrWriteFailure", i, err)
		}
	}

	close(stop)
	wg.Wait()

	if landed != writes {
		t.Errorf("%d of %d writes landed", landed, writes)
	}
}

// TestHeldTarget: a WriteFile over a file a reader holds open lands, the
// reader keeping the file it opened, on every platform when the reader opened
// it through xos; a reader that opened it through os.Open, with no
// FILE_SHARE_DELETE, makes Windows refuse the rename, so there the write fails
// with ErrWriteFailure and leaves the file whole, and lands once the reader
// lets go.
func TestHeldTarget(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(string) (*os.File, error){"os.Open": os.Open, "xos.Open": xos.Open} {
		path := filepath.Join(t.TempDir(), "target")
		if err := filesystem.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}

		reader, err := open(path)
		if err != nil {
			t.Fatal(err)
		}

		err = filesystem.WriteFile(path, []byte("new"), 0o644)
		refused := runtime.GOOS == "windows" && name == "os.Open"

		switch {
		case !refused && err != nil:
			t.Fatalf("%s held: %v; a rename over an open file lands here", name, err)
		case !refused:
			if got := take(path); !bytes.Equal(got.data, []byte("new")) {
				t.Fatalf("%s held: the write landed as %q", name, got.data)
			}

			if still, readErr := io.ReadAll(reader); readErr != nil || string(still) != "old" {
				t.Fatalf("%s held: the reader reads %q, %v; want the file it opened", name, still, readErr)
			}
		case !errors.Is(err, fault.ErrWriteFailure):
			t.Fatalf("%s held: %v, want ErrWriteFailure", name, err)
		default:
			if got := take(path); !bytes.Equal(got.data, []byte("old")) {
				t.Fatalf("%s held: the refused write left %q", name, got.data)
			}
		}

		if err = reader.Close(); err != nil {
			t.Fatal(err)
		}

		if err = filesystem.WriteFile(path, []byte("after"), 0o644); err != nil {
			t.Fatalf("%s released: %v", name, err)
		}

		if got := take(path); !bytes.Equal(got.data, []byte("after")) {
			t.Fatalf("%s released: the path holds %q", name, got.data)
		}
	}
}

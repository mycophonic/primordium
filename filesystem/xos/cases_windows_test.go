//go:build windows

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

package xos_test

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/mycophonic/primordium/filesystem/xos"
)

// TestFileFlags: the FILE_FLAG_* bits of an open flag reach CreateFile, and
// a bit there that is no such flag is refused, as with os.OpenFile.
func TestFileFlags(t *testing.T) {
	t.Parallel()

	for _, flag := range []int{
		windows.O_FILE_FLAG_SEQUENTIAL_SCAN,
		windows.O_FILE_FLAG_RANDOM_ACCESS,
		windows.O_FILE_FLAG_OVERLAPPED,
		windows.O_FILE_FLAG_WRITE_THROUGH,
		windows.O_FILE_FLAG_DELETE_ON_CLOSE,
		0x00400000, // within the mask, no FILE_FLAG
	} {
		ours, theirs := pair(t, func(t *testing.T, path string) {
			t.Helper()

			if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
		})

		got := openAndUse(xos.OpenFile, ours, os.O_RDWR|flag, 0o644)
		want := openAndUse(os.OpenFile, theirs, os.O_RDWR|flag, 0o644)

		if got != want {
			t.Fatalf("flag %#x: xos %q, os %q", flag, got, want)
		}

		if got, want := onDisk(ours), onDisk(theirs); got != want {
			t.Fatalf("flag %#x: xos left %q, os %q", flag, got, want)
		}
	}
}

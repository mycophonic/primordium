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

package mmap_test

// The bounded check: sizes around a page, a file larger than the mapping,
// two mappings of one file, the file written behind a mapping, and every
// call on a spent mapping.

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/mmap"
)

func sizes() []int {
	page := os.Getpagesize()

	return []int{1, 2, page - 1, page, page + 1, 2*page - 1, 2 * page, 3*page + 7}
}

// TestBoundedMap: for every size, a mapping of a file of exactly that size
// and of a larger file is size bytes long, writes through to the file after
// Sync, and reads what the file is given.
func TestBoundedMap(t *testing.T) {
	t.Parallel()

	for _, size := range sizes() {
		for _, extra := range []int{0, 1, os.Getpagesize()} {
			file := fileOf(t, size+extra)

			mapping, err := mmap.Map(file, size)
			if err != nil {
				t.Fatalf("Map(%d of %d bytes): %v", size, size+extra, err)
			}

			data := mapping.Bytes()
			if len(data) != size {
				t.Fatalf("Map(%d): %d bytes", size, len(data))
			}

			// Through the mapping, then the file: the last byte and the first,
			// one byte when the mapping is one byte long.
			data[size-1] = 'z'
			data[0] = 'a'

			last := byte('z')
			if size == 1 {
				last = 'a'
			}

			if err = mapping.Sync(); err != nil {
				t.Fatalf("Sync after a write of %d bytes: %v", size, err)
			}

			onFile := make([]byte, size)
			if _, err = file.ReadAt(onFile, 0); err != nil {
				t.Fatal(err)
			}

			if onFile[0] != 'a' || onFile[size-1] != last {
				t.Fatalf("size %d: the file holds %q, %q at the ends after Sync", size, onFile[0], onFile[size-1])
			}

			// Through the file, then the mapping.
			if _, err = file.WriteAt([]byte{'f'}, int64(size-1)); err != nil {
				t.Fatal(err)
			}

			if data[size-1] != 'f' {
				t.Fatalf("size %d: the mapping shows %q after the file was written", size, data[size-1])
			}

			if err = mapping.Unmap(); err != nil {
				t.Fatalf("Unmap of %d bytes: %v", size, err)
			}
		}
	}
}

// TestTwoMappings: two mappings of one file see each other's writes at once.
func TestTwoMappings(t *testing.T) {
	t.Parallel()

	size := 2 * os.Getpagesize()
	file := fileOf(t, size)

	first, err := mmap.Map(file, size)
	if err != nil {
		t.Fatal(err)
	}

	second, err := mmap.Map(file, size)
	if err != nil {
		t.Fatal(err)
	}

	copy(first.Bytes()[os.Getpagesize():], "shared")

	if got := second.Bytes()[os.Getpagesize() : os.Getpagesize()+6]; !bytes.Equal(got, []byte("shared")) {
		t.Fatalf("the second mapping shows %q", got)
	}

	for _, mapping := range []*mmap.Mapping{first, second} {
		if err = mapping.Unmap(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestUnsyncedWritesLand: a write never synced is on the file once the
// mapping is gone and the file closed.
func TestUnsyncedWritesLand(t *testing.T) {
	t.Parallel()

	file := fileOf(t, 64)
	path := file.Name()

	mapping, err := mmap.Map(file, 64)
	if err != nil {
		t.Fatal(err)
	}

	copy(mapping.Bytes(), "persistent")

	if err = mapping.Unmap(); err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content[:10], []byte("persistent")) {
		t.Fatalf("the file holds %q, %v", content, err)
	}
}

// TestRefusals: a size below 1, a size past the file, and a closed file.
func TestRefusals(t *testing.T) {
	t.Parallel()

	file := fileOf(t, 10)

	for _, size := range []int{0, -1, 11, 4096} {
		if mapping, err := mmap.Map(file, size); !errors.Is(err, fault.ErrInvalidArgument) || mapping != nil {
			t.Fatalf("Map(%d of 10 bytes) = %v, %v; want ErrInvalidArgument", size, mapping, err)
		}
	}

	closed := fileOf(t, 10)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}

	if mapping, err := mmap.Map(closed, 10); err == nil || mapping != nil {
		t.Fatalf("Map of a closed file = %v, %v; want an error", mapping, err)
	}
}

// TestSpentMapping: after Unmap, Bytes is nil and Sync and Unmap are
// ErrInvalidArgument.
func TestSpentMapping(t *testing.T) {
	t.Parallel()

	mapping, err := mmap.Map(fileOf(t, 10), 10)
	if err != nil {
		t.Fatal(err)
	}

	if err = mapping.Unmap(); err != nil {
		t.Fatal(err)
	}

	if mapping.Bytes() != nil {
		t.Fatal("Bytes is not nil after Unmap")
	}

	if err = mapping.Sync(); !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("Sync after Unmap = %v, want ErrInvalidArgument", err)
	}

	if err = mapping.Unmap(); !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("a second Unmap = %v, want ErrInvalidArgument", err)
	}
}

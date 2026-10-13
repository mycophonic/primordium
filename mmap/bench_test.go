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

import (
	"os"
	"strconv"
	"testing"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/mmap"
)

// The guards: the package exists so an index can address its file as memory.
// What it costs to map a file (every open and every grow), to make the
// written pages durable, and to read or write a record at a random offset
// against doing the same through the file descriptor shows here; a change
// that keeps every test green and puts a copy or a syscall on those paths
// shows as a ratio.

const (
	regionSize = 16 * bytesize.MiB
	recordSize = 64
)

// scatter is the offset of the i-th record, spread over the region the way
// hashed keys are: no two consecutive records share a page.
func scatter(i int) int64 {
	const golden = 11400714819323198485

	records := uint64(regionSize / recordSize)

	return int64((uint64(i)*golden)%records) * recordSize
}

func BenchmarkMap(b *testing.B) {
	for _, size := range []int{os.Getpagesize(), bytesize.MiB, 64 * bytesize.MiB} {
		file := fileOf(b, size)

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for b.Loop() {
				mapping, err := mmap.Map(file, size)
				if err != nil {
					b.Fatal(err)
				}

				if err := mapping.Unmap(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSync(b *testing.B) {
	page := os.Getpagesize()
	mapping := mapped(b, regionSize)
	data := mapping.Bytes()

	// One dirty page, then every page dirty: msync's cost per page written
	// and its floor.
	b.Run("page", func(b *testing.B) {
		b.SetBytes(int64(page))

		for b.Loop() {
			data[0]++

			if err := mapping.Sync(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("all", func(b *testing.B) {
		b.SetBytes(regionSize)

		for b.Loop() {
			for off := 0; off < regionSize; off += page {
				data[off]++
			}

			if err := mapping.Sync(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRandomRead(b *testing.B) {
	file := fileOf(b, regionSize)
	mapping := mapped(b, regionSize, file)
	data := mapping.Bytes()
	record := make([]byte, recordSize)

	b.Run("mapping", func(b *testing.B) {
		b.SetBytes(recordSize)

		for i := 0; b.Loop(); i++ {
			off := scatter(i)
			copy(record, data[off:off+recordSize])
		}
	})

	b.Run("readat", func(b *testing.B) {
		b.SetBytes(recordSize)

		for i := 0; b.Loop(); i++ {
			if _, err := file.ReadAt(record, scatter(i)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRandomWrite(b *testing.B) {
	file := fileOf(b, regionSize)
	mapping := mapped(b, regionSize, file)
	data := mapping.Bytes()
	record := make([]byte, recordSize)

	for i := range record {
		record[i] = byte(i)
	}

	b.Run("mapping", func(b *testing.B) {
		b.SetBytes(recordSize)

		for i := 0; b.Loop(); i++ {
			off := scatter(i)
			copy(data[off:off+recordSize], record)
		}
	})

	b.Run("writeat", func(b *testing.B) {
		b.SetBytes(recordSize)

		for i := 0; b.Loop(); i++ {
			if _, err := file.WriteAt(record, scatter(i)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// mapped maps size bytes of file, or of a fresh file of that size, and
// unmaps when the benchmark ends.
func mapped(b *testing.B, size int, files ...*os.File) *mmap.Mapping {
	b.Helper()

	var file *os.File
	if len(files) > 0 {
		file = files[0]
	} else {
		file = fileOf(b, size)
	}

	mapping, err := mmap.Map(file, size)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = mapping.Unmap() })

	return mapping
}

//go:build unix

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

package mmap

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/mycophonic/primordium/fault"
)

// Mapping holds platform-specific mmap state. Unix needs no extra state.
type Mapping struct{}

// MapFile maps the file into read-write shared memory.
func MapFile(file *os.File, size int) ([]byte, Mapping, error) {
	if size <= 0 {
		return nil, Mapping{}, fmt.Errorf("%w: mmap size must be positive, got %d", fault.ErrInvalidArgument, size)
	}

	//nolint:gosec // G115: fd is a small non-negative int
	data, err := syscall.Mmap(
		int(file.Fd()),
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return nil, Mapping{}, fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return data, Mapping{}, nil
}

// UnmapFile unmaps previously mapped memory.
func UnmapFile(data []byte, _ Mapping) error {
	if len(data) == 0 {
		return nil
	}

	if err := syscall.Munmap(data); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return nil
}

// SyncFile flushes the mapped region to disk.
// On Unix, msync(MS_SYNC) provides full durability; f is unused.
// On Windows, FlushFileBuffers is called after FlushViewOfFile to
// ensure data reaches physical disk, not just the filesystem cache.
func SyncFile(data []byte, _ *os.File) error {
	if len(data) == 0 {
		return nil
	}

	//nolint:gosec // unsafe.Pointer required by msync syscall interface; data is a live mmap'd slice
	_, _, errno := syscall.Syscall(
		syscall.SYS_MSYNC,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		syscall.MS_SYNC,
	)
	if errno != 0 {
		return fmt.Errorf("%w: msync: %w", fault.ErrSystemFailure, errno)
	}

	return nil
}

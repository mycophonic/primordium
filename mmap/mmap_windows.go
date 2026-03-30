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

package mmap

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/mycophonic/primordium/fault"
)

// Mapping holds the Windows file Mapping handle and base address.
type Mapping struct {
	handle syscall.Handle
	addr   uintptr
}

// MapFile maps the file into read-write shared memory.
func MapFile(file *os.File, size int) ([]byte, Mapping, error) {
	if size <= 0 {
		return nil, Mapping{}, fmt.Errorf("%w: mmap size must be positive, got %d", fault.ErrInvalidArgument, size)
	}

	handle, err := syscall.CreateFileMapping(
		syscall.Handle(file.Fd()),
		nil,
		syscall.PAGE_READWRITE,
		uint32(uint64(size)>>32),
		uint32(size), //nolint:gosec // size validated positive above
		nil,
	)
	if err != nil {
		return nil, Mapping{}, fmt.Errorf("%w: CreateFileMapping: %w", fault.ErrSystemFailure, err)
	}

	addr, err := syscall.MapViewOfFile(
		handle,
		syscall.FILE_MAP_READ|syscall.FILE_MAP_WRITE,
		0,
		0,
		uintptr(size),
	)
	if err != nil {
		//revive:disable-next-line:unhandled-error // best-effort cleanup on failure path
		syscall.CloseHandle(handle) //nolint:gosec // G104

		return nil, Mapping{}, fmt.Errorf("%w: MapViewOfFile: %w", fault.ErrSystemFailure, err)
	}

	//nolint:gosec,govet // G103/unsafeptr: uintptr→Pointer from MapViewOfFile, pinned by OS mapping
	data := unsafe.Slice(
		(*byte)(unsafe.Pointer(addr)),
		size,
	)

	return data, Mapping{handle: handle, addr: addr}, nil
}

// UnmapFile unmaps previously mapped memory and closes the Mapping handle.
func UnmapFile(_ []byte, mapping Mapping) error {
	if mapping.addr == 0 {
		return nil
	}

	unmapErr := syscall.UnmapViewOfFile(mapping.addr)

	closeErr := syscall.CloseHandle(mapping.handle)

	if unmapErr != nil {
		return fmt.Errorf("%w: UnmapViewOfFile: %w", fault.ErrSystemFailure, unmapErr)
	}

	if closeErr != nil {
		return fmt.Errorf("%w: CloseHandle: %w", fault.ErrSystemFailure, closeErr)
	}

	return nil
}

// SyncFile flushes the mapped region to disk.
// On Unix, msync(MS_SYNC) provides full durability; f is unused.
// On Windows, FlushFileBuffers is called after FlushViewOfFile to
// ensure data reaches physical disk, not just the filesystem cache.
func SyncFile(data []byte, file *os.File) error {
	if len(data) == 0 {
		return nil
	}

	if err := syscall.FlushViewOfFile(
		uintptr(unsafe.Pointer(&data[0])), //nolint:gosec // G103: required for mmap syscall interop
		uintptr(len(data)),
	); err != nil {
		return fmt.Errorf("%w: FlushViewOfFile: %w", fault.ErrSystemFailure, err)
	}

	if err := syscall.FlushFileBuffers(syscall.Handle(file.Fd())); err != nil {
		return fmt.Errorf("%w: FlushFileBuffers: %w", fault.ErrSystemFailure, err)
	}

	return nil
}

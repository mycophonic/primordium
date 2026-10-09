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
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// view is what the platform keeps of a mapping besides its bytes: on Windows,
// the file-mapping handle and the view's address, which Unmap needs.
type view struct {
	handle windows.Handle
	addr   uintptr
}

func mapView(file *os.File, size int) ([]byte, view, error) {
	handle, err := windows.CreateFileMapping(
		windows.Handle(file.Fd()),
		nil,
		windows.PAGE_READWRITE,
		uint32(uint64(size)>>32), // #nosec G115 -- the high and low words of a positive size
		uint32(size),             // #nosec G115
		nil,
	)
	if err != nil {
		return nil, view{}, err //nolint:wrapcheck // the platform layer; Map wraps
	}

	addr, err := windows.MapViewOfFile(handle, windows.FILE_MAP_READ|windows.FILE_MAP_WRITE, 0, 0, uintptr(size))
	if err != nil {
		_ = windows.CloseHandle(handle)

		return nil, view{}, err //nolint:wrapcheck // the platform layer; Map wraps
	}

	//nolint:govet // unsafeptr: the address is a view the OS keeps mapped until UnmapViewOfFile
	data := unsafe.Slice(
		(*byte)(unsafe.Pointer(addr)),
		size,
	) // #nosec G103 -- the view's address, as MapViewOfFile returns it

	return data, view{handle: handle, addr: addr}, nil
}

// syncView is FlushViewOfFile, which writes the pages to the file, then
// FlushFileBuffers, which takes them to the disk: the first alone leaves
// them in the filesystem's cache.
func syncView(data []byte, file *os.File, _ view) error {
	address := uintptr(unsafe.Pointer(&data[0])) // #nosec G103 -- the region's address, as the call takes it

	if err := windows.FlushViewOfFile(address, uintptr(len(data))); err != nil {
		return err //nolint:wrapcheck // the platform layer; Sync wraps
	}

	return windows.FlushFileBuffers(windows.Handle(file.Fd())) //nolint:wrapcheck // the platform layer; Sync wraps
}

func unmapView(_ []byte, v view) error {
	err := windows.UnmapViewOfFile(v.addr)

	if closeErr := windows.CloseHandle(v.handle); err == nil {
		err = closeErr
	}

	return err
}

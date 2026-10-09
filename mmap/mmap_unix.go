//go:build !windows

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

	"golang.org/x/sys/unix"
)

// view is what the platform keeps of a mapping besides its bytes: on Unix,
// nothing, the bytes are the mapping.
type view struct{}

func mapView(file *os.File, size int) ([]byte, view, error) {
	data, err := unix.Mmap(int(file.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)

	return data, view{}, err //nolint:wrapcheck // the platform layer; Map wraps
}

// syncView is msync(MS_SYNC): the pages are written and the call returns
// when they are on the file.
func syncView(data []byte, _ *os.File, _ view) error {
	return unix.Msync(data, unix.MS_SYNC) //nolint:wrapcheck // the platform layer; Sync wraps
}

func unmapView(data []byte, _ view) error {
	return unix.Munmap(data) //nolint:wrapcheck // the platform layer; Unmap wraps
}

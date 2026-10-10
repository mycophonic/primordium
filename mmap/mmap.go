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

	"github.com/mycophonic/primordium/fault"
)

// Mapping is the first size bytes of a file, mapped read-write and shared:
// a write through Bytes is a write to the file, seen by every other mapping
// of it, and durable once Sync returns.
type Mapping struct {
	data []byte
	file *os.File
	view view
}

// Map maps the first size bytes of file, which must hold at least that
// many: on Unix a page past the file's end faults when touched, and on
// Windows the mapping would grow the file; both are refused here as
// ErrInvalidArgument, as is a size below 1.
func Map(file *os.File, size int) (*Mapping, error) {
	if size <= 0 {
		return nil, fmt.Errorf("%w: mmap size must be positive, got %d", fault.ErrInvalidArgument, size)
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	if info.Size() < int64(size) {
		return nil, fmt.Errorf(
			"%w: mmap size %d past the file's %d bytes",
			fault.ErrInvalidArgument,
			size,
			info.Size(),
		)
	}

	data, platform, err := mapView(file, size)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return &Mapping{data: data, file: file, view: platform}, nil
}

// Bytes is the mapped region, nil once Unmap has run.
func (m *Mapping) Bytes() []byte {
	return m.data
}

// Sync writes the region's changes through to the file and waits until
// they are durable.
func (m *Mapping) Sync() error {
	if m.data == nil {
		return fmt.Errorf("%w: sync of an unmapped mapping", fault.ErrInvalidArgument)
	}

	if err := syncView(m.data, m.file, m.view); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return nil
}

// Unmap releases the mapping; Bytes is nil from then on, and a second Unmap
// is ErrInvalidArgument. What was written and not synced still reaches the
// file, as the kernel writes it back.
func (m *Mapping) Unmap() error {
	if m.data == nil {
		return fmt.Errorf("%w: unmap of an unmapped mapping", fault.ErrInvalidArgument)
	}

	err := unmapView(m.data, m.view)
	m.data = nil

	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return nil
}

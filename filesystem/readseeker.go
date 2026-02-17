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

package filesystem

import (
	"io"
)

// ReadSeeker wraps an io.ReadSeeker with buffered reading. Seeks invalidate
// the buffer except for small forward SeekCurrent operations that fall within
// the buffered data. If the underlying reader implements io.Closer, Close
// will close it.
type ReadSeeker struct {
	buf    []byte
	off    int // read offset within buf
	end    int // valid data end within buf
	source io.ReadSeeker
}

// NewReadSeeker returns a new ReadSeeker with the default buffer size (4096 bytes).
func NewReadSeeker(source io.ReadSeeker) *ReadSeeker {
	return NewReadSeekerWithSize(source, defaultBufferSize)
}

// NewReadSeekerWithSize returns a new ReadSeeker with a buffer of at least size bytes.
func NewReadSeekerWithSize(source io.ReadSeeker, size int) *ReadSeeker {
	if size <= 0 {
		size = defaultBufferSize
	}

	return &ReadSeeker{
		buf:    make([]byte, size),
		source: source,
	}
}

// Read reads up to len(p) bytes into p. Small reads are served from the buffer.
// Reads larger than the buffer bypass it and read directly from the underlying source.
//
//nolint:wrapcheck // I/O wrapper must return unwrapped errors (io.EOF, etc.)
func (rs *ReadSeeker) Read(dest []byte) (int, error) {
	if len(dest) == 0 {
		return 0, nil
	}

	// Serve from buffer if data is available.
	if rs.off < rs.end {
		n := copy(dest, rs.buf[rs.off:rs.end])
		rs.off += n

		return n, nil
	}

	// Large reads bypass the buffer entirely.
	if len(dest) >= len(rs.buf) {
		return rs.source.Read(dest)
	}

	// Refill the buffer.
	n, err := rs.source.Read(rs.buf)
	if n > 0 {
		rs.off = 0
		rs.end = n

		copied := copy(dest, rs.buf[:rs.end])
		rs.off = copied

		return copied, nil
	}

	return 0, err
}

// Seek sets the offset for the next Read. For io.SeekCurrent with a small
// forward offset, the seek is satisfied within the buffer without a syscall.
// All other seeks invalidate the buffer and delegate to the underlying source.
//
//nolint:wrapcheck // I/O wrapper must return unwrapped errors
func (rs *ReadSeeker) Seek(offset int64, whence int) (int64, error) {
	// For SeekCurrent, adjust offset to account for buffered but unconsumed bytes.
	// The underlying source is ahead of the logical read position by (rs.end - rs.off).
	if whence == io.SeekCurrent {
		// Optimize small forward seeks within buffered data.
		if offset >= 0 {
			newOff := rs.off + int(offset)
			if newOff <= rs.end {
				rs.off = newOff

				// Ask the underlying source for the absolute position.
				pos, err := rs.source.Seek(0, io.SeekCurrent)
				if err != nil {
					return 0, err
				}

				return pos - int64(rs.end-rs.off), nil
			}
		}

		// Adjust offset to account for buffered but unconsumed bytes.
		offset -= int64(rs.end - rs.off)
	}

	// Invalidate the buffer.
	rs.off = 0
	rs.end = 0

	return rs.source.Seek(offset, whence)
}

// Close closes the underlying reader if it implements io.Closer.
//
//nolint:wrapcheck // passthrough to underlying closer
func (rs *ReadSeeker) Close() error {
	if closer, ok := rs.source.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

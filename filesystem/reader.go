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
	"bufio"
	"io"
)

// Reader wraps an io.Reader with buffering. If the underlying reader implements
// io.Closer, Close will close it.
type Reader struct {
	r   *bufio.Reader
	src io.Reader
}

// NewReader returns a new Reader with the default buffer size (4096 bytes).
func NewReader(r io.Reader) *Reader {
	return NewReaderWithSize(r, defaultBufferSize)
}

// NewReaderWithSize returns a new Reader with a buffer of at least size bytes.
func NewReaderWithSize(r io.Reader, size int) *Reader {
	return &Reader{
		r:   bufio.NewReaderSize(r, size),
		src: r,
	}
}

// Read reads up to len(p) bytes from the buffered reader.
//
//nolint:wrapcheck // I/O wrapper must return unwrapped errors (io.EOF, etc.)
func (r *Reader) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

// Close closes the underlying reader if it implements io.Closer.
//
//nolint:wrapcheck // passthrough to underlying closer
func (r *Reader) Close() error {
	if closer, ok := r.src.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

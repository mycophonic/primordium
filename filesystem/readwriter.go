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
	"errors"
	"io"
)

// ReadWriter wraps an io.ReadWriter with buffered reading and writing.
// If the underlying reader/writer implements io.Closer, Close will flush
// the write buffer and then close it.
type ReadWriter struct {
	r   *bufio.Reader
	w   *bufio.Writer
	src io.ReadWriter
}

// NewReadWriter returns a new ReadWriter with the default buffer size (4096 bytes).
func NewReadWriter(rw io.ReadWriter) *ReadWriter {
	return NewReadWriterWithSize(rw, defaultBufferSize)
}

// NewReadWriterWithSize returns a new ReadWriter with buffers of at least size bytes.
func NewReadWriterWithSize(rw io.ReadWriter, size int) *ReadWriter {
	return &ReadWriter{
		r:   bufio.NewReaderSize(rw, size),
		w:   bufio.NewWriterSize(rw, size),
		src: rw,
	}
}

// Read reads up to len(p) bytes from the buffered reader.
//
//nolint:wrapcheck // I/O wrapper must return unwrapped errors (io.EOF, etc.)
func (rw *ReadWriter) Read(p []byte) (int, error) {
	return rw.r.Read(p)
}

// Write writes p to the buffered writer.
//
//nolint:wrapcheck // I/O wrapper must return unwrapped errors
func (rw *ReadWriter) Write(p []byte) (int, error) {
	return rw.w.Write(p)
}

// Close flushes the write buffer, then closes the underlying reader/writer
// if it implements io.Closer.
//
//nolint:wrapcheck // passthrough to underlying closer
func (rw *ReadWriter) Close() error {
	flushErr := rw.w.Flush()

	if closer, ok := rw.src.(io.Closer); ok {
		return errors.Join(flushErr, closer.Close())
	}

	return flushErr
}

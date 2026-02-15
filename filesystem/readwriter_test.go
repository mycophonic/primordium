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

package filesystem_test

import (
	"bytes"
	"io"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem"
)

// memReadWriter is a simple in-memory io.ReadWriter for testing.
type memReadWriter struct {
	readBuf  *bytes.Reader
	writeBuf *bytes.Buffer
}

func (m *memReadWriter) Read(p []byte) (int, error) {
	return m.readBuf.Read(p)
}

func (m *memReadWriter) Write(p []byte) (int, error) {
	return m.writeBuf.Write(p)
}

func TestReadWriter_ReadAndWrite(t *testing.T) {
	t.Parallel()

	readData := []byte("read this data")
	rw := &memReadWriter{
		readBuf:  bytes.NewReader(readData),
		writeBuf: &bytes.Buffer{},
	}

	brw := filesystem.NewReadWriter(rw)

	got, err := io.ReadAll(brw)
	assert.NilError(t, err, "read should succeed")
	assert.Assert(t, bytes.Equal(got, readData), "read data mismatch")

	writeData := []byte("write this data")
	n, err := brw.Write(writeData)
	assert.NilError(t, err, "write should succeed")
	assert.Assert(t, n == len(writeData), "short write: got %d, want %d", n, len(writeData))

	err = brw.Close()
	assert.NilError(t, err, "close should succeed")
	assert.Assert(t, bytes.Equal(rw.writeBuf.Bytes(), writeData),
		"written data mismatch after flush: got %q, want %q", rw.writeBuf.Bytes(), writeData)
}

type trackingReadWriteCloser struct {
	memReadWriter

	closed bool
}

func (t *trackingReadWriteCloser) Close() error {
	t.closed = true

	return nil
}

func TestReadWriter_CloseFlushesAndCloses(t *testing.T) {
	t.Parallel()

	tc := &trackingReadWriteCloser{
		memReadWriter: memReadWriter{
			readBuf:  bytes.NewReader(nil),
			writeBuf: &bytes.Buffer{},
		},
	}

	brw := filesystem.NewReadWriter(tc)

	_, err := brw.Write([]byte("buffered"))
	assert.NilError(t, err, "write should succeed")

	err = brw.Close()
	assert.NilError(t, err, "close should succeed")
	assert.Assert(t, tc.closed, "underlying closer should have been called")
	assert.Assert(t, bytes.Equal(tc.writeBuf.Bytes(), []byte("buffered")),
		"data should be flushed before close")
}

func TestReadWriter_CloseNonCloserStillFlushes(t *testing.T) {
	t.Parallel()

	rw := &memReadWriter{
		readBuf:  bytes.NewReader(nil),
		writeBuf: &bytes.Buffer{},
	}

	brw := filesystem.NewReadWriter(rw)

	_, err := brw.Write([]byte("data"))
	assert.NilError(t, err, "write should succeed")

	err = brw.Close()
	assert.NilError(t, err, "close on non-Closer should return nil")
	assert.Assert(t, bytes.Equal(rw.writeBuf.Bytes(), []byte("data")),
		"data should be flushed even without Close")
}

func TestReadWriter_CustomBufferSize(t *testing.T) {
	t.Parallel()

	rw := &memReadWriter{
		readBuf:  bytes.NewReader([]byte("hello")),
		writeBuf: &bytes.Buffer{},
	}

	brw := filesystem.NewReadWriterWithSize(rw, 128)

	got, err := io.ReadAll(brw)
	assert.NilError(t, err, "read should succeed")
	assert.Assert(t, bytes.Equal(got, []byte("hello")), "data mismatch")
}

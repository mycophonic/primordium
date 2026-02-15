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
	"strings"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem"
)

// countingReader wraps an io.Reader and counts how many times Read is called.
type countingReader struct {
	r     io.Reader
	calls atomic.Int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	cr.calls.Add(1)

	return cr.r.Read(p)
}

func TestReader_BuffersSmallReads(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("x", 8192)
	cr := &countingReader{r: strings.NewReader(data)}
	reader := filesystem.NewReader(cr)

	buf := make([]byte, 1)

	for range 100 {
		_, err := reader.Read(buf)
		assert.NilError(t, err, "read should succeed")
	}

	// 100 one-byte reads should result in far fewer underlying reads due to buffering.
	calls := cr.calls.Load()
	assert.Assert(t, calls < 10, "expected fewer than 10 underlying reads, got %d", calls)
}

func TestReader_ReadsAllData(t *testing.T) {
	t.Parallel()

	data := []byte("hello, buffered world")
	reader := filesystem.NewReader(bytes.NewReader(data))

	got, err := io.ReadAll(reader)
	assert.NilError(t, err, "ReadAll should succeed")
	assert.Assert(t, bytes.Equal(got, data), "data mismatch: got %q, want %q", got, data)
}

type trackingCloser struct {
	io.Reader

	closed bool
}

func (tc *trackingCloser) Close() error {
	tc.closed = true

	return nil
}

func TestReader_ClosePassthrough(t *testing.T) {
	t.Parallel()

	tc := &trackingCloser{Reader: strings.NewReader("data")}
	reader := filesystem.NewReader(tc)

	err := reader.Close()
	assert.NilError(t, err, "close should succeed")
	assert.Assert(t, tc.closed, "underlying closer should have been called")
}

func TestReader_CloseNonCloser(t *testing.T) {
	t.Parallel()

	reader := filesystem.NewReader(strings.NewReader("data"))
	err := reader.Close()
	assert.NilError(t, err, "close on non-Closer should return nil")
}

func TestReader_CustomBufferSize(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("a", 256)
	cr := &countingReader{r: strings.NewReader(data)}
	reader := filesystem.NewReaderWithSize(cr, 64)

	got, err := io.ReadAll(reader)
	assert.NilError(t, err, "ReadAll should succeed")
	assert.Assert(t, len(got) == 256, "should read all 256 bytes, got %d", len(got))

	// With a 64-byte buffer, underlying reads should be fewer than 256 (one per byte).
	calls := cr.calls.Load()
	assert.Assert(t, calls < 256, "buffering should reduce underlying read calls, got %d", calls)
}

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
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem"
)

// countingReadSeeker wraps a bytes.Reader and counts Read and Seek calls.
type countingReadSeeker struct {
	r         *bytes.Reader
	readCalls atomic.Int64
	seekCalls atomic.Int64
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	c.readCalls.Add(1)

	return c.r.Read(p)
}

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	c.seekCalls.Add(1)

	return c.r.Seek(offset, whence)
}

func TestReadSeeker_BuffersSmallReads(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("abcdefgh"), 512) // 4096 bytes
	crs := &countingReadSeeker{r: bytes.NewReader(data)}
	rs := filesystem.NewReadSeeker(crs)

	buf := make([]byte, 1)

	for range 100 {
		_, err := rs.Read(buf)
		assert.NilError(t, err, "read should succeed")
	}

	// 100 one-byte reads should need very few underlying reads.
	calls := crs.readCalls.Load()
	assert.Assert(t, calls < 5, "expected fewer than 5 underlying reads, got %d", calls)
}

func TestReadSeeker_ReadsAllData(t *testing.T) {
	t.Parallel()

	data := []byte("hello, buffered readseeker")
	rs := filesystem.NewReadSeeker(bytes.NewReader(data))

	got, err := io.ReadAll(rs)
	assert.NilError(t, err, "ReadAll should succeed")
	assert.Assert(t, bytes.Equal(got, data), "data mismatch: got %q, want %q", got, data)
}

func TestReadSeeker_LargeReadBypassesBuffer(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("x"), 8192)
	crs := &countingReadSeeker{r: bytes.NewReader(data)}
	rs := filesystem.NewReadSeekerWithSize(crs, 64)

	// Read more than the buffer size in one call.
	buf := make([]byte, 128)
	n, err := rs.Read(buf)
	assert.NilError(t, err, "read should succeed")
	assert.Assert(t, n > 0, "should read some bytes")

	// Should go directly to underlying reader (bypass buffer).
	assert.Assert(t, crs.readCalls.Load() == 1, "large read should be a single underlying read")
}

func TestReadSeeker_SeekInvalidatesBuffer(t *testing.T) {
	t.Parallel()

	data := []byte("0123456789abcdef")
	rs := filesystem.NewReadSeekerWithSize(bytes.NewReader(data), 64)

	// Read some data to fill the buffer.
	buf := make([]byte, 4)
	_, err := rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(buf, []byte("0123")), "first read should return '0123'")

	// Seek to position 8.
	pos, err := rs.Seek(8, io.SeekStart)
	assert.NilError(t, err, "seek should succeed")
	assert.Assert(t, pos == 8, "seek position should be 8, got %d", pos)

	// Read from new position.
	_, err = rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(buf, []byte("89ab")), "read after seek should return '89ab', got %q", buf)
}

func TestReadSeeker_SeekCurrentWithinBuffer(t *testing.T) {
	t.Parallel()

	data := []byte("0123456789abcdef")
	crs := &countingReadSeeker{r: bytes.NewReader(data)}
	rs := filesystem.NewReadSeekerWithSize(crs, 64)

	// Read 2 bytes to partially consume buffer.
	buf := make([]byte, 2)
	_, err := rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(buf, []byte("01")), "first read should return '01'")

	seeksBefore := crs.seekCalls.Load()

	// SeekCurrent +2 should stay within buffer.
	pos, err := rs.Seek(2, io.SeekCurrent)
	assert.NilError(t, err, "seek should succeed")
	assert.Assert(t, pos == 4, "position should be 4, got %d", pos)

	// Read from new position.
	_, err = rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(buf, []byte("45")), "read after small seek should return '45', got %q", buf)

	// The SeekCurrent within buffer still needs one underlying Seek to get absolute position,
	// but does not need to refill the buffer.
	seeksAfter := crs.seekCalls.Load()
	assert.Assert(t, seeksAfter-seeksBefore <= 1,
		"small SeekCurrent should need at most 1 underlying seek for position, got %d", seeksAfter-seeksBefore)
}

func TestReadSeeker_SeekCurrentBeyondBuffer(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("x"), 256)
	data[100] = 'Y'
	rs := filesystem.NewReadSeekerWithSize(bytes.NewReader(data), 16)

	// Read to fill buffer.
	buf := make([]byte, 1)
	_, err := rs.Read(buf)
	assert.NilError(t, err)

	// Seek forward beyond buffer.
	pos, err := rs.Seek(99, io.SeekCurrent)
	assert.NilError(t, err, "seek should succeed")
	assert.Assert(t, pos == 100, "position should be 100, got %d", pos)

	_, err = rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, buf[0] == 'Y', "should read 'Y' at position 100, got %q", buf[0])
}

func TestReadSeeker_SeekEnd(t *testing.T) {
	t.Parallel()

	data := []byte("0123456789")
	rs := filesystem.NewReadSeeker(bytes.NewReader(data))

	pos, err := rs.Seek(-2, io.SeekEnd)
	assert.NilError(t, err, "seek from end should succeed")
	assert.Assert(t, pos == 8, "position should be 8, got %d", pos)

	buf := make([]byte, 2)
	_, err = rs.Read(buf)
	assert.NilError(t, err)
	assert.Assert(t, bytes.Equal(buf, []byte("89")), "read from end should return '89', got %q", buf)
}

type trackingReadSeekCloser struct {
	io.ReadSeeker

	closed bool
}

func (t *trackingReadSeekCloser) Close() error {
	t.closed = true

	return nil
}

func TestReadSeeker_ClosePassthrough(t *testing.T) {
	t.Parallel()

	tc := &trackingReadSeekCloser{ReadSeeker: bytes.NewReader([]byte("data"))}
	rs := filesystem.NewReadSeeker(tc)

	err := rs.Close()
	assert.NilError(t, err, "close should succeed")
	assert.Assert(t, tc.closed, "underlying closer should have been called")
}

func TestReadSeeker_CloseNonCloser(t *testing.T) {
	t.Parallel()

	rs := filesystem.NewReadSeeker(bytes.NewReader([]byte("data")))
	err := rs.Close()
	assert.NilError(t, err, "close on non-Closer should return nil")
}

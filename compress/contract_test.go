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

package compress_test

// The contract of the package, from its documentation and its callers: it is
// forkcloser/compress with fault's classes. Decompress recognizes every
// format the side-effect imports register and streams the payload back
// whole; what no format claims is ErrNotImplemented; an empty, cut or
// otherwise bad stream and a failing source are ErrReadFailure, at
// Decompress or on a later Read; a Read after Close is ErrInvalidArgument;
// Compress is the zstd half of the round trip, and its reader carries the
// same classes. The bounded sizes and the formats' own edges are the
// dependency's contract, checked there.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/forkcloser/compress/gzip"
	"github.com/forkcloser/compress/lz4"
	"github.com/forkcloser/compress/xz"
	"github.com/forkcloser/compress/zstd"
	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/compress"
	_ "github.com/mycophonic/primordium/compress/bzip2"
	_ "github.com/mycophonic/primordium/compress/gzip"
	_ "github.com/mycophonic/primordium/compress/lz4"
	_ "github.com/mycophonic/primordium/compress/xz"
	"github.com/mycophonic/primordium/fault"
)

// testPayload is what testdata/payload.bz2 decompresses to.
const testPayload = "the quick brown fox jumps over the lazy dog"

var errBoom = errors.New("boom")

// failingReader fails every Read with its error.
type failingReader struct {
	err error
}

func (r *failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

// writer opens one format's encoder over buf, as forkcloser/compress's own
// format packages write it.
type writer func(tb testing.TB, buf *bytes.Buffer) io.WriteCloser

// writers are the formats Decompress must recognize and that a writer exists
// for; bzip2 has none in Go and is checked from a fixture.
func writers() map[string]writer {
	return map[string]writer{
		"zstd": func(tb testing.TB, buf *bytes.Buffer) io.WriteCloser {
			tb.Helper()

			w, err := zstd.NewWriter(buf)
			assert.NilError(tb, err)

			return w
		},
		"gzip": func(tb testing.TB, buf *bytes.Buffer) io.WriteCloser {
			tb.Helper()

			w, err := gzip.NewWriter(buf)
			assert.NilError(tb, err)

			return w
		},
		"lz4": func(tb testing.TB, buf *bytes.Buffer) io.WriteCloser {
			tb.Helper()

			w, err := lz4.NewWriter(buf)
			assert.NilError(tb, err)

			return w
		},
		"xz": func(tb testing.TB, buf *bytes.Buffer) io.WriteCloser {
			tb.Helper()

			w, err := xz.NewWriter(buf)
			assert.NilError(tb, err)

			return w
		},
	}
}

// encode compresses payload with one format's writer.
func encode(tb testing.TB, open writer, payload []byte) []byte {
	tb.Helper()

	var buf bytes.Buffer

	w := open(tb, &buf)

	_, err := w.Write(payload)
	assert.NilError(tb, err)
	assert.NilError(tb, w.Close())

	return buf.Bytes()
}

// payload is n bytes that compress, but not to nothing.
func payload(n int) []byte {
	const text = "the quick brown fox jumps over the lazy dog; "

	out := make([]byte, n)
	for i := range out {
		out[i] = text[i%len(text)] ^ byte(i/len(text)%7)
	}

	return out
}

// decompressed runs Decompress over data and drains it.
func decompressed(t *testing.T, data []byte) ([]byte, error) {
	t.Helper()

	reader, err := compress.Decompress(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		_ = reader.Close() // a decoder may report the stream's error again here

		return data, readErr
	}

	return data, reader.Close()
}

// TestRoundTrip: every registered format and Compress itself, at a byte, at
// the input buffer's edge and past it, back whole through Decompress.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, 1, 256*bytesize.KiB + 1} {
		for name, open := range writers() {
			t.Run(name+"/"+strconv.Itoa(size), func(t *testing.T) {
				t.Parallel()

				want := payload(size)

				got, err := decompressed(t, encode(t, open, want))
				assert.NilError(t, err)
				assert.Assert(t, bytes.Equal(got, want), "%d bytes back, want %d", len(got), len(want))
			})
		}

		t.Run("compress/"+strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			want := payload(size)

			reader, err := compress.Compress(bytes.NewReader(want))
			assert.NilError(t, err)

			data, err := io.ReadAll(reader)
			assert.NilError(t, err)
			assert.NilError(t, reader.Close())

			got, err := decompressed(t, data)
			assert.NilError(t, err)
			assert.Assert(t, bytes.Equal(got, want), "%d bytes back, want %d", len(got), len(want))
		})
	}
}

// TestBzip2Fixture: bzip2, which Go reads but does not write, from a fixture.
func TestBzip2Fixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "payload.bz2"))
	assert.NilError(t, err)

	got, err := decompressed(t, data)
	assert.NilError(t, err)
	assert.Equal(t, string(got), testPayload)
}

// TestFailureClasses: what each failure becomes.
func TestFailureClasses(t *testing.T) {
	t.Parallel()

	t.Run("no format claims it", func(t *testing.T) {
		t.Parallel()

		for name, input := range map[string][]byte{
			"one byte":                {0x00},
			"shorter than any magic":  {0xAB, 0xCD},
			"unknown, longer":         bytes.Repeat([]byte{0x01}, 1024),
			"zstd magic, three bytes": {0x28, 0xB5, 0x2F},
		} {
			_, err := compress.Decompress(bytes.NewReader(input))
			assert.ErrorIs(t, err, fault.ErrNotImplemented, name)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		_, err := compress.Decompress(bytes.NewReader(nil))
		assert.ErrorIs(t, err, fault.ErrReadFailure)
	})

	t.Run("cut short", func(t *testing.T) {
		t.Parallel()

		whole := encode(t, writers()["zstd"], payload(256*bytesize.KiB+1))

		_, err := decompressed(t, whole[:len(whole)/2])
		assert.ErrorIs(t, err, fault.ErrReadFailure)
	})

	t.Run("source fails at the magic", func(t *testing.T) {
		t.Parallel()

		_, err := compress.Decompress(&failingReader{err: errBoom})
		assert.ErrorIs(t, err, fault.ErrReadFailure)
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("source fails under Compress", func(t *testing.T) {
		t.Parallel()

		compressed, err := compress.Compress(&failingReader{err: errBoom})
		assert.NilError(t, err)

		_, err = io.ReadAll(compressed)
		assert.ErrorIs(t, err, fault.ErrReadFailure)
		assert.ErrorIs(t, err, errBoom)
		assert.NilError(t, compressed.Close())
	})

	t.Run("read after close", func(t *testing.T) {
		t.Parallel()

		reader, err := compress.Decompress(bytes.NewReader(encode(t, writers()["gzip"], payload(16))))
		assert.NilError(t, err)
		assert.NilError(t, reader.Close())

		_, err = reader.Read(make([]byte, 1))
		assert.ErrorIs(t, err, fault.ErrInvalidArgument)
	})

	t.Run("read ahead keeps the class", func(t *testing.T) {
		t.Parallel()

		reader, err := compress.Decompress(bytes.NewReader(encode(t, writers()["xz"], payload(16))))
		assert.NilError(t, err)

		ahead := compress.ReadAhead(reader)

		got, err := io.ReadAll(ahead)
		assert.NilError(t, err)
		assert.Assert(t, bytes.Equal(got, payload(16)))
		assert.NilError(t, ahead.Close())

		_, err = ahead.Read(make([]byte, 1))
		assert.ErrorIs(t, err, fault.ErrInvalidArgument)
	})
}

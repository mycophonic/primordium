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

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/forkcloser/xz"
	"github.com/klauspost/compress/zstd"
	"github.com/klauspost/pgzip"
	"github.com/pierrec/lz4/v4"

	"github.com/mycophonic/primordium/compress"
	_ "github.com/mycophonic/primordium/compress/bzip2"
	_ "github.com/mycophonic/primordium/compress/gzip"
	_ "github.com/mycophonic/primordium/compress/lz4"
	_ "github.com/mycophonic/primordium/compress/xz"
	"github.com/mycophonic/primordium/fault"
)

const testPayload = "the quick brown fox jumps over the lazy dog"

func TestCompressDecompress_Roundtrip(t *testing.T) {
	t.Parallel()

	input := bytes.NewReader([]byte(testPayload))

	compressed, err := compress.Compress(input)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	defer compressed.Close()

	decompressed, err := compress.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}

	got, err := io.ReadAll(decompressed)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if err := decompressed.Close(); err != nil {
		t.Fatalf("Close decompressed: %v", err)
	}

	if string(got) != testPayload {
		t.Errorf("got %q, want %q", got, testPayload)
	}
}

func TestDecompress_LZ4(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	lw := lz4.NewWriter(&buf)

	lw.Write([]byte(testPayload))
	lw.Close()

	rc, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != testPayload {
		t.Errorf("lz4: got %q, want %q", got, testPayload)
	}
}

func TestDecompress_Zstd(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}

	enc.Write([]byte(testPayload))
	enc.Close()

	rc, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != testPayload {
		t.Errorf("zstd: got %q, want %q", got, testPayload)
	}
}

func TestDecompress_Gzip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	gw := pgzip.NewWriter(&buf)

	gw.Write([]byte(testPayload))
	gw.Close()

	rc, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != testPayload {
		t.Errorf("gzip: got %q, want %q", got, testPayload)
	}
}

func TestDecompress_XZ(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	xw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}

	xw.Write([]byte(testPayload))
	xw.Close()

	rc, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != testPayload {
		t.Errorf("xz: got %q, want %q", got, testPayload)
	}
}

func TestDecompress_Bzip2(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("bzip2"); err != nil {
		t.Skip("bzip2 binary not available")
	}

	cmd := exec.Command("bzip2")
	cmd.Stdin = bytes.NewReader([]byte(testPayload))

	bz2Data, err := cmd.Output()
	if err != nil {
		t.Fatalf("bzip2 compress: %v", err)
	}

	rc, err := compress.Decompress(bytes.NewReader(bz2Data))
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != testPayload {
		t.Errorf("bzip2: got %q, want %q", got, testPayload)
	}
}

func TestDecompress_UnknownFormat(t *testing.T) {
	t.Parallel()

	_, err := compress.Decompress(bytes.NewReader([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}))
	if !errors.Is(err, fault.ErrNotImplemented) {
		t.Fatalf("expected fault.ErrNotImplemented, got: %v", err)
	}
}

func TestDecompress_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := compress.Decompress(bytes.NewReader(nil))
	if !errors.Is(err, fault.ErrReadFailure) {
		t.Fatalf("expected fault.ErrReadFailure, got: %v", err)
	}
}

func TestCompress_ReaderError(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	failing := &failingReader{err: errBoom}

	compressed, err := compress.Compress(failing)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}

	defer compressed.Close()

	_, err = io.ReadAll(compressed)
	if err == nil {
		t.Fatal("expected error from failing reader")
	}

	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got: %v", err)
	}
}

type failingReader struct {
	err error
}

func (r *failingReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestDecompress_PartialPeek(t *testing.T) {
	t.Parallel()

	// 2 bytes is shorter than any magic (min 4 bytes for zstd).
	// Decompress should still return a meaningful error, not panic.
	_, err := compress.Decompress(bytes.NewReader([]byte{0xAB, 0xCD}))
	if !errors.Is(err, fault.ErrNotImplemented) {
		t.Fatalf("expected fault.ErrNotImplemented for partial peek, got: %v", err)
	}
}

// TestDecompress_LZ4ParallelMultiBlock exercises the concurrent decode path
// the lz4 subpackage enables: a payload spanning many independent blocks,
// followed by a skippable frame (the sector padding a blob store appends),
// must round-trip exactly and reach clean EOF.
func TestDecompress_LZ4ParallelMultiBlock(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("parallel lz4 block decode round-trip "), 200000)

	var buf bytes.Buffer

	writer := lz4.NewWriter(&buf)
	if err := writer.Apply(lz4.BlockSizeOption(lz4.Block64Kb), lz4.ConcurrencyOption(-1)); err != nil {
		t.Fatalf("writer options: %v", err)
	}

	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("compress: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	// Trailing skippable frame: magic 0x184D2A50 + LE length + zeros.
	tail := make([]byte, 512)
	binary.LittleEndian.PutUint32(tail[0:4], 0x184D2A50)
	binary.LittleEndian.PutUint32(tail[4:8], uint32(len(tail)-8))
	buf.Write(tail)

	decompressed, err := compress.Decompress(&buf)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}

	defer func() { _ = decompressed.Close() }()

	got, err := io.ReadAll(decompressed)
	if err != nil {
		t.Fatalf("parallel decode failed (or the skippable tail broke it): %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

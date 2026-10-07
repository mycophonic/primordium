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

package iox_test

// The bounded check: every sequence of up to three calls, over every small
// stream, buffer size and source (one that returns all it can, one that
// returns a byte at a time), held to the contract after every call. Sizes and
// offsets sit at and around the edges: empty, one, the buffer's size and past
// it, the stream's end and past it, before its start.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"testing/iotest"

	"github.com/mycophonic/primordium/filesystem/iox"
)

var (
	errWrite = errors.New("write failed")
	errClose = errors.New("close failed")
)

func streamLengths() []int { return []int{0, 1, 2, 3, 5, 8} }

func bufferSizes() []int { return []int{1, 2, 3, 5} }

func chunks() []int { return []int{0, 1} }

// faults are where a source returns its error with data: never, at the start,
// one byte in, and three.
func faults() []int { return []int{-1, 0, 1, 3} }

func streamData(length int) []byte {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte('a' + i)
	}

	return data
}

// readSeekerOps are the calls on a ReadSeeker: reads around the buffer's size,
// seeks around the stream's start and end, from every whence.
func readSeekerOps(bufferSize int) []op {
	var ops []op

	for _, size := range []int{0, 1, 2, bufferSize - 1, bufferSize, bufferSize + 1, 8} {
		if size >= 0 {
			ops = append(ops, op{size: size})
		}
	}

	for _, whence := range []int{io.SeekStart, io.SeekCurrent, io.SeekEnd} {
		for _, offset := range []int64{-2, -1, 0, 1, 2, 3, 8} {
			ops = append(ops, op{seek: true, offset: offset, whence: whence})
		}
	}

	return ops
}

// sequences calls visit with every sequence of up to depth ops.
func sequences(ops []op, depth int, visit func([]op)) {
	var walk func(prefix []op)

	walk = func(prefix []op) {
		visit(prefix)

		if len(prefix) < depth {
			for _, next := range ops {
				walk(append(slices.Clip(prefix), next))
			}
		}
	}

	walk(nil)
}

// runReadSeeker plays ops on a ReadSeeker over data, held to the contract.
func runReadSeeker(data []byte, bufferSize, chunk, fault int, ops []op) error {
	src := newSource(data, chunk)
	src.faultAt = fault
	wrapper := iox.NewReadSeekerWithSize(src, bufferSize)
	ref := &stream{data: data, source: src}
	buffered := 0 // bytes the wrapper holds that the stream has not yet given out

	for i, call := range ops {
		if call.seek {
			got, err := wrapper.Seek(call.offset, call.whence)
			if checkErr := ref.seek(call.offset, call.whence, got, err); checkErr != nil {
				return fmt.Errorf(
					"call %d, Seek(%d, %d) = %d, %s: %w",
					i,
					call.offset,
					call.whence,
					got,
					text(err),
					checkErr,
				)
			}

			// A small forward SeekCurrent within the buffered bytes keeps them;
			// any other seek empties the buffer, which a seek from an unknown
			// buffer may or may not have done.
			kind := noSeek

			switch {
			case err != nil:
			case buffered < 0:
				kind = seekUnsure
			case call.whence == io.SeekCurrent && call.offset >= 0 && int64(buffered) >= call.offset:
				kind = seekKept
				buffered -= int(call.offset)
			default:
				kind = seekMoved
				buffered = -1
			}

			if kind == seekUnsure {
				buffered = -1
			}

			ref.note(kind)

			continue
		}

		before := src.reads
		buf := make([]byte, call.size)
		n, err := wrapper.Read(buf)

		if checkErr := ref.read(call.size, n, buf, err); checkErr != nil {
			return fmt.Errorf("call %d, Read(%d) = %d, %s: %w", i, call.size, n, text(err), checkErr)
		}

		calls := src.reads - before
		if calls > 1 {
			return fmt.Errorf("%w: call %d, Read(%d) reached the source %d times", errBroken, i, call.size, calls)
		}

		if buffered > 0 && call.size > 0 && calls != 0 {
			return fmt.Errorf(
				"%w: call %d, Read(%d) reached the source with %d bytes buffered",
				errBroken,
				i,
				call.size,
				buffered,
			)
		}

		buffered = trackBuffer(buffered, calls, n, src.gave)

		ref.note(noSeek)
	}

	return nil
}

// text is an error as a failure message shows it, nil included.
func text(err error) string {
	if err == nil {
		return "no error"
	}

	return err.Error()
}

// trackBuffer follows how many bytes the wrapper holds: what the source gave
// it on the Read that reached the source, less what it handed back.
func trackBuffer(buffered, calls, n, gave int) int {
	switch {
	case calls == 1:
		return gave - n
	case buffered < 0:
		return -1
	default:
		return buffered - n
	}
}

func TestBoundedReadSeeker(t *testing.T) {
	t.Parallel()

	for _, length := range streamLengths() {
		for _, bufferSize := range bufferSizes() {
			for _, chunk := range chunks() {
				for _, fault := range faults() {
					data := streamData(length)

					sequences(readSeekerOps(bufferSize), 3, func(ops []op) {
						if err := runReadSeeker(data, bufferSize, chunk, fault, ops); err != nil {
							t.Fatalf(
								"stream %q, buffer %d, chunk %d, fault %d, %+v: %v",
								data,
								bufferSize,
								chunk,
								fault,
								ops,
								err,
							)
						}
					})
				}
			}
		}
	}
}

// runReader plays reads of the given sizes on r over data, held to the
// contract: the stream's bytes, in order, and one source read at most each.
func runReader(r io.Reader, src *source, data []byte, sizes []int) error {
	ref := &stream{data: data, source: src}

	for i, size := range sizes {
		before := src.reads
		buf := make([]byte, size)
		n, err := r.Read(buf)

		if checkErr := ref.read(size, n, buf, err); checkErr != nil {
			return fmt.Errorf("read %d, Read(%d) = %d, %s: %w", i, size, n, text(err), checkErr)
		}

		if src.reads-before > 1 {
			return fmt.Errorf("%w: read %d, Read(%d) reached the source %d times", errBroken, i, size, src.reads-before)
		}

		ref.note(noSeek)
	}

	return nil
}

func readOps(bufferSize int) []op {
	var ops []op

	for _, size := range []int{0, 1, 2, bufferSize, bufferSize + 1, 8} {
		ops = append(ops, op{size: size})
	}

	return ops
}

func sizesOf(ops []op) []int {
	sizes := make([]int, len(ops))
	for i, call := range ops {
		sizes[i] = call.size
	}

	return sizes
}

func TestBoundedReader(t *testing.T) {
	t.Parallel()

	for _, length := range streamLengths() {
		for _, bufferSize := range bufferSizes() {
			for _, chunk := range chunks() {
				for _, fault := range faults() {
					data := streamData(length)

					sequences(readOps(bufferSize), 4, func(ops []op) {
						src := newSource(data, chunk)
						src.faultAt = fault

						if err := runReader(
							iox.NewReaderWithSize(src, bufferSize),
							src,
							data,
							sizesOf(ops),
						); err != nil {
							t.Fatalf(
								"stream %q, buffer %d, chunk %d, fault %d: %v",
								data,
								bufferSize,
								chunk,
								fault,
								err,
							)
						}
					})
				}
			}
		}
	}
}

// split is a ReadWriter whose reads come from a source and whose writes go
// to a destination, so each side is checked on its own.
type split struct {
	*source

	dest   bytes.Buffer
	writes int
	failW  error
}

func (s *split) Write(p []byte) (int, error) {
	s.writes++

	if s.failW != nil {
		return 0, s.failW
	}

	return s.dest.Write(p)
}

func TestBoundedReadWriter(t *testing.T) {
	t.Parallel()

	for _, length := range streamLengths() {
		for _, bufferSize := range bufferSizes() {
			ops := append(readOps(bufferSize), op{seek: true, size: 1}, op{seek: true, size: bufferSize + 1})

			sequences(ops, 4, func(calls []op) {
				data := streamData(length)
				dest := &split{source: newSource(data, 0)}
				wrapper := iox.NewReadWriterWithSize(dest, bufferSize)
				ref := &stream{data: data}

				var written []byte

				for i, call := range calls {
					if call.seek { // here: a Write of size bytes
						chunk := bytes.Repeat([]byte{byte('A' + i)}, call.size)
						if n, err := wrapper.Write(chunk); n != len(chunk) || err != nil {
							t.Fatalf("Write(%d) = %d, %v", call.size, n, err)
						}

						written = append(written, chunk...)

						continue
					}

					buf := make([]byte, call.size)
					n, err := wrapper.Read(buf)

					if checkErr := ref.read(call.size, n, buf, err); checkErr != nil {
						t.Fatalf(
							"stream %q, buffer %d, %+v, Read(%d): %v",
							data,
							bufferSize,
							calls,
							call.size,
							checkErr,
						)
					}
				}

				if err := wrapper.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}

				if !bytes.Equal(dest.dest.Bytes(), written) {
					t.Fatalf(
						"buffer %d, %+v: destination holds %q after Close, want %q",
						bufferSize,
						calls,
						dest.dest.Bytes(),
						written,
					)
				}
			})
		}
	}
}

// TestReadWriterCloseReportsFlush: a flush that fails is Close's error, and a
// closing destination is still closed.
func TestReadWriterCloseReportsFlush(t *testing.T) {
	t.Parallel()

	plain := &split{source: newSource(nil, 0), failW: errWrite}
	wrapper := iox.NewReadWriterWithSize(plain, 4)
	_, _ = wrapper.Write([]byte("ab"))

	if err := wrapper.Close(); !errors.Is(err, errWrite) {
		t.Fatalf("Close = %v, want the flush's error", err)
	}

	closing := &closingReadWriter{split: &split{source: newSource(nil, 0), failW: errWrite}, err: errClose}
	wrapper = iox.NewReadWriterWithSize(closing, 4)
	_, _ = wrapper.Write([]byte("ab"))

	err := wrapper.Close()
	if !errors.Is(err, errWrite) || !errors.Is(err, errClose) || closing.closed != 1 {
		t.Fatalf("Close = %v, closed %d times; want both errors and one close", err, closing.closed)
	}
}

type closingReadWriter struct {
	*split

	closed int
	err    error
}

func (c *closingReadWriter) Close() error {
	c.closed++

	return c.err
}

// TestClose: every wrapper closes its source if, and only if, it is an
// io.Closer, once, and returns its error.
func TestClose(t *testing.T) {
	t.Parallel()

	for _, wrap := range []func(io.ReadSeeker) io.Closer{
		func(s io.ReadSeeker) io.Closer { return iox.NewReader(s) },
		func(s io.ReadSeeker) io.Closer { return iox.NewReadSeeker(s) },
	} {
		closing := &closingSource{source: newSource(nil, 0), err: errClose}
		if err := wrap(closing).Close(); !errors.Is(err, errClose) || closing.closed != 1 {
			t.Fatalf("Close = %v, closed %d times; want its error, once", err, closing.closed)
		}

		if err := wrap(newSource(nil, 0)).Close(); err != nil {
			t.Fatalf("Close over a non-Closer = %v", err)
		}
	}

	closing := &closingReadWriter{split: &split{source: newSource(nil, 0)}}
	if err := iox.NewReadWriter(closing).Close(); err != nil || closing.closed != 1 {
		t.Fatalf("ReadWriter Close = %v, closed %d times", err, closing.closed)
	}

	if err := iox.NewReadWriter(&split{source: newSource(nil, 0)}).Close(); err != nil {
		t.Fatalf("ReadWriter Close over a non-Closer = %v", err)
	}
}

// TestDefaultBufferSize: the default buffer is 4096 bytes, so a small first
// Read asks the source for 4096.
func TestDefaultBufferSize(t *testing.T) {
	t.Parallel()

	data := streamData(8)

	for name, wrap := range map[string]func(*source) io.Reader{
		"Reader":     func(s *source) io.Reader { return iox.NewReader(s) },
		"ReadSeeker": func(s *source) io.Reader { return iox.NewReadSeeker(s) },
		"ReadWriter": func(s *source) io.Reader { return iox.NewReadWriter(&split{source: s}) },
	} {
		src := newSource(data, 0)
		if _, err := wrap(src).Read(make([]byte, 1)); err != nil || src.asked != 4096 {
			t.Fatalf("%s: a first one-byte Read asked the source for %d bytes (%v), want 4096", name, src.asked, err)
		}
	}
}

// TestIOTestConformance runs the standard library's own conformance check
// for readers and seekers, testing/iotest.TestReader, on every small stream
// and buffer size.
func TestIOTestConformance(t *testing.T) {
	t.Parallel()

	for _, length := range append(streamLengths(), 100) {
		for _, bufferSize := range append(bufferSizes(), 4096) {
			data := streamData(length)

			if err := iotest.TestReader(iox.NewReaderWithSize(bytes.NewReader(data), bufferSize), data); err != nil {
				t.Errorf("Reader, stream %d, buffer %d: %v", length, bufferSize, err)
			}

			if err := iotest.TestReader(
				iox.NewReadSeekerWithSize(bytes.NewReader(data), bufferSize),
				data,
			); err != nil {
				t.Errorf("ReadSeeker, stream %d, buffer %d: %v", length, bufferSize, err)
			}
		}
	}
}

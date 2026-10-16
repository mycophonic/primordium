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

package compress

import (
	"errors"
	"fmt"
	"io"

	fcompress "github.com/forkcloser/compress"

	"github.com/mycophonic/primordium/fault"
)

// Decompressor creates a decompressed reader from a compressed stream.
type Decompressor = fcompress.Decompressor

// Register makes a decompression format available to Decompress. The format
// packages register themselves on import; a caller with a format of its own
// registers it here, and its reader's errors come back classified like the
// others'.
func Register(name string, magic []byte, fn Decompressor) {
	fcompress.Register(name, magic, fn)
}

// Compress returns a reader that yields zstd-compressed data from reader. A
// failure of reader is ErrReadFailure on the read; the caller must close the
// returned ReadCloser to release the encoder, and a Read after that is
// ErrInvalidArgument. The error is always nil: it stays for the callers that
// check it.
func Compress(reader io.Reader) (io.ReadCloser, error) {
	return &classified{fcompress.Compress(reader)}, nil
}

// Decompress detects the format from the stream's magic bytes and returns a
// streaming decompressed reader. A stream no registered format claims is
// ErrNotImplemented; an empty stream, one its decoder refuses or that ends
// early, and a source that fails, here or on a later Read, are
// ErrReadFailure. The caller must close the returned ReadCloser; a Read after
// that is ErrInvalidArgument.
func Decompress(reader io.Reader) (io.ReadCloser, error) {
	decoder, err := fcompress.Decompress(reader)
	if err != nil {
		return nil, classify(err)
	}

	return &classified{decoder}, nil
}

// ReadAhead returns a reader that decodes from decoder on a goroutine of its
// own, a few megabytes ahead of the caller, for a consumer slower than a
// serial decoder. The bytes are the decoder's; its errors come back
// classified. Close stops the goroutine and closes the decoder; a Read after
// Close is ErrInvalidArgument. A reader never closed keeps its goroutine.
func ReadAhead(decoder io.ReadCloser) io.ReadCloser {
	return &classified{fcompress.ReadAhead(decoder)}
}

// classified is a forkcloser/compress reader whose errors are this module's
// classes.
type classified struct {
	reader io.ReadCloser
}

func (c *classified) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)

	return n, classify(err)
}

func (c *classified) Close() error {
	return classify(c.reader.Close())
}

// classify maps forkcloser/compress's five classes onto fault's: the input
// not being for the package is ErrNotImplemented, a bad stream or a failing
// source ErrReadFailure, a failing destination ErrWriteFailure, a call after
// Close ErrInvalidArgument. nil, io.EOF and an error already classified pass
// as they are; one of no class is the dependency's defect, ErrSystemFailure.
func classify(err error) error {
	switch {
	case err == nil, errors.Is(err, io.EOF), isClassified(err):
		return err
	case errors.Is(err, fcompress.ErrUnknownFormat):
		return fmt.Errorf("%w: %w", fault.ErrNotImplemented, err)
	case errors.Is(err, fcompress.ErrCorrupt), errors.Is(err, fcompress.ErrSource):
		return fmt.Errorf("%w: %w", fault.ErrReadFailure, err)
	case errors.Is(err, fcompress.ErrDestination):
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, err)
	case errors.Is(err, fcompress.ErrClosed):
		return fmt.Errorf("%w: %w", fault.ErrInvalidArgument, err)
	default:
		return fmt.Errorf("%w: compress: an error of no class: %w", fault.ErrSystemFailure, err)
	}
}

// isClassified reports whether err already carries one of the classes
// classify hands out, as a reader of this package's own does under ReadAhead.
func isClassified(err error) bool {
	for _, class := range []error{
		fault.ErrNotImplemented, fault.ErrReadFailure, fault.ErrWriteFailure,
		fault.ErrInvalidArgument, fault.ErrSystemFailure, fault.ErrFilesystemFailure,
	} {
		if errors.Is(err, class) {
			return true
		}
	}

	return false
}

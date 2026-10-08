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

// The contract, from the io package's documentation and iox's own. A wrapper
// is the stream it wraps, buffered:
//   - io.Reader: a Read returns the stream's next bytes, at most len(p) of
//     them, and io.EOF only at the end. Over a source that never returns no
//     byte and no error, a Read of len(p) > 0 that returns no byte returns an
//     error; a source that does is outside this contract, as io.Reader
//     discourages it and tells the caller it means "nothing happened", so a
//     wrapper may pass it on (bufio's io.ErrNoProgress is a safeguard beyond
// it). A source may return io.EOF with its last bytes or after them. A source may return bytes and an error together:
// the caller sees
//     that error once, where the stream had it, before any byte past it; a
//     small forward SeekCurrent within the buffered bytes keeps it, any other
//     seek that succeeds drops it, as it belonged to the old position.
//   - io.Seeker: Seek returns the new offset from the start, as the stream
//     itself would; "seeking to an offset before the start of the file is an
//     error", and a failed Seek leaves the position where it was.
//   - Buffering (iox docs): a Read reaches the source at most once, and not
//     at all while buffered bytes remain; a small forward SeekCurrent within
//     the buffered bytes keeps them; the default buffer is 4096 bytes.
//   - ReadWriter: every byte written reaches the destination, in order, by
//     the time Close returns, and Close returns the flush's error.
//   - Close closes the source if, and only if, it is an io.Closer.
// bytes.Reader over the same bytes is the reference stream.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

var (
	// errBroken is a broken contract; the message says which part.
	errBroken = errors.New("contract broken")
	// errFault is the error a source returns once, with the data before it.
	errFault = errors.New("source fault")
)

// source is the stream under a wrapper: a bytes.Reader that counts its calls,
// and that can return fewer bytes than asked (io.Reader allows it).
type source struct {
	reader *bytes.Reader
	chunk  int // most bytes one Read returns; 0 for no limit; -1 for no limit, with io.EOF on the last bytes
	reads  int
	seeks  int
	asked  int // the size of the last Read asked of it
	gave   int // the bytes the last Read returned

	faultAt int  // the offset at which a Read returns errFault, once; -1 for none
	faulted bool // whether it has
}

func newSource(data []byte, chunk int) *source {
	return &source{reader: bytes.NewReader(data), chunk: chunk, faultAt: -1}
}

func (s *source) Read(p []byte) (int, error) {
	s.reads++
	s.asked = len(p)

	if s.chunk > 0 && len(p) > s.chunk {
		p = p[:s.chunk]
	}

	// A fault sits within the stream or at its end; past it, there is none.
	at := s.offset()
	fault := !s.faulted && s.faultAt >= at && s.faultAt < at+len(p) && s.faultAt <= int(s.reader.Size())

	if fault {
		p = p[:s.faultAt-at]
		s.faulted = true
	}

	n, err := s.reader.Read(p)
	s.gave = n

	if fault {
		return n, errFault
	}

	// io.Reader lets a source return io.EOF with the last bytes, not after.
	if s.chunk < 0 && err == nil && n > 0 && s.reader.Len() == 0 {
		return n, io.EOF
	}

	return n, err
}

func (s *source) Seek(offset int64, whence int) (int64, error) {
	s.seeks++

	return s.reader.Seek(offset, whence)
}

// offset is where the source stands, past its end included.
func (s *source) offset() int {
	at, _ := s.reader.Seek(0, io.SeekCurrent) // bytes.Reader's never fails

	return int(at)
}

// closingSource is a source that is also an io.Closer.
type closingSource struct {
	*source

	closed int
	err    error
}

func (c *closingSource) Close() error {
	c.closed++

	return c.err
}

// op is one call on a wrapper: a Read of size bytes, or a Seek.
type op struct {
	seek   bool
	size   int
	offset int64
	whence int
}

// stream is the reference: where the wrapped stream is, what it holds, and
// whether an error is owed to the caller.
type stream struct {
	data []byte
	pos  int64

	source  *source // whose fault, if any, the caller is owed
	owed    bool    // the source has faulted and the caller has not seen it
	dropped bool    // a seek that moved the source came after the fault
	excused bool    // a seek of unknown kind came after it: either way is right
	seen    bool
}

// seekKind is what a seek did to a held error, as far as the check can tell.
type seekKind int

const (
	noSeek     seekKind = iota // a read, or a seek that failed: nothing moved
	seekKept                   // within the buffered bytes: the error is still owed
	seekMoved                  // the source moved: the error is dropped
	seekUnsure                 // the buffer was unknown: either is right
)

// read checks a wrapper's Read against the reference, and moves it.
func (s *stream) read(asked, n int, got []byte, err error) error {
	switch {
	case n < 0 || n > asked:
		return fmt.Errorf("%w: read count out of range", errBroken)
	case asked > 0 && n == 0 && err == nil:
		return fmt.Errorf("%w: an empty read without an error", errBroken)
	case !bytes.Equal(got[:n], s.data[min(s.pos, int64(len(s.data))):min(s.pos+int64(n), int64(len(s.data)))]):
		return fmt.Errorf("%w: read bytes that are not the stream's next", errBroken)
	case n > 0 && s.pos+int64(n) > int64(len(s.data)):
		return fmt.Errorf("%w: read past the end", errBroken)
	}

	if s.owed && n > 0 && s.pos+int64(n) > int64(s.source.faultAt) {
		return fmt.Errorf("%w: read past the source's error without returning it", errBroken)
	}

	s.pos += int64(n)

	if errors.Is(err, io.EOF) && s.pos < int64(len(s.data)) {
		return fmt.Errorf("%w: io.EOF before the end", errBroken)
	}

	if errors.Is(err, errFault) {
		if s.dropped {
			return fmt.Errorf("%w: the source's error returned after a seek dropped it", errBroken)
		}

		if s.seen || s.pos != int64(s.source.faultAt) {
			return fmt.Errorf("%w: the source's error returned at %d, or twice", errBroken, s.pos)
		}

		s.seen, s.owed = true, false
	}

	return nil
}

// note records, after each call, what the caller is owed of the source's
// fault: from the call in which the source faulted until the caller sees it,
// as the seeks in between leave it.
func (s *stream) note(kind seekKind) {
	if s.source == nil || !s.source.faulted || s.seen || s.dropped || s.excused {
		return
	}

	switch kind {
	case noSeek, seekKept:
		s.owed = true
	case seekMoved:
		s.owed, s.dropped = false, true
	case seekUnsure:
		s.owed, s.excused = false, true
	}
}

// seek checks a wrapper's Seek against the reference, and moves it.
func (s *stream) seek(offset int64, whence int, got int64, err error) error {
	reference := bytes.NewReader(s.data)
	if _, startErr := reference.Seek(s.pos, io.SeekStart); startErr != nil {
		return startErr
	}

	want, wantErr := reference.Seek(offset, whence)

	switch {
	case (err == nil) != (wantErr == nil):
		return fmt.Errorf("%w: seek succeeded or failed unlike the stream", errBroken)
	case err == nil && got != want:
		return fmt.Errorf("%w: seek landed elsewhere than the stream", errBroken)
	}

	if err == nil {
		s.pos = want
	}

	return nil
}

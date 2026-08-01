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

package lz4

import (
	"fmt"
	"io"

	plz4 "github.com/pierrec/lz4/v4"

	"github.com/mycophonic/primordium/compress"
	"github.com/mycophonic/primordium/fault"
)

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	compress.Register("lz4", []byte{0x04, 0x22, 0x4D, 0x18}, New)
}

// New returns an lz4 decompressed reader that decodes frame blocks on
// GOMAXPROCS goroutines. Parallel decode requires block-independent frames,
// which is the only kind pierrec's writer produces (and the format's
// default); on this library's own blob-scale content it decodes 2.2x faster
// with two workers than serially, because block reads and checksums also
// move off the critical path.
//
// Worker lifecycle, measured against pierrec/lz4 v4.1.27: workers exit
// cleanly when the stream is fully drained to EOF and when the underlying
// source errors mid-stream — but ABANDONING a partially-consumed reader
// parks its workers (and their block buffers) permanently, as the library
// exposes no cancellation. Consumers that stop short of EOF by design (a
// tar reader halting at the end-of-archive marker, say) must drain the
// remainder before dropping the reader.
func New(reader io.Reader) (io.ReadCloser, error) {
	decoder := plz4.NewReader(reader)
	if err := decoder.Apply(plz4.ConcurrencyOption(-1)); err != nil {
		return nil, fmt.Errorf("%w: lz4 concurrency: %w", fault.ErrNotImplemented, err)
	}

	return io.NopCloser(decoder), nil
}

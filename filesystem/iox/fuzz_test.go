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

// The fuzz targets play arbitrary streams and call sequences, past the bounded
// check's sizes, held to the same contract.

import (
	"io"
	"testing"

	"github.com/mycophonic/primordium/filesystem/iox"
)

// decode turns script into calls, three bytes each: what (a read, or a seek
// from one of the three whences), a size or offset, and its sign.
func decode(script []byte) []op {
	var ops []op

	for len(script) >= 3 {
		kind, amount, sign := script[0]%4, int64(script[1]), script[2]%2
		script = script[3:]

		if kind == 0 {
			ops = append(ops, op{size: int(amount)})

			continue
		}

		if sign == 1 {
			amount = -amount
		}

		ops = append(
			ops,
			op{seek: true, offset: amount, whence: []int{io.SeekStart, io.SeekCurrent, io.SeekEnd}[kind-1]},
		)
	}

	return ops
}

func FuzzReadSeeker(f *testing.F) {
	f.Add([]byte("abcdefgh"), byte(2), byte(0), byte(0), []byte{0, 1, 0, 1, 2, 1, 0, 1, 0})
	f.Add([]byte(""), byte(1), byte(1), byte(0), []byte{1, 1, 0, 0, 1, 0})
	f.Add([]byte("abc"), byte(4), byte(0), byte(2), []byte{0, 1, 0, 2, 1, 0, 0, 2, 0, 3, 0, 1})

	f.Fuzz(func(t *testing.T, data []byte, bufferSize, chunk, fault byte, script []byte) {
		faultAt := int(fault) - 1 // 0 for none

		if err := runReadSeeker(data, int(bufferSize%16)+1, int(chunk%4), faultAt, decode(script)); err != nil {
			t.Fatalf("stream %q, buffer %d, chunk %d, fault %d: %v", data, bufferSize%16+1, chunk%4, faultAt, err)
		}
	})
}

func FuzzReader(f *testing.F) {
	f.Add([]byte("abcdefgh"), byte(2), byte(0), byte(0), []byte{1, 3, 8, 0})
	f.Add([]byte(""), byte(1), byte(1), byte(1), []byte{1})

	f.Fuzz(func(t *testing.T, data []byte, bufferSize, chunk, fault byte, sizes []byte) {
		reads := make([]int, len(sizes))
		for i, size := range sizes {
			reads[i] = int(size)
		}

		src := newSource(data, int(chunk%4))
		src.faultAt = int(fault) - 1

		if err := runReader(iox.NewReaderWithSize(src, int(bufferSize%16)+1), src, data, reads); err != nil {
			t.Fatalf("stream %q, buffer %d, chunk %d: %v", data, bufferSize%16+1, chunk%4, err)
		}
	})
}

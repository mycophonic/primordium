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

package simd_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/mycophonic/primordium/simd"
)

// floats reads data as little-endian float32 values, the tail byte dropped.
func floats(data []byte) []float32 {
	out := make([]float32, len(data)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[4*i:]))
	}

	return out
}

// FuzzDotFloat32: whatever the operands, bit patterns included, the SIMD dot
// product is the scalar one within tolerance, or both are NaN.
func FuzzDotFloat32(f *testing.F) {
	f.Add([]byte{}, []byte{})
	f.Add([]byte{0, 0, 0x80, 0x3f}, []byte{0, 0, 0, 0x40})                      // 1.0 · 2.0
	f.Add([]byte{0, 0, 0xc0, 0x7f, 0, 0, 0x80, 0x3f}, []byte{0, 0, 0x80, 0x3f}) // NaN, 1.0 · 1.0

	f.Fuzz(func(t *testing.T, a, b []byte) {
		first, second := floats(a), floats(b)

		got := simd.DotFloat32(first, second)
		sum, budget := exact(first, second)

		switch {
		case math.IsNaN(sum):
			if !math.IsNaN(float64(got)) {
				t.Fatalf("len %d/%d: got %v, want NaN", len(first), len(second), got)
			}
		case math.IsInf(sum, 0) || math.Abs(sum) > math.MaxFloat32 || budget > math.MaxFloat32:
			// Beyond float32: an infinity, or a NaN from infinities of both signs, either way.
			if !math.IsInf(float64(got), 0) && !math.IsNaN(float64(got)) {
				t.Fatalf("len %d/%d: got %v, want an overflow", len(first), len(second), got)
			}
		default:
			if !within(got, sum, budget) {
				t.Fatalf("len %d/%d: got %v, want %v ± %v", len(first), len(second), got, sum, budget)
			}
		}
	})
}

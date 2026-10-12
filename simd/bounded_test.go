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

// The bounded check: every length across the lanes and their tails, with
// values of every sign and magnitude, held to the scalar reference within the
// tolerance lane accumulation allows; the fixed-size product likewise.

import (
	"math"
	"math/rand/v2"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/simd"
)

// exact is the dot product summed in float64, the truth both the SIMD lanes
// and the scalar loop approximate; budget is the rounding they are allowed,
// one float32 ulp per term on the sum of the terms' magnitudes.
func exact(first, second []float32) (sum, budget float64) {
	for i := range min(len(first), len(second)) {
		term := float64(first[i]) * float64(second[i])
		sum += term
		budget += math.Abs(term)
	}

	return sum, budget * float64(len(first)+1) * 0x1p-23
}

// within reports whether got is the exact sum within the budget.
func within(got float32, sum, budget float64) bool {
	return math.Abs(float64(got)-sum) <= budget+0x1p-30
}

// values fills out with pseudo-random float32 values of both signs across
// magnitudes, the same for the same seed.
func values(out []float32, seed uint64) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))

	for i := range out {
		mantissa := rng.Float64()*2 - 1
		exponent := rng.IntN(24) - 12

		out[i] = float32(math.Ldexp(mantissa, exponent))
	}
}

// TestBoundedDot: lengths 0 to 70 cover the 4-, 8- and 16-lane widths and
// every tail after them; the operands may differ in length and may alias.
func TestBoundedDot(t *testing.T) {
	t.Parallel()

	for length := range 71 {
		for _, extra := range []int{0, 1, 5} {
			first := make([]float32, length)
			second := make([]float32, length+extra)
			values(first, uint64(length))
			values(second, uint64(length+1000+extra))

			sum, budget := exact(first, second)
			got := simd.DotFloat32(first, second)
			assert.Assert(
				t,
				within(got, sum, budget),
				"len %d/%d: got %v, want %v ± %v",
				length,
				length+extra,
				got,
				sum,
				budget,
			)

			got = simd.DotFloat32(second, first)
			assert.Assert(
				t,
				within(got, sum, budget),
				"len %d/%d swapped: got %v, want %v ± %v",
				length+extra,
				length,
				got,
				sum,
				budget,
			)
		}

		same := make([]float32, length)
		values(same, uint64(length))

		sum, budget := exact(same, same)
		got := simd.DotFloat32(same, same)
		assert.Assert(t, within(got, sum, budget), "len %d aliased: got %v, want %v ± %v", length, got, sum, budget)
	}
}

// TestDotSpecialValues: an infinity or a NaN in the operands reaches the
// result as the scalar sum carries it.
func TestDotSpecialValues(t *testing.T) {
	t.Parallel()

	specials := []float32{
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		float32(math.NaN()),
		0,
		float32(math.Copysign(0, -1)),
	}

	for _, special := range specials {
		for _, length := range []int{1, 4, 7, 16, 33} {
			for position := range length {
				first := make([]float32, length)
				second := make([]float32, length)

				values(first, 7)
				values(second, 11)

				first[position] = special

				want := dotFloat32Reference(first, second)
				got := simd.DotFloat32(first, second)

				switch {
				case math.IsNaN(float64(want)):
					assert.Assert(
						t,
						math.IsNaN(float64(got)),
						"len %d pos %d %v: got %v, want NaN",
						length,
						position,
						special,
						got,
					)
				case math.IsInf(float64(want), 0):
					assert.Equal(t, got, want, "len %d pos %d %v", length, position, special)
				default:
					sum, budget := exact(first, second)
					assert.Assert(
						t,
						within(got, sum, budget),
						"len %d pos %d %v: got %v, want %v",
						length,
						position,
						special,
						got,
						sum,
					)
				}
			}
		}
	}
}

// TestBoundedMatVec: random matrices and vectors across magnitudes against
// the reference, row by row.
func TestBoundedMatVec(t *testing.T) {
	t.Parallel()

	for seed := range uint64(64) {
		var (
			mat [64][32]float32
			vec [32]float32
			dst [64]float32
		)

		for i := range mat {
			values(mat[i][:], seed*64+uint64(i))
		}

		values(vec[:], seed+4096)

		simd.MatVecMul64x32(&dst, &mat, &vec)

		for i := range dst {
			sum, budget := exact(mat[i][:], vec[:])
			assert.Assert(
				t,
				within(dst[i], sum, budget),
				"seed %d row %d: got %v, want %v ± %v",
				seed,
				i,
				dst[i],
				sum,
				budget,
			)
		}
	}
}

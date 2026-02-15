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
	"math"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/simd"
)

func matVecMul64x32Reference(mat *[64][32]float32, vec *[32]float32) [64]float32 {
	var dst [64]float32

	for i := range 64 {
		var sum float32
		for j := range 32 {
			sum += mat[i][j] * vec[j]
		}

		dst[i] = sum
	}

	return dst
}

func TestMatVecMul64x32_Identity(t *testing.T) {
	t.Parallel()

	// Each row i has a 1.0 at column i (for i < 32), rest zeros.
	// Result should be vec itself for first 32 rows, zeros for rest.
	var mat [64][32]float32

	var vec [32]float32

	for i := range 32 {
		mat[i][i] = 1.0
		vec[i] = float32(i + 1)
	}

	var dst [64]float32
	simd.MatVecMul64x32(&dst, &mat, &vec)

	for i := range 32 {
		assert.Equal(t, dst[i], float32(i+1), "row %d", i)
	}

	for i := 32; i < 64; i++ {
		assert.Equal(t, dst[i], float32(0), "row %d", i)
	}
}

func TestMatVecMul64x32_AllZeros(t *testing.T) {
	t.Parallel()

	var mat [64][32]float32

	var vec [32]float32

	var dst [64]float32

	simd.MatVecMul64x32(&dst, &mat, &vec)

	for i := range 64 {
		assert.Equal(t, dst[i], float32(0), "row %d", i)
	}
}

func TestMatVecMul64x32_UniformRow(t *testing.T) {
	t.Parallel()

	// Every element in mat is 1.0, vec is 1..32.
	// Each row dot product should be sum(1..32) = 528.
	var mat [64][32]float32

	var vec [32]float32

	for i := range 64 {
		for j := range 32 {
			mat[i][j] = 1.0
		}
	}

	for j := range 32 {
		vec[j] = float32(j + 1)
	}

	var dst [64]float32
	simd.MatVecMul64x32(&dst, &mat, &vec)

	expected := float32(32 * 33 / 2) // 528

	for i := range 64 {
		assert.Equal(t, dst[i], expected, "row %d", i)
	}
}

func TestMatVecMul64x32_Negatives(t *testing.T) {
	t.Parallel()

	var mat [64][32]float32

	var vec [32]float32

	for j := range 32 {
		vec[j] = float32(j) - 15.5
	}

	for i := range 64 {
		for j := range 32 {
			mat[i][j] = float32(i) - float32(j)
		}
	}

	var dst [64]float32
	simd.MatVecMul64x32(&dst, &mat, &vec)

	expected := matVecMul64x32Reference(&mat, &vec)

	for i := range 64 {
		assert.Assert(t, closeEnough(dst[i], expected[i]),
			"row %d: got %v, want %v", i, dst[i], expected[i])
	}
}

func TestMatVecMul64x32_CosineMatrix(t *testing.T) {
	t.Parallel()

	// Simulate the actual MP3 synthesis cosine window values.
	var mat [64][32]float32

	var vec [32]float32

	for i := range 64 {
		for j := range 32 {
			mat[i][j] = float32(math.Cos(float64((16+i)*(2*j+1)) * (math.Pi / 64.0)))
		}
	}

	for j := range 32 {
		vec[j] = float32(math.Sin(float64(j) * 0.1))
	}

	var dst [64]float32
	simd.MatVecMul64x32(&dst, &mat, &vec)

	expected := matVecMul64x32Reference(&mat, &vec)

	for i := range 64 {
		assert.Assert(t, closeEnough(dst[i], expected[i]),
			"row %d: got %v, want %v", i, dst[i], expected[i])
	}
}

func BenchmarkMatVecMul64x32(b *testing.B) {
	var mat [64][32]float32

	var vec [32]float32

	var dst [64]float32

	for i := range 64 {
		for j := range 32 {
			mat[i][j] = float32(i*32+j) * 0.001
		}
	}

	for j := range 32 {
		vec[j] = float32(j) * 0.1
	}

	b.ResetTimer()

	for b.Loop() {
		simd.MatVecMul64x32(&dst, &mat, &vec)
	}
}

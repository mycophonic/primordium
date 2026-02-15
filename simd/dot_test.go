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

func dotFloat32Reference(first, second []float32) float32 {
	var sum float32

	count := min(len(first), len(second))
	for idx := range count {
		sum += first[idx] * second[idx]
	}

	return sum
}

func makeSequence(length int, start float32) []float32 {
	result := make([]float32, length)
	for idx := range result {
		result[idx] = start + float32(idx)
	}

	return result
}

func TestDotFloat32_EmptySlices(t *testing.T) {
	t.Parallel()

	assert.Equal(t, simd.DotFloat32(nil, nil), float32(0))
	assert.Equal(t, simd.DotFloat32([]float32{}, []float32{}), float32(0))
	assert.Equal(t, simd.DotFloat32(nil, []float32{1, 2, 3}), float32(0))
	assert.Equal(t, simd.DotFloat32([]float32{1, 2, 3}, nil), float32(0))
}

func TestDotFloat32_SingleElement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, simd.DotFloat32([]float32{3}, []float32{7}), float32(21))
	assert.Equal(t, simd.DotFloat32([]float32{-2}, []float32{5}), float32(-10))
}

func TestDotFloat32_MismatchedLengths(t *testing.T) {
	t.Parallel()

	first := []float32{1, 2, 3, 4, 5}
	second := []float32{10, 20, 30}
	expected := float32(1*10 + 2*20 + 3*30)

	assert.Equal(t, simd.DotFloat32(first, second), expected)
	assert.Equal(t, simd.DotFloat32(second, first), expected)
}

func TestDotFloat32_AllZeros(t *testing.T) {
	t.Parallel()

	zeros := make([]float32, 64)
	ones := makeSequence(64, 1)

	assert.Equal(t, simd.DotFloat32(zeros, ones), float32(0))
	assert.Equal(t, simd.DotFloat32(zeros, zeros), float32(0))
}

func TestDotFloat32_NegativeValues(t *testing.T) {
	t.Parallel()

	first := []float32{-1, -2, -3, -4}
	second := []float32{4, 3, 2, 1}
	expected := float32(-1*4 + -2*3 + -3*2 + -4*1) // -20

	assert.Equal(t, simd.DotFloat32(first, second), expected)
}

func TestDotFloat32_KnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		length int
	}{
		{name: "4 elements", length: 4},
		{name: "16 elements", length: 16},
		{name: "32 elements", length: 32},
		{name: "33 elements (16+16+1 tail)", length: 33},
		{name: "512 elements", length: 512},
		{name: "513 elements (tail)", length: 513},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first := makeSequence(testCase.length, 1)
			second := makeSequence(testCase.length, 0.5)
			expected := dotFloat32Reference(first, second)
			result := simd.DotFloat32(first, second)

			assert.Assert(t, closeEnough(result, expected),
				"length=%d: got %v, want %v", testCase.length, result, expected)
		})
	}
}

func TestDotFloat32_LargeVector(t *testing.T) {
	t.Parallel()

	const length = 2048

	first := makeSequence(length, 0.1)
	second := makeSequence(length, 0.2)
	expected := dotFloat32Reference(first, second)
	result := simd.DotFloat32(first, second)

	assert.Assert(t, closeEnough(result, expected),
		"length=%d: got %v, want %v", length, result, expected)
}

func TestDotFloat32_TailSizes(t *testing.T) {
	t.Parallel()

	for tailLen := 1; tailLen <= 19; tailLen++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()

			first := makeSequence(tailLen, 1)
			second := makeSequence(tailLen, 2)
			expected := dotFloat32Reference(first, second)
			result := simd.DotFloat32(first, second)

			assert.Assert(t, closeEnough(result, expected),
				"length=%d: got %v, want %v", tailLen, result, expected)
		})
	}
}

func closeEnough(got, want float32) bool {
	if got == want {
		return true
	}

	diff := float64(got) - float64(want)
	magnitude := math.Max(math.Abs(float64(got)), math.Abs(float64(want)))

	if magnitude == 0 {
		return math.Abs(diff) < 1e-7
	}

	// Absolute tolerance floor: differences below 1e-5 are insignificant
	// regardless of relative magnitude (handles values near zero).
	absDiff := math.Abs(diff)
	if absDiff < 1e-5 {
		return true
	}

	// Relative tolerance of 1e-5 accounts for FMA (fused multiply-add) vs
	// separate multiply+add rounding differences in SIMD vs scalar code.
	return absDiff/magnitude < 1e-5
}

func BenchmarkDotFloat32_32(b *testing.B) {
	first := makeSequence(32, 1)
	second := makeSequence(32, 0.5)

	b.ResetTimer()

	for b.Loop() {
		simd.DotFloat32(first, second)
	}
}

func BenchmarkDotFloat32_512(b *testing.B) {
	first := makeSequence(512, 1)
	second := makeSequence(512, 0.5)

	b.ResetTimer()

	for b.Loop() {
		simd.DotFloat32(first, second)
	}
}

func BenchmarkDotFloat32_1024(b *testing.B) {
	first := makeSequence(1024, 1)
	second := makeSequence(1024, 0.5)

	b.ResetTimer()

	for b.Loop() {
		simd.DotFloat32(first, second)
	}
}

//go:build !arm64 && !amd64

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

package simd

//revive:disable:add-constant
func matVecProduct64x32(
	dst *[64]float32,
	mat *[64][32]float32,
	vec *[32]float32,
) {
	for i := range 64 {
		var sum float32

		row := &mat[i]
		for j := range 32 {
			sum += row[j] * vec[j]
		}

		dst[i] = sum
	}
}

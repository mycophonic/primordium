//go:build amd64

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

//revive:disable:add-constant // Array dimensions are fixed by the SIMD assembly contract.

//go:noescape
func matVecProduct64x32SSE(
	dst *[64]float32,
	mat *[64][32]float32,
	vec *[32]float32,
)

func matVecProduct64x32(
	dst *[64]float32,
	mat *[64][32]float32,
	vec *[32]float32,
) {
	matVecProduct64x32SSE(dst, mat, vec)
}

//revive:enable:add-constant

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

//revive:disable:add-constant
package simd

// MatVecMul64x32 computes dst = mat × vec, where mat is a 64×32 matrix
// and vec is a 32-element vector, producing a 64-element result.
//
// This is the core primitive for the polyphase synthesis filterbank in
// MP3 decoding. Using fixed dimensions allows the SIMD implementation
// to keep the vector in registers across all 64 row dot products,
// eliminating per-row function call overhead.
func MatVecMul64x32(
	dst *[64]float32,
	mat *[64][32]float32,
	vec *[32]float32,
) {
	matVecProduct64x32(dst, mat, vec)
}

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

// Package simd provides SIMD-accelerated operations for float32 slices:
// NEON on arm64, SSE on amd64, a scalar loop elsewhere. The sums are
// accumulated in lanes, so a result may differ from the scalar sum in its
// last bits; callers compare with a tolerance, never for equality.
//
// All functions are safe for concurrent use.
package simd

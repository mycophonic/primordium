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

import "github.com/klauspost/cpuid/v2"

// hasAVX2 is true when the CPU supports AVX2 instructions.
// Set once at init, read-only thereafter. Will be read by future AVX2
// dispatch paths in dot_amd64.go and matvec_amd64.go.
var hasAVX2 bool //nolint:gochecknoglobals

//nolint:gochecknoinits // Runtime CPU feature detection must run at init.
func init() {
	hasAVX2 = cpuid.CPU.Supports(cpuid.AVX2)
}

// Ensure hasAVX2 is not flagged as unused before AVX2 dispatch is wired up.
var _ = hasAVX2

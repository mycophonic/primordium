// Copyright Mycophonic.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include "textflag.h"

// NEON WORD encodings for vector float32 instructions not supported
// by the Go arm64 assembler.
//
// FMLA Vd.4S, Vn.4S, Vm.4S  = 0x4E20CC00 | Vm<<16 | Vn<<5 | Vd
// FADD Vd.4S, Vn.4S, Vm.4S  = 0x4E20D400 | Vm<<16 | Vn<<5 | Vd
// FADDP Vd.4S, Vn.4S, Vm.4S = 0x6E20D400 | Vm<<16 | Vn<<5 | Vd
// FADDP Sd, Vn.2S            = 0x7E30D800 | Vn<<5  | Vd

// func dotFloat32(first, second []float32) float32
//
// Caller guarantees len(first) == len(second) > 0.
TEXT ·dotProductF32(SB), NOSPLIT, $0-52
	MOVD first_base+0(FP), R0   // R0 = &first[0]
	MOVD first_len+8(FP), R1    // R1 = count
	MOVD second_base+24(FP), R2 // R2 = &second[0]

	// Zero 4 accumulator registers.
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16

	// --- Main loop: 16 elements per iteration ---
	CMP  $16, R1
	BLT  loop4

loop16:
	// Load 16 floats from first.
	VLD1.P 16(R0), [V4.S4]
	VLD1.P 16(R0), [V5.S4]
	VLD1.P 16(R0), [V6.S4]
	VLD1.P 16(R0), [V7.S4]

	// Load 16 floats from second.
	VLD1.P 16(R2), [V8.S4]
	VLD1.P 16(R2), [V9.S4]
	VLD1.P 16(R2), [V10.S4]
	VLD1.P 16(R2), [V11.S4]

	// Fused multiply-add into 4 accumulators.
	WORD $0x4E28CC80 // FMLA V0.4S, V4.4S, V8.4S
	WORD $0x4E29CCA1 // FMLA V1.4S, V5.4S, V9.4S
	WORD $0x4E2ACCC2 // FMLA V2.4S, V6.4S, V10.4S
	WORD $0x4E2BCCE3 // FMLA V3.4S, V7.4S, V11.4S

	SUB $16, R1
	CMP $16, R1
	BGE loop16

	// Merge 4 accumulators into V0.
	WORD $0x4E21D400 // FADD V0.4S, V0.4S, V1.4S
	WORD $0x4E23D442 // FADD V2.4S, V2.4S, V3.4S
	WORD $0x4E22D400 // FADD V0.4S, V0.4S, V2.4S

	// --- Tail loop: 4 elements per iteration ---
loop4:
	CMP $4, R1
	BLT reduce

	VLD1.P 16(R0), [V4.S4]
	VLD1.P 16(R2), [V5.S4]
	WORD $0x4E25CC80 // FMLA V0.4S, V4.4S, V5.4S

	SUB $4, R1
	B   loop4

reduce:
	// Horizontal sum: 4 lanes -> 1 scalar.
	WORD $0x6E20D400 // FADDP V0.4S, V0.4S, V0.4S  -> [a+b, c+d, a+b, c+d]
	WORD $0x7E30D800 // FADDP S0, V0.2S             -> a+b+c+d

	// --- Scalar tail: remaining 0-3 elements ---
	CBZ R1, done

scalar:
	FMOVS (R0), F1
	FMOVS (R2), F2
	FMULS F1, F2, F1
	FADDS F0, F1, F0
	ADD   $4, R0
	ADD   $4, R2
	SUB   $1, R1
	CBNZ  R1, scalar

done:
	FMOVS F0, ret+48(FP)
	RET

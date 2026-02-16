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

// NEON WORD encodings (Go arm64 assembler lacks vector float mnemonics):
//
// FMLA Vd.4S, Vn.4S, Vm.4S  = 0x4E20CC00 | Vm<<16 | Vn<<5 | Vd
// FADD Vd.4S, Vn.4S, Vm.4S  = 0x4E20D400 | Vm<<16 | Vn<<5 | Vd
// FADDP Vd.4S, Vn.4S, Vm.4S = 0x6E20D400 | Vm<<16 | Vn<<5 | Vd
// FADDP Sd, Vn.2S            = 0x7E30D800 | Vn<<5  | Vd

// func matVecProduct64x32(dst *[64]float32, mat *[64][32]float32, vec *[32]float32)
//
// Computes dst[i] = dot(mat[i], vec) for i in 0..63.
// The 32-element vector is loaded once into V16-V23 and reused for all 64 rows.
TEXT ·matVecProduct64x32(SB), NOSPLIT, $0-24
	MOVD dst+0(FP), R0       // R0 = &dst[0]
	MOVD mat+8(FP), R1       // R1 = &mat[0][0]
	MOVD vec+16(FP), R2      // R2 = &vec[0]

	// Load vec[0:32] into V16-V23 (8 regs × 4 floats = 32 floats).
	// These stay in registers for all 64 row iterations.
	VLD1.P 16(R2), [V16.S4]
	VLD1.P 16(R2), [V17.S4]
	VLD1.P 16(R2), [V18.S4]
	VLD1.P 16(R2), [V19.S4]
	VLD1.P 16(R2), [V20.S4]
	VLD1.P 16(R2), [V21.S4]
	VLD1.P 16(R2), [V22.S4]
	VLD1   (R2), [V23.S4]

	MOVD $64, R3              // row counter

row:
	// Zero 2 accumulator registers.
	VEOR V24.B16, V24.B16, V24.B16
	VEOR V25.B16, V25.B16, V25.B16

	// Load row[0:32] into V0-V7 (32 floats, 128 bytes per row).
	VLD1.P 16(R1), [V0.S4]
	VLD1.P 16(R1), [V1.S4]
	VLD1.P 16(R1), [V2.S4]
	VLD1.P 16(R1), [V3.S4]
	VLD1.P 16(R1), [V4.S4]
	VLD1.P 16(R1), [V5.S4]
	VLD1.P 16(R1), [V6.S4]
	VLD1.P 16(R1), [V7.S4]

	// Fused multiply-add: 2 accumulators interleaved for ILP.
	WORD $0x4E30CC18          // FMLA V24.4S, V0.4S, V16.4S
	WORD $0x4E31CC39          // FMLA V25.4S, V1.4S, V17.4S
	WORD $0x4E32CC58          // FMLA V24.4S, V2.4S, V18.4S
	WORD $0x4E33CC79          // FMLA V25.4S, V3.4S, V19.4S
	WORD $0x4E34CC98          // FMLA V24.4S, V4.4S, V20.4S
	WORD $0x4E35CCB9          // FMLA V25.4S, V5.4S, V21.4S
	WORD $0x4E36CCD8          // FMLA V24.4S, V6.4S, V22.4S
	WORD $0x4E37CCF9          // FMLA V25.4S, V7.4S, V23.4S

	// Reduce: merge accumulators → horizontal sum → scalar.
	WORD $0x4E39D718          // FADD V24.4S, V24.4S, V25.4S
	WORD $0x6E38D718          // FADDP V24.4S, V24.4S, V24.4S
	WORD $0x7E30DB18          // FADDP S24, V24.2S

	// Store scalar result and advance dst pointer.
	FMOVS F24, (R0)
	ADD   $4, R0

	SUB   $1, R3
	CBNZ  R3, row

	RET

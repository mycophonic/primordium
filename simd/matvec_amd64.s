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

// HADDPS X0, X0 -- SSE3 horizontal add, encoded as raw bytes because
// the Go assembler may not support the mnemonic directly.
#define HADDPS_X0_X0 LONG $0xC07C0FF2

// func matVecProduct64x32(dst *[64]float32, mat *[64][32]float32, vec *[32]float32)
//
// Computes dst[i] = dot(mat[i], vec) for i in 0..63.
// The 32-element vector is loaded once into X8-X15 and reused for all 64 rows.
TEXT ·matVecProduct64x32(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), AX       // AX = &dst[0]
	MOVQ mat+8(FP), SI       // SI = &mat[0][0]
	MOVQ vec+16(FP), DI      // DI = &vec[0]

	// Load vec[0:32] into X8-X15 (8 regs × 4 floats = 32 floats).
	// These stay in registers for all 64 row iterations.
	MOVUPS (DI), X8
	MOVUPS 16(DI), X9
	MOVUPS 32(DI), X10
	MOVUPS 48(DI), X11
	MOVUPS 64(DI), X12
	MOVUPS 80(DI), X13
	MOVUPS 96(DI), X14
	MOVUPS 112(DI), X15

	MOVQ $64, CX              // row counter

row:
	// Load row[0:32], multiply with vec, accumulate into X0/X1.
	// Two independent accumulator chains for instruction-level parallelism.
	MOVUPS (SI), X0
	MULPS  X8, X0              // acc0 = row[0:4] * vec[0:4]
	MOVUPS 16(SI), X1
	MULPS  X9, X1              // acc1 = row[4:8] * vec[4:8]

	MOVUPS 32(SI), X2
	MULPS  X10, X2
	ADDPS  X2, X0              // acc0 += row[8:12] * vec[8:12]

	MOVUPS 48(SI), X3
	MULPS  X11, X3
	ADDPS  X3, X1              // acc1 += row[12:16] * vec[12:16]

	MOVUPS 64(SI), X4
	MULPS  X12, X4
	ADDPS  X4, X0              // acc0 += row[16:20] * vec[16:20]

	MOVUPS 80(SI), X5
	MULPS  X13, X5
	ADDPS  X5, X1              // acc1 += row[20:24] * vec[20:24]

	MOVUPS 96(SI), X6
	MULPS  X14, X6
	ADDPS  X6, X0              // acc0 += row[24:28] * vec[24:28]

	MOVUPS 112(SI), X7
	MULPS  X15, X7
	ADDPS  X7, X1              // acc1 += row[28:32] * vec[28:32]

	// Merge accumulators and horizontal reduce.
	ADDPS  X1, X0              // acc0 += acc1
	HADDPS_X0_X0               // [a+b, c+d, a+b, c+d]
	HADDPS_X0_X0               // [sum, sum, sum, sum]

	// Store scalar result and advance pointers.
	MOVSS  X0, (AX)

	ADDQ   $128, SI            // next row (32 × 4 bytes)
	ADDQ   $4, AX              // next dst element
	DECQ   CX
	JNZ    row

	RET

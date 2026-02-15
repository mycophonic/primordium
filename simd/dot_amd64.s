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

// func dotFloat32(first, second []float32) float32
//
// Caller guarantees len(first) == len(second) > 0.
TEXT ·dotProductF32(SB), NOSPLIT, $0-52
	MOVQ  first_base+0(FP), SI  // SI = &first[0]
	MOVQ  first_len+8(FP), CX   // CX = count
	MOVQ  second_base+24(FP), DI // DI = &second[0]

	XORPS X0, X0                 // acc0 = 0
	XORPS X1, X1                 // acc1 = 0
	XORQ  AX, AX                 // index = 0

	// --- Main loop: 16 elements per iteration ---
	MOVQ CX, BX
	SHRQ $4, BX                  // BX = count / 16
	JZ   loop4_setup

loop16:
	MOVUPS (SI)(AX*4), X2        // load first[i:i+4]
	MOVUPS (DI)(AX*4), X6        // load second[i:i+4]
	MULPS  X6, X2                // first[i:i+4] * second[i:i+4]

	MOVUPS 16(SI)(AX*4), X3      // load first[i+4:i+8]
	MOVUPS 16(DI)(AX*4), X6      // load second[i+4:i+8]
	MULPS  X6, X3                // first[i+4:i+8] * second[i+4:i+8]

	MOVUPS 32(SI)(AX*4), X4      // load first[i+8:i+12]
	MOVUPS 32(DI)(AX*4), X6      // load second[i+8:i+12]
	MULPS  X6, X4                // first[i+8:i+12] * second[i+8:i+12]

	MOVUPS 48(SI)(AX*4), X5      // load first[i+12:i+16]
	MOVUPS 48(DI)(AX*4), X6      // load second[i+12:i+16]
	MULPS  X6, X5                // first[i+12:i+16] * second[i+12:i+16]

	ADDPS  X2, X0                // acc0 += X2 (interleaved for ILP)
	ADDPS  X3, X1                // acc1 += X3
	ADDPS  X4, X0                // acc0 += X4
	ADDPS  X5, X1                // acc1 += X5

	ADDQ   $16, AX
	DECQ   BX
	JNZ    loop16

	ADDPS  X1, X0                // merge accumulators

	// --- Tail loop: 4 elements per iteration ---
loop4_setup:
	MOVQ  CX, BX
	ANDQ  $0xF, BX               // BX = count % 16
	SHRQ  $2, BX                 // BX = remaining / 4
	JZ    hreduce

loop4:
	MOVUPS (SI)(AX*4), X2
	MOVUPS (DI)(AX*4), X6        // load second (unaligned-safe)
	MULPS  X6, X2
	ADDPS  X2, X0

	ADDQ   $4, AX
	DECQ   BX
	JNZ    loop4

	// --- Horizontal reduce: 4 lanes -> 1 scalar ---
hreduce:
	HADDPS_X0_X0                 // [a+b, c+d, a+b, c+d]
	HADDPS_X0_X0                 // [sum, sum, sum, sum]

	// --- Scalar tail: remaining 0-3 elements ---
	MOVQ  CX, BX
	ANDQ  $3, BX                 // BX = count % 4
	JZ    done

scalar:
	MOVSS  (SI)(AX*4), X2
	MULSS  (DI)(AX*4), X2
	ADDSS  X2, X0

	INCQ   AX
	DECQ   BX
	JNZ    scalar

done:
	MOVSS  X0, ret+48(FP)
	RET

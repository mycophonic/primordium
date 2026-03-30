# simd — Code & API Audit

Audited: 2026-03-29
Platform: darwin/arm64 (Apple M1 Max)
Go: 1.24

---

## Package Overview

SIMD-accelerated float32 dot product and matrix-vector multiply.
Three tiers: ARM64 NEON, x86-64 SSE, pure Go scalar fallback.

Single external caller: `saprobe-mp3/internal/frame` (polyphase synthesis filterbank).

## File Inventory

| File | Purpose |
|---|---|
| `doc.go` | Package documentation (mentions runtime cpuid detection for future AVX2 dispatch) |
| `cpu_amd64.go` | AVX2 CPUID detection via `klauspost/cpuid/v2`; sets `hasAVX2` at init |
| `dot.go` | `DotFloat32` public API (variable-length) |
| `dot_generic.go` | Scalar fallback (`!arm64 && !amd64`) |
| `dot_arm64.go` | NEON assembly declaration |
| `dot_arm64.s` | NEON dot product implementation |
| `dot_amd64.go` | SSE wrapper: declares `dotProductF32SSE`, wraps as `dotProductF32` |
| `dot_amd64.s` | SSE dot product implementation (`dotProductF32SSE`) |
| `matvec.go` | `MatVecMul64x32` public API (fixed 64x32) |
| `matvec_generic.go` | Scalar fallback |
| `matvec_arm64.go` | NEON assembly declaration |
| `matvec_arm64.s` | NEON matrix-vector implementation |
| `matvec_amd64.go` | SSE wrapper: declares `matVecProduct64x32SSE`, wraps as `matVecProduct64x32` |
| `matvec_amd64.s` | SSE matrix-vector implementation (`matVecProduct64x32SSE`) |
| `dot_test.go` | DotFloat32 tests and benchmarks |
| `matvec_test.go` | MatVecMul64x32 tests and benchmarks |

17 files total. Clean separation. No dead files.

---

## API Assessment

### `DotFloat32(first, second []float32) float32`

Well designed. Handles nil, empty, and mismatched lengths cleanly at the Go level
before dispatching to the assembly. The `min(len(first), len(second))` truncation
is documented and intuitive.

The re-slicing `first[:count], second[:count]` before calling `dotProductF32`
is important: it gives the assembly a single `count` to work with and ensures
both slices have identical length. This is clean.

### `MatVecMul64x32(dst *[64]float32, mat *[64][32]float32, vec *[32]float32)`

Fixed dimensions are the right call for this use case. The pointer-to-array API
eliminates bounds checking in the assembly and makes the contract explicit: this
is a 64x32 multiply, nothing else. The caller (`saprobe-mp3`) uses exactly a
64x32 cosine matrix for polyphase synthesis, so the dimensions are a perfect fit.

No nil-guard on the pointers. This is correct — nil pointers to fixed-size arrays
are a programming error, not a runtime condition. Panicking via nil dereference in
the assembly is the appropriate behavior.

### Missing API

`DotFloat32` accepts slices of any length but the only production caller
(`MatVecMul64x32`) uses fixed 32-element dot products internally via its own
assembly. `DotFloat32` has no production callers today. It exists as public API
presumably for future use or direct use by external consumers. This is fine; it's
a natural companion operation and the marginal maintenance cost is low.

---

## Assembly Correctness

### ARM64 NEON — Instruction Encoding Verification

The Go arm64 assembler lacks mnemonics for vector float operations (FMLA, FADD,
FADDP), so they are encoded as raw `WORD` directives. The encoding formulas are
documented in each `.s` file:

```
FMLA  Vd.4S, Vn.4S, Vm.4S  = 0x4E20CC00 | Vm<<16 | Vn<<5 | Vd
FADD  Vd.4S, Vn.4S, Vm.4S  = 0x4E20D400 | Vm<<16 | Vn<<5 | Vd
FADDP Vd.4S, Vn.4S, Vm.4S  = 0x6E20D400 | Vm<<16 | Vn<<5 | Vd
FADDP Sd, Vn.2S             = 0x7E30D800 | Vn<<5  | Vd
```

Every WORD encoding was verified against these formulas:

**dot_arm64.s** (5 FMLA + 3 FADD + 2 FADDP = 10 encoded instructions):

| Line | Encoding | Decoded | Correct |
|---|---|---|---|
| 57 | `0x4E28CC80` | FMLA V0.4S, V4.4S, V8.4S | Yes |
| 58 | `0x4E29CCA1` | FMLA V1.4S, V5.4S, V9.4S | Yes |
| 59 | `0x4E2ACCC2` | FMLA V2.4S, V6.4S, V10.4S | Yes |
| 60 | `0x4E2BCCE3` | FMLA V3.4S, V7.4S, V11.4S | Yes |
| 67 | `0x4E21D400` | FADD V0.4S, V0.4S, V1.4S | Yes |
| 68 | `0x4E23D442` | FADD V2.4S, V2.4S, V3.4S | Yes |
| 69 | `0x4E22D400` | FADD V0.4S, V0.4S, V2.4S | Yes |
| 78 | `0x4E25CC80` | FMLA V0.4S, V4.4S, V5.4S | Yes |
| 85 | `0x6E20D400` | FADDP V0.4S, V0.4S, V0.4S | Yes |
| 86 | `0x7E30D800` | FADDP S0, V0.2S | Yes |

**matvec_arm64.s** (8 FMLA + 1 FADD + 2 FADDP = 11 encoded instructions):

| Line | Encoding | Decoded | Correct |
|---|---|---|---|
| 62 | `0x4E30CC18` | FMLA V24.4S, V0.4S, V16.4S | Yes |
| 63 | `0x4E31CC39` | FMLA V25.4S, V1.4S, V17.4S | Yes |
| 64 | `0x4E32CC58` | FMLA V24.4S, V2.4S, V18.4S | Yes |
| 65 | `0x4E33CC79` | FMLA V25.4S, V3.4S, V19.4S | Yes |
| 66 | `0x4E34CC98` | FMLA V24.4S, V4.4S, V20.4S | Yes |
| 67 | `0x4E35CCB9` | FMLA V25.4S, V5.4S, V21.4S | Yes |
| 68 | `0x4E36CCD8` | FMLA V24.4S, V6.4S, V22.4S | Yes |
| 69 | `0x4E37CCF9` | FMLA V25.4S, V7.4S, V23.4S | Yes |
| 72 | `0x4E39D718` | FADD V24.4S, V24.4S, V25.4S | Yes |
| 73 | `0x6E38D718` | FADDP V24.4S, V24.4S, V24.4S | Yes |
| 74 | `0x7E30DB18` | FADDP S24, V24.2S | Yes |

All 21 WORD encodings are correct.

### x86-64 SSE — Instruction Encoding Verification

Single encoded instruction: `HADDPS XMM0, XMM0` (SSE3).

```
HADDPS xmm0, xmm0 → F2 0F 7C C0 → little-endian LONG = 0xC07C0FF2
```

The macro `#define HADDPS_X0_X0 LONG $0xC07C0FF2` is correct.

Note: HADDPS always operates on XMM0 in both files. This is fine because X0 is
the sole accumulator (or the merged accumulator) at the point of reduction.

### ARM64 NEON — Algorithm Analysis

**dot_arm64.s:**
- Main loop: 16 elements/iteration using 4 independent accumulator registers (V0-V3).
  Each FMLA feeds a different accumulator, maximizing ILP on out-of-order cores.
  On Apple M-series, the NEON FP pipeline can sustain 4 FMLA/cycle with sufficient
  independent destinations.
- Accumulator merge: 3 FADD instructions to reduce V0-V3 → V0. This is a
  dependency chain but only executes once per call (not per iteration).
- Tail loop (4 elements): single accumulator, acceptable for ≤12 remaining elements.
- Horizontal reduction: FADDP V0.4S + FADDP S0 reduces 4 lanes to scalar.
- Scalar tail (0-3 elements): FMOVS/FMULS/FADDS loop.

The VLD1.P post-increment loads are used correctly throughout. The last VLD1 in
`matvec_arm64.s` (line 42) correctly uses `VLD1` without `.P` for the final
vector segment since the pointer is not needed afterward.

**matvec_arm64.s:**
- Vector (V16-V23) loaded once, reused across all 64 rows. This eliminates 63
  redundant 128-byte loads per call.
- Per-row: 8 VLD1.P loads + 8 FMLA + 1 FADD + 2 FADDP + 1 FMOVS store.
- 2 accumulators (V24, V25) interleaved: FMLA instructions alternate destinations
  to avoid back-to-back dependencies on the same register.
- Row data loaded into V0-V7 (callee-saved V8-V15 are not used for temporaries,
  which is correct — though in Go plan9 asm, callee-saved conventions differ from
  the platform ABI, so this is more of a good practice than a strict requirement).

### x86-64 SSE — Algorithm Analysis

**dot_amd64.s:**
- Main loop: 16 elements/iteration, 2 accumulator chains (X0, X1). Each iteration:
  4 MOVUPS loads + 4 MULPS + 4 ADDPS, with products alternating between X0 and X1.
- Accumulator merge: `ADDPS X1, X0` after the main loop.
- Tail loop (4 elements): single chain, adequate.
- Horizontal reduction: 2x HADDPS reduces 4 lanes → scalar.
- Scalar tail: MOVSS/MULSS/ADDSS for 0-3 remaining.

This uses separate MULPS + ADDPS rather than fused multiply-add. x86-64 SSE does
not have FMA; that requires FMA3 (Haswell+). This is the correct approach for an
SSE3 baseline.

**matvec_amd64.s:**
- Vector pre-loaded into X8-X15 (8 registers × 4 floats = 32 floats), reused
  across all 64 rows. Same optimization as the ARM64 version.
- Per-row: 8 MOVUPS loads into X0-X7, 8 MULPS with X8-X15, then ADDPS reduction
  with 2 accumulator chains, HADDPS horizontal sum, MOVSS store.
- Uses all 16 XMM registers (X0-X15). This is efficient but means there are zero
  spare registers. The current algorithm doesn't need any, so this is fine.

### Register Usage Summary

| | ARM64 | x86-64 |
|---|---|---|
| **dot** accumulators | V0-V3 (4) | X0-X1 (2) |
| **dot** temporaries | V4-V11 | X2-X6 |
| **matvec** vector | V16-V23 (8 regs) | X8-X15 (8 regs) |
| **matvec** accumulators | V24-V25 (2) | X0-X1 (2) |
| **matvec** row data | V0-V7 (8) | X0-X7 (8) |

ARM64 has 32 NEON registers; x86-64 has 16 XMM registers. Both implementations
use their register files efficiently without spilling.

### Frame Sizes

All four assembly functions use `$0-N` frame sizes (no local stack frame), which
is correct for leaf functions that don't call other functions or need stack space.
The `NOSPLIT` flag is appropriate since the stack usage is zero.

---

## Numerical Precision

ARM64 uses FMLA (fused multiply-add): `acc += a * b` is computed with a single
rounding. x86-64 uses MULPS + ADDPS (separate multiply and add): two rounding
operations. The scalar fallback also uses separate multiply and add.

This means ARM64 results may differ slightly from x86-64 and scalar results for
the same inputs, due to intermediate rounding. The test suite accounts for this
with the `closeEnough` tolerance function (relative tolerance of 1e-5, absolute
floor of 1e-5).

For the actual use case (MP3 polyphase synthesis), this precision difference is
insignificant — MP3 output is 16-bit PCM, which has ~4.5 decimal digits of
precision. The 1e-5 tolerance is many orders of magnitude tighter than needed.

---

## Test Quality

### Coverage

100.0% statement coverage across all Go files.

### DotFloat32 Tests (8 tests)

| Test | What it covers |
|---|---|
| `EmptySlices` | nil/nil, empty/empty, nil/non-nil, non-nil/nil |
| `SingleElement` | Positive and negative single-element products |
| `MismatchedLengths` | Truncation to min length, symmetric |
| `AllZeros` | Zero × non-zero, zero × zero |
| `NegativeValues` | Negative × positive mixed signs |
| `KnownValues` | Sizes 4, 16, 32, 33, 512, 513 vs reference |
| `LargeVector` | 2048 elements vs reference |
| `TailSizes` | Exhaustive 1-19 vs reference (exercises all loop tiers) |

`TailSizes` is particularly valuable: it covers every combination of main loop
iterations (0 or 1) × tail loop iterations (0-3) × scalar tail (0-3), ensuring
the loop structure handles all residual counts correctly.

### MatVecMul64x32 Tests (5 tests)

| Test | What it covers |
|---|---|
| `Identity` | Partial identity matrix (32 non-zero rows + 32 zero rows) |
| `AllZeros` | Zero matrix × zero vector |
| `UniformRow` | All-ones matrix × sequential vector, exact result (528) |
| `Negatives` | Mixed sign values, vs reference |
| `CosineMatrix` | Realistic MP3 cosine window values, vs reference |

The `CosineMatrix` test directly simulates the production workload, which is
excellent. It uses `cos((16+i)*(2j+1)*pi/64)` — the actual DCT-IV matrix from
ISO 11172-3.

### Tolerance Function

`closeEnough` uses a hybrid approach:
1. Exact equality check (fast path).
2. Absolute tolerance floor of 1e-5 for near-zero values.
3. Relative tolerance of 1e-5 for larger magnitudes.

This is appropriate. The `magnitude == 0` guard (line 166) with absolute
tolerance 1e-7 is technically dead code — if both values are zero, the exact
equality check on line 159 catches it first. If one is zero and the other
isn't, `magnitude` is non-zero and falls through to the relative check. This
is harmless but the branch will never execute.

### Missing Test Considerations

- No test for subnormal float inputs. Not a concern for MP3 synthesis values.
- No test for NaN/Inf propagation. The assembly will propagate NaN per IEEE 754
  naturally. Not testing this is fine; NaN inputs are a programming error.
- No test for very large vectors (>64K elements). `DotFloat32` would work but
  floating-point accumulation error grows with vector size. Not relevant to the
  actual use case (max 32 elements in production).

---

## Benchmarks

```
BenchmarkDotFloat32_32       124,933,249    9.609 ns/op    0 B/op    0 allocs/op
BenchmarkDotFloat32_512       25,039,035    48.25 ns/op    0 B/op    0 allocs/op
BenchmarkDotFloat32_1024      12,124,216    98.72 ns/op    0 B/op    0 allocs/op
BenchmarkMatVecMul64x32        2,127,141    565.0 ns/op    0 B/op    0 allocs/op
```

(Apple M1 Max, arm64, Go 1.24)

Zero allocations across the board. Expected for assembly functions with no
heap interaction.

**DotFloat32 scaling:** 32→512 is 16x elements, 5.0x time. 512→1024 is 2x
elements, 2.05x time. Near-linear scaling above the fixed overhead threshold.

**MatVecMul64x32:** 565 ns for 64 × 32-element dot products = 8.8 ns per row.
This is consistent with the DotFloat32_32 benchmark (9.6 ns) minus the
per-call overhead (the vector stays in registers across rows).

For MP3 context: 565 ns per synthesis call × 36 calls per frame × 38.3
frames/second (128 kbps) ≈ 0.78 ms/second. Negligible.

### Missing Benchmarks

- No scalar fallback benchmark to quantify the SIMD speedup factor. Would be
  useful for documentation purposes but is not a correctness concern.

---

## Architecture & Organization

### Build Tag Dispatch

Clean three-way dispatch:
- `//go:build arm64` — NEON assembly
- `//go:build amd64` — SSE assembly
- `//go:build !arm64 && !amd64` — scalar fallback

Each platform has a `.go` declaration file with `//go:noescape` and a `.s`
implementation file. The generic fallback combines both in a single `.go` file.
This is standard Go assembly practice.

### SSE3 Assumption

The x86-64 code uses HADDPS, which requires SSE3 (Prescott, 2004). There is no
runtime feature detection via `golang.org/x/sys/cpu` or CPUID. This is a
reasonable decision: SSE3 is universally available on any x86-64 processor
manufactured since 2004. The Go runtime itself assumes SSE2, and SSE3 has been
baseline for over two decades.

### AVX2 Dispatch Infrastructure (New)

The x86-64 implementation is currently SSE-only (128-bit), but infrastructure
for runtime AVX2 dispatch is now in place:

- `cpu_amd64.go` detects AVX2 support at init time via `cpuid.CPU.Supports(cpuid.AVX2)`
  and stores the result in the `hasAVX2` package-level variable.
- `dot_amd64.go` and `matvec_amd64.go` now use SSE-suffixed assembly function
  names (`dotProductF32SSE`, `matVecProduct64x32SSE`) wrapped by Go-level
  `dotProductF32` / `matVecProduct64x32` functions. This indirection layer is
  the natural dispatch point for a future `if hasAVX2 { ... }` branch.

No AVX2 assembly exists yet. For the actual use case (32-element dot products
inside `MatVecMul64x32`), the benefit would be modest because the vector is
already register-resident and the loop body is only 8 iterations. The NEON
path handles the primary deployment target (Apple Silicon).

### `//go:noescape` Usage

All four assembly declarations use `//go:noescape`. This is critical for
performance: it tells the compiler that pointer arguments do not escape to the
heap, preventing unnecessary heap allocations at call sites. Correct usage here.

### NOSPLIT

All assembly functions are `NOSPLIT` with zero-size frames. This is correct —
they are leaf functions that don't grow the stack. The Go runtime won't need to
check for stack splits during these calls.

---

## Issues

### Severity: None (Informational Only)

1. **Dead branch in `closeEnough`** (dot_test.go:166-168): The `magnitude == 0`
   guard never fires because the exact equality check on line 159 catches the
   both-zero case first, and if only one value is zero, magnitude is non-zero.
   Harmless — the branch is a safety net that costs nothing.

2. **Accumulator count asymmetry**: `dot_arm64.s` uses 4 accumulators in the
   main loop; `dot_amd64.s` uses 2. This is appropriate — ARM64 has 32 NEON
   registers vs 16 XMM on x86-64, so ARM64 can afford more accumulators for
   deeper ILP without pressure.

---

## Verdict

Clean, correct, well-tested package. All 21 NEON WORD encodings and the SSE3
HADDPS encoding are verified correct. 100% test coverage. Zero allocations.
Algorithm design shows good understanding of SIMD ILP (interleaved accumulators,
register-resident vectors for `MatVecMul64x32`). API is minimal and fit for
purpose. No bugs found. No changes recommended.

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

package human_test

// The contract, from human's docs:
//   - ParseSize reads decimal digits with an optional fraction, then at most
//     one space, then a unit symbol or nothing: B, kB..EB (powers of 1000),
//     KiB..EiB (powers of 1024), in any case; the value is the exact product,
//     a fraction of a byte truncated; anything else is ErrInvalidSize (the
//     number) or ErrInvalidUnit (the symbol), both ErrInvalidArgument, and a
//     count past an int64 is ErrInvalidSize;
//   - DecimalSize and BinarySize print a size as a mantissa in [1, base) with
//     the largest unit that allows it, or in bytes below the base, to the
//     precision asked (four by default; a precision below 1 counts as 1, one
//     past 15 as 15);
//     what they print, ParseSize reads back to that precision.

import (
	"math/big"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/bytesize"
)

// units are every symbol ParseSize accepts, with the bytes each counts.
func units() map[string]int64 {
	return map[string]int64{
		"":    1,
		"B":   1,
		"kB":  bytesize.KB,
		"MB":  bytesize.MB,
		"GB":  bytesize.GB,
		"TB":  bytesize.TB,
		"PB":  bytesize.PB,
		"EB":  bytesize.EB,
		"KiB": bytesize.KiB,
		"MiB": bytesize.MiB,
		"GiB": bytesize.GiB,
		"TiB": bytesize.TiB,
		"PiB": bytesize.PiB,
		"EiB": bytesize.EiB,
	}
}

// cases of a symbol: as written, lower, upper.
func cases(symbol string) []string {
	return []string{symbol, strings.ToLower(symbol), strings.ToUpper(symbol)}
}

// exact is number times factor, truncated, computed as a rational: the model
// the parser is held to. ok is false where the result does not fit an int64.
func exact(t *testing.T, number string, factor int64) (value int64, ok bool) {
	t.Helper()

	rat, parsed := new(big.Rat).SetString(number)
	if !parsed {
		t.Fatalf("model cannot read %q", number)
	}

	rat.Mul(rat, new(big.Rat).SetInt64(factor))

	truncated := new(big.Int).Quo(rat.Num(), rat.Denom())

	return truncated.Int64(), truncated.IsInt64()
}

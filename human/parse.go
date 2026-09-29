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

package human

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
)

// float64Bits is the bit size strconv.ParseFloat parses a float64 at.
const float64Bits = 64

// ParseSize reads a size written as a number and an optional unit symbol:
// "32", "32B", "32.5 kB", "17MiB". The unit alone decides the value; case
// does not matter:
//
//   - B is a byte, and a number without a unit counts bytes;
//   - kB, MB, GB, TB, PB and EB are powers of 1000;
//   - KiB, MiB, GiB, TiB, PiB and EiB are powers of 1024.
//
// Anything else is ErrInvalidUnit: a bare "k", "KiBB", "bytes". The number
// is decimal digits with an optional fraction ("32", "32.5"), and one space
// may separate it from the unit. A fraction of a byte is truncated. A size
// an int64 cannot count is ErrInvalidSize.
func ParseSize(size string) (int64, error) {
	number, symbol := split(size)

	if !isDecimal(number) {
		return 0, fmt.Errorf("%w: %w: %q", fault.ErrInvalidArgument, ErrInvalidSize, size)
	}

	factor, known := unitFactor(symbol)
	if !known {
		return 0, fmt.Errorf("%w: %w: %q in %q", fault.ErrInvalidArgument, ErrInvalidUnit, symbol, size)
	}

	value, err := strconv.ParseFloat(number, float64Bits)
	if err != nil {
		return 0, fmt.Errorf("%w: %w: %q: %w", fault.ErrInvalidArgument, ErrInvalidSize, size, err)
	}

	// float64(math.MaxInt64) rounds up to 2^63, the first value an int64
	// cannot hold; converting one at or past it gives no defined result.
	value *= factor
	if value >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("%w: %w: %q does not fit an int64", fault.ErrInvalidArgument, ErrInvalidSize, size)
	}

	return int64(value), nil
}

// split cuts size where its number ends, dropping one space before the unit.
// A space with no unit after it is kept, so that the symbol is refused.
func split(size string) (number, symbol string) {
	end := strings.IndexFunc(size, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if end == -1 {
		return size, ""
	}

	rest := size[end:]
	if unit, spaced := strings.CutPrefix(rest, " "); spaced && unit != "" {
		return size[:end], unit
	}

	return size[:end], rest
}

// isDecimal reports whether number is digits with an optional fraction:
// "32" or "32.5", never "", ".5", "32." or "3.2.1".
func isDecimal(number string) bool {
	whole, fraction, hasPoint := strings.Cut(number, ".")

	return digits(whole) && (!hasPoint || digits(fraction))
}

// digits reports whether s is one or more ASCII digits.
func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// unitFactor is the number of bytes one symbol counts, whatever its case; an
// empty symbol is a byte.
func unitFactor(symbol string) (float64, bool) {
	switch strings.ToLower(symbol) {
	case "", "b":
		return 1, true
	case "kb":
		return bytesize.KB, true
	case "mb":
		return bytesize.MB, true
	case "gb":
		return bytesize.GB, true
	case "tb":
		return bytesize.TB, true
	case "pb":
		return bytesize.PB, true
	case "eb":
		return bytesize.EB, true
	case "kib":
		return bytesize.KiB, true
	case "mib":
		return bytesize.MiB, true
	case "gib":
		return bytesize.GiB, true
	case "tib":
		return bytesize.TiB, true
	case "pib":
		return bytesize.PiB, true
	case "eib":
		return bytesize.EiB, true
	default:
		return 0, false
	}
}

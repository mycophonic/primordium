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

// The bounded check: every unit in every case, spaced or not, with numbers
// around each power and with fractions of up to three digits, held to the
// exact model; every three-decimal kB and MB size; the formatters at every
// precision over sizes around each unit's edge, read back by ParseSize.

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/human"
)

func TestBoundedParse(t *testing.T) {
	t.Parallel()

	wholes := []string{"0", "1", "9", "10", "999", "1000", "1023", "1024", "9007199254740991", "9223372036854775807"}
	fractions := []string{"", ".0", ".1", ".5", ".9", ".01", ".99", ".001", ".125", ".999"}

	for symbol, factor := range units() {
		for _, written := range cases(symbol) {
			for _, whole := range wholes {
				for _, fraction := range fractions {
					number := whole + fraction

					for _, size := range []string{number + written, number + " " + written} {
						if written == "" && strings.HasSuffix(size, " ") {
							continue // a space with nothing after it is a bad unit, checked below
						}

						want, fits := exact(t, number, factor)
						got, err := human.ParseSize(size)

						switch {
						case fits && (err != nil || got != want):
							t.Fatalf("ParseSize(%q) = %d, %v; want %d", size, got, err, want)
						case !fits && (!errors.Is(err, human.ErrInvalidSize) || got != 0):
							t.Fatalf("ParseSize(%q) = %d, %v; want ErrInvalidSize past int64", size, got, err)
						}
					}
				}
			}
		}
	}
}

// TestEveryThreeDecimalKilobyte: n/1000 kB is n bytes for every n, and
// n/1000 MB is n thousand; through a float64, 741 of each were one short.
func TestEveryThreeDecimalKilobyte(t *testing.T) {
	t.Parallel()

	for n := range int64(100000) {
		number := strconv.FormatInt(n/1000, 10) + "." + fmt.Sprintf("%03d", n%1000)

		if got, err := human.ParseSize(number + "kB"); err != nil || got != n {
			t.Fatalf("ParseSize(%q) = %d, %v; want %d", number+"kB", got, err, n)
		}

		if got, err := human.ParseSize(number + "MB"); err != nil || got != n*1000 {
			t.Fatalf("ParseSize(%q) = %d, %v; want %d", number+"MB", got, err, n*1000)
		}
	}
}

// TestBoundedInvalid: around the grammar's edges, each malformed size is
// refused by the right error, with ErrInvalidArgument, and nothing returned.
func TestBoundedInvalid(t *testing.T) {
	t.Parallel()

	invalid := map[string]error{}

	for _, number := range []string{"", ".", ".5", "5.", "1.2.3", "-1", "+1", " 1", "1e3", "0x1", "1_0", "١"} {
		invalid[number] = human.ErrInvalidSize
		if number == "1e3" || number == "0x1" || number == "1_0" {
			invalid[number] = human.ErrInvalidUnit // the digits parse, the rest is a unit
		}
	}

	for _, unit := range []string{"k", "K", "M", "Ki", "iB", "bytes", "kBB", "ZB", "YiB", "kB ", "  kB", " kB ", "k B", " "} {
		invalid["1"+unit] = human.ErrInvalidUnit
	}

	for size, want := range invalid {
		got, err := human.ParseSize(size)
		if !errors.Is(err, want) || !errors.Is(err, fault.ErrInvalidArgument) || got != 0 {
			t.Fatalf("ParseSize(%q) = %d, %v; want %v", size, got, err, want)
		}
	}
}

// TestBoundedFormat: at every precision from below 1 to 8, over sizes around
// each unit's edge, the mantissa is in [1, base) under the largest unit that
// allows it (bytes below the base), has at most precision significant digits,
// and reads back to within that precision.
func TestBoundedFormat(t *testing.T) {
	t.Parallel()

	var sizes []float64

	for _, base := range []float64{bytesize.KB, bytesize.KiB} {
		for power := range 7 {
			edge := math.Pow(base, float64(power))
			sizes = append(sizes, edge-1, edge, edge+1, edge*1.5, edge*(base-1))
		}
	}

	for _, size := range append(sizes, 0, 1, 123456789) {
		for _, precision := range []int{-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 14, 15, 16, 17, 400} {
			for name, printer := range map[string]struct {
				format func(float64, int) string
				base   float64
				units  []string
			}{
				"DecimalSizeWithPrecision": {human.DecimalSizeWithPrecision, bytesize.KB, []string{"B", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}},
				"BinarySizeWithPrecision":  {human.BinarySizeWithPrecision, bytesize.KiB, []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}},
			} {
				out := printer.format(size, precision)
				effective := min(max(precision, 1), 15)

				unit := -1
				for i, symbol := range printer.units {
					if strings.HasSuffix(out, symbol) && (unit == -1 || len(symbol) > len(printer.units[unit])) {
						unit = i
					}
				}

				if unit == -1 {
					t.Fatalf("%s(%v, %d) = %q, want a unit", name, size, precision, out)
				}

				mantissa, _ := strings.CutSuffix(out, printer.units[unit])

				value, err := strconv.ParseFloat(mantissa, 64)
				if err != nil || value < 0 || (unit > 0 && value < 1) ||
					(value >= printer.base && unit < len(printer.units)-1) {
					t.Fatalf("%s(%v, %d) = %q: mantissa %v out of [1, base)", name, size, precision, out, value)
				}

				if digits := significant(mantissa); digits > effective {
					t.Fatalf(
						"%s(%v, %d) = %q: %d significant digits, want at most %d",
						name,
						size,
						precision,
						out,
						digits,
						effective,
					)
				}

				if unit > 6 || size >= math.MaxInt64 {
					continue // ZB and YB, and sizes past an int64, are printed, not parsed
				}

				parsed, err := human.ParseSize(out)
				if err != nil {
					t.Fatalf("ParseSize(%q) from %s(%v, %d): %v", out, name, size, precision, err)
				}

				// Rounding to effective digits moves the value by at most half a
				// unit in the last digit kept, a relative 5 * 10^-effective; past
				// that, the float64 itself is only good to 2^-52.
				tolerance := size*(5*math.Pow(10, -float64(effective))+math.Pow(2, -52)) + 1
				if math.Abs(float64(parsed)-size) > tolerance {
					t.Fatalf(
						"ParseSize(%q) = %d, from %s(%v, %d), off by more than %v",
						out,
						parsed,
						name,
						size,
						precision,
						tolerance,
					)
				}
			}
		}
	}
}

// significant counts the significant digits of a printed mantissa: its
// digits without the point, leading zeros and trailing zeros.
func significant(mantissa string) int {
	digits := strings.ReplaceAll(mantissa, ".", "")

	return len(strings.Trim(digits, "0"))
}

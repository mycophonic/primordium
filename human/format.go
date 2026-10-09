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

// Adapted from: https://github.com/docker/go-units/blob/master/size.go under Apache License

/*
   Copyright 2015 Docker, Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       https://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package human

import (
	"math"
	"strconv"

	"github.com/mycophonic/primordium/bytesize"
)

// defaultPrecision is the number of significant digits DecimalSize and
// BinarySize print.
const defaultPrecision = 4

// maxPrecision is the decimal digits a float64 carries exactly (DBL_DIG):
// rounding to more scales past 2^53 and loses what it meant to keep, and
// far past it the power of ten overflows to NaN.
const maxPrecision = 15

const decimalBase = 10

// DecimalSize formats size in decimal (SI) multiples, to four significant
// digits: "2.746MB", "796kB", "10000YB" past the largest multiple.
func DecimalSize(size float64) string {
	return DecimalSizeWithPrecision(size, defaultPrecision)
}

// DecimalSizeWithPrecision is DecimalSize to precision significant digits; a
// precision below 1 counts as 1, and one past 15, the digits a float64 carries exactly, as 15.
func DecimalSizeWithPrecision(size float64, precision int) string {
	abbreviations := [...]string{"B", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}

	return format(size, bytesize.KB, abbreviations[:], precision)
}

// BinarySize formats size in binary (IEC) multiples, to four significant
// digits: "44KiB", "17MiB", "3.42GiB".
func BinarySize(size float64) string {
	return BinarySizeWithPrecision(size, defaultPrecision)
}

// BinarySizeWithPrecision is BinarySize to precision significant digits; a
// precision below 1 counts as 1, and one past 15, the digits a float64 carries exactly, as 15.
func BinarySizeWithPrecision(size float64, precision int) string {
	abbreviations := [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}

	return format(size, bytesize.KiB, abbreviations[:], precision)
}

// format divides size by base until it is below base or the largest
// abbreviation is reached, rounds it to precision significant digits, and
// prints it with that abbreviation. A mantissa that rounds up to the base
// moves up a unit: 999 bytes to one digit is 1kB, not 1e+03B.
func format(size, base float64, abbreviations []string, precision int) string {
	precision = min(max(precision, 1), maxPrecision) // below 1 counts as 1, past 15 as 15

	index := 0
	for size >= base && index < len(abbreviations)-1 {
		size /= base
		index++
	}

	mantissa := roundSignificant(size, precision)
	if mantissa >= base && index < len(abbreviations)-1 {
		mantissa /= base // exactly 1 of the next unit: 1023.9995KiB to 4 digits is 1MiB
		index++
	}

	return strconv.FormatFloat(mantissa, 'f', -1, 64) + abbreviations[index]
}

// roundSignificant rounds value to digits significant digits.
func roundSignificant(value float64, digits int) float64 {
	if value == 0 || math.IsInf(value, 0) || math.IsNaN(value) {
		return value
	}

	scale := math.Pow(decimalBase, float64(digits-1)-math.Floor(math.Log10(math.Abs(value))))

	return math.Round(value*scale) / scale
}

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
	"fmt"

	"github.com/mycophonic/primordium/bytesize"
)

// defaultPrecision is the number of significant digits DecimalSize and
// BinarySize print.
const defaultPrecision = 4

// DecimalSize formats size in decimal (SI) multiples, to four significant
// digits: "2.746MB", "796kB", "1e+04YB" past the largest multiple.
func DecimalSize(size float64) string {
	return DecimalSizeWithPrecision(size, defaultPrecision)
}

// DecimalSizeWithPrecision is DecimalSize to precision significant digits.
func DecimalSizeWithPrecision(size float64, precision int) string {
	abbreviations := [...]string{"B", "kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}

	return format(size, bytesize.KB, abbreviations[:], precision)
}

// BinarySize formats size in binary (IEC) multiples, to four significant
// digits: "44KiB", "17MiB", "3.42GiB".
func BinarySize(size float64) string {
	return BinarySizeWithPrecision(size, defaultPrecision)
}

// BinarySizeWithPrecision is BinarySize to precision significant digits.
func BinarySizeWithPrecision(size float64, precision int) string {
	abbreviations := [...]string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}

	return format(size, bytesize.KiB, abbreviations[:], precision)
}

// format divides size by base until it is below base or the largest
// abbreviation is reached, and prints it with that abbreviation.
func format(size, base float64, abbreviations []string, precision int) string {
	index := 0
	for size >= base && index < len(abbreviations)-1 {
		size /= base
		index++
	}

	return fmt.Sprintf("%.*g%s", precision, size, abbreviations[index])
}

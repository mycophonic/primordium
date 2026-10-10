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

import (
	"errors"
	"math"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/human"
)

// FuzzParseSize: whatever the string, ParseSize returns a non-negative count
// or an ErrInvalidArgument naming the size or the unit, and a count it
// returns, formatted and parsed again, is the same count within four digits.
func FuzzParseSize(f *testing.F) {
	for _, seed := range []string{"", "32", "32.5 kB", "17MiB", "9223372036854775807", "1.001kB", "32k", " 32", "1e3"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, size string) {
		got, err := human.ParseSize(size)

		switch {
		case err != nil:
			if got != 0 || !errors.Is(err, fault.ErrInvalidArgument) ||
				(!errors.Is(err, human.ErrInvalidSize) && !errors.Is(err, human.ErrInvalidUnit)) {
				t.Fatalf("ParseSize(%q) = %d, %v", size, got, err)
			}
		case got < 0:
			t.Fatalf("ParseSize(%q) = %d", size, got)
		default:
			again, err := human.ParseSize(human.DecimalSize(float64(got)))
			if err != nil || math.Abs(float64(again-got)) > float64(got)/1000+1 {
				t.Fatalf("ParseSize(%q) = %d reads back as %d, %v", size, got, again, err)
			}
		}
	})
}

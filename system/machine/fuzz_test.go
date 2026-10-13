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

package machine_test

import (
	"math"
	"testing"

	"github.com/mycophonic/primordium/system/machine"
)

// FuzzClassify: a tier is one of the three, and never falls when a core or
// a byte is added.
func FuzzClassify(f *testing.F) {
	f.Add(4, uint64(8<<30))
	f.Add(9, uint64(32<<30))
	f.Add(0, uint64(0))
	f.Add(math.MaxInt, uint64(math.MaxUint64))

	f.Fuzz(func(t *testing.T, cores int, ram uint64) {
		tier := machine.Classify(cores, ram)
		if tier < machine.TierLow || tier > machine.TierHigh {
			t.Fatalf("Classify(%d, %d) = %d", cores, ram, int(tier))
		}

		if cores < math.MaxInt && machine.Classify(cores+1, ram) < tier {
			t.Fatalf("Classify(%d, %d) = %v, a core more is lower", cores, ram, tier)
		}

		if ram < math.MaxUint64 && machine.Classify(cores, ram+1) < tier {
			t.Fatalf("Classify(%d, %d) = %v, a byte more is lower", cores, ram, tier)
		}
	})
}

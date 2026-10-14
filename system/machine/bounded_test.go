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

// The bounded check: every core count around both thresholds against every
// RAM size around both thresholds and the GiB rounding, held to the
// documented rule; then the machines the rounding exists for.

import (
	"math"
	"testing"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/system/machine"
)

// documented is the rule as machine's docs state it, with the rounding.
func documented(cores int, ram uint64) machine.Tier {
	gib := ram / bytesize.GiB
	if ram%bytesize.GiB != 0 {
		gib++
	}

	switch {
	case cores >= 9 && gib >= 32:
		return machine.TierHigh
	case cores <= 4 || gib <= 8:
		return machine.TierLow
	default:
		return machine.TierMid
	}
}

func TestBoundedClassify(t *testing.T) {
	t.Parallel()

	cores := []int{0, 1, 4, 5, 8, 9, 10, 256}
	rams := []uint64{
		0, 1,
		8*bytesize.GiB - 1, 8 * bytesize.GiB, 8*bytesize.GiB + 1, 9 * bytesize.GiB,
		31 * bytesize.GiB, 32*bytesize.GiB - 1, 32 * bytesize.GiB, 32*bytesize.GiB + 1,
		64 * bytesize.GiB, math.MaxUint64,
	}

	for _, c := range cores {
		for _, r := range rams {
			if got, want := machine.Classify(c, r), documented(c, r); got != want {
				t.Errorf("Classify(%d, %d) = %v, want %v", c, r, got, want)
			}
		}
	}
}

// TestClassifyReportedRAM: what the kernels report for installed sizes.
func TestClassifyReportedRAM(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		cores int
		ram   uint64
		want  machine.Tier
	}{
		{"32 GiB box, Linux MemTotal", 16, 32*bytesize.GiB - 700*bytesize.MiB, machine.TierHigh},
		{"32 GiB box, macOS hw.memsize", 10, 32 * bytesize.GiB, machine.TierHigh},
		{"8 GiB box, Linux MemTotal", 4, 8*bytesize.GiB - 300*bytesize.MiB, machine.TierLow},
		{"8 GiB box, 8 cores", 8, 8 * bytesize.GiB, machine.TierLow},
		{"16 GiB box, 8 cores", 8, 16*bytesize.GiB - 400*bytesize.MiB, machine.TierMid},
		{"16 GiB box, 12 cores", 12, 16 * bytesize.GiB, machine.TierMid},
		{"64 GiB box, 4 cores", 4, 64 * bytesize.GiB, machine.TierLow},
	} {
		if got := machine.Classify(tc.cores, tc.ram); got != tc.want {
			t.Errorf("%s: Classify(%d, %d) = %v, want %v", tc.name, tc.cores, tc.ram, got, tc.want)
		}
	}
}

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

// The contract, from machine's docs:
//   - Host describes the current machine: CPUCores is the logical core
//     count, RAMAvailable ≤ RAMTotal, DiskFree ≤ DiskTotal, and DiskTotal,
//     RAMTotal and CPUCores are nonzero; CPUModel may be empty where the
//     platform has no brand string; Tier is Classify(CPUCores, RAMTotal);
//   - Classify: low is 4 cores or fewer, or 8 GiB or less; high is 9 cores
//     or more and 32 GiB or more; mid is the rest; RAM is rounded up to the
//     whole GiB first;
//   - Tier.String names the three tiers and never panics.

import (
	"runtime"
	"testing"

	"github.com/mycophonic/primordium/system/machine"
)

func TestHost(t *testing.T) {
	t.Parallel()

	info, err := machine.Host()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v", info)

	if info.CPUCores != runtime.NumCPU() {
		t.Errorf("CPUCores %d, NumCPU %d", info.CPUCores, runtime.NumCPU())
	}

	if info.RAMTotal == 0 || info.DiskTotal == 0 {
		t.Errorf("RAMTotal %d, DiskTotal %d: a zero", info.RAMTotal, info.DiskTotal)
	}

	if info.RAMAvailable > info.RAMTotal {
		t.Errorf("RAMAvailable %d above RAMTotal %d", info.RAMAvailable, info.RAMTotal)
	}

	if info.DiskFree > info.DiskTotal {
		t.Errorf("DiskFree %d above DiskTotal %d", info.DiskFree, info.DiskTotal)
	}

	if want := machine.Classify(info.CPUCores, info.RAMTotal); info.Tier != want {
		t.Errorf("Tier %v, Classify(%d, %d) %v", info.Tier, info.CPUCores, info.RAMTotal, want)
	}
}

func TestTierString(t *testing.T) {
	t.Parallel()

	for tier, want := range map[machine.Tier]string{
		machine.TierLow:  "low",
		machine.TierMid:  "mid",
		machine.TierHigh: "high",
		machine.Tier(3):  "tier(3)",
		machine.Tier(-1): "tier(-1)",
	} {
		if got := tier.String(); got != want {
			t.Errorf("Tier(%d).String() = %q, want %q", int(tier), got, want)
		}
	}
}

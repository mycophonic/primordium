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

package machine

import (
	"fmt"

	"github.com/mycophonic/primordium/bytesize"
)

// Info holds host machine characteristics.
type Info struct {
	// Disk space on the volume where the user home directory lives.
	DiskTotal uint64 // bytes
	DiskFree  uint64 // bytes, available to unprivileged user

	// Physical memory.
	RAMTotal     uint64 // bytes
	RAMAvailable uint64 // bytes, estimate of memory usable without swapping

	// CPU.
	CPUCores int    // logical cores
	CPUModel string // brand string (e.g. "Apple M1 Max"); "" where the platform has none

	// Classify(CPUCores, RAMTotal).
	Tier Tier
}

// Tier classifies machine capability.
type Tier int

// The tiers, from the logical core count and the RAM: low is 4 cores or
// fewer, or 8 GiB or less; high is 9 cores or more and 32 GiB or more; mid
// is the rest.
const (
	TierLow Tier = iota
	TierMid
	TierHigh
)

func (t Tier) String() string {
	switch t {
	case TierLow:
		return "low"
	case TierMid:
		return "mid"
	case TierHigh:
		return "high"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

// Classify is the tier of a machine with cores logical cores and ram bytes
// of memory as the kernel reports it. Linux and Windows report the RAM they
// keep, below the installed size by what they reserve, so ram is rounded up
// to the whole GiB before the thresholds apply: anything above the GiB below
// the high threshold is that threshold, and the low one is already a bound.
func Classify(cores int, ram uint64) Tier {
	if cores >= highMinCores && ram > highMinRAM-bytesize.GiB {
		return TierHigh
	}

	if cores <= lowMaxCores || ram <= lowMaxRAM {
		return TierLow
	}

	return TierMid
}

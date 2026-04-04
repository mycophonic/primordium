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
	CPUModel string // brand string (e.g. "Apple M1 Max")

	// Classification derived from cores and RAM.
	Tier Tier
}

// Tier classifies machine capability.
type Tier int

// Capability tiers derived from core count and total RAM.
const (
	TierLow  Tier = iota // ≤4 cores OR ≤8 GB RAM
	TierMid              // 5–8 cores AND 9–31 GB RAM
	TierHigh             // ≥9 cores AND ≥32 GB RAM
)

func (t Tier) String() string {
	return [...]string{
		"low", "mid", "high",
	}[t]
}

// classify returns the tier based on core count and total RAM.
func classify(cores int, ramBytes uint64) Tier {
	if cores >= highMinCores && (ramBytes >= highMinRAM) {
		return TierHigh
	}

	if cores <= lowMaxCores || ramBytes <= lowMaxRAM {
		return TierLow
	}

	return TierMid
}

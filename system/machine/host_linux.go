//go:build linux

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
	"bufio"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/dirs"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Host returns information about the current machine.
func Host() (Info, error) {
	home := dirs.HomeDir()

	var stat syscall.Statfs_t
	if err := syscall.Statfs(home, &stat); err != nil {
		return Info{}, fmt.Errorf("%w: statfs: %w", fault.ErrSystemFailure, err)
	}

	ramTotal, ramAvail, err := readMemInfo()
	if err != nil {
		return Info{}, err
	}

	cpuModel := readCPUModel()
	cores := runtime.NumCPU()

	// Bsize is a filesystem block size, always positive.
	// #nosec G115 -- see above
	return Info{
		DiskTotal:    stat.Blocks * uint64(stat.Bsize),
		DiskFree:     stat.Bavail * uint64(stat.Bsize),
		RAMTotal:     ramTotal,
		RAMAvailable: ramAvail,
		CPUCores:     cores,
		CPUModel:     cpuModel,
		Tier:         classify(cores, ramTotal),
	}, nil
}

// readMemInfo parses MemTotal and MemAvailable from /proc/meminfo (values in kB).
func readMemInfo() (total, available uint64, err error) {
	meminfo, err := xos.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("%w: open meminfo: %w", fault.ErrReadFailure, err)
	}

	defer func() { _ = meminfo.Close() }() // read-only: a close error carries nothing

	var gotTotal, gotAvail bool

	scanner := bufio.NewScanner(meminfo)
	for scanner.Scan() {
		line := scanner.Text()

		if val, ok := parseMemInfoLine(line, "MemTotal:"); ok {
			total = val
			gotTotal = true
		} else if val, ok := parseMemInfoLine(line, "MemAvailable:"); ok {
			available = val
			gotAvail = true
		}

		if gotTotal && gotAvail {
			break
		}
	}

	if !gotTotal {
		return 0, 0, fmt.Errorf("%w: MemTotal not found in /proc/meminfo", fault.ErrReadFailure)
	}

	// MemAvailable was added in Linux 3.14. If missing, leave available as zero.
	return total, available, nil
}

// parseMemInfoLine extracts the byte value from a /proc/meminfo line matching prefix.
// Lines look like: "MemTotal:       16384000 kB".
func parseMemInfoLine(line, prefix string) (uint64, bool) {
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}

	fields := strings.Fields(line)
	if len(fields) < 2 { // "<Key>: <value> kB"
		return 0, false
	}

	//revive:disable-next-line:add-constant // decimal base
	kilobytes, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}

	// meminfo's "kB" is 1024 bytes, not the SI kilobyte.
	return kilobytes * bytesize.KiB, true
}

// readCPUModel returns the first "model name" from /proc/cpuinfo, or empty string on failure.
func readCPUModel() string {
	cpuinfo, err := xos.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}

	defer func() { _ = cpuinfo.Close() }() // read-only: a close error carries nothing

	scanner := bufio.NewScanner(cpuinfo)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			_, value, ok := strings.Cut(line, ":")
			if ok {
				return strings.TrimSpace(value)
			}
		}
	}

	return ""
}

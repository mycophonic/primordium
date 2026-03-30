//go:build darwin

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
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/dirs"
)

// Host returns information about the current machine.
func Host() (Info, error) {
	home := dirs.HomeDir()

	var stat syscall.Statfs_t
	if err := syscall.Statfs(home, &stat); err != nil {
		return Info{}, fmt.Errorf("%w: statfs: %w", fault.ErrSystemFailure, err)
	}

	ram, err := readMemSize()
	if err != nil {
		return Info{}, err
	}

	ramAvail, err := readMemAvailable()
	if err != nil {
		return Info{}, err
	}

	cpuModel, err := syscall.Sysctl("machdep.cpu.brand_string")
	if err != nil {
		return Info{}, fmt.Errorf("%w: sysctl cpu brand: %w", fault.ErrSystemFailure, err)
	}

	cores := runtime.NumCPU()

	return Info{
		DiskTotal:    stat.Blocks * uint64(stat.Bsize),
		DiskFree:     stat.Bavail * uint64(stat.Bsize),
		RAMTotal:     ram,
		RAMAvailable: ramAvail,
		CPUCores:     cores,
		CPUModel:     cpuModel,
		Tier:         classify(cores, ram),
	}, nil
}

// readMemSize returns the total physical memory in bytes.
func readMemSize() (uint64, error) {
	ram, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("%w: sysctl hw.memsize: %w", fault.ErrSystemFailure, err)
	}

	return ram, nil
}

// readMemAvailable estimates available memory by parsing vm_stat output.
// Returns (free + inactive pages) × page size — a rough estimate of memory
// usable without swapping. macOS has no single "available" metric like Linux.
func readMemAvailable() (uint64, error) {
	out, err := exec.CommandContext(context.Background(), "vm_stat").Output()
	if err != nil {
		return 0, fmt.Errorf("%w: vm_stat: %w", fault.ErrCommandFailure, err)
	}

	var pageSize, free, inactive uint64

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()

		// First line: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
		if strings.Contains(line, "page size of") {
			pageSize, err = parseVMStatPageSize(line)
			if err != nil {
				return 0, err
			}

			continue
		}

		if val, ok := parseVMStatLine(line, "Pages free:"); ok {
			free = val
		} else if val, ok := parseVMStatLine(line, "Pages inactive:"); ok {
			inactive = val
		}
	}

	if pageSize == 0 {
		return 0, fmt.Errorf("%w: vm_stat: page size not found", fault.ErrReadFailure)
	}

	return (free + inactive) * pageSize, nil
}

// parseVMStatPageSize extracts the page size from the vm_stat header line.
func parseVMStatPageSize(line string) (uint64, error) {
	// "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
	_, after, ok := strings.Cut(line, "page size of ")
	if !ok {
		return 0, fmt.Errorf("%w: vm_stat: unexpected header: %s", fault.ErrReadFailure, line)
	}

	sizeStr, _, _ := strings.Cut(after, " ")

	//revive:disable-next-line:add-constant // decimal base
	size, err := strconv.ParseUint(sizeStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: vm_stat: parse page size: %w", fault.ErrReadFailure, err)
	}

	return size, nil
}

// parseVMStatLine extracts a page count from a vm_stat line matching the given prefix.
// Lines look like: "Pages free:                                3636.".
func parseVMStatLine(line, prefix string) (uint64, bool) {
	if !strings.HasPrefix(line, prefix) {
		return 0, false
	}

	text := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	text = strings.TrimSuffix(text, ".")

	//revive:disable-next-line:add-constant // decimal base
	val, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, false
	}

	return val, true
}

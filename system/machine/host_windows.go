//go:build windows

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
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/dirs"
)

// memoryStatusEx is MEMORYSTATUSEX, which x/sys/windows does not bind.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

//nolint:gochecknoglobals // a lazily bound system procedure
var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// Host returns information about the current machine.
func Host() (Info, error) {
	home := dirs.HomeDir()

	diskTotal, diskFree, err := diskSpace(home)
	if err != nil {
		return Info{}, err
	}

	ramTotal, ramAvail, err := physicalMemory()
	if err != nil {
		return Info{}, err
	}

	cpuModel := readCPUModel()
	cores := runtime.NumCPU()

	return Info{
		DiskTotal:    diskTotal,
		DiskFree:     diskFree,
		RAMTotal:     ramTotal,
		RAMAvailable: ramAvail,
		CPUCores:     cores,
		CPUModel:     cpuModel,
		Tier:         classify(cores, ramTotal),
	}, nil
}

// diskSpace returns the total bytes of the volume holding path and the
// bytes available to the caller, which quotas may hold below the volume's
// free bytes.
func diskSpace(path string) (total, free uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: utf16 path: %w", fault.ErrSystemFailure, err)
	}

	var totalFree uint64

	if err := windows.GetDiskFreeSpaceEx(pathPtr, &free, &total, &totalFree); err != nil {
		return 0, 0, fmt.Errorf("%w: GetDiskFreeSpaceEx: %w", fault.ErrSystemFailure, err)
	}

	return total, free, nil
}

// physicalMemory returns the total and available physical RAM in bytes.
func physicalMemory() (total, available uint64, err error) {
	var mem memoryStatusEx

	mem.length = uint32(unsafe.Sizeof(mem))

	ret, _, callErr := procGlobalMemoryStatusEx.Call(
		uintptr(unsafe.Pointer(&mem)), // #nosec G103 -- the Win32 call takes the struct's address
	)
	if ret == 0 {
		return 0, 0, fmt.Errorf("%w: GlobalMemoryStatusEx: %w", fault.ErrSystemFailure, callErr)
	}

	return mem.totalPhys, mem.availPhys, nil
}

// readCPUModel reads the processor brand string from the registry, or ""
// when it cannot; the string is padded with leading spaces on many CPUs.
func readCPUModel() string {
	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ""
	}

	defer func() { _ = key.Close() }() // a read-only key: a close error carries nothing

	model, _, err := key.GetStringValue("ProcessorNameString")
	if err != nil {
		return ""
	}

	return strings.TrimSpace(model)
}

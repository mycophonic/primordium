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
	"syscall"
	"unsafe"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/dirs"
)

// memoryStatusEx mirrors the Windows MEMORYSTATUSEX structure.
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

//nolint:gochecknoglobals
var (
	modkernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx   = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

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

// diskSpace returns the total and available bytes for the volume containing path.
func diskSpace(path string) (total, free uint64, err error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: utf16 path: %w", fault.ErrSystemFailure, err)
	}

	//nolint:gosec // G103: required for Win32 syscall interop
	ret, _, callErr := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		0,
	)
	if ret == 0 {
		return 0, 0, fmt.Errorf("%w: GetDiskFreeSpaceEx: %w", fault.ErrSystemFailure, callErr)
	}

	return total, free, nil
}

// physicalMemory returns the total and available physical RAM in bytes.
func physicalMemory() (total, available uint64, err error) {
	var mem memoryStatusEx

	mem.length = uint32(unsafe.Sizeof(mem))

	//nolint:gosec // G103: required for Win32 syscall interop
	ret, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem)))
	if ret == 0 {
		return 0, 0, fmt.Errorf("%w: GlobalMemoryStatusEx: %w", fault.ErrSystemFailure, callErr)
	}

	return mem.totalPhys, mem.availPhys, nil
}

// readCPUModel reads the processor brand string from the Windows registry.
func readCPUModel() string {
	var hKey syscall.Handle

	subkey, err := syscall.UTF16PtrFromString(
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`,
	)
	if err != nil {
		return ""
	}

	if err := syscall.RegOpenKeyEx(
		syscall.HKEY_LOCAL_MACHINE, subkey, 0, syscall.KEY_READ, &hKey,
	); err != nil {
		return ""
	}

	defer syscall.RegCloseKey(hKey)

	valueName, err := syscall.UTF16PtrFromString("ProcessorNameString")
	if err != nil {
		return ""
	}

	// Query required buffer size.
	var dataType, dataSize uint32

	if err := syscall.RegQueryValueEx(
		hKey, valueName, nil, &dataType, nil, &dataSize,
	); err != nil {
		return ""
	}

	if dataSize == 0 {
		return ""
	}

	// Read the value.
	buf := make([]uint16, dataSize/2) //nolint:mnd // bytes to uint16 elements

	if err := syscall.RegQueryValueEx( //nolint:gosec // G103: required for registry syscall interop
		hKey, valueName, nil, &dataType, (*byte)(unsafe.Pointer(&buf[0])), &dataSize,
	); err != nil {
		return ""
	}

	return syscall.UTF16ToString(buf)
}

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

package index

import "syscall"

// processQueryLimitedInformation is the minimum access right needed to
// query whether a process exists. Not defined in the syscall package.
const processQueryLimitedInformation = 0x1000

// isProcessAlive reports whether a process with the given PID exists.
func isProcessAlive(pid int) bool {
	handle, err := syscall.OpenProcess(
		processQueryLimitedInformation,
		false,
		uint32(pid), //nolint:gosec // G115: PIDs are non-negative
	)
	if err != nil {
		return false
	}

	//revive:disable-next-line:unhandled-error // best-effort close
	syscall.CloseHandle(handle) //nolint:errcheck,gosec

	return true
}

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

import "golang.org/x/sys/windows"

// processStart is the process's creation time in nanoseconds since 1601, as
// GetProcessTimes reports it.
func processStart(pid int) (uint64, bool) {
	// #nosec G115 -- PIDs are non-negative
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}

	defer func() { _ = windows.CloseHandle(handle) }() // a probe handle: a close error carries nothing

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}

	// #nosec G115 -- a creation time is after 1601
	return uint64(creation.Nanoseconds()), true
}

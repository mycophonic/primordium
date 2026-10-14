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

import "golang.org/x/sys/unix"

// processStart is the process's start time in microseconds since the epoch,
// from the kernel's process table.
func processStart(pid int) (uint64, bool) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, false
	}

	started := proc.Proc.P_starttime

	// #nosec G115 -- a start time is after the epoch
	return uint64(started.Sec)*1_000_000 + uint64(started.Usec), true
}

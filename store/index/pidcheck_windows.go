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

import (
	"errors"
	"syscall"
)

// processQueryLimitedInformation is the least access that answers whether a
// process exists; the syscall package does not define it.
const processQueryLimitedInformation = 0x1000

// isProcessAlive reports whether pid exists. Access denied means the
// process is there and not ours to open, which is alive: a live writer
// taken for dead hands its lock to a second writer.
func isProcessAlive(pid int) bool {
	handle, err := syscall.OpenProcess(
		processQueryLimitedInformation,
		false,
		uint32(pid), // #nosec G115 -- PIDs are non-negative
	)
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}

	_ = syscall.CloseHandle(handle) // a probe handle: a close error carries nothing

	return true
}

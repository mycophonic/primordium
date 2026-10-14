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
	"os"
	"sync"
)

//nolint:gochecknoglobals // the process's own start time, read once
var (
	ownStartOnce sync.Once
	ownStart     uint64
)

// startOfSelf is this process's start time as the platform reports it, or
// zero when it cannot.
func startOfSelf() uint64 {
	ownStartOnce.Do(func() { ownStart, _ = processStart(os.Getpid()) })

	return ownStart
}

// sameProcess reports whether pid is alive and, when start is known, is the
// process that recorded it: a PID the kernel has handed to another process
// since its holder died is not the holder it names. A start the platform
// cannot read counts as the same: a live process is never taken for dead.
func sameProcess(pid int, start uint64) bool {
	if !isProcessAlive(pid) {
		return false
	}

	if start == 0 {
		return true
	}

	now, ok := processStart(pid)

	return !ok || now == start
}

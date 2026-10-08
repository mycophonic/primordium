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

package umask

import (
	"math"
	"sync"
)

//nolint:gochecknoglobals // the umask is process-wide state
var (
	mutex sync.Mutex
	found *uint32 // the mask Disable found; nil until it has run
)

// Disable zeroes the process umask, once: from then on a file or directory
// gets exactly the mode its creation asks for. Later calls do nothing.
func Disable() {
	mutex.Lock()
	defer mutex.Unlock()

	if found != nil {
		return
	}

	mask := umask(0)
	if mask < 0 || mask > math.MaxUint32 {
		panic("the process umask is out of range")
	}

	masked := uint32(mask)
	found = &masked
}

// Get is the mask the process started with, the operator's, as [Disable]
// found it. It panics when [Disable] has not run: a umask cannot be read
// without being set.
func Get() uint32 {
	mutex.Lock()
	defer mutex.Unlock()

	if found == nil {
		panic("umask.Get before umask.Disable")
	}

	return *found
}

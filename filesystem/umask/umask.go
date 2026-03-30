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

//nolint:gochecknoglobals
var (
	mutex       sync.Mutex
	getOnce     sync.Once
	currentMask = defaultUmask
)

// Set sets the file mode creation mask (umask) for the current process.
func Set(mask uint32) {
	mutex.Lock()
	defer mutex.Unlock()

	if mask == currentMask {
		return
	}

	currentMask = mask
	_ = umask(int(mask))
}

// Get retrieves the current file mode creation mask (umask) for the current process.
func Get() uint32 {
	mutex.Lock()
	defer mutex.Unlock()

	getOnce.Do(func() {
		cMask := umask(0)

		if cMask > math.MaxUint32 || cMask < 0 {
			panic("currently set user umask is out of range")
		}

		currentMask = uint32(cMask)
	})

	return currentMask
}

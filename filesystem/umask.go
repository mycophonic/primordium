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

package filesystem

var currentMask = defaultUmask //nolint:gochecknoglobals

// SetUmask sets the file mode creation mask (umask) for the current process.
func SetUmask(mask uint32) {
	if mask == currentMask {
		return
	}

	currentMask = mask
	_ = umask(int(mask))
}

// GetUmask retrieves the current file mode creation mask (umask) for the current process.
func GetUmask() uint32 {
	return currentMask
}

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

package dirs

import "github.com/mycophonic/primordium/filesystem/pathcheck"

// SetAppName sets the app name to be used for common locations resolution.
// Must be called exactly once, before any directory functions are used.
// Subsequent calls are silently ignored.
func SetAppName(appName string) {
	nameOnce.Do(func() {
		if err := pathcheck.ValidateComponent(appName); err != nil {
			panic(err)
		}

		name = appName
	})
}

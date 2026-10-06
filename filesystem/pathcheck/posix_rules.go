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

package pathcheck

import (
	"fmt"
	"regexp"
)

var (
	posixKeywords = regexp.MustCompile(`^\.{1,2}$`)
	posixReserved = regexp.MustCompile(`[\x{0}/]`)
)

func validatePosix(component string) error {
	if posixReserved.MatchString(component) {
		return fmt.Errorf("%w: %q (%q)", errForbiddenChars, component, posixReserved)
	}

	if posixKeywords.MatchString(component) {
		return fmt.Errorf("%w: %q (%q)", errForbiddenKeywords, component, posixKeywords)
	}

	return nil
}

// socketPathMaxLinux is the maximum length of a Unix socket path on Linux.
// There, and on illumos and Solaris, sun_path is 108 bytes (including null terminator).
// See: unix(7) man page, /usr/include/sys/un.h.
const socketPathMaxLinux = 108

// socketPathMaxDarwin is the maximum length of a Unix socket path on macOS,
// where sun_path is 104 bytes (including null terminator).
// See: /usr/include/sys/un.h.
const socketPathMaxDarwin = 104

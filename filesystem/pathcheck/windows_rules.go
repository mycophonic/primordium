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
	"strings"
)

// See https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
var (
	windowsKeywords = regexp.MustCompile(`(?i)^(con|prn|nul|aux|com[1-9¹²³]|lpt[1-9¹²³])([.].*)?$`)
	windowsReserved = regexp.MustCompile(`[\x{0}-\x{1f}<>:"/\\|?*]`)
)

func validateWindows(component string) error {
	if windowsReserved.MatchString(component) {
		return fmt.Errorf("%w: %q (%q)", errForbiddenChars, component, windowsReserved)
	}

	if windowsKeywords.MatchString(component) {
		return fmt.Errorf("%w: %q (%q)", errForbiddenKeywords, component, windowsKeywords)
	}

	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return fmt.Errorf("%w: %q", errNoEndingSpaceDot, component)
	}

	return nil
}

// socketPathMaxWindows is the maximum length of a Unix socket path on Windows.
// On Windows (10+), sun_path is 108 bytes (including null terminator), same as Linux.
// AF_UNIX support was added in Windows 10 Build 17063.
//
// References:
//   - https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/
//   - Windows SDK: afunix.h defines UNIX_PATH_MAX = 108
const socketPathMaxWindows = 108

// longPathPrefix opens a long path; longUNCPrefix follows it for a share
// (`\\?\UNC\server\share`).
const (
	longPathPrefix = `\\?\`
	longUNCPrefix  = `UNC\`
)

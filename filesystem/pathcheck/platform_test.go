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

package pathcheck_test

import (
	"errors"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

func TestNative(t *testing.T) {
	t.Parallel()

	want, ok := map[string]pathcheck.Platform{
		"linux":   pathcheck.Linux(),
		"android": pathcheck.Linux(),
		"illumos": pathcheck.Linux(),
		"solaris": pathcheck.Linux(),
		"windows": pathcheck.Windows(),
	}[runtime.GOOS]
	if !ok {
		want = pathcheck.Darwin()
	}

	assert.Equal(t, pathcheck.Native(), want)
}

// TestWindowsLongPathNeedsDriveRoot: a long path to a drive starts at its root
// (`\\?\C:\`). Without it, `\\?\C:` is the volume device, and `\\?\C:name` is
// relative to the drive's current directory, which Windows rules out after
// `\\?\`.
func TestWindowsLongPathNeedsDriveRoot(t *testing.T) {
	t.Parallel()

	windows := pathcheck.Windows()

	for _, path := range []string{`\\?\C:\`, `\\?\c:\dir\name`} {
		assert.NilError(t, windows.Validate(path), "%q", path)
	}

	for _, path := range []string{`\\?\C:`, `\\?\C:name`, `\\?\a:b`, `\\?\c:dir\name`} {
		assert.Assert(t, errors.Is(windows.Validate(path), pathcheck.ErrInvalidPath), "%q", path)
	}
}

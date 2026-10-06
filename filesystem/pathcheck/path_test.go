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
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// TestPackageFunctionsUseNative checks that the package-level functions, which
// consumers call, answer as the running platform does, error identity
// included. The platform rules themselves are the fuzz targets' and the Quint
// traces' to check.
func TestPackageFunctionsUseNative(t *testing.T) {
	t.Parallel()

	native := pathcheck.Native()

	for _, input := range []string{
		"name", ".", "..", "", "a:b", "nul", "a\\b", "/a/b", "/a/../b", `C:\a\b`, strings.Repeat("x", 108),
	} {
		component, nativeComponent := pathcheck.ValidateComponent(input), native.ValidateComponent(input)
		assert.Equal(t, component == nil, nativeComponent == nil, "ValidateComponent(%q)", input)

		path, nativePath := pathcheck.Validate(input), native.Validate(input)
		assert.Equal(t, path == nil, nativePath == nil, "Validate(%q)", input)

		socket, nativeSocket := pathcheck.ValidateSocket(input), native.ValidateSocket(input)
		assert.Equal(t, socket == nil, nativeSocket == nil, "ValidateSocket(%q)", input)

		for _, err := range []error{component, path, socket} {
			if err != nil {
				assert.Assert(t, errors.Is(err, pathcheck.ErrInvalidPath), "%q: %v", input, err)
				assert.Assert(t, errors.Is(err, fault.ErrInvalidArgument), "%q: %v", input, err)
			}
		}
	}
}

func TestValidateSocket_ErrorMessageContainsDetails(t *testing.T) {
	t.Parallel()

	// Create a path that's definitely too long
	longPath := strings.Repeat("a", 200)

	err := pathcheck.ValidateSocket(longPath)
	if err == nil {
		t.Fatal("expected error for long path")
	}

	errMsg := err.Error()

	// Error should contain useful debugging info
	if !strings.Contains(errMsg, runtime.GOOS) {
		t.Errorf("error message should contain OS name, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "200") {
		t.Errorf("error message should contain actual length (200), got: %s", errMsg)
	}
}

func TestValidateRelativePathInAbsoluteForm(t *testing.T) {
	t.Parallel()

	for _, typed := range []string{"./Dockerfile", "../sibling/Dockerfile", "dir/./file"} {
		t.Run(typed, func(t *testing.T) {
			t.Parallel()

			native := filepath.FromSlash(typed)

			assert.Assert(t, errors.Is(pathcheck.Validate(native), pathcheck.ErrInvalidPath),
				"a relative component is refused as typed")

			abs, err := filepath.Abs(native)
			assert.NilError(t, err)
			assert.NilError(t, pathcheck.Validate(abs), "the absolute form of %q should validate", typed)
		})
	}
}

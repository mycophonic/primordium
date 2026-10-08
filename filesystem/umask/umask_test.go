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

package umask_test

// The contract: Disable zeroes the process umask once and keeps the mask it
// found; Get is that mask, and panics before Disable. The Unix and Windows
// files set the process up and check what Disable did to it.

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/mycophonic/primordium/filesystem/umask"
)

// getFirstVariable asks the test binary, run again by
// TestGetBeforeDisablePanics, to call Get before anything else.
const getFirstVariable = "PRIMORDIUM_UMASK_GET_FIRST"

// getFirst calls Get with no Disable before it: 3 when it panics, as the
// contract says, 0 when it does not.
func getFirst() (code int) {
	defer func() {
		if recover() != nil {
			code = 3
		}
	}()

	_ = umask.Get()

	return 0
}

// TestGetBeforeDisablePanics: Get panics in a process where Disable has not
// run; a run of its own, since this one disables first.
func TestGetBeforeDisablePanics(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")

	cmd.Env = append(os.Environ(), getFirstVariable+"=1")

	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("Get before Disable did not panic: %v", err)
	}
}

// TestDisableIsIdempotent: a second Disable changes nothing Get reports.
func TestDisableIsIdempotent(t *testing.T) {
	t.Parallel()

	umask.Disable()

	first := umask.Get()

	umask.Disable()

	if got := umask.Get(); got != first {
		t.Fatalf("after a second Disable, Get() = %#o, want %#o", got, first)
	}
}

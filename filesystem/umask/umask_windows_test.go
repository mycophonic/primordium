//go:build windows

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

import (
	"os"
	"testing"

	"github.com/mycophonic/primordium/filesystem/umask"
)

func TestMain(m *testing.M) {
	if os.Getenv(getFirstVariable) != "" {
		os.Exit(getFirst())
	}

	m.Run()
}

// TestDisable: Windows has no umask, so Get is 0.
func TestDisable(t *testing.T) {
	t.Parallel()

	umask.Disable()

	if got := umask.Get(); got != 0 {
		t.Fatalf("Get() = %#o on Windows, want 0", got)
	}
}

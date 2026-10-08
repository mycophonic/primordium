//go:build !windows

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
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/filesystem/umask"
)

// startMask is the operator's mask the process is given before Disable.
const startMask = 0o027

func TestMain(m *testing.M) {
	if os.Getenv(getFirstVariable) != "" {
		os.Exit(getFirst())
	}

	syscall.Umask(startMask)
	m.Run()
}

// TestDisable: Get is the mask the process had, and a file created after
// Disable gets exactly the mode asked for, which that mask would have
// stripped.
func TestDisable(t *testing.T) {
	t.Parallel()

	umask.Disable()

	if got := umask.Get(); got != startMask {
		t.Fatalf("Get() = %#o, want the process's %#o", got, startMask)
	}

	path := filepath.Join(t.TempDir(), "created")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		t.Fatal(err)
	}

	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o666 {
		t.Fatalf("a file created as 0o666 after Disable is %v", info.Mode().Perm())
	}
}

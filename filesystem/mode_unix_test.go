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

package filesystem_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/filesystem"
)

// testUmask is the process's umask for the run, so that what a mode is
// stripped to is known.
const testUmask = 0o022

func TestMain(m *testing.M) {
	syscall.Umask(testUmask)
	m.Run()
}

// expectedMode is the mode a written file has: what was asked, less the
// process's umask.
func expectedMode(perm os.FileMode) os.FileMode {
	return perm &^ testUmask
}

// TestModeUnderUmask: a written file has the mode os.WriteFile gives, under
// a umask that strips what was asked. The umask is the process's, so this
// runs sequential: Go runs every sequential test before any parallel one
// starts, and nothing else creates files meanwhile.
//
//nolint:paralleltest // sets the process's umask
func TestModeUnderUmask(t *testing.T) {
	syscall.Umask(0o077)
	defer syscall.Umask(testUmask)

	dir := t.TempDir()

	ours, theirs := filepath.Join(dir, "ours"), filepath.Join(dir, "theirs")

	if err := filesystem.WriteFile(ours, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(theirs, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := os.Stat(ours)
	if err != nil {
		t.Fatal(err)
	}

	want, err := os.Stat(theirs)
	if err != nil {
		t.Fatal(err)
	}

	if got.Mode().Perm() != want.Mode().Perm() || want.Mode().Perm() != 0o600 {
		t.Fatalf("under umask 0o077, WriteFile gives %v, os.WriteFile %v", got.Mode().Perm(), want.Mode().Perm())
	}
}

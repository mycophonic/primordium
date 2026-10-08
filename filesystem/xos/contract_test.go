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

package xos_test

// The contract, from xos's docs: each function is its os namesake ("the same
// filesystem features as golang os package"), and on Windows a file it opens
// can still be renamed or removed (FILE_SHARE_DELETE). So the os package is
// the reference: each xos call runs beside its os namesake, on the same
// filesystem state, and the two agree on what the caller can see: the error's
// kind, what is read, what ends up on disk, and with what mode.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// state is a filesystem state a call starts from, set up afresh in its own
// directory for each side of the comparison.
type state struct {
	name  string
	setup func(t *testing.T, path string)
}

// states are the filesystem states, the two behind a symbolic link only where
// the platform lets the test make one.
func states(t *testing.T) []state {
	t.Helper()

	file := func(data string, mode os.FileMode) func(*testing.T, string) {
		return func(t *testing.T, path string) {
			t.Helper()

			if err := os.WriteFile(path, []byte(data), mode); err != nil {
				t.Fatal(err)
			}

			// Windows cannot remove a read-only file: give it back write
			// before the temporary directory goes.
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		}
	}

	all := []state{
		{"missing", func(*testing.T, string) {}},
		{"empty", file("", 0o644)},
		{"holding data", file("hello", 0o644)},
		{"read-only", file("hello", 0o444)},
		{"a directory", func(t *testing.T, path string) {
			t.Helper()

			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}

	if !canSymlink(t) {
		return all
	}

	return append(all,
		state{"behind a symlink", func(t *testing.T, path string) {
			t.Helper()

			file("hello", 0o644)(t, filepath.Join(filepath.Dir(path), "real"))

			if err := os.Symlink("real", path); err != nil {
				t.Fatal(err)
			}
		}},
		state{"a dangling symlink", func(t *testing.T, path string) {
			t.Helper()

			if err := os.Symlink("missing", path); err != nil {
				t.Fatal(err)
			}
		}},
	)
}

// canSymlink reports whether the platform lets the test make a symbolic link,
// which Windows allows only to an administrator or in developer mode.
func canSymlink(t *testing.T) bool {
	t.Helper()

	err := os.Symlink("target", filepath.Join(t.TempDir(), "link"))
	if err != nil {
		t.Logf("no symbolic links here, so no state behind one: %v", err)
	}

	return err == nil
}

// errorKind is what a caller can tell of an error: none, or which of the
// errors io/fs and syscall name it is.
func errorKind(err error) string {
	for _, kind := range []struct {
		name   string
		target error
	}{
		{"not exist", fs.ErrNotExist},
		{"exist", fs.ErrExist},
		{"permission", fs.ErrPermission},
		{"is a directory", syscall.EISDIR},
		{"not a directory", syscall.ENOTDIR},
		{"invalid", fs.ErrInvalid},
	} {
		if errors.Is(err, kind.target) {
			return kind.name
		}
	}

	if err != nil {
		return "other"
	}

	return "none"
}

// errorSeen is what a caller sees of an error: its kind, and its text with the
// directory it happened in masked, the two sides of a comparison running in
// directories of their own.
func errorSeen(err error, dir string) string {
	if err == nil {
		return "none"
	}

	return errorKind(err) + ": " + strings.ReplaceAll(err.Error(), dir, "DIR")
}

// onDisk is what a path holds after a call: nothing, a directory, or a file's
// content and permission bits.
func onDisk(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return "nothing"
	}

	if info.IsDir() {
		return "a directory"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "unreadable " + info.Mode().Perm().String()
	}

	return info.Mode().Perm().String() + " " + string(data)
}

// pair sets up the same state in two directories, one for xos and one for os.
func pair(t *testing.T, setup func(*testing.T, string)) (ours, theirs string) {
	t.Helper()

	ours, theirs = filepath.Join(t.TempDir(), "target"), filepath.Join(t.TempDir(), "target")
	setup(t, ours)
	setup(t, theirs)

	return ours, theirs
}

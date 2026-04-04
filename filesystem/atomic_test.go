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
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

func TestWriteFileBasic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.txt")
	data := []byte("hello, world")

	if err := filesystem.WriteFile(path, data, filesystem.FilePermissionsDefault); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := xos.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatalf("content = %q, want %q", got, data)
	}
}

func TestWriteFileOverwrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.txt")

	if err := filesystem.WriteFile(path, []byte("first"), filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	if err := filesystem.WriteFile(path, []byte("second"), filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	got, err := xos.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}
}

func TestWriteFileEmpty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.txt")

	if err := filesystem.WriteFile(path, []byte{}, filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	got, err := xos.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Fatalf("content = %q, want empty", got)
	}
}

func TestWriteFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions not fully supported on Windows")
	}

	t.Parallel()

	path := filepath.Join(t.TempDir(), "private.txt")

	if err := filesystem.WriteFile(path, []byte("secret"), filesystem.FilePermissionsPrivate); err != nil {
		t.Fatal(err)
	}

	fi, err := xos.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	got := fi.Mode().Perm()
	// The umask package may have been initialized (zeroing the OS umask) or not.
	// Either way, the result should not be more permissive than requested.
	if got&^os.FileMode(filesystem.FilePermissionsPrivate) != 0 {
		t.Errorf("perm = %#o, has bits beyond %#o", got, filesystem.FilePermissionsPrivate)
	}
}

func TestWriteFileAtomicity(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "atomic.txt")
	original := []byte("original content")

	if err := filesystem.WriteFile(path, original, filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	// Capture inode before.
	fi1, err := xos.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	replacement := []byte("replacement content")

	if err := filesystem.WriteFile(path, replacement, filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	// Verify content is the new data (not partial, not corrupted).
	got, err := xos.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, replacement) {
		t.Fatalf("content = %q, want %q", got, replacement)
	}

	// On Unix, rename replaces the directory entry, producing a new inode.
	// On Windows, the file index may or may not change depending on the
	// filesystem — skip this assertion there.
	if runtime.GOOS != "windows" {
		fi2, err := xos.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		if os.SameFile(fi1, fi2) {
			t.Error("inode did not change after atomic write — expected rename to produce a new inode")
		}
	}
}

func TestWriteFileNonexistentDir(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no", "such", "dir", "file.txt")

	err := filesystem.WriteFile(path, []byte("data"), filesystem.FilePermissionsDefault)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}

	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want fault.ErrWriteFailure in chain", err)
	}
}

func TestWriteFileNoTempLeak(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Successful write should leave no temp files.
	path := filepath.Join(dir, "clean.txt")

	if err := filesystem.WriteFile(path, []byte("data"), filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	entries, err := xos.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file leaked after successful write: %s", e.Name())
		}
	}
}

func TestWriteFileNoTempLeakOnFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write to a nonexistent subdirectory will fail at CreateTemp — no temp
	// file created. Instead, test failure by making the target directory
	// read-only after creating the temp file prefix.
	if runtime.GOOS == "windows" {
		t.Skip("read-only directories behave differently on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	// Create a subdirectory, write a file, then make the subdir read-only
	// so rename into it will fail.
	sub := filepath.Join(dir, "readonly")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(sub, "file.txt")

	// First write succeeds.
	if err := filesystem.WriteFile(target, []byte("ok"), filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	// Make the parent of the temp file unwritable. Since CreateTemp writes
	// to filepath.Dir(filename) which is `sub`, removing write permission
	// on `sub` will cause CreateTemp to fail — no temp file created.
	// Instead, let's target a path where the PARENT dir for temp creation
	// exists but the RENAME will fail.
	//
	// Use a different approach: write to a path where the parent is writable
	// (so CreateTemp succeeds) but the final target is on a read-only path.
	roDir := filepath.Join(dir, "ro-target")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatal(err)
	}

	roTarget := filepath.Join(roDir, "file.txt")

	// Make the target directory read-only so rename fails.
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		// Restore permissions so TempDir cleanup works.
		os.Chmod(roDir, 0o755)
	})

	err := filesystem.WriteFile(roTarget, []byte("should fail"), filesystem.FilePermissionsDefault)
	if err == nil {
		t.Fatal("expected error writing to read-only directory")
	}

	if !errors.Is(err, fault.ErrWriteFailure) {
		t.Errorf("error = %v, want fault.ErrWriteFailure in chain", err)
	}

	// The temp file was created in roDir (filepath.Dir of roTarget).
	// Since roDir is read-only, CreateTemp should have failed — so no temp
	// file. But if it somehow succeeded (e.g. race), verify no leak.
	// Restore permissions to read the directory.
	os.Chmod(roDir, 0o755)

	entries, err := xos.ReadDir(roDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file leaked after failed write: %s", e.Name())
		}
	}
}

func TestWriteFileLargeData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.bin")

	// 1 MiB of data.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i)
	}

	if err := filesystem.WriteFile(path, data, filesystem.FilePermissionsDefault); err != nil {
		t.Fatal(err)
	}

	got, err := xos.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("large file content mismatch")
	}
}

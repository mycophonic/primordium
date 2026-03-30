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

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go stdlib src/os/os_test.go for xos package testing.

//nolint:paralleltest,tparallel,thelper // Ported stdlib tests; preserving original structure.
package xos_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/filesystem/xos"
)

// ---------------------------------------------------------------------------
// Test environment helpers (replacing internal/testenv)
// ---------------------------------------------------------------------------

func mustHaveSymlink(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
}

func hasSymlink() bool {
	dir, err := os.MkdirTemp("", "symcheck")
	if err != nil {
		return false
	}

	defer os.RemoveAll(dir) //nolint:errcheck // Best-effort cleanup.

	return os.Symlink("t", filepath.Join(dir, "l")) == nil
}

func hasLink() bool {
	dir, err := os.MkdirTemp("", "linkcheck")
	if err != nil {
		return false
	}

	defer os.RemoveAll(dir) //nolint:errcheck // Best-effort cleanup.

	f, err := os.Create(filepath.Join(dir, "t"))
	if err != nil {
		return false
	}

	f.Close()

	return os.Link(filepath.Join(dir, "t"), filepath.Join(dir, "l")) == nil
}

// ---------------------------------------------------------------------------
// Platform-specific system directory for read-only stat/readdir tests
// ---------------------------------------------------------------------------

type sysDir struct {
	name  string
	files []string
}

var sysdir = func() *sysDir { //nolint:gochecknoglobals // Test fixture.
	switch runtime.GOOS {
	case "android":
		return &sysDir{
			"/system/lib",
			[]string{
				"libmedia.so",
				"libpowermanager.so",
			},
		}
	case "windows":
		return &sysDir{
			os.Getenv("SystemRoot") + "\\system32\\drivers\\etc",
			[]string{
				"networks",
				"protocol",
				"services",
			},
		}
	case "plan9":
		return &sysDir{
			"/lib/ndb",
			[]string{
				"common",
				"local",
			},
		}
	default:
		return &sysDir{
			"/etc",
			[]string{
				"group",
				"hosts",
				"passwd",
			},
		}
	}
}()

var (
	sfdir  = sysdir.name     //nolint:gochecknoglobals // Test fixture.
	sfname = sysdir.files[0] //nolint:gochecknoglobals // Test fixture.
)

// dot lists files expected in the xos package source directory.
var dot = []string{ //nolint:gochecknoglobals // Test fixture.
	"doc.go",
	"create.go",
	"tempfile.go",
	"file_test.go",
}

// ---------------------------------------------------------------------------
// Helpers (adapted from stdlib os_test.go)
// ---------------------------------------------------------------------------

func size(name string, t *testing.T) int64 {
	file, err := xos.Open(name)
	if err != nil {
		t.Fatal("open failed:", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}()

	n, err := io.Copy(io.Discard, file)
	if err != nil {
		t.Fatal(err)
	}

	return n
}

func equal(name1, name2 string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name1, name2)
	}

	return name1 == name2
}

func newFile(t *testing.T) *os.File {
	t.Helper()

	f, err := xos.CreateTemp("", "_Go_"+t.Name())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Fatal(err)
		}

		if err := os.Remove(f.Name()); err != nil {
			t.Fatal(err)
		}
	})

	return f
}

func checkSize(t *testing.T, f *os.File, size int64) {
	t.Helper()

	dir, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat %q (looking for size %d): %s", f.Name(), size, err)
	}

	if dir.Size() != size {
		t.Errorf("Stat %q: size %d want %d", f.Name(), dir.Size(), size)
	}
}

func touch(t *testing.T, name string) {
	t.Helper()

	f, err := xos.Create(name)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// Read a directory one entry at a time.
func smallReaddirnames(file *os.File, length int, t *testing.T) []string {
	names := make([]string, length)
	count := 0

	for {
		d, err := file.Readdirnames(1)
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("readdirnames %q failed: %v", file.Name(), err)
		}

		if len(d) == 0 {
			t.Fatalf("readdirnames %q returned empty slice and no error", file.Name())
		}

		names[count] = d[0]
		count++
	}

	return names[0:count]
}

// writeFile is a simplified version of the stdlib helper (no Root parameter).
func writeFile(t *testing.T, fname string, flag int, text string) string {
	t.Helper()

	f, err := xos.OpenFile(fname, flag, 0o666)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	n, err := io.WriteString(f, text)
	if err != nil {
		t.Fatalf("WriteString: %d, %v", n, err)
	}

	f.Close()

	data, err := xos.ReadFile(fname)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	return string(data)
}

func testReaddirnames(dir string, contents []string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		file, err := xos.Open(dir)
		if err != nil {
			t.Fatalf("open %q failed: %v", dir, err)
		}

		defer file.Close()

		s, err2 := file.Readdirnames(-1)
		if err2 != nil {
			t.Fatalf("Readdirnames %q failed: %v", dir, err2)
		}

		for _, m := range contents {
			found := false

			for _, n := range s {
				if n == "." || n == ".." {
					t.Errorf("got %q in directory", n)
				}

				if !equal(m, n) {
					continue
				}

				if found {
					t.Error("present twice:", m)
				}

				found = true
			}

			if !found {
				t.Error("could not find", m)
			}
		}

		if s == nil {
			t.Error("Readdirnames returned nil instead of empty slice")
		}
	}
}

func readdirSubtest(dir string, contents []string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		file, err := xos.Open(dir)
		if err != nil {
			t.Fatalf("open %q failed: %v", dir, err)
		}

		defer file.Close()

		s, err2 := file.Readdir(-1)
		if err2 != nil {
			t.Fatalf("Readdir %q failed: %v", dir, err2)
		}

		for _, m := range contents {
			found := false

			for _, n := range s {
				if n.Name() == "." || n.Name() == ".." {
					t.Errorf("got %q in directory", n.Name())
				}

				if !equal(m, n.Name()) {
					continue
				}

				if found {
					t.Error("present twice:", m)
				}

				found = true
			}

			if !found {
				t.Error("could not find", m)
			}
		}

		if s == nil {
			t.Error("Readdir returned nil instead of empty slice")
		}
	}
}

func readDirEntrySubtest(dir string, contents []string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		file, err := xos.Open(dir)
		if err != nil {
			t.Fatalf("open %q failed: %v", dir, err)
		}

		defer file.Close()

		s, err2 := file.ReadDir(-1)
		if err2 != nil {
			t.Fatalf("ReadDir %q failed: %v", dir, err2)
		}

		for _, m := range contents {
			found := false

			for _, n := range s {
				if n.Name() == "." || n.Name() == ".." {
					t.Errorf("got %q in directory", n)
				}

				if !equal(m, n.Name()) {
					continue
				}

				if found {
					t.Error("present twice:", m)
				}

				found = true

				lstat, err := os.Lstat(dir + "/" + m)
				if err != nil {
					t.Fatal(err)
				}

				if n.IsDir() != lstat.IsDir() {
					t.Errorf("%s: IsDir=%v, want %v", m, n.IsDir(), lstat.IsDir())
				}

				if n.Type() != lstat.Mode().Type() {
					t.Errorf("%s: Type=%v, want %v", m, n.Type(), lstat.Mode().Type())
				}

				info, err := n.Info()
				if err != nil {
					t.Errorf("%s: Info: %v", m, err)

					continue
				}

				if !os.SameFile(info, lstat) {
					t.Errorf("%s: Info: SameFile(info, lstat) = false", m)
				}
			}

			if !found {
				t.Error("could not find", m)
			}
		}

		if s == nil {
			t.Error("ReadDir returned nil instead of empty slice")
		}
	}
}

func testDevNullFileInfo(t *testing.T, statname, devNullName string, fi os.FileInfo) {
	t.Helper()

	pre := fmt.Sprintf("%s(%q): ", statname, devNullName)

	if fi.Size() != 0 {
		t.Errorf(pre+"wrong file size have %d want 0", fi.Size())
	}

	if fi.Mode()&os.ModeDevice == 0 {
		t.Errorf(pre+"wrong file mode %q: ModeDevice is not set", fi.Mode())
	}

	if fi.Mode()&os.ModeCharDevice == 0 {
		t.Errorf(pre+"wrong file mode %q: ModeCharDevice is not set", fi.Mode())
	}

	if fi.Mode().IsRegular() {
		t.Errorf(pre+"wrong file mode %q: IsRegular returns true", fi.Mode())
	}
}

func assertDevNullFile(t *testing.T, devNullName string) {
	t.Helper()

	f, err := xos.Open(devNullName)
	if err != nil {
		t.Fatalf("Open(%s): %v", devNullName, err)
	}

	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat(%s): %v", devNullName, err)
	}

	testDevNullFileInfo(t, "f.Stat", devNullName, fi)

	fi, err = xos.Stat(devNullName)
	if err != nil {
		t.Fatalf("Stat(%s): %v", devNullName, err)
	}

	testDevNullFileInfo(t, "Stat", devNullName, fi)
}

func doubleCloseSubtest(path string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()

		file, err := xos.Open(path)
		if err != nil {
			t.Fatal(err)
		}

		if err := file.Close(); err != nil {
			t.Fatalf("unexpected error from Close: %v", err)
		}

		if err := file.Close(); err == nil {
			t.Error("second Close did not fail")
		} else if pe := (&os.PathError{}); !errors.As(err, &pe) {
			t.Errorf("second Close: got %T, want %T", err, pe)
		} else if !errors.Is(pe.Err, os.ErrClosed) {
			t.Errorf("second Close: got %q, want %q", pe.Err, os.ErrClosed)
		} else {
			t.Logf("second close returned expected error %q", err)
		}
	}
}

func assertOpenFileKeepsPermissions(t *testing.T) {
	t.Helper()
	t.Parallel()

	dir := t.TempDir()
	name := filepath.Join(dir, "x")

	f, err := xos.Create(name)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Error(err)
	}

	f, err = xos.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}

	if fi, err := f.Stat(); err != nil {
		t.Error(err)
	} else if fi.Mode()&0o222 == 0 {
		t.Errorf("f.Stat.Mode after OpenFile is %v, should be writable", fi.Mode())
	}

	if err := f.Close(); err != nil {
		t.Error(err)
	}

	if fi, err := xos.Stat(name); err != nil {
		t.Error(err)
	} else if fi.Mode()&0o222 == 0 {
		t.Errorf("Stat after OpenFile is %v, should be writable", fi.Mode())
	}
}

// ---------------------------------------------------------------------------
// Stat tests
// ---------------------------------------------------------------------------

func TestStat(t *testing.T) {
	t.Parallel()

	path := sfdir + "/" + sfname

	dir, err := xos.Stat(path)
	if err != nil {
		t.Fatal("stat failed:", err)
	}

	if !equal(sfname, dir.Name()) {
		t.Error("name should be", sfname, "; is", dir.Name())
	}

	filesize := size(path, t)
	if dir.Size() != filesize {
		t.Error("size should be", filesize, "; is", dir.Size())
	}
}

func TestFstat(t *testing.T) {
	t.Parallel()

	path := sfdir + "/" + sfname

	file, err1 := xos.Open(path)
	if err1 != nil {
		t.Fatal("open failed:", err1)
	}

	defer file.Close()

	dir, err2 := file.Stat()
	if err2 != nil {
		t.Fatal("fstat failed:", err2)
	}

	if !equal(sfname, dir.Name()) {
		t.Error("name should be", sfname, "; is", dir.Name())
	}

	filesize := size(path, t)
	if dir.Size() != filesize {
		t.Error("size should be", filesize, "; is", dir.Size())
	}
}

func TestStatError(t *testing.T) {
	t.Chdir(t.TempDir())

	path := "no-such-file"

	fi, err := xos.Stat(path)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	if fi != nil {
		t.Errorf("got %v, want nil", fi)
	}

	if perr := (&os.PathError{}); !errors.As(err, &perr) {
		t.Errorf("got %T, want %T", err, perr)
	}

	mustHaveSymlink(t)

	link := "symlink"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}

	fi, err = xos.Stat(link)
	if err == nil {
		t.Fatal("got nil, want error")
	}

	if fi != nil {
		t.Errorf("got %v, want nil", fi)
	}

	if perr := (&os.PathError{}); !errors.As(err, &perr) {
		t.Errorf("got %T, want %T", err, perr)
	}
}

func TestStatDirWithTrailingSlash(t *testing.T) {
	t.Parallel()

	path := t.TempDir()

	if _, err := xos.Stat(path); err != nil {
		t.Fatalf("stat %s failed: %s", path, err)
	}

	path += "/"

	if _, err := xos.Stat(path); err != nil {
		t.Fatalf("stat %s failed: %s", path, err)
	}
}

func TestStatDirModeExec(t *testing.T) {
	if runtime.GOOS == "wasip1" {
		t.Skip("Chmod is not supported on " + runtime.GOOS)
	}

	t.Parallel()

	const mode = 0o111

	path := t.TempDir()
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatalf("Chmod %q 0777: %v", path, err)
	}

	dir, err := xos.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q (looking for mode %#o): %s", path, mode, err)
	}

	if dir.Mode()&mode != mode {
		t.Errorf("Stat %q: mode %#o want %#o", path, dir.Mode()&mode, mode)
	}
}

func TestStatRelativeSymlink(t *testing.T) {
	mustHaveSymlink(t)
	t.Parallel()

	tmpdir := t.TempDir()
	target := filepath.Join(tmpdir, "target")

	f, err := xos.Create(target)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(tmpdir, "link")
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}

	st1, err := xos.Stat(link)
	if err != nil {
		t.Fatal(err)
	}

	if !os.SameFile(st, st1) {
		t.Error("Stat doesn't follow relative symlink")
	}

	if runtime.GOOS == "windows" {
		os.Remove(link)

		if err := os.Symlink(target[len(filepath.VolumeName(target)):], link); err != nil {
			t.Fatal(err)
		}

		st1, err := xos.Stat(link)
		if err != nil {
			t.Fatal(err)
		}

		if !os.SameFile(st, st1) {
			t.Error("Stat doesn't follow relative symlink")
		}
	}
}

// ---------------------------------------------------------------------------
// Open tests
// ---------------------------------------------------------------------------

func TestRead0(t *testing.T) {
	t.Parallel()

	path := sfdir + "/" + sfname

	f, err := xos.Open(path)
	if err != nil {
		t.Fatal("open failed:", err)
	}

	defer f.Close()

	b := make([]byte, 0)

	n, err := f.Read(b)
	if n != 0 || err != nil {
		t.Errorf("Read(0) = %d, %v, want 0, nil", n, err)
	}

	b = make([]byte, 100)

	n, err = f.Read(b)
	if n <= 0 || err != nil {
		t.Errorf("Read(100) = %d, %v, want >0, nil", n, err)
	}
}

func TestReadClosed(t *testing.T) {
	t.Parallel()

	path := sfdir + "/" + sfname

	file, err := xos.Open(path)
	if err != nil {
		t.Fatal("open failed:", err)
	}

	file.Close()

	b := make([]byte, 100)
	_, err = file.Read(b)

	var e *os.PathError
	if !errors.As(err, &e) || !errors.Is(e.Err, os.ErrClosed) {
		t.Fatalf("Read: got %T(%v), want %T(%v)", err, err, e, os.ErrClosed)
	}
}

func TestOpenNoName(t *testing.T) {
	f, err := xos.Open("")
	if err == nil {
		f.Close()
		t.Fatal(`Open("") succeeded`)
	}
}

func TestOpenError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a file and a directory for error mapping tests.
	touch(t, filepath.Join(dir, "is-a-file"))

	if err := os.Mkdir(filepath.Join(dir, "is-a-dir"), 0o777); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		path  string
		mode  int
		error error
	}{
		{"no-such-file", os.O_RDONLY, syscall.ENOENT},
		{"is-a-dir", os.O_WRONLY, syscall.EISDIR},
		{"is-a-file/no-such-file", os.O_WRONLY, syscall.ENOTDIR},
	} {
		path := filepath.Join(dir, tt.path)
		name := fmt.Sprintf("OpenFile(%q, %d)", path, tt.mode)

		f, err := xos.OpenFile(path, tt.mode, 0)
		if err == nil {
			t.Errorf("%v succeeded", name)
			f.Close()

			continue
		}

		var perr *os.PathError
		if !errors.As(err, &perr) {
			t.Errorf("%v returns error of %T type; want *PathError", name, err)
		}

		if !errors.Is(perr.Err, tt.error) {
			if runtime.GOOS == "dragonfly" {
				// DragonFly incorrectly returns EACCES rather than EISDIR
				// when a directory is opened for write.
				if errors.Is(tt.error, syscall.EISDIR) && errors.Is(perr.Err, syscall.EACCES) {
					continue
				}
			}

			t.Errorf("%v = _, %q; want %q", name, perr.Err.Error(), tt.error.Error())
		}
	}
}

func TestDevNullFile(t *testing.T) {
	t.Parallel()

	assertDevNullFile(t, os.DevNull)

	if runtime.GOOS == "windows" {
		assertDevNullFile(t, "./nul")
		assertDevNullFile(t, "//./nul")
	}
}

func TestOpenFileDevNull(t *testing.T) {
	t.Parallel()

	f, err := xos.OpenFile(os.DevNull, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile(DevNull): %v", err)
	}

	f.Close()
}

func TestDoubleCloseError(t *testing.T) {
	t.Parallel()

	t.Run("file", doubleCloseSubtest(filepath.Join(sfdir, sfname)))
	t.Run("dir", doubleCloseSubtest(sfdir))
}

func TestSameFile(t *testing.T) {
	t.Chdir(t.TempDir())

	fa, err := xos.Create("a")
	if err != nil {
		t.Fatalf("Create(a): %v", err)
	}

	fa.Close()

	fb, err := xos.Create("b")
	if err != nil {
		t.Fatalf("Create(b): %v", err)
	}

	fb.Close()

	ia1, err := xos.Stat("a")
	if err != nil {
		t.Fatalf("Stat(a): %v", err)
	}

	ia2, err := xos.Stat("a")
	if err != nil {
		t.Fatalf("Stat(a): %v", err)
	}

	if !os.SameFile(ia1, ia2) {
		t.Error("files should be same")
	}

	ib, err := xos.Stat("b")
	if err != nil {
		t.Fatalf("Stat(b): %v", err)
	}

	if os.SameFile(ia1, ib) {
		t.Error("files should be different")
	}
}

// ---------------------------------------------------------------------------
// OpenFile tests
// ---------------------------------------------------------------------------

func TestFileRDWRFlags(t *testing.T) {
	for _, test := range []struct {
		name string
		flag int
	}{
		{"O_RDONLY", os.O_RDONLY},
		{"O_WRONLY", os.O_WRONLY},
		{"O_RDWR", os.O_RDWR},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			const filename = "f"

			content := []byte("content")

			if err := xos.WriteFile(filename, content, 0o666); err != nil {
				t.Fatal(err)
			}

			f, err := xos.OpenFile(filename, test.flag, 0)
			if err != nil {
				t.Fatal(err)
			}

			defer f.Close()

			got, err := io.ReadAll(f)
			if test.flag == os.O_WRONLY {
				if err == nil {
					t.Errorf("read file: %q, %v; want error", got, err)
				}
			} else {
				if err != nil || !bytes.Equal(got, content) {
					t.Errorf("read file: %q, %v; want %q, <nil>", got, err, content)
				}
			}

			if _, err := f.Seek(0, 0); err != nil {
				t.Fatalf("f.Seek: %v", err)
			}

			newcontent := []byte("CONTENT")
			_, err = f.Write(newcontent)

			if test.flag == os.O_RDONLY {
				if err == nil {
					t.Error("write file: succeeded, want error")
				}
			} else {
				if err != nil {
					t.Errorf("write file: %v, want success", err)
				}
			}

			f.Close()

			got, err = xos.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}

			want := content
			if test.flag != os.O_RDONLY {
				want = newcontent
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("after write, file contains %q, want %q", got, want)
			}
		})
	}
}

func TestFilePermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root")
	}

	for _, test := range []struct {
		name string
		mode os.FileMode
	}{
		{"r", 0o444},
		{"w", 0o222},
		{"rw", 0o666},
	} {
		t.Run(test.name, func(t *testing.T) {
			switch runtime.GOOS {
			case "windows":
				if test.mode&0o444 == 0 {
					t.Skip("write-only files not supported on " + runtime.GOOS)
				}
			case "wasip1":
				t.Skip("file permissions not supported on " + runtime.GOOS)
			default:
			}

			t.Chdir(t.TempDir())

			const filename = "f"

			f, err := xos.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_EXCL, test.mode)
			if err != nil {
				t.Fatal(err)
			}

			f.Close()

			b, err := xos.ReadFile(filename)
			if test.mode&0o444 != 0 {
				if err != nil {
					t.Errorf("ReadFile = %v; want success", err)
				}
			} else {
				if err == nil {
					t.Errorf("ReadFile = %q, <nil>; want failure", string(b))
				}
			}

			_, err = xos.Stat(filename)
			if err != nil {
				t.Errorf("Stat = %v; want success", err)
			}

			err = xos.WriteFile(filename, nil, 0o666)
			if test.mode&0o222 != 0 {
				if err != nil {
					t.Errorf("WriteFile = %v; want success", err)
				}
			} else {
				if err == nil {
					t.Errorf("WriteFile(%q) = <nil>; want failure", filename)
				}
			}
		})
	}
}

func TestOpenFileKeepsPermissions(t *testing.T) {
	assertOpenFileKeepsPermissions(t)
}

func TestOpenFileCreateExclDanglingSymlink(t *testing.T) {
	mustHaveSymlink(t)

	t.Chdir(t.TempDir())

	const link = "link"
	if err := os.Symlink("does_not_exist", link); err != nil {
		t.Fatal(err)
	}

	f, err := xos.OpenFile(link, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err == nil {
		f.Close()
	}

	if !errors.Is(err, os.ErrExist) {
		t.Errorf("OpenFile of a dangling symlink with O_CREATE|O_EXCL = %v, want ErrExist", err)
	}

	if _, err := xos.Stat(link); err == nil {
		t.Error("OpenFile of a dangling symlink with O_CREATE|O_EXCL created a file")
	}
}

func TestAppend(t *testing.T) {
	t.Chdir(t.TempDir())

	const f = "append.txt"

	s := writeFile(t, f, os.O_CREATE|os.O_TRUNC|os.O_RDWR, "new")
	if s != "new" {
		t.Fatalf("writeFile: have %q want %q", s, "new")
	}

	s = writeFile(t, f, os.O_APPEND|os.O_RDWR, "|append")
	if s != "new|append" {
		t.Fatalf("writeFile: have %q want %q", s, "new|append")
	}

	s = writeFile(t, f, os.O_CREATE|os.O_APPEND|os.O_RDWR, "|append")
	if s != "new|append|append" {
		t.Fatalf("writeFile: have %q want %q", s, "new|append|append")
	}

	if err := os.Remove(f); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	s = writeFile(t, f, os.O_CREATE|os.O_APPEND|os.O_RDWR, "new&append")
	if s != "new&append" {
		t.Fatalf("writeFile: after append have %q want %q", s, "new&append")
	}

	s = writeFile(t, f, os.O_CREATE|os.O_RDWR, "old")
	if s != "old&append" {
		t.Fatalf("writeFile: after create have %q want %q", s, "old&append")
	}

	s = writeFile(t, f, os.O_CREATE|os.O_TRUNC|os.O_RDWR, "new")
	if s != "new" {
		t.Fatalf("writeFile: after truncate have %q want %q", s, "new")
	}
}

func TestAppendDoesntOverwrite(t *testing.T) {
	t.Chdir(t.TempDir())

	const name = "file"

	if err := xos.WriteFile(name, []byte("hello"), 0o666); err != nil {
		t.Fatal(err)
	}

	f, err := xos.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(" world"); err != nil {
		f.Close()
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := xos.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}

	want := "hello world"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Truncate tests
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	checkSize(t, f, 0)
	f.WriteString("hello, world\n")
	checkSize(t, f, 13)
	xos.Truncate(f.Name(), 10)
	checkSize(t, f, 10)
	xos.Truncate(f.Name(), 1024)
	checkSize(t, f, 1024)
	xos.Truncate(f.Name(), 0)
	checkSize(t, f, 0)

	_, err := f.WriteString("surprise!")
	if err == nil {
		checkSize(t, f, 13+9) // wrote at offset past where hello, world was.
	}
}

func TestFTruncate(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	checkSize(t, f, 0)
	f.WriteString("hello, world\n")
	checkSize(t, f, 13)
	f.Truncate(10)
	checkSize(t, f, 10)
	f.Truncate(1024)
	checkSize(t, f, 1024)
	f.Truncate(0)
	checkSize(t, f, 0)

	_, err := f.WriteString("surprise!")
	if err == nil {
		checkSize(t, f, 13+9) // wrote at offset past where hello, world was.
	}
}

func TestTruncateNonexistentFile(t *testing.T) {
	t.Parallel()

	assertPathError := func(t testing.TB, path string, err error) {
		t.Helper()

		if pe := (&os.PathError{}); !errors.As(err, &pe) || !os.IsNotExist(err) || pe.Path != path {
			t.Errorf("got error: %v\nwant an ErrNotExist PathError with path %q", err, path)
		}
	}

	path := filepath.Join(t.TempDir(), "nonexistent")

	err := xos.Truncate(path, 1)
	assertPathError(t, path, err)

	// Truncate shouldn't create any new file.
	_, err = xos.Stat(path)
	assertPathError(t, path, err)
}

// ---------------------------------------------------------------------------
// Seek tests
// ---------------------------------------------------------------------------

func TestSeek(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	const data = "hello, world\n"
	io.WriteString(f, data)

	type test struct {
		in     int64
		whence int
		out    int64
	}

	tests := []test{
		{0, io.SeekCurrent, int64(len(data))},
		{0, io.SeekStart, 0},
		{5, io.SeekStart, 5},
		{0, io.SeekEnd, int64(len(data))},
		{0, io.SeekStart, 0},
		{-1, io.SeekEnd, int64(len(data)) - 1},
		{1 << 33, io.SeekStart, 1 << 33},
		{1 << 33, io.SeekEnd, 1<<33 + int64(len(data))},

		// Issue 21681, Windows 4G-1, etc:
		{1<<32 - 1, io.SeekStart, 1<<32 - 1},
		{0, io.SeekCurrent, 1<<32 - 1},
		{2<<32 - 1, io.SeekStart, 2<<32 - 1},
		{0, io.SeekCurrent, 2<<32 - 1},
	}

	for i, tt := range tests {
		off, err := f.Seek(tt.in, tt.whence)
		if off != tt.out || err != nil {
			t.Errorf("#%d: Seek(%v, %v) = %v, %v want %v, nil", i, tt.in, tt.whence, off, err, tt.out)
		}
	}
}

// ---------------------------------------------------------------------------
// ReadAt / WriteAt tests
// ---------------------------------------------------------------------------

func TestReadAt(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	const data = "hello, world\n"
	io.WriteString(f, data)

	b := make([]byte, 5)

	n, err := f.ReadAt(b, 7)
	if err != nil || n != len(b) {
		t.Fatalf("ReadAt 7: %d, %v", n, err)
	}

	if string(b) != "world" {
		t.Fatalf("ReadAt 7: have %q want %q", string(b), "world")
	}
}

func TestReadAtOffset(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	const data = "hello, world\n"
	io.WriteString(f, data)

	f.Seek(0, 0)

	b := make([]byte, 5)

	n, err := f.ReadAt(b, 7)
	if err != nil || n != len(b) {
		t.Fatalf("ReadAt 7: %d, %v", n, err)
	}

	if string(b) != "world" {
		t.Fatalf("ReadAt 7: have %q want %q", string(b), "world")
	}

	n, err = f.Read(b)
	if err != nil || n != len(b) {
		t.Fatalf("Read: %d, %v", n, err)
	}

	if string(b) != "hello" {
		t.Fatalf("Read: have %q want %q", string(b), "hello")
	}
}

func TestReadAtNegativeOffset(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	const data = "hello, world\n"
	io.WriteString(f, data)

	f.Seek(0, 0)

	b := make([]byte, 5)
	n, err := f.ReadAt(b, -10)

	const wantsub = "negative offset"
	if !strings.Contains(fmt.Sprint(err), wantsub) || n != 0 {
		t.Errorf("ReadAt(-10) = %v, %v; want 0, ...%q...", n, err, wantsub)
	}
}

func TestReadAtEOF(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	_, err := f.ReadAt(make([]byte, 10), 0)

	switch err {
	case io.EOF:
		// all good
	case nil:
		t.Fatal("ReadAt succeeded")
	default:
		t.Fatalf("ReadAt failed: %s", err)
	}
}

func TestWriteAt(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	const data = "hello, world"
	io.WriteString(f, data)

	n, err := f.WriteAt([]byte("WOR"), 7)
	if err != nil || n != 3 {
		t.Fatalf("WriteAt 7: %d, %v", n, err)
	}

	n, err = io.WriteString(f, "!") // test that WriteAt doesn't change the file offset
	if err != nil || n != 1 {
		t.Fatal(err)
	}

	got, err := xos.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile %s: %v", f.Name(), err)
	}

	want := "hello, WORld!"
	if string(got) != want {
		t.Fatalf("after write: have %q want %q", string(got), want)
	}
}

func TestWriteAtConcurrent(t *testing.T) {
	t.Parallel()

	f := newFile(t)
	io.WriteString(f, "0000000000")

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Go(func() {
			n, err := f.WriteAt([]byte(strconv.Itoa(i)), int64(i))
			if err != nil || n != 1 {
				t.Errorf("WriteAt %d: %d, %v", i, n, err)
			}

			n, err = io.WriteString(f, "!") // test that WriteAt doesn't change the file offset
			if err != nil || n != 1 {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	got, err := xos.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile %s: %v", f.Name(), err)
	}

	want := "0123456789!!!!!!!!!!"
	if string(got) != want {
		t.Fatalf("after write: have %q want %q", string(got), want)
	}
}

func TestWriteAtNegativeOffset(t *testing.T) {
	t.Parallel()

	f := newFile(t)

	n, err := f.WriteAt([]byte("WORLD"), -10)

	const wantsub = "negative offset"
	if !strings.Contains(fmt.Sprint(err), wantsub) || n != 0 {
		t.Errorf("WriteAt(-10) = %v, %v; want 0, ...%q...", n, err, wantsub)
	}
}

// ---------------------------------------------------------------------------
// Readdir tests
// ---------------------------------------------------------------------------

func TestFileReaddirnames(t *testing.T) {
	t.Parallel()

	t.Run(".", testReaddirnames(".", dot))
	t.Run("sysdir", testReaddirnames(sysdir.name, sysdir.files))
	t.Run("TempDir", testReaddirnames(t.TempDir(), nil))
}

func TestFileReaddirFileInfo(t *testing.T) {
	t.Parallel()

	t.Run(".", readdirSubtest(".", dot))
	t.Run("sysdir", readdirSubtest(sysdir.name, sysdir.files))
	t.Run("TempDir", readdirSubtest(t.TempDir(), nil))
}

func TestFileReadDirEntries(t *testing.T) {
	t.Parallel()

	t.Run(".", readDirEntrySubtest(".", dot))
	t.Run("sysdir", readDirEntrySubtest(sysdir.name, sysdir.files))
	t.Run("TempDir", readDirEntrySubtest(t.TempDir(), nil))
}

func TestReaddirNValues(t *testing.T) {
	if testing.Short() {
		t.Skip("test.short; skipping")
	}

	t.Parallel()

	dir := t.TempDir()

	for i := 1; i <= 105; i++ {
		f, err := xos.Create(filepath.Join(dir, fmt.Sprintf("%d", i)))
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		f.WriteString(strings.Repeat("X", i))
		f.Close()
	}

	var d *os.File

	openDir := func() {
		var err error

		d, err = xos.Open(dir)
		if err != nil {
			t.Fatalf("Open directory: %v", err)
		}
	}

	readdirExpect := func(n, want int, wantErr error) {
		t.Helper()

		fi, err := d.Readdir(n)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Readdir of %d got error %v, want %v", n, err, wantErr)
		}

		if g, e := len(fi), want; g != e {
			t.Errorf("Readdir of %d got %d files, want %d", n, g, e)
		}
	}

	readDirExpect := func(n, want int, wantErr error) {
		t.Helper()

		de, err := d.ReadDir(n)
		if !errors.Is(err, wantErr) {
			t.Fatalf("ReadDir of %d got error %v, want %v", n, err, wantErr)
		}

		if g, e := len(de), want; g != e {
			t.Errorf("ReadDir of %d got %d files, want %d", n, g, e)
		}
	}

	readdirnamesExpect := func(n, want int, wantErr error) {
		t.Helper()

		fi, err := d.Readdirnames(n)
		if !errors.Is(err, wantErr) {
			t.Fatalf("Readdirnames of %d got error %v, want %v", n, err, wantErr)
		}

		if g, e := len(fi), want; g != e {
			t.Errorf("Readdirnames of %d got %d files, want %d", n, g, e)
		}
	}

	for _, fn := range []func(int, int, error){readdirExpect, readdirnamesExpect, readDirExpect} {
		// Test the slurp case
		openDir()
		fn(0, 105, nil)
		fn(0, 0, nil)
		d.Close()

		// Slurp with -1 instead
		openDir()
		fn(-1, 105, nil)
		fn(-2, 0, nil)
		fn(0, 0, nil)
		d.Close()

		// Test the bounded case
		openDir()
		fn(1, 1, nil)
		fn(2, 2, nil)
		fn(105, 102, nil) // and tests buffer >100 case
		fn(3, 0, io.EOF)
		d.Close()
	}
}

// Readdir on a regular file should fail.
func TestReaddirOfFile(t *testing.T) {
	t.Parallel()

	f, err := xos.CreateTemp(t.TempDir(), "_Go_ReaddirOfFile")
	if err != nil {
		t.Fatal(err)
	}

	f.WriteString("foo")
	f.Close()

	reg, err := xos.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	defer reg.Close()

	names, err := reg.Readdirnames(-1)
	if err == nil {
		t.Error("Readdirnames succeeded; want non-nil error")
	}

	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Path != f.Name() {
		t.Errorf("Readdirnames returned %q; want a PathError with path %q", err, f.Name())
	}

	if len(names) > 0 {
		t.Errorf("unexpected dir names in regular file: %q", names)
	}
}

func TestReaddirnamesOneAtATime(t *testing.T) {
	t.Parallel()

	// big directory that doesn't change often.
	dir := "/usr/bin"

	switch runtime.GOOS {
	case "android":
		dir = "/system/bin"
	case "ios", "wasip1":
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}

		dir = wd
	case "plan9":
		dir = "/bin"
	case "windows":
		dir = os.Getenv("SystemRoot") + "\\system32"
	default:
	}

	file, err := xos.Open(dir)
	if err != nil {
		t.Fatalf("open %q failed: %v", dir, err)
	}

	defer file.Close()

	all, err1 := file.Readdirnames(-1)
	if err1 != nil {
		t.Fatalf("readdirnames %q failed: %v", dir, err1)
	}

	file1, err2 := xos.Open(dir)
	if err2 != nil {
		t.Fatalf("open %q failed: %v", dir, err2)
	}

	defer file1.Close()

	small := smallReaddirnames(file1, len(all)+100, t)
	if len(small) < len(all) {
		t.Fatalf("len(small) is %d, less than %d", len(small), len(all))
	}

	for i, n := range all {
		if small[i] != n {
			t.Errorf("small read %q mismatch: %v", small[i], n)
		}
	}
}

func TestDirSeek(t *testing.T) {
	t.Parallel()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	f, err := xos.Open(wd)
	if err != nil {
		t.Fatal(err)
	}

	dirnames1, err := f.Readdirnames(0)
	if err != nil {
		t.Fatal(err)
	}

	ret, err := f.Seek(0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if ret != 0 {
		t.Fatalf("seek result not zero: %d", ret)
	}

	dirnames2, err := f.Readdirnames(0)
	if err != nil {
		t.Fatal(err)
	}

	if len(dirnames1) != len(dirnames2) {
		t.Fatalf("listings have different lengths: %d and %d\n", len(dirnames1), len(dirnames2))
	}

	for i, n1 := range dirnames1 {
		n2 := dirnames2[i]
		if n1 != n2 {
			t.Fatalf("different name i=%d n1=%s n2=%s\n", i, n1, n2)
		}
	}
}

// ---------------------------------------------------------------------------
// LongPath test
// ---------------------------------------------------------------------------

func TestLongPath(t *testing.T) {
	t.Parallel()

	tmpdir := t.TempDir()

	// Test the boundary of 247 and fewer bytes (normal) and 248 and more bytes (adjusted).
	sizes := []int{247, 248, 249, 400}

	for len(tmpdir) < 400 {
		tmpdir += "/dir3456789"
	}

	for _, sz := range sizes {
		t.Run(fmt.Sprintf("length=%d", sz), func(t *testing.T) {
			sizedTempDir := tmpdir[:sz-1] + "x"

			if err := os.MkdirAll(sizedTempDir, 0o755); err != nil {
				t.Fatalf("MkdirAll failed: %v", err)
			}

			data := []byte("hello world\n")

			if err := xos.WriteFile(sizedTempDir+"/foo.txt", data, 0o644); err != nil {
				t.Fatalf("WriteFile() failed: %v", err)
			}

			if err := os.Rename(sizedTempDir+"/foo.txt", sizedTempDir+"/bar.txt"); err != nil {
				t.Fatalf("Rename failed: %v", err)
			}

			names := []string{"bar.txt"}

			if hasSymlink() {
				if err := os.Symlink(sizedTempDir+"/bar.txt", sizedTempDir+"/symlink.txt"); err != nil {
					t.Fatalf("Symlink failed: %v", err)
				}

				names = append(names, "symlink.txt")
			}

			if hasLink() {
				if err := os.Link(sizedTempDir+"/bar.txt", sizedTempDir+"/link.txt"); err != nil {
					t.Fatalf("Link failed: %v", err)
				}

				names = append(names, "link.txt")
			}

			for _, wantSize := range []int64{int64(len(data)), 0} {
				for _, name := range names {
					path := sizedTempDir + "/" + name

					dir, err := xos.Stat(path)
					if err != nil {
						t.Fatalf("Stat(%q) failed: %v", path, err)
					}

					filesize := size(path, t)
					if dir.Size() != filesize || filesize != wantSize {
						t.Errorf("Size(%q) is %d, len(ReadFile()) is %d, want %d", path, dir.Size(), filesize, wantSize)
					}

					if runtime.GOOS != "wasip1" {
						if err := os.Chmod(path, dir.Mode()); err != nil {
							t.Fatalf("Chmod(%q) failed: %v", path, err)
						}
					}
				}

				if err := xos.Truncate(sizedTempDir+"/bar.txt", 0); err != nil {
					t.Fatalf("Truncate failed: %v", err)
				}
			}
		})
	}
}

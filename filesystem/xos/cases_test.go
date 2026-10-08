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

// Adapted from Go's os tests (src/os/os_test.go):
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package xos_test

// The cases the bounded check does not reach, each through xos's own open: a
// path that names no file (none, a device, a dangling link), the errors os
// maps itself, and a path past Windows's MAX_PATH.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/xos"
)

func TestOpenNoName(t *testing.T) {
	t.Parallel()

	file, err := xos.Open("")
	if err == nil {
		assert.Check(t, file.Close())
		t.Fatal(`Open("") succeeded`)
	}
}

// TestOpenErrors: a missing file, a directory opened for writing and a file
// in the way of a path fail with the errno os gives, in a *PathError.
func TestOpenErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "is-a-file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(dir, "is-a-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		flag int
		want error
	}{
		{"no-such-file", os.O_RDONLY, syscall.ENOENT},
		{"is-a-dir", os.O_WRONLY, syscall.EISDIR},
		{"is-a-file/no-such-file", os.O_WRONLY, syscall.ENOTDIR},
	} {
		path := filepath.Join(dir, test.path)

		file, err := xos.OpenFile(path, test.flag, 0)
		if err == nil {
			assert.Check(t, file.Close())
			t.Fatalf("OpenFile(%q, %#x) succeeded", path, test.flag)
		}

		var pathErr *os.PathError
		if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, test.want) {
			t.Fatalf("OpenFile(%q, %#x) = %v, want *PathError holding %v", path, test.flag, err, test.want)
		}
	}
}

// TestDevNull: the null device opens, as a character device of no size, by
// every name Windows gives it, and for writing with O_CREATE|O_TRUNC.
func TestDevNull(t *testing.T) {
	t.Parallel()

	names := []string{os.DevNull}
	if runtime.GOOS == "windows" {
		names = append(names, "./nul", "//./nul")
	}

	for _, name := range names {
		file, err := xos.Open(name)
		if err != nil {
			t.Fatalf("Open(%s): %v", name, err)
		}

		fromFile, err := file.Stat()
		assert.Check(t, file.Close())

		if err != nil {
			t.Fatalf("Open(%s).Stat(): %v", name, err)
		}

		fromPath, err := xos.Stat(name)
		if err != nil {
			t.Fatalf("Stat(%s): %v", name, err)
		}

		for what, info := range map[string]os.FileInfo{"File.Stat": fromFile, "Stat": fromPath} {
			if info.Size() != 0 || info.Mode()&os.ModeCharDevice == 0 || info.Mode().IsRegular() {
				t.Fatalf(
					"%s(%q): size %d, mode %v; want a character device of no size",
					what,
					name,
					info.Size(),
					info.Mode(),
				)
			}
		}
	}

	file, err := xos.OpenFile(os.DevNull, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile(DevNull, O_WRONLY|O_CREATE|O_TRUNC): %v", err)
	}

	assert.Check(t, file.Close())
}

// TestCreateExclOverDanglingSymlink: O_CREATE|O_EXCL on a link to nothing is
// ErrExist and creates nothing, the link not being followed.
func TestCreateExclOverDanglingSymlink(t *testing.T) {
	t.Parallel()

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink("does_not_exist", link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	file, err := xos.OpenFile(link, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err == nil {
		assert.Check(t, file.Close())
	}

	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("OpenFile of a dangling symlink with O_CREATE|O_EXCL = %v, want ErrExist", err)
	}

	if _, err := xos.Stat(link); err == nil {
		t.Fatal("OpenFile of a dangling symlink with O_CREATE|O_EXCL created a file")
	}
}

// TestLongPath: a path around and past 248 bytes, where Windows needs the
// long-path form, through WriteFile, Stat, ReadFile and Truncate, the file
// itself and a symbolic and a hard link to it where the platform has them.
func TestLongPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	for len(base) < 400 {
		base += "/dir3456789"
	}

	for _, length := range []int{247, 248, 249, 400} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			t.Parallel()

			dir := base[:length-1] + "x"
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}

			data := []byte("hello world\n")

			if err := xos.WriteFile(dir+"/foo.txt", data, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}

			if err := os.Rename(dir+"/foo.txt", dir+"/bar.txt"); err != nil {
				t.Fatal(err)
			}

			names := []string{"bar.txt"}

			if os.Symlink(dir+"/bar.txt", dir+"/symlink.txt") == nil {
				names = append(names, "symlink.txt")
			}

			if os.Link(dir+"/bar.txt", dir+"/link.txt") == nil {
				names = append(names, "link.txt")
			}

			for _, wantSize := range []int64{int64(len(data)), 0} {
				for _, name := range names {
					path := dir + "/" + name

					info, err := xos.Stat(path)
					if err != nil {
						t.Fatalf("Stat(%q): %v", path, err)
					}

					read, err := xos.ReadFile(path)
					if err != nil {
						t.Fatalf("ReadFile(%q): %v", path, err)
					}

					if info.Size() != wantSize || int64(len(read)) != wantSize {
						t.Fatalf("%q: Stat says %d bytes, ReadFile %d, want %d", path, info.Size(), len(read), wantSize)
					}
				}

				if err := xos.Truncate(dir+"/bar.txt", 0); err != nil {
					t.Fatalf("Truncate: %v", err)
				}
			}
		})
	}
}

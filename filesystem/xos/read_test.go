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

// Adapted from Go stdlib src/os/read_test.go for xos package testing.

package xos_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/filesystem/xos"
)

func checkNamedSize(t *testing.T, path string, size int64) {
	t.Helper()

	dir, err := xos.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q (looking for size %d): %s", path, size, err)
	}

	if dir.Size() != size {
		t.Errorf("Stat %q: size %d want %d", path, dir.Size(), size)
	}
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	filename := "rumpelstilzchen"

	_, err := xos.ReadFile(filename)
	if err == nil {
		t.Fatalf("ReadFile %s: error expected, none found", filename)
	}

	filename = "read_test.go"

	contents, err := xos.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", filename, err)
	}

	checkNamedSize(t, filename, int64(len(contents)))
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	f, err := xos.CreateTemp("", "xos-test")
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()
	defer os.Remove(f.Name())

	msg := "Programming today is a race between software engineers striving to " +
		"build bigger and better idiot-proof programs, and the Universe trying " +
		"to produce bigger and better idiots. So far, the Universe is winning."

	if err := xos.WriteFile(f.Name(), []byte(msg), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", f.Name(), err)
	}

	data, err := xos.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile %s: %v", f.Name(), err)
	}

	if string(data) != msg {
		t.Fatalf("ReadFile: wrong data:\nhave %q\nwant %q", string(data), msg)
	}
}

func TestReadOnlyWriteFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Root can write to read-only files anyway, so skip the read-only test.")
	}

	if runtime.GOOS == "wasip1" {
		t.Skip("no support for file permissions on " + runtime.GOOS)
	}

	t.Parallel()

	// We don't want to use CreateTemp directly, since that opens a file for us as 0600.
	filename := filepath.Join(t.TempDir(), "blurp.txt")

	shmorp := []byte("shmorp")
	florp := []byte("florp")

	err := xos.WriteFile(filename, shmorp, 0o444)
	if err != nil {
		t.Fatalf("WriteFile %s: %v", filename, err)
	}

	err = xos.WriteFile(filename, florp, 0o444)
	if err == nil {
		t.Fatalf("Expected an error when writing to read-only file %s", filename)
	}

	got, err := xos.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", filename, err)
	}

	if !bytes.Equal(got, shmorp) {
		t.Fatalf("want %s, got %s", shmorp, got)
	}
}

func TestReadDir(t *testing.T) {
	t.Parallel()

	dirname := "rumpelstilzchen"
	if _, err := xos.ReadDir(dirname); err == nil {
		t.Fatalf("ReadDir %s: error expected, none found", dirname)
	}

	// ReadDir on a regular file should return ENOTDIR.
	filename := filepath.Join(t.TempDir(), "foo")

	f, err := xos.Create(filename)
	if err != nil {
		t.Fatal(err)
	}

	f.Close()

	if list, err := xos.ReadDir(filename); list != nil || !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("ReadDir %s: (nil, ENOTDIR) expected, got (%v, %v)", filename, list, err)
	}

	// Populate a temp directory and verify ReadDir finds the entries.
	dir := t.TempDir()

	testFile := filepath.Join(dir, "testfile.txt")

	tf, err := xos.Create(testFile)
	if err != nil {
		t.Fatal(err)
	}

	tf.Close()

	subDir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := xos.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}

	foundFile := false
	foundSubDir := false

	for _, entry := range list {
		switch {
		case !entry.IsDir() && entry.Name() == "testfile.txt":
			foundFile = true
		case entry.IsDir() && entry.Name() == "subdir":
			foundSubDir = true
		default:
		}
	}

	if !foundFile {
		t.Fatalf("ReadDir %s: testfile.txt not found", dir)
	}

	if !foundSubDir {
		t.Fatalf("ReadDir %s: subdir directory not found", dir)
	}
}

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

package compress_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/compress"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

func TestTar(t *testing.T) {
	t.Parallel()

	// Create source directory structure.
	baseDir := t.TempDir()
	relDir := "mydir"
	srcDir := filepath.Join(baseDir, relDir)

	if err := os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := filesystem.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := filesystem.WriteFile(filepath.Join(srcDir, "sub", "world.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create tar and read all output.
	tarData, err := io.ReadAll(compress.Tar(baseDir, relDir))
	if err != nil {
		t.Fatalf("Tar: %v", err)
	}

	// Read tar and verify entries.
	tarReader := tar.NewReader(bytes.NewReader(tarData))
	entries := make(map[string]string)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}

		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tarReader)
			if err != nil {
				t.Fatalf("read %s: %v", header.Name, err)
			}

			entries[header.Name] = string(data)
		}
	}

	if got, ok := entries["mydir/hello.txt"]; !ok || got != "hello" {
		t.Errorf("hello.txt: got %q, want %q", got, "hello")
	}

	if got, ok := entries["mydir/sub/world.txt"]; !ok || got != "world" {
		t.Errorf("sub/world.txt: got %q, want %q", got, "world")
	}
}

func TestUntar(t *testing.T) {
	t.Parallel()

	// Build a tar archive in memory.
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	writeEntry := func(name, content string) {
		t.Helper()

		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}

		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}

	writeEntry("file1.txt", "alpha")
	writeEntry("subdir/file2.txt", "beta")

	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}

	// Extract.
	destDir := t.TempDir()
	if err := compress.Untar(&buf, destDir); err != nil {
		t.Fatalf("Untar: %v", err)
	}

	// Verify.
	got1, err := xos.ReadFile(filepath.Join(destDir, "file1.txt"))
	if err != nil {
		t.Fatalf("read file1.txt: %v", err)
	}

	if string(got1) != "alpha" {
		t.Errorf("file1.txt: got %q, want %q", got1, "alpha")
	}

	got2, err := xos.ReadFile(filepath.Join(destDir, "subdir", "file2.txt"))
	if err != nil {
		t.Fatalf("read subdir/file2.txt: %v", err)
	}

	if string(got2) != "beta" {
		t.Errorf("subdir/file2.txt: got %q, want %q", got2, "beta")
	}
}

func TestUntar_PathTraversal(t *testing.T) {
	t.Parallel()

	// Build a tar with a path traversal entry.
	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)

	header := &tar.Header{
		Name: "../escape.txt",
		Mode: 0o644,
		Size: 4,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatal(err)
	}

	tarWriter.Write([]byte("evil"))
	tarWriter.Close()

	destDir := t.TempDir()
	err := compress.Untar(&buf, destDir)

	if !errors.Is(err, compress.ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestTar_Roundtrip(t *testing.T) {
	t.Parallel()

	// Create source.
	baseDir := t.TempDir()
	relDir := "data"
	srcDir := filepath.Join(baseDir, relDir)

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := filesystem.WriteFile(filepath.Join(srcDir, "test.bin"), []byte("roundtrip-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tar → Untar roundtrip.
	destDir := t.TempDir()
	if err := compress.Untar(compress.Tar(baseDir, relDir), destDir); err != nil {
		t.Fatalf("Tar→Untar: %v", err)
	}

	// Verify.
	got, err := xos.ReadFile(filepath.Join(destDir, "data", "test.bin"))
	if err != nil {
		t.Fatalf("read test.bin: %v", err)
	}

	if string(got) != "roundtrip-content" {
		t.Errorf("got %q, want %q", got, "roundtrip-content")
	}
}

func TestUntar_EmptyArchive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	tarWriter := tar.NewWriter(&buf)
	tarWriter.Close()

	destDir := t.TempDir()

	err := compress.Untar(&buf, destDir)
	if err != nil {
		t.Fatalf("Untar empty archive: %v", err)
	}
}

func TestTar_NonexistentSource(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	rc := compress.Tar(baseDir, "does-not-exist")
	defer rc.Close()

	_, err := io.ReadAll(rc)
	if err == nil {
		t.Fatal("expected error for nonexistent source directory")
	}
}

func TestTar_EmptyDirectory(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	relDir := "empty"

	if err := os.MkdirAll(filepath.Join(baseDir, relDir), 0o755); err != nil {
		t.Fatal(err)
	}

	tarData, err := io.ReadAll(compress.Tar(baseDir, relDir))
	if err != nil {
		t.Fatalf("Tar: %v", err)
	}

	// Should produce a valid archive with just the directory entry.
	tarReader := tar.NewReader(bytes.NewReader(tarData))

	var count int

	for {
		_, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}

		count++
	}

	if count == 0 {
		t.Error("expected at least the directory entry in the archive")
	}
}

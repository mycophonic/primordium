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

package compress

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Tar returns a reader that yields a tar archive of baseDir/relDir.
// Archive entries are rooted at relDir, preserving the directory structure
// relative to baseDir. The caller must close the returned ReadCloser.
func Tar(baseDir, relDir string) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		_ = pipeWriter.CloseWithError(writeTar(pipeWriter, baseDir, relDir))
	}()

	return pipeReader
}

func writeTar(writer io.Writer, baseDir, relDir string) error {
	srcDir := filepath.Join(baseDir, relDir)

	tarWriter := tar.NewWriter(writer)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Build relative path from baseDir so the archive preserves the directory structure.
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		// Use forward slashes in tar entries.
		relPath = strings.ReplaceAll(relPath, string(os.PathSeparator), "/")

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("tar header: %w", err)
		}

		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write header: %w", err)
		}

		if info.IsDir() {
			return nil
		}

		file, err := xos.Open(path)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}

		defer file.Close()

		if _, err := io.Copy(tarWriter, file); err != nil {
			return fmt.Errorf("copy file: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk: %w", err)
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}

	return nil
}

// Untar extracts a tar archive from reader into destDir.
// Only regular files and directories are extracted; other entry types
// (symlinks, etc.) are skipped. Path traversal attempts are rejected.
func Untar(reader io.Reader, destDir string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		target := filepath.Join(destDir, filepath.Clean(header.Name))

		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("%w: %s", ErrPathTraversal, header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, filesystem.DirPermissionsDefault); err != nil {
				return fmt.Errorf("mkdir %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), filesystem.DirPermissionsDefault); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", header.Name, err)
			}

			if err := extractFile(target, tarReader); err != nil {
				return fmt.Errorf("write %s: %w", header.Name, err)
			}
		default:
			// Skip unsupported entry types (symlinks, etc.).
		}
	}

	return nil
}

func extractFile(path string, reader io.Reader) error {
	out, err := xos.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filesystem.FilePermissionsDefault)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}

	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

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

	fcompress "github.com/forkcloser/compress"
	"github.com/forkcloser/xos"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
)

// Tar returns a reader that yields a tar archive of baseDir/relDir. Archive
// entries are rooted at relDir, preserving the directory structure relative
// to baseDir. Regular files and directories are archived, as Untar extracts;
// a symlink, a device or a pipe is left out, except that relDir itself may be
// a symlink to the directory to archive. A tree that cannot be read is
// ErrFilesystemFailure on the read, an entry tar cannot describe
// ErrInvalidArgument, a consumer that stopped reading ErrWriteFailure. The
// caller must close the returned ReadCloser.
//
// Deprecated: the path-taking archiving goes with the release after this
// one. The module's archiving is forkcloser/compress's Tar over an fs.FS.
func Tar(baseDir, relDir string) io.ReadCloser {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		_ = pipeWriter.CloseWithError(writeTar(pipeWriter, baseDir, relDir))
	}()

	return pipeReader
}

func writeTar(writer io.Writer, baseDir, relDir string) error {
	// The root is resolved: Walk lstats it, and a root that is a symlink would
	// be one entry the filter below leaves out, an empty archive in silence.
	root, err := filepath.EvalSymlinks(filepath.Join(baseDir, relDir))
	if err != nil {
		return fmt.Errorf("%w: resolve: %w", fault.ErrFilesystemFailure, err)
	}

	tarWriter := tar.NewWriter(writer)

	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: walk: %w", fault.ErrFilesystemFailure, walkErr)
		}

		return writeTarEntry(tarWriter, root, relDir, path, info)
	})
	if err != nil {
		return err //nolint:wrapcheck // the callback's own errors, classified where they arise
	}

	if err = tarWriter.Close(); err != nil {
		return fmt.Errorf("%w: close tar: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

// writeTarEntry writes one walked path: its header, named relDir plus the
// path under root, and for a regular file its content. Anything else is left
// out: FileInfoHeader would describe a symlink as one with no target and no
// size, and Open would then copy the target's content into that entry.
func writeTarEntry(tarWriter *tar.Writer, root, relDir, path string, info os.FileInfo) error {
	if !info.IsDir() && !info.Mode().IsRegular() {
		return nil
	}

	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("%w: rel path: %w", fault.ErrInvalidArgument, err)
	}

	// Tar entries use forward slashes.
	relPath = filepath.ToSlash(filepath.Join(relDir, relPath))

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("%w: tar header: %w", fault.ErrInvalidArgument, err)
	}

	header.Name = relPath

	if err = tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("%w: write header: %w", fault.ErrWriteFailure, err)
	}

	if info.IsDir() {
		return nil
	}

	file, err := xos.Open(path)
	if err != nil {
		return fmt.Errorf("%w: open file: %w", fault.ErrFilesystemFailure, err)
	}

	defer func() { _ = file.Close() }() // read-only: a close error carries nothing

	if _, err = io.Copy(tarWriter, file); err != nil {
		return fmt.Errorf("%w: copy file: %w", fault.ErrWriteFailure, err)
	}

	return nil
}

// Untar extracts a tar archive from reader into destDir, created if need be:
// its directories and regular files, with the module's permissions; every
// other kind of entry, symlinks included, is skipped. Every create is confined
// to destDir at the OS level, so an entry named to land outside it, by "..",
// an absolute name or a symlink in the tree, is refused, as ErrWriteFailure
// like any failure to create. An archive that does not parse, or a reader
// that fails, is ErrReadFailure; a destDir that cannot be opened is
// ErrFilesystemFailure.
//
// Deprecated: the path-taking archiving goes with the release after this
// one. The module's archiving is forkcloser/compress's Untar into an *os.Root.
func Untar(reader io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, filesystem.DirPermissionsDefault); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	root, err := os.OpenRoot(destDir)
	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	defer func() { _ = root.Close() }() // a directory handle: its close error carries nothing

	return classify(fcompress.Untar(reader, root))
}

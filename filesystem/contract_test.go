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

// The contract, from WriteFile's docs (an atomic drop-in for os.WriteFile,
// through a temporary file and a rename) and os.Rename's ("if newpath already
// exists and is not a directory, Rename replaces it"):
//   - a WriteFile ends one of two ways: it succeeds, and the path holds
//     exactly the data, a regular file created with the mode asked for, less
//     the process's umask, as os.WriteFile creates one (Windows keeps only
//     whether the owner may write); or it fails with fault.ErrWriteFailure,
//     and the path is exactly as it was;
//   - it succeeds where its directory exists and the path is missing or a
//     file the rename may replace, held open or not: on Windows, xos.Rename
//     replaces a file whose holders opened it with FILE_SHARE_DELETE, as xos
//     opens, on a volume with the POSIX-semantics rename (NTFS; the property
//     is the volume's, and a FAT volume refuses as os does), and refuses one
//     a holder opened without, as os.Open does; it
//     fails where the directory is missing or the path is a directory; over
//     a read-only file the platform decides (a rename on Unix answers to the
//     directory, on Windows to the file), within the first rule;
//   - either way, it leaves no temporary file behind;
//   - a reader sees the old content or the new, never a mix.

import (
	"bytes"
	"os"
)

// snapshot is what a path holds, as the caller can see it.
type snapshot struct {
	kind string // "missing", "dir" or "file"
	data []byte
	mode os.FileMode
}

func take(path string) snapshot {
	info, err := os.Lstat(path)

	switch {
	case err != nil:
		return snapshot{kind: "missing"}
	case info.IsDir():
		return snapshot{kind: "dir", mode: info.Mode().Perm()}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		data = nil
	}

	return snapshot{kind: "file", data: data, mode: info.Mode().Perm()}
}

func (s snapshot) equal(other snapshot) bool {
	return s.kind == other.kind && bytes.Equal(s.data, other.data) && s.mode == other.mode
}

// entries lists what dir holds.
func entries(dir string) map[string]bool {
	listed, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	names := map[string]bool{}
	for _, entry := range listed {
		names[entry.Name()] = true
	}

	return names
}

// strays are what dir holds now that it did not before, the target aside:
// what a write left behind, whatever its name.
func strays(dir, target string, before map[string]bool) []string {
	var found []string

	for name := range entries(dir) {
		if !before[name] && name != target {
			found = append(found, name)
		}
	}

	return found
}

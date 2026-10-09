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

package mmap_test

// The contract, from mmap's docs and mmap(2)'s (MAP_SHARED: "updates to the
// mapping are visible to other processes mapping the same region, and are
// carried through to the underlying file"):
//   - Map maps the first size bytes of a file holding at least that many;
//     a size below 1 or past the file's end is ErrInvalidArgument, and a
//     file that cannot be mapped is ErrSystemFailure;
//   - Bytes is size bytes long and holds the file's bytes; a write through
//     it is seen by every other mapping of the file and, once Sync returns,
//     by a read of the file; a write to the file is seen through Bytes;
//   - Unmap releases the mapping, after which Bytes is nil and Sync or a
//     second Unmap is ErrInvalidArgument; what was written reaches the file
//     with or without a Sync.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/filesystem/xos"
)

// fileOf creates a file of size bytes, zero-filled, open read-write.
func fileOf(t *testing.T, size int) *os.File {
	t.Helper()

	file, err := xos.OpenFile(filepath.Join(t.TempDir(), "mapped"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}

	if err = file.Truncate(int64(size)); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = file.Close() })

	return file
}

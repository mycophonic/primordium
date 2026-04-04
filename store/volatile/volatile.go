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

package volatile

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/store/refcount"
)

// Volatile provides content-addressed ephemeral storage with file-based reference counting.
// Safe for concurrent access across multiple processes.
// Resistant to process crashes - OS automatically releases file locks when process dies.
type Volatile struct {
	rc        *refcount.Locker
	algorithm digest.Algorithm
}

// New creates a new volatile store at the given root directory.
// See types.Algorithm for supported hashing algorithms.
func New(root string, algorithm digest.Algorithm) *Volatile {
	return &Volatile{
		rc:        refcount.New(root),
		algorithm: algorithm,
	}
}

// Acquire ensures content exists at a path and returns that path with a release function.
// Multiple concurrent readers (even across processes) can acquire the same content.
// The file is guaranteed to exist until release is called.
// Release is soft - if other readers exist, the file is not deleted.
// Crash-resistant: OS releases file locks when process dies, enabling cleanup.
func (v *Volatile) Acquire(content []byte) (string, func(), error) {
	h := v.algorithm.Hash()
	h.Write(content)
	contentHash := hex.EncodeToString(h.Sum(nil))

	//nolint:wrapcheck
	return v.rc.Acquire(contentHash, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, volatileDataFile)

		// Write data if it doesn't exist (atomic write)
		if _, err := xos.Stat(dataPath); err != nil {
			if !os.IsNotExist(err) {
				return "", nil, fmt.Errorf("%w: stat data file: %w", fault.ErrFilesystemFailure, err)
			}

			if err := filesystem.WriteFile(dataPath, content, filesystem.FilePermissionsPrivate); err != nil {
				return "", nil, fmt.Errorf("%w: data file: %w", fault.ErrWriteFailure, err)
			}
		}

		// No cleanup needed - Locker handles directory removal
		return dataPath, nil, nil
	})
}

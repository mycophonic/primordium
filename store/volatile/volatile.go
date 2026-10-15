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

	"github.com/forkcloser/xos"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/store/refcount"
)

// Volatile is a content-addressed store whose files live while any process
// holds them. A holder that dies releases, the OS drops its lock.
type Volatile struct {
	rc        *refcount.Locker
	algorithm digest.Algorithm
}

// New is a Volatile at root, addressing content by algorithm. It panics when
// root is not a valid path.
func New(root string, algorithm digest.Algorithm) *Volatile {
	return &Volatile{
		rc:        refcount.New(root),
		algorithm: algorithm,
	}
}

// Acquire holds content and returns the path of a file holding it, with
// release, which must be called when done and is idempotent. Holders of the
// same content, in any process, get the same path; the file stays until the
// last of them releases.
func (v *Volatile) Acquire(content []byte) (string, func(), error) {
	h := v.algorithm.Hash()
	h.Write(content)
	contentHash := hex.EncodeToString(h.Sum(nil))

	//nolint:wrapcheck // this module's errors are classified at their source
	return v.rc.Acquire(contentHash, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, volatileDataFile)

		// A data file that exists is whole: WriteFile renames it into place,
		// so a writer that died left a temporary beside it, never a part.
		if _, err := xos.Stat(dataPath); err != nil {
			if !os.IsNotExist(err) {
				return "", nil, fmt.Errorf("%w: stat data file: %w", fault.ErrFilesystemFailure, err)
			}

			if err := filesystem.WriteFile(dataPath, content, filesystem.FilePermissionsPrivate); err != nil {
				return "", nil, fmt.Errorf("data file: %w", err)
			}
		}

		return dataPath, nil, nil
	})
}

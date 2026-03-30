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

package filesystem

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/umask"
	"github.com/mycophonic/primordium/filesystem/xos"
)

// Adapted from: https://github.com/containerd/continuity/blob/main/ioutils.go under Apache License

/*
   Copyright The containerd Authors.

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

// WriteFile atomically writes data to a file by first writing to a temp file and calling rename.
// Generally speaking, this should almost always be used as a dropin for os.WriteFile.
// The only exception is when inodes matter.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	perm = (^os.FileMode(umask.Get())) & perm

	tmpFile, err := xos.CreateTemp(filepath.Dir(filename), ".tmp-"+filepath.Base(filename))
	if err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	defer func() {
		// Clean up temp file on any failure. Remove before Close so that
		// the name is still valid; ignore errors — best effort cleanup.
		if err != nil {
			_ = os.Remove(tmpFile.Name())
			_ = tmpFile.Close()
		}
	}()

	if err = os.Chmod(tmpFile.Name(), perm); err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	if _, err = tmpFile.Write(data); err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	if err = tmpFile.Sync(); err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	if err = tmpFile.Close(); err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	if err = os.Rename(tmpFile.Name(), filename); err != nil {
		return errors.Join(fault.ErrWriteFailure, err)
	}

	return nil
}

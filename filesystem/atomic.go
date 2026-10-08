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
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mycophonic/primordium/fault"
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
// The file is created with perm before umask, as os.WriteFile creates one.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	tmpFile, err := createTemp(filepath.Dir(filename), ".tmp-"+filepath.Base(filename), perm)
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

// tempAttempts bounds the retries on a name collision, as os.CreateTemp does.
const tempAttempts = 10000

// createTemp is os.CreateTemp with the mode the caller chose: created with
// perm, before umask, the temporary file lands with the mode os.WriteFile
// would have given, which a Chmod after the fact would not.
//
//nolint:wrapcheck // WriteFile, its only caller, wraps what it returns
func createTemp(dir, prefix string, perm os.FileMode) (*os.File, error) {
	for try := 0; ; try++ {
		// The suffix is a name, not a secret: O_EXCL is what makes it unique.
		name := filepath.Join(dir, prefix+strconv.FormatUint(uint64(rand.Uint32()), 10)) // #nosec G404 -- see above

		file, err := xos.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, fs.ErrExist) || try >= tempAttempts {
			return file, err
		}
	}
}

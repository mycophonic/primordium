//go:build !windows

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
	"syscall"

	"github.com/forkcloser/xos"
)

// syncDir flushes dir, which is what makes a rename inside it outlast a crash.
//
//nolint:wrapcheck // WriteFile, its only caller, wraps what it returns
func syncDir(dir string) error {
	handle, err := xos.Open(dir)
	if err != nil {
		// A directory that may be written and not read refuses the open, and
		// there is no flushing it: the rename it just took is not a failure.
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}

		return err
	}

	err = handle.Sync()
	// A filesystem with no flush for a directory answers EINVAL or
	// unsupported: there is nothing to flush it with, and nothing failed.
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, errors.ErrUnsupported) {
		err = nil
	}

	return errors.Join(err, handle.Close())
}

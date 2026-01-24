/*
   Copyright Farcloser.

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
)

// DirectorySize calculates the total size of all files in a directory and its subdirectories.
func DirectorySize(path string) (int64, error) {
	var size int64

	iterator := func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			size += info.Size()
		}

		return err
	}

	err := filepath.Walk(path, iterator)
	if err != nil {
		return 0, errors.Join(ErrGenericFailure, err)
	}

	return size, nil
}

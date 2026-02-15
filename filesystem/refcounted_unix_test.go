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

package filesystem_test

import (
	"testing"

	"github.com/mycophonic/primordium/filesystem"
)

func TestNewLocker_PanicsOnInvalidPath(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid path")
		}
	}()

	// Path with traversal should panic
	filesystem.NewLocker("/foo/../bar")
}

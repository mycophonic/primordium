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

package refcount_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/store/refcount"
)

// TestKeyLength: a key is a path component, so 255 characters is the longest
// one, held and released like any other, and 256 is refused.
func TestKeyLength(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)
	longest := strings.Repeat("k", 255)

	path, release, err := locker.Acquire(longest, writing)
	if err != nil {
		t.Fatal(err)
	}

	if path != filepath.Join(root, longest, dataFile) {
		t.Fatalf("Acquire = %q, want the entry under the key", path)
	}

	release()
	gone(t, filepath.Join(root, longest))

	if _, _, err = locker.Acquire(longest+"k", writing); !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("Acquire of a 256-character key = %v, want ErrInvalidArgument", err)
	}
}

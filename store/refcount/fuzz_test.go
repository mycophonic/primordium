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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/store/refcount"
)

// FuzzAcquireKey: whatever the key, Acquire either refuses it as
// ErrInvalidArgument and creates nothing, or holds the entry named by it
// under the root and removes it on release; it never escapes the root.
func FuzzAcquireKey(f *testing.F) {
	for _, seed := range []string{
		"key", "", " ", ".", "..", "a/b", `a\b`, "x.", "CON", "ünï", "\xff",
		strings.Repeat("k", 255), strings.Repeat("k", 256),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, key string) {
		root := filepath.Join(t.TempDir(), "root")
		locker := refcount.New(root)

		path, release, err := locker.Acquire(key, writing)
		if err != nil {
			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Fatalf("Acquire(%q) = %v, want ErrInvalidArgument or nothing", key, err)
			}

			if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("a refused key created the root: %v", err)
			}

			return
		}

		entry := filepath.Join(root, key)
		if path != filepath.Join(entry, dataFile) {
			t.Fatalf("Acquire(%q) = %q, want the entry under the key", key, path)
		}

		release()

		if _, err := os.Stat(entry); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("the entry for %q is still there after its release: %v", key, err)
		}
	})
}

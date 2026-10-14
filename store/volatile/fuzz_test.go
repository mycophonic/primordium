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

package volatile_test

import (
	"bytes"
	"testing"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/store/volatile"
)

// FuzzAcquire: whatever the content, the file holds it byte for byte, a
// second holder gets the same path, and the root is empty once both release.
func FuzzAcquire(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("content"))
	f.Add([]byte{0})
	f.Add(bytes.Repeat([]byte{0xff}, 4096))

	f.Fuzz(func(t *testing.T, content []byte) {
		root := t.TempDir()
		store := volatile.New(root, digest.SHA256)

		path, release, err := store.Acquire(content)
		if err != nil {
			t.Fatal(err)
		}

		again, releaseAgain, err := store.Acquire(content)
		if err != nil {
			t.Fatal(err)
		}

		if again != path {
			t.Fatalf("a second holder got %q, the first %q", again, path)
		}

		holds(t, path, content)
		release()
		holds(t, path, content)
		releaseAgain()
		empty(t, root)
	})
}

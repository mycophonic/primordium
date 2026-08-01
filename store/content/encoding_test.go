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

//nolint:testpackage
package content

import (
	"testing"

	"github.com/mycophonic/primordium/digest"
)

// TestAlgorithmIDStability pins the on-disk algorithm IDs. These values are
// persisted in index records — changing them silently corrupts existing indexes.
// If this test fails, an ID was changed or reassigned. Do not update the
// expected values without a migration strategy.
func TestAlgorithmIDStability(t *testing.T) {
	t.Parallel()

	pinned := []struct {
		alg  digest.Algorithm
		want uint8
	}{
		{digest.SHA1, 1},
		{digest.SHA256, 2},
		{digest.SHA384, 3},
		{digest.SHA512, 4},
		{digest.BLAKE2b256, 5},
		{digest.BLAKE2b512, 6},
		{digest.BLAKE3256, 7},
	}

	for _, tt := range pinned {
		if got := algorithmToID[tt.alg]; got != tt.want {
			t.Errorf("algorithmToID[%s] = %d, want %d (on-disk format changed!)", tt.alg, got, tt.want)
		}
	}

	for _, tt := range pinned {
		if got := algorithmFromID[tt.want]; got != tt.alg {
			t.Errorf("algorithmFromID[%d] = %s, want %s (on-disk format changed!)", tt.want, got, tt.alg)
		}
	}
}

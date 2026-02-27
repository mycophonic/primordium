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

package digest

import (
	"encoding/hex"
)

const (
	shorten = 16
)

// Hashpath returns a hash from a filepath with low-collision risk.
// 16 hex characters = 64 bits of entropy.
// Birthday paradox: ~2^32 (~4 billion) items needed for 50% collision probability.
// For 100,000 files:
// P(collision) ≈ n²/(2d) = (10^5)² / (2 × 2^64) ≈ 2.7 × 10^-10.
func Hashpath(filePath string) string {
	h := BLAKE2b256.Hash()
	_, _ = h.Write([]byte(filePath))

	return hex.EncodeToString(h.Sum(nil))[:shorten]
}

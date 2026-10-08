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

// HashPath is a 16-hex-digit BLAKE2b hash of a path, for naming a file after
// it. Its 64 bits keep the odds of a collision among 100,000 paths near 3e-10.
func HashPath(filePath string) string {
	h := BLAKE2b256.Hash()
	_, _ = h.Write([]byte(filePath))

	return hex.EncodeToString(h.Sum(nil))[:shorten]
}

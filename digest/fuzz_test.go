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

package digest_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
)

// FuzzFromString: whatever the string, FromString either refuses it with
// ErrInvalidArgument or accepts it, and an accepted digest is its own String,
// is lowercase hex of its algorithm's size, and is what New makes of its bytes.
func FuzzFromString(f *testing.F) {
	for _, seed := range []string{
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"md5:d41d8cd98f00b204e9800998ecf8427e",
		"blake3-256:af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		"sha256:", "sha256", ":", "", "sha256:E3B0", "sha256:ab:cd", "md4:d41d8cd98f00b204e9800998ecf8427e",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		parsed, err := digest.FromString(input)
		if err != nil {
			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Fatalf("FromString(%q): %v is not ErrInvalidArgument", input, err)
			}

			return
		}

		if parsed.String() != input {
			t.Fatalf("FromString(%q).String() = %q", input, parsed.String())
		}

		raw, err := hex.DecodeString(parsed.Encoded())
		if err != nil || hex.EncodeToString(raw) != parsed.Encoded() {
			t.Fatalf("FromString(%q) accepted an encoding that is not lowercase hex: %q", input, parsed.Encoded())
		}

		if len(raw) != parsed.Algorithm().Hash().Size() {
			t.Fatalf("FromString(%q) accepted %d bytes for %s", input, len(raw), parsed.Algorithm())
		}

		made, err := digest.New(parsed.Algorithm(), raw)
		if err != nil || made.String() != parsed.String() {
			t.Fatalf("New(%s, bytes of %q) = %v, %v", parsed.Algorithm(), input, made, err)
		}
	})
}

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
	"regexp"
	"testing"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
)

func TestFromString_ValidDigests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantAlg digest.Algorithm
		wantEnc string
	}{
		{
			name:    "md5",
			input:   "md5:d41d8cd98f00b204e9800998ecf8427e",
			wantAlg: digest.MD5,
			wantEnc: "d41d8cd98f00b204e9800998ecf8427e",
		},
		{
			name:    "sha256",
			input:   "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantAlg: digest.SHA256,
			wantEnc: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:    "sha1",
			input:   "sha1:da39a3ee5e6b4b0d3255bfef95601890afd80709",
			wantAlg: digest.SHA1,
			wantEnc: "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			name:    "sha384",
			input:   "sha384:38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
			wantAlg: digest.SHA384,
			wantEnc: "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
		},
		{
			name:    "sha512",
			input:   "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
			wantAlg: digest.SHA512,
			wantEnc: "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
		},
		{
			name:    "blake2b-256",
			input:   "blake2b-256:0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8",
			wantAlg: digest.BLAKE2b256,
			wantEnc: "0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8",
		},
		{
			name:    "blake2b-512",
			input:   "blake2b-512:786a02f742015903c6c6fd852552d272912f4740e15847618a86e217f71f5419d25e1031afee585313896444934eb04b903a685b1448b755d56f701afe9be2ce",
			wantAlg: digest.BLAKE2b512,
			wantEnc: "786a02f742015903c6c6fd852552d272912f4740e15847618a86e217f71f5419d25e1031afee585313896444934eb04b903a685b1448b755d56f701afe9be2ce",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := digest.FromString(tt.input)
			if err != nil {
				t.Fatalf("FromString(%q) returned error: %v", tt.input, err)
			}

			if d.Algorithm() != tt.wantAlg {
				t.Errorf("Algorithm() = %q, want %q", d.Algorithm(), tt.wantAlg)
			}

			if d.Encoded() != tt.wantEnc {
				t.Errorf("Encoded() = %q, want %q", d.Encoded(), tt.wantEnc)
			}

			// Verify round-trip: algorithm + ":" + encoded == original input
			reconstructed := string(d.Algorithm()) + ":" + d.Encoded()
			if reconstructed != tt.input {
				t.Errorf("reconstructed = %q, want %q", reconstructed, tt.input)
			}
		})
	}
}

func TestFromString_NoColon(t *testing.T) {
	t.Parallel()

	_, err := digest.FromString("sha256nocolon")
	if err == nil {
		t.Fatal("expected error for digest without colon, got nil")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got: %v", err)
	}
}

func TestFromString_UnknownAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := digest.FromString("md4:d41d8cd98f00b204e9800998ecf8427e")
	if err == nil {
		t.Fatal("expected error for unknown algorithm, got nil")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got: %v", err)
	}
}

func TestFromString_InvalidEncoded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "sha256 too short",
			input: "sha256:e3b0c44298fc1c149afbf4c8996fb924",
		},
		{
			name:  "sha256 too long",
			input: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855aa",
		},
		{
			name:  "sha256 uppercase",
			input: "sha256:E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
		},
		{
			name:  "sha256 invalid chars",
			input: "sha256:g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "sha1 too short",
			input: "sha1:da39a3ee5e6b4b0d3255bfef",
		},
		{
			name:  "sha384 wrong length",
			input: "sha384:38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da",
		},
		{
			name:  "sha512 wrong length",
			input: "sha512:cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce",
		},
		{
			name:  "empty encoded",
			input: "sha256:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := digest.FromString(tt.input)
			if err == nil {
				t.Fatalf("expected error for invalid encoded %q, got nil", tt.input)
			}

			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Errorf("expected ErrInvalidArgument, got: %v", err)
			}
		})
	}
}

func TestAlgorithm_Hash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		alg      digest.Algorithm
		wantSize int
	}{
		{digest.MD5, 16},
		{digest.SHA1, 20},
		{digest.SHA256, 32},
		{digest.SHA384, 48},
		{digest.SHA512, 64},
		{digest.BLAKE2b256, 32},
		{digest.BLAKE2b512, 64},
	}

	for _, tt := range tests {
		t.Run(string(tt.alg), func(t *testing.T) {
			t.Parallel()

			h := tt.alg.Hash()
			if h.Size() != tt.wantSize {
				t.Errorf("Hash().Size() = %d, want %d", h.Size(), tt.wantSize)
			}

			// Verify it's functional
			h.Write([]byte("test"))
			sum := h.Sum(nil)

			if len(sum) != tt.wantSize {
				t.Errorf("Sum length = %d, want %d", len(sum), tt.wantSize)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		alg     digest.Algorithm
		rawSize int
	}{
		{digest.MD5, 16},
		{digest.SHA1, 20},
		{digest.SHA256, 32},
		{digest.SHA384, 48},
		{digest.SHA512, 64},
		{digest.BLAKE2b256, 32},
		{digest.BLAKE2b512, 64},
	}

	for _, tt := range tests {
		t.Run(string(tt.alg), func(t *testing.T) {
			t.Parallel()

			// Hash some data to get valid raw bytes.
			h := tt.alg.Hash()
			h.Write([]byte("test data"))
			raw := h.Sum(nil)

			d, err := digest.New(tt.alg, raw)
			if err != nil {
				t.Fatalf("New(%s, ...) returned error: %v", tt.alg, err)
			}

			if d.Algorithm() != tt.alg {
				t.Errorf("Algorithm() = %q, want %q", d.Algorithm(), tt.alg)
			}

			if d.Encoded() != hex.EncodeToString(raw) {
				t.Errorf("Encoded() = %q, want %q", d.Encoded(), hex.EncodeToString(raw))
			}
		})
	}
}

func TestNew_UnknownAlgorithm(t *testing.T) {
	t.Parallel()

	_, err := digest.New("md4", make([]byte, 16))
	if err == nil {
		t.Fatal("expected error for unknown algorithm, got nil")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got: %v", err)
	}
}

func TestNew_WrongSize(t *testing.T) {
	t.Parallel()

	_, err := digest.New(digest.SHA256, make([]byte, 20))
	if err == nil {
		t.Fatal("expected error for wrong byte length, got nil")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got: %v", err)
	}
}

func TestNew_EncodedRoundTrip(t *testing.T) {
	t.Parallel()

	// Hash data → raw bytes → New → Encoded → must match hex of original.
	h := digest.SHA256.Hash()
	h.Write([]byte("round trip test"))
	raw := h.Sum(nil)

	d, err := digest.New(digest.SHA256, raw)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if d.Encoded() != hex.EncodeToString(raw) {
		t.Errorf("Encoded() = %s, want %s", d.Encoded(), hex.EncodeToString(raw))
	}
}

func TestFromString_NewRoundTrip(t *testing.T) {
	t.Parallel()

	input := "blake2b-256:0e5751c026e543b2e8ab2eb06099daa1d1e5df47778f7787faab45cdf12fe3a8"

	d1, err := digest.FromString(input)
	if err != nil {
		t.Fatalf("FromString returned error: %v", err)
	}

	// FromString → Encoded → decode → New → String must equal original.
	raw, _ := hex.DecodeString(d1.Encoded())

	d2, err := digest.New(d1.Algorithm(), raw)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if d2.String() != input {
		t.Errorf("round-trip String() = %q, want %q", d2.String(), input)
	}
}

func TestHashPath(t *testing.T) {
	t.Parallel()

	result := digest.HashPath("/some/file/path.flac")

	// Must be exactly 16 lowercase hex characters.
	if len(result) != 16 {
		t.Fatalf("HashPath length = %d, want 16", len(result))
	}

	if !regexp.MustCompile(`^[a-f0-9]{16}$`).MatchString(result) {
		t.Errorf("HashPath = %q, want lowercase hex", result)
	}

	// Must be deterministic.
	again := digest.HashPath("/some/file/path.flac")
	if result != again {
		t.Errorf("HashPath not deterministic: %q != %q", result, again)
	}

	// Different input must produce different output.
	other := digest.HashPath("/different/path.flac")
	if result == other {
		t.Errorf("HashPath collision: %q == %q for different inputs", result, other)
	}
}

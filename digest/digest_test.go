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
	"fmt"
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
		{
			name:    "blake3-256",
			input:   "blake3-256:af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
			wantAlg: digest.BLAKE3256,
			wantEnc: "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
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
		{digest.BLAKE3256, 32},
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
		{digest.BLAKE3256, 32},
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

// TestBLAKE3256_OfficialVectors pins the BLAKE3 implementation against the
// reference test vectors. The digest here is a persistence contract — content
// is addressed by it — so an upstream change that altered output would be a
// silent, unrecoverable corruption of every stored blob. The inputs are the
// standard repeating 0..250 byte pattern from the BLAKE3 specification.
func TestBLAKE3256_OfficialVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		inputLen int
		want     string
	}{
		{0, "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"},
		{1, "2d3adedff11b61f14c886e35afa036736dcd87a74d27b5c1510225d0f592e213"},
		{1024, "42214739f095a406f3fc83deb889744ac00df831c10daa55189b5d121c855af7"},
		{2048, "e776b6028c7cd22a4d0ba182a8bf62205d2ef576467e838ed6f2529b85fba24a"},
		{3072, "b98cb0ff3623be03326b373de6b9095218513e64f1ee2edd2525c7ad1e5cffd2"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("len%d", tt.inputLen), func(t *testing.T) {
			t.Parallel()

			input := make([]byte, tt.inputLen)
			for i := range input {
				input[i] = byte(i % 251)
			}

			h := digest.BLAKE3256.Hash()
			h.Write(input)

			if got := hex.EncodeToString(h.Sum(nil)); got != tt.want {
				t.Errorf("BLAKE3-256 of %d bytes = %s, want %s", tt.inputLen, got, tt.want)
			}
		})
	}
}

// TestBLAKE3256_ChunkedWriteEquivalence covers the parallel Write path: this
// implementation splits each Write across goroutines, so the digest must not
// depend on how the caller chunks its input. Sizes straddle the 1KiB chunk
// and the eigentree boundaries where the split decisions are made.
func TestBLAKE3256_ChunkedWriteEquivalence(t *testing.T) {
	t.Parallel()

	input := make([]byte, 1<<20+12345)
	for i := range input {
		input[i] = byte(i % 251)
	}

	oneShot := digest.BLAKE3256.Hash()
	oneShot.Write(input)
	want := hex.EncodeToString(oneShot.Sum(nil))

	for _, chunk := range []int{1, 63, 1024, 1025, 32 << 10, 1 << 20} {
		t.Run(fmt.Sprintf("chunk%d", chunk), func(t *testing.T) {
			t.Parallel()

			h := digest.BLAKE3256.Hash()
			for off := 0; off < len(input); off += chunk {
				h.Write(input[off:min(off+chunk, len(input))])
			}

			if got := hex.EncodeToString(h.Sum(nil)); got != want {
				t.Errorf("chunked by %d = %s, want %s", chunk, got, want)
			}
		})
	}
}

// TestAlgorithmRegistriesConsistent guards the hazard called out in AUDIT.md:
// Hash(), New() and FromString() each read a different map, so an algorithm
// added to one but not the others fails only at run time — a missing regexp
// entry nil-derefs in FromString, a missing size entry makes New reject a
// known algorithm.
func TestAlgorithmRegistriesConsistent(t *testing.T) {
	t.Parallel()

	all := []digest.Algorithm{
		digest.MD5, digest.SHA1, digest.SHA256, digest.SHA384, digest.SHA512,
		digest.BLAKE2b256, digest.BLAKE2b512, digest.BLAKE3256,
	}

	for _, alg := range all {
		t.Run(string(alg), func(t *testing.T) {
			t.Parallel()

			// Present in hashConstructors, and the size agrees with digestSizes
			// via New's length check.
			h := alg.Hash()
			raw := h.Sum(nil)

			dgst, err := digest.New(alg, raw)
			if err != nil {
				t.Fatalf("New(%s, %d bytes): %v", alg, len(raw), err)
			}

			// Present in anchoredEncodedRegexps, and the regexp accepts what
			// the hash actually produces.
			parsed, err := digest.FromString(dgst.String())
			if err != nil {
				t.Fatalf("FromString(%s): %v", dgst.String(), err)
			}

			if parsed.Algorithm() != alg || parsed.Encoded() != dgst.Encoded() {
				t.Errorf("round trip changed digest: %s -> %s", dgst, parsed)
			}
		})
	}
}

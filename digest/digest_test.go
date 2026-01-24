package digest_test

import (
	"errors"
	"testing"

	"github.com/farcloser/primordium/digest"
	"github.com/farcloser/primordium/fault"
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

	_, err := digest.FromString("md5:d41d8cd98f00b204e9800998ecf8427e")
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

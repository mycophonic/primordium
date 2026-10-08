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
	"crypto"
	_ "crypto/md5"  // #nosec G501 -- MD5 needed for external digest verification
	_ "crypto/sha1" // #nosec G505 -- SHA1 needed for legacy git compatibility
	_ "crypto/sha256"
	_ "crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"

	"github.com/forkcloser/blake3"
	"golang.org/x/crypto/blake2b"

	"github.com/mycophonic/primordium/fault"
)

// Supported digest algorithms.
const (
	MD5        Algorithm = "md5"
	SHA1       Algorithm = "sha1"
	SHA256     Algorithm = "sha256"
	SHA384     Algorithm = "sha384"
	SHA512     Algorithm = "sha512"
	BLAKE2b256 Algorithm = "blake2b-256"
	BLAKE2b512 Algorithm = "blake2b-512"
	BLAKE3256  Algorithm = "blake3-256"
)

// algorithm is what the package knows of one: how it hashes, and how many
// bytes its digest holds.
type algorithm struct {
	hash func() hash.Hash
	size int
}

//nolint:gochecknoglobals // the algorithm registry
var algorithms = map[Algorithm]algorithm{
	MD5:        {crypto.MD5.New, crypto.MD5.Size()},
	SHA1:       {crypto.SHA1.New, crypto.SHA1.Size()},
	SHA256:     {crypto.SHA256.New, crypto.SHA256.Size()},
	SHA384:     {crypto.SHA384.New, crypto.SHA384.Size()},
	SHA512:     {crypto.SHA512.New, crypto.SHA512.Size()},
	BLAKE2b256: {newBLAKE2b256, blake2b.Size256},
	BLAKE2b512: {newBLAKE2b512, blake2b.Size},
	BLAKE3256:  {newBLAKE3256, blake3Size256},
}

func newBLAKE2b256() hash.Hash {
	h, err := blake2b.New256(nil)
	if err != nil {
		panic(err)
	}

	return h
}

func newBLAKE2b512() hash.Hash {
	h, err := blake2b.New512(nil)
	if err != nil {
		panic(err)
	}

	return h
}

// blake3Size256 is the size of a BLAKE3-256 digest, in bytes.
const blake3Size256 = 32

// newBLAKE3256 returns an unkeyed BLAKE3 hasher with a 256-bit output.
//
// This implementation parallelises across goroutines within each Write call,
// so callers hashing bulk content should feed it large buffers — see
// stageBufferSize in store/content for the measured trade-off.
func newBLAKE3256() hash.Hash {
	return blake3.New(blake3Size256, nil)
}

// Algorithm represents a digest algorithm identifier.
type Algorithm string

// Hash returns a new hash as used by the algorithm. If not available, the
// method will panic.
func (a Algorithm) Hash() hash.Hash {
	known, ok := algorithms[a]
	if !ok {
		panic(fmt.Sprintf("unknown algorithm: %s", a))
	}

	return known.hash()
}

// Digest represents a content digest with an algorithm and encoded hash.
type Digest interface {
	// Algorithm is the algorithm that produced the digest.
	Algorithm() Algorithm
	// Encoded is the digest's lowercase hex encoding.
	Encoded() string
	// String is the digest as "algorithm:encoded".
	String() string
}

type digest struct {
	algorithm Algorithm
	encoded   string
}

// New creates a Digest from an algorithm and raw hash bytes.
//
//nolint:iface // a nil Digest means none, and callers pass nil for that
func New(alg Algorithm, raw []byte) (Digest, error) {
	known, ok := algorithms[alg]
	if !ok {
		return nil, fmt.Errorf("%w: unknown algorithm %s", fault.ErrInvalidArgument, alg)
	}

	if len(raw) != known.size {
		return nil, fmt.Errorf(
			"%w: expected %d bytes for %s, got %d",
			fault.ErrInvalidArgument,
			known.size,
			alg,
			len(raw),
		)
	}

	return &digest{
		algorithm: alg,
		encoded:   hex.EncodeToString(raw),
	}, nil
}

// FromString parses a digest string in the format "algorithm:encoded".
//
//nolint:iface // a nil Digest means none, and callers pass nil for that
func FromString(dgst string) (Digest, error) {
	before, after, ok := strings.Cut(dgst, ":")

	if !ok {
		return nil, fmt.Errorf("%w: digest %s has no colon", fault.ErrInvalidArgument, dgst)
	}

	alg := Algorithm(before)

	known, ok := algorithms[alg]
	if !ok {
		return nil, fmt.Errorf("%w: digest %s has unknown algorithm", fault.ErrInvalidArgument, dgst)
	}

	encoded := after
	if len(encoded) != hex.EncodedLen(known.size) || !isLowerHex(encoded) {
		return nil, fmt.Errorf("%w: digest %s has invalid encoded hash for algorithm", fault.ErrInvalidArgument, dgst)
	}

	return &digest{
		algorithm: alg,
		encoded:   encoded,
	}, nil
}

// isLowerHex reports whether s is lowercase hex, the one spelling of an
// encoded digest: hex.DecodeString would take "A-F" too.
func isLowerHex(s string) bool {
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

func (d *digest) Algorithm() Algorithm {
	return d.algorithm
}

func (d *digest) Encoded() string {
	return d.encoded
}

func (d *digest) String() string {
	return string(d.algorithm) + ":" + d.encoded
}

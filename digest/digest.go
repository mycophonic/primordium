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
	_ "crypto/md5"  //nolint:gosec // MD5 needed for external digest verification
	_ "crypto/sha1" //nolint:gosec // SHA1 needed for legacy git compatibility
	_ "crypto/sha256"
	_ "crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
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

//nolint:gochecknoglobals // Package-level registry is appropriate here
var (
	// hashConstructors maps algorithms to their hash constructor functions.
	hashConstructors = map[Algorithm]func() hash.Hash{
		MD5:        crypto.MD5.New,
		SHA1:       crypto.SHA1.New,
		SHA256:     crypto.SHA256.New,
		SHA384:     crypto.SHA384.New,
		SHA512:     crypto.SHA512.New,
		BLAKE2b256: newBLAKE2b256,
		BLAKE2b512: newBLAKE2b512,
		BLAKE3256:  newBLAKE3256,
	}

	// anchoredEncodedRegexps contains anchored regular expressions for hex-encoded digests.
	// Note that /A-F/ disallowed.
	anchoredEncodedRegexps = map[Algorithm]*regexp.Regexp{
		MD5:        regexp.MustCompile(`^[a-f0-9]{32}$`),
		SHA1:       regexp.MustCompile("^[a-f0-9]{40}$"),
		SHA256:     regexp.MustCompile(`^[a-f0-9]{64}$`),
		SHA384:     regexp.MustCompile(`^[a-f0-9]{96}$`),
		SHA512:     regexp.MustCompile(`^[a-f0-9]{128}$`),
		BLAKE2b256: regexp.MustCompile(`^[a-f0-9]{64}$`),
		BLAKE2b512: regexp.MustCompile(`^[a-f0-9]{128}$`),
		BLAKE3256:  regexp.MustCompile(`^[a-f0-9]{64}$`),
	}

	// digestSizes maps algorithms to their expected byte lengths.
	digestSizes = map[Algorithm]int{
		MD5:        16,
		SHA1:       20,
		SHA256:     32,
		SHA384:     48,
		SHA512:     64,
		BLAKE2b256: 32,
		BLAKE2b512: 64,
		BLAKE3256:  32,
	}
)

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

// newBLAKE3256 returns an unkeyed BLAKE3 hasher with a 256-bit output.
//
// This implementation parallelises across goroutines within each Write call,
// so callers hashing bulk content should feed it large buffers — see
// stageBufferSize in store/content for the measured trade-off.
func newBLAKE3256() hash.Hash {
	return blake3.New(32, nil)
}

// Algorithm represents a digest algorithm identifier.
type Algorithm string

// Hash returns a new hash as used by the algorithm. If not available, the
// method will panic.
func (a Algorithm) Hash() hash.Hash {
	constructor, ok := hashConstructors[a]
	if !ok {
		panic(fmt.Sprintf("unknown algorithm: %s", a))
	}

	return constructor()
}

// Digest represents a content digest with an algorithm and encoded hash.
type Digest interface {
	Algorithm() Algorithm
	Encoded() string
	String() string
}

type digest struct {
	algorithm Algorithm
	encoded   string
}

// New creates a Digest from an algorithm and raw hash bytes.
func New(alg Algorithm, raw []byte) (Digest, error) {
	size, ok := digestSizes[alg]
	if !ok {
		return nil, fmt.Errorf("%w: unknown algorithm %s", fault.ErrInvalidArgument, alg)
	}

	if len(raw) != size {
		return nil, fmt.Errorf("%w: expected %d bytes for %s, got %d", fault.ErrInvalidArgument, size, alg, len(raw))
	}

	return &digest{
		algorithm: alg,
		encoded:   hex.EncodeToString(raw),
	}, nil
}

// FromString parses a digest string in the format "algorithm:encoded".
func FromString(dgst string) (Digest, error) {
	before, after, ok := strings.Cut(dgst, ":")

	if !ok {
		return nil, fmt.Errorf("%w: digest %s has no colon", fault.ErrInvalidArgument, dgst)
	}

	alg := Algorithm(before)
	if _, ok := hashConstructors[alg]; !ok {
		return nil, fmt.Errorf("%w: digest %s has unknown algorithm", fault.ErrInvalidArgument, dgst)
	}

	encoded := after
	if !anchoredEncodedRegexps[alg].MatchString(encoded) {
		return nil, fmt.Errorf("%w: digest %s has invalid encoded hash for algorithm", fault.ErrInvalidArgument, dgst)
	}

	return &digest{
		algorithm: alg,
		encoded:   encoded,
	}, nil
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

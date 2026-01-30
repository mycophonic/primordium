package digest

import (
	"crypto"
	_ "crypto/sha1" //nolint:gosec // SHA1 needed for legacy git compatibility
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"hash"
	"regexp"
	"strings"

	"golang.org/x/crypto/blake2b"

	"github.com/mycophonic/primordium/fault"
)

// Supported digest algorithms.
const (
	SHA1       Algorithm = "sha1"
	SHA256     Algorithm = "sha256"
	SHA384     Algorithm = "sha384"
	SHA512     Algorithm = "sha512"
	BLAKE2b256 Algorithm = "blake2b-256"
	BLAKE2b512 Algorithm = "blake2b-512"
)

//nolint:gochecknoglobals // Package-level registry is appropriate here
var (
	// hashConstructors maps algorithms to their hash constructor functions.
	hashConstructors = map[Algorithm]func() hash.Hash{
		SHA1:       crypto.SHA1.New,
		SHA256:     crypto.SHA256.New,
		SHA384:     crypto.SHA384.New,
		SHA512:     crypto.SHA512.New,
		BLAKE2b256: newBLAKE2b256,
		BLAKE2b512: newBLAKE2b512,
	}

	// anchoredEncodedRegexps contains anchored regular expressions for hex-encoded digests.
	// Note that /A-F/ disallowed.
	anchoredEncodedRegexps = map[Algorithm]*regexp.Regexp{
		SHA1:       regexp.MustCompile("^[a-f0-9]{40}$"),
		SHA256:     regexp.MustCompile(`^[a-f0-9]{64}$`),
		SHA384:     regexp.MustCompile(`^[a-f0-9]{96}$`),
		SHA512:     regexp.MustCompile(`^[a-f0-9]{128}$`),
		BLAKE2b256: regexp.MustCompile(`^[a-f0-9]{64}$`),
		BLAKE2b512: regexp.MustCompile(`^[a-f0-9]{128}$`),
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

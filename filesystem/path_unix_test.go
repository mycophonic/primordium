//go:build !windows

package filesystem_test

import (
	"errors"
	"testing"

	"github.com/farcloser/primordium/fault"
	"github.com/farcloser/primordium/filesystem"
)

func TestValidatePath_PathTraversal(t *testing.T) {
	t.Parallel()

	// Path traversal attempts should be rejected
	tests := []struct {
		name string
		path string
	}{
		{"dot-dot", "/foo/../bar"},
		{"dot-dot-only", ".."},
		{"leading-dot-dot", "../secret"},
		{"nested-traversal", "/a/b/../../c"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := filesystem.ValidatePath(tc.path)
			if err == nil {
				t.Errorf("ValidatePath(%q) should reject path traversal", tc.path)
			}

			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Errorf("ValidatePath(%q) error = %v, want fault.ErrInvalidArgument", tc.path, err)
			}
		})
	}
}

func TestValidatePath_ValidPaths(t *testing.T) {
	t.Parallel()

	tests := []string{
		"/usr/local/bin",
		"/home/user/.config",
		"relative/path",
		"single",
		"/",
		"",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if err := filesystem.ValidatePath(path); err != nil {
				t.Errorf("ValidatePath(%q) = %v, want nil", path, err)
			}
		})
	}
}

func TestValidatePath_DoubleSeparators(t *testing.T) {
	t.Parallel()

	// Double separators should be handled (empty components skipped)
	if err := filesystem.ValidatePath("/foo//bar"); err != nil {
		t.Errorf("ValidatePath with double separator failed: %v", err)
	}
}

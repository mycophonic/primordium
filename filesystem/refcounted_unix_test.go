//go:build !windows

package filesystem_test

import (
	"testing"

	"github.com/mycophonic/primordium/filesystem"
)

func TestNewLocker_PanicsOnInvalidPath(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid path")
		}
	}()

	// Path with traversal should panic
	filesystem.NewLocker("/foo/../bar")
}

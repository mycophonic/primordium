package filesystem_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/farcloser/primordium/fault"
	"github.com/farcloser/primordium/filesystem"
)

func TestValidateSocketPath_BoundaryLengths(t *testing.T) {
	t.Parallel()

	// Determine platform-specific max length
	var maxUsable int

	switch runtime.GOOS {
	case "linux":
		maxUsable = 107 // 108 - 1 for null terminator
	case "windows":
		// Windows doesn't have Unix sockets in the traditional sense
		t.Skip("Windows does not have Unix socket path limits")
	default:
		maxUsable = 103 // 104 - 1 for null terminator (macOS/BSD)
	}

	tests := []struct {
		name    string
		length  int
		wantErr bool
	}{
		{"exactly-at-limit", maxUsable, false},
		{"one-over-limit", maxUsable + 1, true},
		{"well-under-limit", 50, false},
		{"way-over-limit", maxUsable + 100, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := strings.Repeat("x", tc.length)
			err := filesystem.ValidateSocketPath(path)

			if tc.wantErr && err == nil {
				t.Errorf("ValidateSocketPath(len=%d) should fail, got nil", tc.length)
			}

			if !tc.wantErr && err != nil {
				t.Errorf("ValidateSocketPath(len=%d) = %v, want nil", tc.length, err)
			}

			if tc.wantErr && err != nil && !errors.Is(err, fault.ErrInvalidArgument) {
				t.Errorf("ValidateSocketPath error = %v, want fault.ErrInvalidArgument", err)
			}
		})
	}
}

func TestValidateSocketPath_ErrorMessageContainsDetails(t *testing.T) {
	t.Parallel()

	// Create a path that's definitely too long
	longPath := strings.Repeat("a", 200)

	err := filesystem.ValidateSocketPath(longPath)
	if err == nil {
		t.Fatal("expected error for long path")
	}

	errMsg := err.Error()

	// Error should contain useful debugging info
	if !strings.Contains(errMsg, runtime.GOOS) {
		t.Errorf("error message should contain OS name, got: %s", errMsg)
	}

	if !strings.Contains(errMsg, "200") {
		t.Errorf("error message should contain actual length (200), got: %s", errMsg)
	}
}

func TestFilesystemRestrictions(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"/",
		"/start",
		"mid/dle",
		"end/",
		".",
		"..",
		"",
		fmt.Sprintf("A%0255s", "A"),
	}

	valid := []string{
		fmt.Sprintf("A%0254s", "A"),
		"test",
		"test-hyphen",
		".start.dot",
		"mid.dot",
		"∞",
	}

	if runtime.GOOS == "windows" {
		invalid = append(invalid, []string{
			"\\start",
			"mid\\dle",
			"end\\",
			"\\",
			"\\.",
			"com².whatever",
			"lpT2",
			"Prn.",
			"nUl",
			"AUX",
			"A<A",
			"A>A",
			"A:A",
			"A\"A",
			"A|A",
			"A?A",
			"A*A",
			"end.dot.",
			"end.space ",
		}...)
	}

	for _, v := range invalid {
		err := filesystem.ValidatePathComponent(v)
		assert.ErrorIs(t, err, filesystem.ErrInvalidPath, v)
	}

	for _, v := range valid {
		err := filesystem.ValidatePathComponent(v)
		assert.NilError(t, err, v)
	}
}

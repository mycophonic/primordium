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

package fault_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/fault"
)

func TestSentinelErrors_Exist(t *testing.T) {
	t.Parallel()

	// Verify all sentinel errors are defined and have non-empty messages
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrSystemFailure", fault.ErrSystemFailure},
		{"ErrFilesystemFailure", fault.ErrFilesystemFailure},
		{"ErrMissingRequirements", fault.ErrMissingRequirements},
		{"ErrNotImplemented", fault.ErrNotImplemented},
		{"ErrInvalidArgument", fault.ErrInvalidArgument},
		{"ErrNotFound", fault.ErrNotFound},
		{"ErrReadFailure", fault.ErrReadFailure},
		{"ErrWriteFailure", fault.ErrWriteFailure},
		{"ErrAuthenticationFailure", fault.ErrAuthenticationFailure},
		{"ErrCancelled", fault.ErrCancelled},
		{"ErrHashMismatch", fault.ErrHashMismatch},
		{"ErrInvalidJSON", fault.ErrInvalidJSON},
		{"ErrNetworkCommunication", fault.ErrNetworkCommunication},
		{"ErrUnacceptableResponse", fault.ErrUnacceptableResponse},
		{"ErrCommandFailure", fault.ErrCommandFailure},
	}

	for _, tt := range sentinels {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.err == nil {
				t.Errorf("%s is nil", tt.name)
			}

			if tt.err.Error() == "" {
				t.Errorf("%s has empty message", tt.name)
			}
		})
	}
}

func TestSentinelErrors_Identity(t *testing.T) {
	t.Parallel()

	// Each sentinel error should be identifiable with errors.Is
	sentinels := []error{
		fault.ErrSystemFailure,
		fault.ErrFilesystemFailure,
		fault.ErrMissingRequirements,
		fault.ErrNotImplemented,
		fault.ErrInvalidArgument,
		fault.ErrNotFound,
		fault.ErrReadFailure,
		fault.ErrWriteFailure,
		fault.ErrAuthenticationFailure,
		fault.ErrCancelled,
		fault.ErrHashMismatch,
		fault.ErrInvalidJSON,
		fault.ErrNetworkCommunication,
		fault.ErrUnacceptableResponse,
		fault.ErrCommandFailure,
	}

	for _, sentinel := range sentinels {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()

			if !errors.Is(sentinel, sentinel) {
				t.Errorf("errors.Is(%v, %v) = false, want true", sentinel, sentinel)
			}
		})
	}
}

func TestSentinelErrors_Wrapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrSystemFailure", fault.ErrSystemFailure},
		{"ErrFilesystemFailure", fault.ErrFilesystemFailure},
		{"ErrNotFound", fault.ErrNotFound},
		{"ErrInvalidArgument", fault.ErrInvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Wrap the sentinel error
			wrapped := fmt.Errorf("operation failed: %w", tt.sentinel)

			// errors.Is should find the sentinel
			if !errors.Is(wrapped, tt.sentinel) {
				t.Errorf("errors.Is(wrapped, %v) = false, want true", tt.sentinel)
			}

			// Double wrap
			doubleWrapped := fmt.Errorf("outer: %w", wrapped)
			if !errors.Is(doubleWrapped, tt.sentinel) {
				t.Errorf("errors.Is(doubleWrapped, %v) = false, want true", tt.sentinel)
			}
		})
	}
}

func TestSentinelErrors_Distinctness(t *testing.T) {
	t.Parallel()

	// Verify that different sentinel errors are distinct
	pairs := []struct {
		name string
		a    error
		b    error
	}{
		{"SystemFailure vs FilesystemFailure", fault.ErrSystemFailure, fault.ErrFilesystemFailure},
		{"NotFound vs NotImplemented", fault.ErrNotFound, fault.ErrNotImplemented},
		{"ReadFailure vs WriteFailure", fault.ErrReadFailure, fault.ErrWriteFailure},
		{"Cancelled vs Timeout", fault.ErrCancelled, fault.ErrTimeout},
		{"InvalidArgument vs InvalidJSON", fault.ErrInvalidArgument, fault.ErrInvalidJSON},
	}

	for _, tt := range pairs {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if errors.Is(tt.a, tt.b) {
				t.Errorf("errors.Is(%v, %v) = true, want false", tt.a, tt.b)
			}

			if errors.Is(tt.b, tt.a) {
				t.Errorf("errors.Is(%v, %v) = true, want false", tt.b, tt.a)
			}
		})
	}
}

func TestSentinelErrors_MultipleWrapping(t *testing.T) {
	t.Parallel()

	// Test wrapping with multiple sentinels using %w
	inner := fmt.Errorf("%w: file not accessible", fault.ErrNotFound)
	outer := fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, inner)

	// Both should be detectable
	if !errors.Is(outer, fault.ErrFilesystemFailure) {
		t.Error("errors.Is(outer, ErrFilesystemFailure) = false, want true")
	}

	if !errors.Is(outer, fault.ErrNotFound) {
		t.Error("errors.Is(outer, ErrNotFound) = false, want true")
	}
}

func TestSentinelErrors_ErrorMessages(t *testing.T) {
	t.Parallel()

	// Verify error messages are meaningful
	tests := []struct {
		err      error
		contains string
	}{
		{fault.ErrSystemFailure, "system"},
		{fault.ErrFilesystemFailure, "filesystem"},
		{fault.ErrNotImplemented, "not implemented"},
		{fault.ErrInvalidArgument, "invalid"},
		{fault.ErrNotFound, "not found"},
		{fault.ErrReadFailure, "read"},
		{fault.ErrWriteFailure, "write"},
		{fault.ErrAuthenticationFailure, "authenticate"},
		{fault.ErrCancelled, "cancelled"},
		{fault.ErrHashMismatch, "hash"},
		{fault.ErrInvalidJSON, "JSON"},
		{fault.ErrNetworkCommunication, "network"},
		{fault.ErrCommandFailure, "command"},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(strings.ToLower(tt.err.Error()), strings.ToLower(tt.contains)) {
				t.Errorf("error message %q should contain %q", tt.err.Error(), tt.contains)
			}
		})
	}
}

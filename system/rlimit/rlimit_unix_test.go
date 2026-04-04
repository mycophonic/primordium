//go:build unix

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

package rlimit_test

import (
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/system/rlimit"
)

func getLimits(t *testing.T) syscall.Rlimit {
	t.Helper()

	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatalf("Getrlimit: %v", err)
	}

	return lim
}

//nolint:paralleltest // all tests mutate process-global RLIMIT_NOFILE
func TestRaiseNoFileLimit_DoesNotLowerSoftLimit(t *testing.T) {
	before := getLimits(t)

	rlimit.RaiseNoFileLimit()

	after := getLimits(t)
	if after.Cur < before.Cur {
		t.Errorf("soft limit lowered: %d -> %d", before.Cur, after.Cur)
	}
}

//nolint:paralleltest // all tests mutate process-global RLIMIT_NOFILE
func TestRaiseNoFileLimit_DoesNotLowerHardLimit(t *testing.T) {
	before := getLimits(t)

	rlimit.RaiseNoFileLimit()

	after := getLimits(t)
	if after.Max < before.Max {
		t.Errorf("hard limit lowered: %d -> %d", before.Max, after.Max)
	}
}

//nolint:paralleltest // all tests mutate process-global RLIMIT_NOFILE
func TestRaiseNoFileLimit_SoftAtLeastDesired(t *testing.T) {
	rlimit.RaiseNoFileLimit()

	after := getLimits(t)

	// The hard limit caps what we can achieve.  If hard >= 65536,
	// soft must be at least 65536.  If hard < 65536, soft must equal hard.
	const desired = 65536

	if after.Max >= desired {
		if after.Cur < desired {
			t.Errorf("soft limit = %d, want >= %d (hard = %d)", after.Cur, desired, after.Max)
		}
	} else {
		if after.Cur != after.Max {
			t.Errorf("soft limit = %d, want %d (clamped to hard)", after.Cur, after.Max)
		}
	}
}

//nolint:paralleltest // all tests mutate process-global RLIMIT_NOFILE
func TestRaiseNoFileLimit_Idempotent(t *testing.T) {
	rlimit.RaiseNoFileLimit()

	first := getLimits(t)

	rlimit.RaiseNoFileLimit()

	second := getLimits(t)

	if first.Cur != second.Cur {
		t.Errorf("soft limit changed on second call: %d -> %d", first.Cur, second.Cur)
	}

	if first.Max != second.Max {
		t.Errorf("hard limit changed on second call: %d -> %d", first.Max, second.Max)
	}
}

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

// The contract, from rlimit's docs:
//   - after RaiseNoFileLimit the soft limit is at least 65536 when the hard
//     limit allows, else the hard limit, and never below what it was;
//   - the hard limit is untouched;
//   - a child started afterwards inherits the soft limit in force, which is
//     what the Go runtime denies it without the call;
//   - a second call changes nothing.
//
// The limits are process-wide, so the tests run in order and the one that
// lowers the hard limit does so in a child: this binary, told by
// PRIMORDIUM_RLIMIT_CHILD.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/mycophonic/primordium/system/rlimit"
)

const (
	childEnv = "PRIMORDIUM_RLIMIT_CHILD"
	desired  = 65536
)

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "clamp":
		// The hard limit below the floor, then the call: the soft limit must
		// land on the hard limit, and a grandchild must see it.
		lim := syscall.Rlimit{Cur: 1000, Max: 1000}
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		rlimit.RaiseNoFileLimit()

		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		fmt.Fprintln(os.Stdout, lim.Cur, lim.Max, childNofile())
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

func limits(t *testing.T) syscall.Rlimit {
	t.Helper()

	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatal(err)
	}

	return lim
}

// childNofile is the soft limit a freshly started child reports.
func childNofile() uint64 {
	out, err := exec.CommandContext(context.Background(), "sh", "-c", "ulimit -n").Output()
	if err != nil {
		return 0
	}

	text := strings.TrimSpace(string(out))
	if text == "unlimited" {
		return ^uint64(0)
	}

	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0
	}

	return value
}

//nolint:paralleltest // process-wide limits, in order
func TestRaise(t *testing.T) {
	before := limits(t)

	rlimit.RaiseNoFileLimit()

	after := limits(t)

	if after.Max != before.Max {
		t.Errorf("hard limit %d -> %d", before.Max, after.Max)
	}

	if after.Cur < before.Cur {
		t.Errorf("soft limit lowered: %d -> %d", before.Cur, after.Cur)
	}

	if want := min(uint64(desired), after.Max); after.Cur < want {
		t.Errorf("soft limit %d, want at least %d (hard %d)", after.Cur, want, after.Max)
	}
}

//nolint:paralleltest // process-wide limits, in order
func TestChildInherits(t *testing.T) {
	rlimit.RaiseNoFileLimit()

	parent := limits(t)

	if got := childNofile(); got != parent.Cur {
		t.Errorf("child sees %d, parent's soft limit is %d", got, parent.Cur)
	}
}

//nolint:paralleltest // process-wide limits, in order
func TestSecondCallChangesNothing(t *testing.T) {
	rlimit.RaiseNoFileLimit()

	first := limits(t)

	rlimit.RaiseNoFileLimit()

	if second := limits(t); second != first {
		t.Errorf("second call: %+v -> %+v", first, second)
	}
}

//nolint:paralleltest // starts a child of this binary
func TestClampedToHardLimit(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"=clamp")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	if got, want := strings.TrimSpace(string(out)), "1000 1000 1000"; got != want {
		t.Fatalf("soft, hard, grandchild: %q, want %q", got, want)
	}
}

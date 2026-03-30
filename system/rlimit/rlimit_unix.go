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

package rlimit

import (
	"log/slog"
	"syscall"
)

// RaiseNoFileLimit ensures exec'd subprocesses inherit a high open-file limit.
//
// Background — Unix has two RLIMIT_NOFILE values:
//
//   - Soft limit (Cur): the actual enforced limit on open file descriptors.
//     Despite the name "soft", this IS the real, enforced ceiling.
//   - Hard limit (Max): the maximum the soft limit can be raised to without
//     root.  A process can lower its own hard limit but never raise it back.
//
// What Go's runtime does (since 1.19):
//
//  1. On startup, the OS gives the process low defaults (e.g., soft=1024,
//     hard=1048576).
//  2. Go's runtime init() raises soft to hard-1 (e.g., 1048575) for the
//     Go process itself.
//  3. Go saves the original soft (1024) internally.
//  4. Before exec'ing a child process, Go restores soft to the saved
//     original (1024).  The child is stuck with the low limit.
//
// Step 4 is the problem.  An explicit syscall.Setrlimit call tells Go to
// discard the saved original, so children inherit the current (high) limit
// instead.  That is the primary purpose of this function.
//
// Best-effort: failure is logged, not fatal.
func RaiseNoFileLimit() {
	// Read the current limits.  At this point Go's runtime has already
	// raised soft to hard-1, so lim.Cur is typically very high already.
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		slog.Warn("failed to get nofile limit", "error", err)

		return
	}

	// Safety net: if the soft limit is still below our floor (e.g., the
	// hard limit is very low), bump it up as far as the hard limit allows.
	if lim.Cur < desiredNofile {
		lim.Cur = min(desiredNofile, lim.Max)
	}

	// The Setrlimit call itself is what matters: it tells Go's runtime to
	// stop restoring the original low limit before exec.  After this,
	// children inherit lim.Cur (typically hard-1, e.g., 1048575).
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		slog.Warn("failed to set nofile limit",
			"target", lim.Cur, "error", err)

		return
	}

	slog.Debug("nofile limit set", "limit", lim.Cur)
}

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

// RaiseNoFileLimit raises the soft RLIMIT_NOFILE to 65536 when it is below
// that and the hard limit allows, and makes the children this process starts
// inherit the soft limit in force. Best effort: a failure is logged, never
// returned.
//
// The call is the mechanism, not the value: the Go runtime raises the soft
// limit at start and restores the original before every exec, until
// syscall.Setrlimit has been called on RLIMIT_NOFILE. x/sys/unix.Setrlimit
// delegates to it for that reason; a raw prlimit or setrlimit would not.
func RaiseNoFileLimit() {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		slog.Warn("failed to get nofile limit", "error", err)

		return
	}

	if lim.Cur < desiredNofile {
		lim.Cur = min(desiredNofile, lim.Max)
	}

	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		slog.Warn("failed to set nofile limit", "target", lim.Cur, "error", err)

		return
	}

	slog.Debug("nofile limit set", "limit", lim.Cur)
}

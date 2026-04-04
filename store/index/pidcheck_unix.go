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

package index

import "syscall"

// isProcessAlive reports whether a process with the given PID exists.
// It uses kill(pid, 0), which checks for process existence without
// delivering any signal.
func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

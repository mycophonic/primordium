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

// Package shutdown manages graceful process exit with registered cleanup
// handlers. Handlers run in LIFO order, exactly once, with a timeout.
//
// Coverage:
//
//   - Normal return from [Run]: handlers run, exit 0.
//   - Error return from [Run]: handlers run, exit 1.
//   - Panic in main goroutine (via [Run]): recovered, handlers run, exit 1.
//   - Panic in goroutine launched via [Go]: recovered, handlers run, exit 1.
//   - SIGSEGV/SIGBUS (Go converts to panic): caught by [Run]/[Go] recovery.
//   - SIGINT/SIGTERM/SIGHUP/SIGQUIT: context cancelled, handlers run with
//     timeout, exit 128+signal.
//
// Not covered:
//
//   - SIGKILL: uncatchable by any user-space process. No cleanup is possible.
//     External coordination (e.g. stale-PID detection, lease expiry) is
//     required to recover from a SIGKILL'd process that held resources.
//   - Panics in bare "go" goroutines: only goroutines launched via [Go] have
//     panic recovery. A panic in a goroutine started with a bare "go"
//     statement crashes the process without running handlers. Use [Go] for
//     all goroutines that must participate in graceful shutdown.
package shutdown

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

// Package umask takes the umask out of file creation: once disabled, a file
// or directory gets exactly the mode its creation asks for.
//
// On Unix the kernel strips the umask's bits from the mode of everything a
// process creates: code that asks for 0o644 gets 0o600 under an operator's
// 0o077, and never learns. [Disable] zeroes the process umask, once, so the
// mode asked for is the mode the file gets; the mask it found is kept for
// [Get].
//
// The zeroed umask is inherited by every child process, like the rest of the
// process's state, and a tool that follows the convention (0o666 less the
// umask) then creates files anyone may write. A program that spawns such
// tools gives them the operator's mask back, in the child
// (sh -c 'umask 077; exec "$@"', or the tool's own setting), never by
// restoring the process umask around the spawn: the umask is process-wide,
// and the restored mask would strip what every other goroutine creates
// meanwhile.
//
// On Windows there is no umask: [Disable] does nothing and [Get] is 0.
package umask

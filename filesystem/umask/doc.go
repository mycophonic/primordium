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

// Package umask provides cross-platform umask management that gives the
// application explicit control over file permissions.
//
// # Design
//
// On Unix, the OS umask silently strips permission bits from every file
// creation. This is a frequent source of bugs: an application creates a file
// with mode 0644 but actually gets 0600 because the user's umask is 0077.
//
// This package deliberately zeroes the OS umask on the first call to [Get],
// so subsequent file creations receive exactly the permissions the caller
// requests. The original umask value is captured and stored internally as a
// hardened default (0077) that security-sensitive code paths can apply
// manually where appropriate (see filesystem.WriteFile).
//
// On Windows, umask has no effect; the platform stub is a no-op.
//
// # API contract
//
//   - [Get] zeroes the OS umask (once) and returns the internal mask value.
//     The first call captures the process's original umask; subsequent calls
//     return the cached value.
//   - [Set] updates the internal mask. If the new value differs from the
//     current one, the OS umask is also updated.
//
// Both functions are safe for concurrent use.
package umask

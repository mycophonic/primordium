//go:build !linux && !windows

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

package filesystem

// maxSocketPathLen is the maximum length of a Unix socket path on this platform.
// On macOS and BSD variants (FreeBSD, NetBSD, OpenBSD, DragonFly), sun_path is 104 bytes
// (including null terminator).
//
// References:
//   - macOS: /usr/include/sys/un.h
//   - FreeBSD: unix(4) man page
//   - NetBSD/OpenBSD: similar to FreeBSD
const maxSocketPathLen = 104

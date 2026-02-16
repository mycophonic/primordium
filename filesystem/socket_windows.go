//go:build windows

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
// On Windows (10+), sun_path is 108 bytes (including null terminator), same as Linux.
// AF_UNIX support was added in Windows 10 Build 17063.
//
// References:
//   - https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/
//   - Windows SDK: afunix.h defines UNIX_PATH_MAX = 108
const maxSocketPathLen = 108

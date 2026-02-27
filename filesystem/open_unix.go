//go:build darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd

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

import "os"

// Open opens a file for reading. On Windows, the handle is opened with
// FILE_SHARE_DELETE so that concurrent rename or deletion is permitted.
// On Unix this is the default behavior.
//
//nolint:wrapcheck // Thin wrapper
func Open(path string) (*os.File, error) {
	return os.Open(path) //nolint:gosec // Caller controls path
}

// OpenFile opens a file with the given flags and permissions. On Windows,
// the handle is opened with FILE_SHARE_DELETE so that concurrent rename
// or deletion is permitted. On Unix this is the default behavior.
//
//nolint:wrapcheck // Thin wrapper
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm) //nolint:gosec // Caller controls path
}

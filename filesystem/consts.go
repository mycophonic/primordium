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

const (
	// FilePermissionsDefault is the default file permission for newly created files.
	FilePermissionsDefault = 0o644
	// DirPermissionsDefault is the default directory permission for newly created directories.
	DirPermissionsDefault = 0o755
	// FilePermissionsPrivate is the permission for private files, only readable and writable by the owner.
	FilePermissionsPrivate = 0o600
	// DirPermissionsPrivate is the permission for private directories, only readable, writable, and executable by the
	// owner.
	DirPermissionsPrivate = 0o700

	defaultUmask           uint32 = 0o077
	pathComponentMaxLength int    = 255
	defaultBufferSize      int    = 4096
)

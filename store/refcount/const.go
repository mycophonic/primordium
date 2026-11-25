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

package refcount

const (
	// rootLockFile is the store's lock, in its root: the one name there
	// that is no key's.
	rootLockFile = ".lock"
	// entryLockFile, in an entry, is the lock on the entry itself.
	entryLockFile = ".entry.lock"
	// holdersLockFile, in an entry, counts its holders: each holds it shared.
	holdersLockFile = ".holders.lock"
)

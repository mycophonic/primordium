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

package flock_test

// The contract, from flock(2) (an advisory lock belongs to an open file
// description: "a call to flock() may block if an incompatible lock is held
// by another process" or another open of the file; on NFS, "in order to place
// an exclusive lock, the file must be opened for writing"), Microsoft's
// LockFileEx ("an exclusive lock denies all other processes both read and
// write access", "a shared lock denies all processes write access", and a
// second handle in the same process is another holder), and flock's own docs:
//   - a lock file is a file the caller names and makes: a lock creates no
//     file and leaves none but it; a directory is not a lock file, on any
//     platform;
//   - each Exclusive, Shared, TryExclusive and TryShared is a holder of its
//     own: shared holders coexist, an exclusive holder coexists with none;
//   - a Try call that conflicts returns ErrLockWouldBlock, not ErrLockFail,
//     and no lock; one that does not conflict succeeds;
//   - a blocking call is granted once, and only once, every conflicting
//     holder has let go;
//   - a lock is on the file at its path: a call that waited on a file
//     removed meanwhile fails as one that found none, and one that waited on
//     a file replaced meanwhile holds the file now there;
//   - Unlock releases its holder; on a nil Lock, or a second time, it is
//     ErrUnlockFail;
//   - a lock that cannot be placed at all is ErrLockFail: a path that holds
//     no file, which is fs.ErrNotExist too, and a directory for a lock file;
//   - a lock file the user may read and not write takes every lock;
//   - WithExclusive and WithShared hold their lock while their function
//     runs, release it after, and return its error.

// holder is one lock in the model: exclusive or shared.
type holder struct {
	exclusive bool
}

// conflicts reports whether a lock of the kind asked for conflicts with
// any of the holders.
func conflicts(holders map[int]holder, exclusive bool) bool {
	for _, held := range holders {
		if exclusive || held.exclusive {
			return true
		}
	}

	return false
}

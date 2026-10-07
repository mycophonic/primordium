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
// by another process" or another open of the file), Microsoft's LockFileEx
// ("an exclusive lock denies all other processes both read and write access",
// "a shared lock denies all processes write access", and a second handle in
// the same process is another holder), and flock's own docs:
//   - each Lock, ReadOnlyLock, TryLock and TryReadOnlyLock is a holder of its
//     own: shared holders coexist, an exclusive holder coexists with none;
//   - a Try call that conflicts returns ErrLockWouldBlock, not ErrLockFail,
//     and no file; one that does not conflict succeeds;
//   - a blocking call is granted once, and only once, every conflicting
//     holder has let go;
//   - Unlock releases its holder and closes its file; Unlock(nil) is
//     ErrLockIsNil;
//   - a lock that cannot be placed at all is ErrLockFail;
//   - WithLock and WithReadOnlyLock hold their lock while their function
//     runs, release it after, and return its error;
//   - Cleanup leaves no lock file of flock's own next to the path.

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

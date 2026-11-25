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

// Package flock locks a lock file between processes, with one contract on
// every platform: flock(2) on Unix, LockFileEx on Windows.
//
// A lock file is a file the caller names and makes, and it is the lock and
// nothing else. flock creates no file and removes none: a lock on a path
// that holds no file fails, as fs.ErrNotExist. What the file holds is not for
// reading or writing, which Windows refuses on a locked file. A directory is
// not a lock file: Windows locks none.
//
// A lock is on the file at its path. A call that waited on a file removed
// meanwhile fails like one that found none, and one that waited on a file
// replaced meanwhile locks the file now there: a holder never holds a file
// no other caller can reach.
//
// Removing a lock file retires the lock. Its holder removes it as its last
// act before Unlock, or under a lock of its own that every user of the file
// takes first. Removed any other way, a lock file stops excluding: its
// holders keep a file nobody else can lock.
package flock

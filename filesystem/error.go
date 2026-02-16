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

import "errors"

var (
	// ErrLockFail is returned when a lock cannot be acquired.
	ErrLockFail = errors.New("failed to acquire lock")
	// ErrLockWouldBlock is returned when a non-blocking lock cannot be acquired
	// because another process holds a conflicting lock.
	ErrLockWouldBlock = errors.New("lock would block")
	// ErrUnlockFail is returned when a lock cannot be released.
	ErrUnlockFail = errors.New("failed to release lock")
	// ErrAtomicWriteFail is returned when an atomic write operation fails.
	ErrAtomicWriteFail = errors.New("failed to write file atomically")
	// ErrLockIsNil is returned when a lock is nil.
	ErrLockIsNil = errors.New("nil lock")
	// ErrInvalidPath is returned when a path is invalid.
	ErrInvalidPath = errors.New("invalid path")
	// ErrGenericFailure is a generic error indicating a filesystem level failure.
	ErrGenericFailure = errors.New("a filesystem level failure happened")
	// ErrBufferFailure is returned when a buffered I/O operation fails.
	ErrBufferFailure = errors.New("buffered I/O failure")

	errInvalidPathTooLong = errors.New("path component must be stricly shorter than 256 characters")
	errInvalidPathEmpty   = errors.New("path component cannot be empty")
)

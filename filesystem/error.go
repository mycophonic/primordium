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

	errInvalidPathTooLong = errors.New("path component must be stricly shorter than 256 characters")
	errInvalidPathEmpty   = errors.New("path component cannot be empty")
)

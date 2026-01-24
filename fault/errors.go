package fault

import "errors"

var (
	// ErrSystemFailure for conditions like running out of entropy.
	// These are uncoverable, catastrophic conditions and some might warrant a panic.
	// Either the system in question is very limited in unexpected ways, or it is dying.
	ErrSystemFailure = errors.New("critical system failure")

	// ErrMissingRequirements indicates that a pre-requisite is not / could not be installed.
	ErrMissingRequirements = errors.New("requirements failed")

	// ErrNotImplemented indicates a concrete structs failed to implement a required method.
	ErrNotImplemented = errors.New("not implemented")

	// ErrInvalidArgument indicates the provided argument is invalid.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrFilesystemFailure covers conditions like failing to open or close a file handler.
	// Permissions issues most of the time, disappeared data in some cases, failing hardware.
	ErrFilesystemFailure = errors.New("filesystem failure")

	// ErrReadFailure indicates the resource (file, image) could not be read (network, filesystem, permission error).
	ErrReadFailure = errors.New("failed to read resource")

	// ErrWriteFailure indicates the resource (file, image) could not be written to (network, filesystem, permission
	// error).
	ErrWriteFailure = errors.New("failed to write resource")

	// ErrNotFound indicates the requested resource (file, image, etc.) could not be found.
	ErrNotFound = errors.New("resource not found")

	// ErrAuthenticationFailure indicates an authentication attempt failed.
	ErrAuthenticationFailure = errors.New("failed to authenticate")

	// ErrCancelled indicates the operation was cancelled via context.
	ErrCancelled = errors.New("operation cancelled")

	// ErrContext is returned on context error.
	ErrContext = errors.New("context errored")

	// ErrTimeout indicates something took longer than reasonnably expected.
	ErrTimeout = errors.New("timeout")

	// ErrCommandFailure indicates a call to an external binary failed.
	ErrCommandFailure = errors.New("command failed")

	// ErrInvalidJSON indicates the provided JSON content is not valid, or the provided struct can't be marshalled.
	ErrInvalidJSON = errors.New("invalid JSON")

	// ErrHashMismatch indicates generally that a hashing method disagrees with expectations.
	// As security and content-addressability is central to us, this is a first-class error, most of the time a
	// fatal one.
	ErrHashMismatch = errors.New("hash mismatch")

	// ErrNetworkError indicates we failed to establish a connection.
	ErrNetworkError = errors.New("network error")

	// ErrUnacceptableResponse indicates an http server returned a non-OK response when we expect one.
	ErrUnacceptableResponse = errors.New("unacceptable response")
)

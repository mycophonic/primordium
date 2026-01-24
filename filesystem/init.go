package filesystem

import "math"

// Inititalize initializes the filesystem package by retrieving the current process umask.
func Inititalize(appName string) {
	// Retrieve the current umask as a starting point
	cMask := umask(0)

	if cMask > math.MaxUint32 || cMask < 0 {
		panic("currently set user umask is out of range")
	}

	currentMask = uint32(cMask)

	name = appName
}

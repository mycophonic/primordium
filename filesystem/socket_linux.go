//go:build linux

package filesystem

// maxSocketPathLen is the maximum length of a Unix socket path on this platform.
// On Linux, sun_path is 108 bytes (including null terminator).
// See: unix(7) man page, /usr/include/sys/un.h.
const maxSocketPathLen = 108

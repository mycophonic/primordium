//go:build !linux && !windows

package filesystem

// maxSocketPathLen is the maximum length of a Unix socket path on this platform.
// On macOS and BSD variants (FreeBSD, NetBSD, OpenBSD, DragonFly), sun_path is 104 bytes
// (including null terminator).
//
// References:
//   - macOS: /usr/include/sys/un.h
//   - FreeBSD: unix(4) man page
//   - NetBSD/OpenBSD: similar to FreeBSD
const maxSocketPathLen = 104

//go:build windows

package filesystem

// maxSocketPathLen is the maximum length of a Unix socket path on this platform.
// On Windows (10+), sun_path is 108 bytes (including null terminator), same as Linux.
// AF_UNIX support was added in Windows 10 Build 17063.
//
// References:
//   - https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/
//   - Windows SDK: afunix.h defines UNIX_PATH_MAX = 108
const maxSocketPathLen = 108

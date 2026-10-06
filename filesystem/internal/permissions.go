package internal

import "os"

const (
	// FilePermissionsDefault is the default file permission for newly created files.
	FilePermissionsDefault os.FileMode = 0o644
	// DirPermissionsDefault is the default directory permission for newly created directories.
	DirPermissionsDefault os.FileMode = 0o755
	// FilePermissionsPrivate is the permission for private files, only readable and writable by the owner.
	FilePermissionsPrivate os.FileMode = 0o600
	// DirPermissionsPrivate is the permission for private directories, only readable, writable, and executable by the
	// owner.
	DirPermissionsPrivate os.FileMode = 0o700
)

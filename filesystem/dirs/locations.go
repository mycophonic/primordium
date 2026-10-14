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

package dirs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/internal"
)

//nolint:gochecknoglobals // resolved once per process
var (
	nameOnce sync.Once
	name     string
)

// appName is the name SetAppName set. Without one, every directory would be
// the user's whole base directory, so asking before SetAppName panics.
func appName() string {
	if name == "" {
		panic("dirs: a directory was asked for before SetAppName")
	}

	return name
}

// HomeDir returns the current user's home directory: $HOME on Unix,
// %USERPROFILE% on Windows. Without one it is ErrSystemFailure: a process
// without a home, as a service account's or a bare container's, is the
// machine's doing, not the program's.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return home, nil
}

// underHome is the path under the home directory, or HomeDir's error.
func underHome(elem ...string) (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(append([]string{home}, elem...)...), nil
}

// RuntimeDir returns the user's runtime directory for storing sockets and other
// ephemeral runtime files. The directory is created if it doesn't exist.
//
// On Linux: $XDG_RUNTIME_DIR/<appname> (typically /run/user/<uid>/<appname>)
// On macOS: $TMPDIR/<appname> (system temp directory)
// On Windows: %TEMP%\<appname>.
func RuntimeDir() (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case osLinux:
		if xdgRuntime, ok := xdgDir("XDG_RUNTIME_DIR"); ok {
			baseDir = filepath.Join(xdgRuntime, appName())
		} else {
			baseDir = filepath.Join(os.TempDir(), appName())
		}
	default:
		// macOS, Windows, and others use temp directory
		baseDir = filepath.Join(os.TempDir(), appName())
	}

	if err := os.MkdirAll(baseDir, internal.DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return baseDir, nil
}

// DataDir returns the app-specific directory for persistent application data.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_DATA_HOME/<appname> (defaults to ~/.local/share/<appname>)
// On macOS: ~/Library/Application Support/<appname>
// On Windows: %LOCALAPPDATA%\<appname>.
func DataDir() (string, error) {
	dir, err := getDataDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(dir, internal.DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return dir, nil
}

func getDataDir() (string, error) {
	switch runtime.GOOS {
	case osDarwin:
		return underHome("Library", "Application Support", appName())
	case osLinux:
		if dataHome, ok := xdgDir("XDG_DATA_HOME"); ok {
			return filepath.Join(dataHome, appName()), nil
		}

		return underHome(".local", "share", appName())
	case osWindows:
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, appName()), nil
		}

		return underHome("AppData", "Local", appName())
	default:
		return underHome(".local", "share", appName())
	}
}

// ConfigDir returns the app-specific directory for user configuration.
// The directory is created if it doesn't exist. Nowhere to put it, no home
// and on Linux no usable $XDG_CONFIG_HOME, is ErrSystemFailure.
//
// On Linux: $XDG_CONFIG_HOME/<appname> (defaults to ~/.config/<appname>)
// On macOS: ~/Library/Application Support/<appname> (same as DataDir)
// On Windows: %AppData%\<appname> (roaming profile, syncs across machines).
func ConfigDir() (string, error) {
	base, err := getConfigBase()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(base, appName())
	if err := os.MkdirAll(configDir, internal.DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return configDir, nil
}

// getConfigBase is the user's configuration directory. On Linux it follows the
// XDG spec, which os.UserConfigDir does not: a relative $XDG_CONFIG_HOME is
// ignored there, where os.UserConfigDir fails.
func getConfigBase() (string, error) {
	if runtime.GOOS == osLinux {
		if configHome, ok := xdgDir("XDG_CONFIG_HOME"); ok {
			return configHome, nil
		}

		return underHome(".config")
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	return base, nil
}

// xdgDir returns an XDG variable's value when the spec lets it count: "All
// paths set in these environment variables must be absolute. If an
// implementation encounters a relative path in any of these variables it
// should consider the path invalid and ignore it" (XDG Base Directory 0.8).
func xdgDir(variable string) (string, bool) {
	dir := os.Getenv(variable)

	return dir, dir != "" && filepath.IsAbs(dir)
}

// CacheDir returns the app-specific directory for cached data.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/<appname> (defaults to ~/.cache/<appname>)
// On macOS: ~/Library/Caches/<appname>
// On Windows: %LOCALAPPDATA%\<appname>\cache.
func CacheDir(sub ...string) (string, error) {
	base, err := getCacheDir()
	if err != nil {
		return "", err
	}

	cacheDir := filepath.Join(append([]string{base}, sub...)...)
	if err := os.MkdirAll(cacheDir, internal.DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return cacheDir, nil
}

func getCacheDir() (string, error) {
	switch runtime.GOOS {
	case osDarwin:
		return underHome("Library", "Caches", appName())
	case osLinux:
		if xdgCache, ok := xdgDir("XDG_CACHE_HOME"); ok {
			return filepath.Join(xdgCache, appName()), nil
		}

		return underHome(".cache", appName())
	case osWindows:
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, appName(), "cache"), nil
		}

		return underHome("AppData", "Local", appName(), "cache")
	default:
		return underHome(".cache", appName())
	}
}

// BinDir returns the app-specific directory for installing tool binaries.
// This keeps the app's tools separate from the user's GOBIN/GOPATH installations.
// Binaries are stored in cache since they can be re-downloaded if needed.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/<appname>/bin (defaults to ~/.cache/<appname>/bin)
// On macOS: ~/Library/Caches/<appname>/bin
// On Windows: %LOCALAPPDATA%\<appname>\cache\bin.
func BinDir() (string, error) {
	return CacheDir("bin")
}

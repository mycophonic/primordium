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

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mycophonic/primordium/fault"
)

const (
	osDarwin  = "darwin"
	osLinux   = "linux"
	osWindows = "windows"
)

//nolint:gochecknoglobals
var name = "uninitialized"

// HomeDir returns the current user's home directory.
// Panics if the home directory cannot be determined, as this indicates
// a fundamentally broken system configuration that cannot be recovered from.
//
// On Unix/Linux/macOS: Returns $HOME
// On Windows: Returns %USERPROFILE%.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("%v: %v", fault.ErrSystemFailure, err))
	}

	return home
}

// RuntimeDir returns the user's runtime directory for storing sockets and other
// ephemeral runtime files. The directory is created if it doesn't exist.
//
// On Linux: $XDG_RUNTIME_DIR/quark (typically /run/user/<uid>/quark)
// On macOS: $TMPDIR/quark (system temp directory)
// On Windows: %TEMP%\quark.
func RuntimeDir() (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case osLinux:
		if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
			baseDir = filepath.Join(xdgRuntime, name)
		} else {
			baseDir = filepath.Join(os.TempDir(), name)
		}
	default:
		// macOS, Windows, and others use temp directory
		baseDir = filepath.Join(os.TempDir(), name)
	}

	if err := os.MkdirAll(baseDir, DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return baseDir, nil
}

// DataDir returns the quark-specific directory for persistent application data.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_DATA_HOME/quark (defaults to ~/.local/share/quark)
// On macOS: ~/Library/Application Support/quark
// On Windows: %LOCALAPPDATA%\quark.
func DataDir() (string, error) {
	dir := getDataDir()

	if err := os.MkdirAll(dir, DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return dir, nil
}

func getDataDir() string {
	switch runtime.GOOS {
	case osDarwin:
		return filepath.Join(HomeDir(), "Library", "Application Support", name)

	case osLinux:
		if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
			return filepath.Join(dataHome, name)
		}

		return filepath.Join(HomeDir(), ".local", "share", name)

	case osWindows:
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, name)
		}

		return filepath.Join(HomeDir(), "AppData", "Local", name)

	default:
		return filepath.Join(HomeDir(), ".local", "share", name)
	}
}

// ConfigDir returns the quark-specific directory for user configuration.
// The directory is created if it doesn't exist.
// Panics if the config directory cannot be determined.
//
// On Linux: $XDG_CONFIG_HOME/quark (defaults to ~/.config/quark)
// On macOS: ~/Library/Application Support/quark (same as DataDir)
// On Windows: %AppData%\quark (roaming profile, syncs across machines).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		panic(fmt.Sprintf("%v: %v", fault.ErrSystemFailure, err))
	}

	configDir := filepath.Join(base, name)

	if err := os.MkdirAll(configDir, DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return configDir, nil
}

// CacheDir returns the quark-specific directory for cached data.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/quark (defaults to ~/.cache/quark)
// On macOS: ~/Library/Caches/quark
// On Windows: %LOCALAPPDATA%\quark\cache.
func CacheDir(sub ...string) (string, error) {
	cacheDir := filepath.Join(append([]string{getCacheDir()}, sub...)...)

	if err := os.MkdirAll(cacheDir, DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return cacheDir, nil
}

func getCacheDir() string {
	switch runtime.GOOS {
	case osDarwin:
		return filepath.Join(HomeDir(), "Library", "Caches", name)

	case osLinux:
		if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
			return filepath.Join(xdgCache, name)
		}

		return filepath.Join(HomeDir(), ".cache", name)

	case osWindows:
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, name, "cache")
		}

		return filepath.Join(HomeDir(), "AppData", "Local", name, "cache")

	default:
		return filepath.Join(HomeDir(), ".cache", name)
	}
}

// BinDir returns the quark-specific directory for installing tool binaries.
// This keeps quark's tools separate from the user's GOBIN/GOPATH installations.
// Binaries are stored in cache since they can be re-downloaded if needed.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/mushroom/bin (defaults to ~/.cache/mushroom/bin)
// On macOS: ~/Library/Caches/mushroom/bin
// On Windows: %LOCALAPPDATA%\mushroom\cache\bin.
func BinDir() (string, error) {
	cacheDirectory, err := CacheDir()
	if err != nil {
		return "", err
	}

	binDirectory := filepath.Join(cacheDirectory, "bin")

	if err := os.MkdirAll(binDirectory, DirPermissionsPrivate); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return binDirectory, nil
}

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
)

//nolint:gochecknoglobals
var (
	nameOnce sync.Once
	name     string
)

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
// On Linux: $XDG_RUNTIME_DIR/<appname> (typically /run/user/<uid>/<appname>)
// On macOS: $TMPDIR/<appname> (system temp directory)
// On Windows: %TEMP%\<appname>.
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

	//nolint:gosec // G703: baseDir from TempDir+hardcoded name
	if err := os.MkdirAll(baseDir, dirPermissions); err != nil {
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
	dir := getDataDir()

	if err := os.MkdirAll(dir, dirPermissions); err != nil {
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

// ConfigDir returns the app-specific directory for user configuration.
// The directory is created if it doesn't exist.
// Panics if the config directory cannot be determined.
//
// On Linux: $XDG_CONFIG_HOME/<appname> (defaults to ~/.config/<appname>)
// On macOS: ~/Library/Application Support/<appname> (same as DataDir)
// On Windows: %AppData%\<appname> (roaming profile, syncs across machines).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		panic(fmt.Sprintf("%v: %v", fault.ErrSystemFailure, err))
	}

	configDir := filepath.Join(base, name)

	if err := os.MkdirAll(configDir, dirPermissions); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return configDir, nil
}

// CacheDir returns the app-specific directory for cached data.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/<appname> (defaults to ~/.cache/<appname>)
// On macOS: ~/Library/Caches/<appname>
// On Windows: %LOCALAPPDATA%\<appname>\cache.
func CacheDir(sub ...string) (string, error) {
	cacheDir := filepath.Join(append([]string{getCacheDir()}, sub...)...)

	if err := os.MkdirAll(cacheDir, dirPermissions); err != nil {
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

// BinDir returns the app-specific directory for installing tool binaries.
// This keeps the app's tools separate from the user's GOBIN/GOPATH installations.
// Binaries are stored in cache since they can be re-downloaded if needed.
// The directory is created if it doesn't exist.
//
// On Linux: $XDG_CACHE_HOME/<appname>/bin (defaults to ~/.cache/<appname>/bin)
// On macOS: ~/Library/Caches/<appname>/bin
// On Windows: %LOCALAPPDATA%\<appname>\cache\bin.
func BinDir() (string, error) {
	cacheDirectory, err := CacheDir()
	if err != nil {
		return "", err
	}

	binDirectory := filepath.Join(cacheDirectory, "bin")

	if err := os.MkdirAll(binDirectory, dirPermissions); err != nil {
		return "", fmt.Errorf("%w: %w", fault.ErrFilesystemFailure, err)
	}

	return binDirectory, nil
}

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

package dirs_test

// The contract, from dirs' docs and the platforms' own:
//   - Linux, the XDG Base Directory spec (0.8): data in $XDG_DATA_HOME, else
//     $HOME/.local/share; configuration in $XDG_CONFIG_HOME, else
//     $HOME/.config; cache in $XDG_CACHE_HOME, else $HOME/.cache; runtime
//     files in $XDG_RUNTIME_DIR, else (dirs' choice) the temporary directory.
//     A variable unset, empty or relative counts as absent: "All paths set in
//     these environment variables must be absolute. If an implementation
//     encounters a relative path in any of these variables it should
//     consider the path invalid and ignore it."
//   - macOS, Apple's File System Programming Guide: data and configuration
//     in ~/Library/Application Support, cache in ~/Library/Caches; runtime
//     files in the temporary directory. The XDG variables play no part.
//   - Windows, the Known Folders reference: data and cache under
//     %LOCALAPPDATA% (default %USERPROFILE%\AppData\Local), the cache in a
//     "cache" directory there; configuration under %APPDATA%; runtime files
//     in the temporary directory.
//   - Every directory is the app's own, named for it; BinDir is "bin" in the
// cache, and CacheDir's sub-path names a directory within the cache; each is created if missing, readable by its owner
// alone ("If,
//     when attempting to write a file, the destination directory is
//     non-existent an attempt should be made to create it with permission
//     0700"), and an existing one keeps its mode.
//   - SetAppName takes the first name, which must be a valid path component,
//     and ignores the rest.

import (
	"os"
	"path/filepath"
	"runtime"
)

// expected is where each directory belongs, per the contract, given the
// environment.
func expected() map[string]string {
	home := os.Getenv("HOME")

	var data, config, cache string

	switch runtime.GOOS {
	case "linux":
		data = xdgOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		config = xdgOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		cache = filepath.Join(xdgOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")), appName)
	case "darwin":
		data = filepath.Join(home, "Library", "Application Support")
		config = data
		cache = filepath.Join(home, "Library", "Caches", appName)
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}

		data = local
		config = os.Getenv("APPDATA")
		cache = filepath.Join(local, appName, "cache")
	}

	runtimeBase := os.TempDir()
	if runtime.GOOS == "linux" {
		runtimeBase = xdgOr("XDG_RUNTIME_DIR", runtimeBase)
	}

	return map[string]string{
		"DataDir":      filepath.Join(data, appName),
		"ConfigDir":    filepath.Join(config, appName),
		"CacheDir":     cache,
		"BinDir":       filepath.Join(cache, "bin"),
		"CacheDir/x/y": filepath.Join(cache, "x", "y"),
		"RuntimeDir":   filepath.Join(runtimeBase, appName),
	}
}

// xdgOr is the XDG variable's value when it is an absolute path, otherwise
// the default.
func xdgOr(variable, fallback string) string {
	if dir := os.Getenv(variable); dir != "" && filepath.IsAbs(dir) {
		return dir
	}

	return fallback
}

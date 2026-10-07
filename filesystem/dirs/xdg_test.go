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

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mycophonic/primordium/filesystem/dirs"
)

const appName = "primordium-dirs-test"

func TestMain(m *testing.M) {
	dirs.SetAppName(appName)
	m.Run()
}

// TestXDGPaths: on Linux, an absolute path in an XDG variable is honoured,
// and a relative one ignored, as the XDG Base Directory spec has it, and the
// default used.
//
//nolint:paralleltest // sets the process's environment
func TestXDGPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the XDG variables are Linux's")
	}

	variables := []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"}

	for _, absolute := range []bool{true, false} {
		home, tmp, xdg := t.TempDir(), t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("TMPDIR", tmp)

		for _, variable := range variables {
			value := filepath.Join(xdg, variable)
			if !absolute {
				value = filepath.Join("relative", variable)
			}

			t.Setenv(variable, value)
		}

		want := map[string]string{
			"DataDir":    filepath.Join(home, ".local", "share", appName),
			"ConfigDir":  filepath.Join(home, ".config", appName),
			"CacheDir":   filepath.Join(home, ".cache", appName),
			"RuntimeDir": filepath.Join(tmp, appName),
		}

		if absolute {
			want = map[string]string{
				"DataDir":    filepath.Join(xdg, "XDG_DATA_HOME", appName),
				"ConfigDir":  filepath.Join(xdg, "XDG_CONFIG_HOME", appName),
				"CacheDir":   filepath.Join(xdg, "XDG_CACHE_HOME", appName),
				"RuntimeDir": filepath.Join(xdg, "XDG_RUNTIME_DIR", appName),
			}
		}

		for name, call := range map[string]func() (string, error){
			"DataDir":    dirs.DataDir,
			"ConfigDir":  dirs.ConfigDir,
			"CacheDir":   func() (string, error) { return dirs.CacheDir() },
			"RuntimeDir": dirs.RuntimeDir,
		} {
			if dir, err := call(); err != nil || dir != want[name] {
				t.Errorf("absolute=%v: %s() = %q, %v; want %q", absolute, name, dir, err, want[name])
			}
		}
	}
}

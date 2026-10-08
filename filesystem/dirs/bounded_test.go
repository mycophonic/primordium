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

// The bounded check: every combination of the variables that bear on the
// directories, each unset, empty, absolute or relative, held to the contract.
// The tests set the process's environment, so none runs in parallel.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/filesystem/dirs"
)

const appName = "primordium-dirs-test"

// childVariable asks the test binary, run again, to try what must come in a
// process of its own, before SetAppName: "invalid", a name pathcheck refuses;
// "before:<directory>", that directory with no name set.
const childVariable = "PRIMORDIUM_DIRS_CHILD"

func TestMain(m *testing.M) {
	if mode := os.Getenv(childVariable); mode != "" {
		os.Exit(child(mode))
	}

	dirs.SetAppName(appName)
	m.Run()
}

// child runs what mode asks: 3 when it panics, as the contract says, 0 when
// it does not.
func child(mode string) (code int) {
	defer func() {
		if recover() != nil {
			code = 3
		}
	}()

	if directory, ok := strings.CutPrefix(mode, "before:"); ok {
		_, _ = directories()[directory]()

		return 0
	}

	dirs.SetAppName("..")

	return 0
}

// panicsInChild runs the test binary again in mode and checks that it
// panicked there.
func panicsInChild(t *testing.T, mode string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")

	cmd.Env = append(os.Environ(), childVariable+"="+mode)

	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("%s did not panic: %v", mode, err)
	}
}

// variables are the environment variables that bear on the directories on
// this platform.
func variables() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"LOCALAPPDATA"}
	default: // the XDG variables, which macOS must ignore
		return []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"}
	}
}

// directories are the functions under check, by name.
func directories() map[string]func() (string, error) {
	return map[string]func() (string, error){
		"DataDir":      dirs.DataDir,
		"ConfigDir":    dirs.ConfigDir,
		"CacheDir":     func() (string, error) { return dirs.CacheDir() },
		"CacheDir/x/y": func() (string, error) { return dirs.CacheDir("x", "y") },
		"BinDir":       dirs.BinDir,
		"RuntimeDir":   dirs.RuntimeDir,
	}
}

func TestBoundedDirectories(t *testing.T) {
	names := variables()
	forms := []string{"unset", "empty", "absolute", "relative"}

	combinations := 1
	for range names {
		combinations *= len(forms)
	}

	for combination := range combinations {
		t.Run("", func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("APPDATA", filepath.Join(home, "Roaming"))
			t.Setenv("TMPDIR", t.TempDir())
			t.Setenv("TEMP", os.Getenv("TMPDIR"))
			t.Setenv("TMP", os.Getenv("TMPDIR"))

			setting := map[string]string{}

			for i, name := range names {
				form := forms[combination/pow(len(forms), i)%len(forms)]
				setting[name] = form

				switch form {
				case "unset":
					t.Setenv(name, "")

					if err := os.Unsetenv(name); err != nil {
						t.Fatal(err)
					}
				case "empty":
					t.Setenv(name, "")
				case "absolute":
					t.Setenv(name, filepath.Join(t.TempDir(), name))
				case "relative":
					t.Setenv(name, filepath.Join("relative", name))
				}
			}

			want := expected()

			for name, call := range directories() {
				existed := exists(want[name])

				got, err := call()
				if err != nil || got != want[name] {
					t.Fatalf("%v: %s() = %q, %v; want %q", setting, name, got, err, want[name])
				}

				info, err := os.Stat(got)
				if err != nil || !info.IsDir() {
					t.Fatalf("%v: %s() did not create %q: %v", setting, name, got, err)
				}

				if runtime.GOOS != "windows" && !existed && info.Mode().Perm()&0o077 != 0 {
					t.Fatalf("%v: %s() created %q as %v, which others may use", setting, name, got, info.Mode().Perm())
				}
			}
		})
	}
}

// TestExistingDirectoryKeepsItsMode: a directory that exists already is not
// changed ("If the destination directory exists already the permissions
// should not be changed").
func TestExistingDirectoryKeepsItsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no mode for a directory to keep")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, name := range variables() {
		t.Setenv(name, "")
	}

	data := expected()["DataDir"]
	if err := os.MkdirAll(data, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(data, 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := dirs.DataDir(); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(data); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("an existing directory changed: %v, %v", info, err)
	}
}

// TestSetAppName: the first name counts and later ones are ignored; a name
// pathcheck refuses panics, in a run of its own, as it must come first.
func TestSetAppName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, name := range variables() {
		t.Setenv(name, "")
	}

	dirs.SetAppName("another-name")

	if got, err := dirs.DataDir(); err != nil || filepath.Base(got) != appName {
		t.Fatalf("after a second SetAppName, DataDir() = %q, %v; want it named %q", got, err, appName)
	}

	panicsInChild(t, "invalid")
}

// TestDirectoriesNeedAName: every directory asked for before SetAppName
// panics, in a run of its own, rather than handing out the base directory.
func TestDirectoriesNeedAName(t *testing.T) {
	t.Parallel()

	for directory := range directories() {
		panicsInChild(t, "before:"+directory)
	}
}

// TestHomeDirPanics: HomeDir panics when the home cannot be determined.
func TestHomeDirPanics(t *testing.T) {
	variable := "HOME"
	if runtime.GOOS == "windows" {
		variable = "USERPROFILE"
	}

	t.Setenv(variable, "")

	defer func() {
		if recover() == nil {
			t.Fatal("HomeDir did not panic with no home")
		}
	}()

	_ = dirs.HomeDir()
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func pow(base, exponent int) int {
	result := 1
	for range exponent {
		result *= base
	}

	return result
}

// TestConfigDirPanics: ConfigDir panics when the configuration directory cannot
// be determined, as it documents: no $HOME (and on Linux no usable
// $XDG_CONFIG_HOME), or on Windows no %APPDATA%.
//
//nolint:paralleltest // sets the process's environment
func TestConfigDirPanics(t *testing.T) {
	for _, variable := range []string{"HOME", "USERPROFILE", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(variable, "")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("ConfigDir did not panic with nowhere to put configuration")
		}
	}()

	_, _ = dirs.ConfigDir()
}

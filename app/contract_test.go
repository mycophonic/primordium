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

package app_test

// The contract, from app's docs:
//   - New without a Name panics; with one, the directories carry the name,
//     the logger is installed, and no DSN is a warning, not a failure;
//   - the context New returns is cancelled by a shutdown signal (shutdown's
//     tests hold that mechanism; here the context is live after New).
//
// New installs process-wide defaults, so each case is a child: this binary,
// told by PRIMORDIUM_APP_CHILD what to do.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/app"
	"github.com/mycophonic/primordium/filesystem/dirs"
)

const childEnv = "PRIMORDIUM_APP_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "no-name":
		app.New(context.Background(), &app.Options{})
	case "named":
		ctx := app.New(context.Background(), &app.Options{Name: "primordium-app-test"})

		cache, err := dirs.CacheDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		fmt.Fprintln(os.Stdout, cache)
		fmt.Fprintln(os.Stdout, "context live:", ctx.Err() == nil)
	default:
		os.Exit(99)
	}
}

func child(t *testing.T, mode string) (stdout, stderr string, code int) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	// The child's directories land under a temporary home.
	home := t.TempDir()

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"="+mode,
		"HOME="+home, "XDG_CACHE_HOME="+home, "LOCALAPPDATA="+home)

	var out, errOut strings.Builder

	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err = cmd.Run()

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return out.String(), errOut.String(), exit.ExitCode()
	}

	if err != nil {
		t.Fatal(err)
	}

	return out.String(), errOut.String(), 0
}

func TestNoNamePanics(t *testing.T) {
	t.Parallel()

	_, stderr, code := child(t, "no-name")

	if code != 2 || !strings.Contains(stderr, "Options.Name") {
		t.Fatalf("exit %d with %q", code, stderr)
	}
}

func TestNamed(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := child(t, "named")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}

	if !strings.Contains(stdout, "primordium-app-test") {
		t.Errorf("the cache directory does not carry the name: %q", stdout)
	}

	if !strings.Contains(stdout, "context live: true") {
		t.Errorf("the context is cancelled before any signal: %q", stdout)
	}

	// The logger is in place, JSON off a terminal, and the missing DSN is a
	// warning on it.
	if !strings.Contains(stderr, `"level":"WARN"`) || !strings.Contains(stderr, "dsn not provided") {
		t.Errorf("no DSN warning on the installed logger: %q", stderr)
	}
}

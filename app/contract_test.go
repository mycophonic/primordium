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
//   - Main without a Name panics; with one, the directories carry the name,
//     the logger is installed, and no DSN is a warning, not a failure;
//   - run gets a context a shutdown signal cancels (shutdown's tests hold
//     that mechanism; here the context is live inside run); when run returns,
//     the handlers run and the process exits with the ExitError run asked for.
//
// Main installs process-wide defaults and exits, so each case is a child: this
// binary, told by PRIMORDIUM_APP_CHILD what to do.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/app"
	"github.com/mycophonic/primordium/app/shutdown"
	"github.com/mycophonic/primordium/filesystem/dirs"
)

const childEnv = "PRIMORDIUM_APP_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "no-name":
		app.Main(&app.Options{}, func(context.Context) error { return nil })
	case "named":
		shutdown.Register(func() { fmt.Fprintln(os.Stdout, "handler ran") })

		app.Main(&app.Options{Name: "primordium-app-test"}, func(ctx context.Context) error {
			cache, err := dirs.CacheDir()
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stdout, cache)
			fmt.Fprintln(os.Stdout, "context live:", ctx.Err() == nil)

			return shutdown.ExitError(3)
		})
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
	if code != 3 {
		t.Fatalf("exit %d, want the ExitError run asked for: %s", code, stderr)
	}

	if !strings.Contains(stdout, "primordium-app-test") {
		t.Errorf("the cache directory does not carry the name: %q", stdout)
	}

	if !strings.Contains(stdout, "context live: true") {
		t.Errorf("the context is cancelled before any signal: %q", stdout)
	}

	if !strings.HasSuffix(stdout, "handler ran\n") {
		t.Errorf("the handlers did not run after run returned: %q", stdout)
	}

	if strings.Contains(stderr, "fatal") {
		t.Errorf("Main said what the program asked for: %q", stderr)
	}

	// The logger is in place, JSON off a terminal, and the missing DSN is a
	// warning on it.
	if !strings.Contains(stderr, `"level":"WARN"`) || !strings.Contains(stderr, "dsn not provided") {
		t.Errorf("no DSN warning on the installed logger: %q", stderr)
	}
}

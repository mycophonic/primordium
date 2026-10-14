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

package shutdown_test

// The contract, from shutdown's docs:
//   - Shutdown runs the handlers in reverse order of registration, once;
//   - Run runs fn, then the handlers, and exits 0 on a nil return, with an
//     ExitError returned or wrapped and nothing said, 1 with the error logged
//     on any other error, 1 on a panic; Go recovers a goroutine's panic the
//     same way; Exit runs the handlers and exits with its code;
//   - a shutdown signal cancels every context SetDefaults handed out, runs
//     the handlers and exits with 128 plus the signal; handlers that overrun
//     ten seconds end the process with 1.
//
// Run, Go and the signals end the process, so those run in a child: this
// binary, told by PRIMORDIUM_SHUTDOWN_CHILD what to do.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mycophonic/primordium/app/shutdown"
)

const childEnv = "PRIMORDIUM_SHUTDOWN_CHILD"

var errFailed = errors.New("failed")

func TestMain(m *testing.M) {
	mode := os.Getenv(childEnv)
	if mode == "" {
		m.Run()

		return
	}

	say := func(line string) { fmt.Fprintln(os.Stdout, line) }

	shutdown.Register(func() { say("handler 1") })
	shutdown.Register(func() { say("handler 2") })

	switch mode {
	case "shutdown-twice":
		shutdown.Register(func() { say("handler 3") })
		shutdown.Shutdown()
		shutdown.Shutdown()
		say("done")
	case "run-ok":
		shutdown.Run(context.Background(), func(context.Context) error { say("body"); return nil })
	case "run-error":
		shutdown.Run(context.Background(), func(context.Context) error { return errFailed })
	case "run-status":
		shutdown.Run(context.Background(), func(context.Context) error {
			say("body")

			return fmt.Errorf("asked for: %w", shutdown.ExitError(2))
		})
	case "exit":
		say("body")
		shutdown.Exit(3)
	case "run-panic":
		shutdown.Run(context.Background(), func(context.Context) error { panic("boom") })
	case "go-panic":
		shutdown.Go(func() { panic("boom") })

		select {}
	case "signal", "signal-slow":
		first := shutdown.SetDefaults(context.Background())
		second := shutdown.SetDefaults(context.Background())

		if mode == "signal-slow" {
			shutdown.Register(func() { time.Sleep(time.Minute) })
		}

		// A handler sees the contexts: it runs after the cancellation, on the
		// goroutine that then exits, where a waiting main goroutine might not
		// be scheduled again before the exit.
		shutdown.Register(func() {
			if first.Err() != nil && second.Err() != nil {
				say("cancelled")
			}
		})

		say("ready")

		select {}
	default:
		os.Exit(99)
	}
}

// child runs this binary in mode and returns its stdout, its stderr and its
// exit code.
func child(t *testing.T, mode string, signal func(*os.Process)) (stdout, stderr string, code int) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"="+mode)

	var errOut strings.Builder

	cmd.Stderr = &errOut

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder

	buf := make([]byte, 4096)

	for {
		n, readErr := pipe.Read(buf)
		out.Write(buf[:n])

		if signal != nil && strings.Contains(out.String(), "ready\n") {
			signal(cmd.Process)

			signal = nil
		}

		if readErr != nil {
			break
		}
	}

	err = cmd.Wait()

	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return out.String(), errOut.String(), exit.ExitCode()
	}

	if err != nil {
		t.Fatal(err)
	}

	return out.String(), errOut.String(), 0
}

const handlersReversed = "handler 2\nhandler 1\n"

// TestShutdownOrderOnce: three handlers, two Shutdown calls, in a child, since
// Shutdown runs once per process.
func TestShutdownOrderOnce(t *testing.T) {
	t.Parallel()

	out, _, code := child(t, "shutdown-twice", nil)

	if want := "handler 3\n" + handlersReversed + "done\n"; out != want || code != 0 {
		t.Fatalf("exit %d with %q, want 0 with %q", code, out, want)
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	for mode, want := range map[string]struct {
		out    string
		code   int
		logged string
	}{
		"run-ok":     {"body\n" + handlersReversed, 0, ""},
		"run-error":  {handlersReversed, 1, "fatal"},
		"run-status": {"body\n" + handlersReversed, 2, ""},
		"run-panic":  {handlersReversed, 1, "panic"},
		"go-panic":   {handlersReversed, 1, "goroutine panic"},
		"exit":       {"body\n" + handlersReversed, 3, ""},
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			out, logged, code := child(t, mode, nil)
			if out != want.out || code != want.code {
				t.Fatalf("exit %d with %q, want %d with %q", code, out, want.code, want.out)
			}

			// What a program asked for is said by the program, not by Run.
			if want.logged == "" && (strings.Contains(logged, "fatal") || strings.Contains(logged, "panic")) {
				t.Fatalf("Run said %q, want nothing", logged)
			}

			if want.logged != "" && !strings.Contains(logged, want.logged) {
				t.Fatalf("Run said %q, want a %q line", logged, want.logged)
			}
		})
	}
}

func TestSignal(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot deliver SIGTERM to another process")
	}

	out, _, code := child(t, "signal", func(proc *os.Process) { _ = proc.Signal(syscall.SIGTERM) })

	if want := "ready\ncancelled\n" + handlersReversed; out != want || code != 128+int(syscall.SIGTERM) {
		t.Fatalf("exit %d with %q, want %d with %q", code, out, 128+int(syscall.SIGTERM), want)
	}
}

// TestSignalHandlersOverrun: a handler that never finishes ends the process
// with 1 after the ten-second limit.
func TestSignalHandlersOverrun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot deliver SIGTERM to another process")
	}

	start := time.Now()
	out, _, code := child(t, "signal-slow", func(proc *os.Process) { _ = proc.Signal(syscall.SIGTERM) })

	if code != 1 || !strings.HasPrefix(out, "ready\ncancelled\n") {
		t.Fatalf("exit %d with %q", code, out)
	}

	if elapsed := time.Since(start); elapsed < 10*time.Second {
		t.Fatalf("gave up after %v", elapsed)
	}
}

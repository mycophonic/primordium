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

package process_test

// The contract, from process's docs:
//   - StopProcess asks the process to exit and waits up to shutdownTimeout
//     on exited, then kills it and waits up to ten seconds more, after which
//     ErrTimeout; on Windows it kills at once;
//   - a process that exits on request is stopped within shutdownTimeout; one
//     that ignores the request is stopped after it; one already gone is nil;
//   - a nil process is nothing to stop; a nil exited is ErrInvalidArgument;
//     a zero shutdownTimeout kills at once.
//
// The child is this test binary, told by PRIMORDIUM_PROCESS_CHILD to exit,
// to sleep, or to ignore SIGTERM and sleep.

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/system/process"
)

const childEnv = "PRIMORDIUM_PROCESS_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "exit":
		ready()
		os.Exit(0)
	case "stubborn":
		signal.Ignore(syscall.SIGTERM)

		fallthrough
	case "sleep":
		ready()
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

// ready tells the parent the child is set up: a request before that would be
// honoured by a child still installing its handler.
func ready() {
	_, _ = os.Stdout.WriteString("ready\n")
}

// child starts this binary in the given mode and returns its process and
// the channel closed when its Wait returns.
func child(t *testing.T, mode string) (*os.Process, <-chan struct{}) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"="+mode)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	if _, err = bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(exited)
	}()

	return cmd.Process, exited
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestNilProcess(t *testing.T) {
	t.Parallel()

	if err := process.StopProcess(nil, time.Second, make(chan struct{})); err != nil {
		t.Fatal(err)
	}
}

func TestNilExited(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "sleep")

	err := process.StopProcess(proc, time.Second, nil)
	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("nil exited: %v", err)
	}

	if closed(exited) {
		t.Fatal("the process was touched")
	}
}

func TestExitsOnRequest(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "sleep")

	start := time.Now()

	if err := process.StopProcess(proc, 5*time.Second, exited); err != nil {
		t.Fatal(err)
	}

	if !closed(exited) {
		t.Fatal("returned before the process exited")
	}

	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Fatalf("took %v, the request was not honoured", elapsed)
	}
}

func TestIgnoresRequest(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "stubborn")

	start := time.Now()

	if err := process.StopProcess(proc, 300*time.Millisecond, exited); err != nil {
		t.Fatal(err)
	}

	if !closed(exited) {
		t.Fatal("returned before the process exited")
	}

	// Windows kills at once; the others wait the timeout out first.
	if elapsed := time.Since(start); runtime.GOOS != "windows" && elapsed < 300*time.Millisecond {
		t.Fatalf("killed after %v, before the timeout", elapsed)
	}
}

func TestZeroTimeoutKillsAtOnce(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "stubborn")

	if err := process.StopProcess(proc, 0, exited); err != nil {
		t.Fatal(err)
	}

	if !closed(exited) {
		t.Fatal("returned before the process exited")
	}
}

func TestAlreadyExited(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "exit")
	<-exited

	if err := process.StopProcess(proc, time.Second, exited); err != nil {
		t.Fatal(err)
	}
}

// TestNeverExits: exited never closes, so the kill wait runs out.
func TestNeverExits(t *testing.T) {
	t.Parallel()

	proc, exited := child(t, "exit")
	<-exited

	start := time.Now()
	err := process.StopProcess(proc, 0, make(chan struct{}))

	if !errors.Is(err, fault.ErrTimeout) {
		t.Fatalf("never-closing exited: %v", err)
	}

	if elapsed := time.Since(start); elapsed < 10*time.Second {
		t.Fatalf("gave up after %v", elapsed)
	}
}

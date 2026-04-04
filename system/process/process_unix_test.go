//go:build unix

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

//revive:disable:add-constant
package process_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/system/process"
)

// startSleepProcess starts a "sleep" subprocess and returns its os.Process
// and an exited channel that closes when the process exits.
func startSleepProcess(t *testing.T) (*os.Process, <-chan struct{}) {
	t.Helper()

	cmd := exec.Command("sleep", "300")
	assert.NilError(t, cmd.Start())

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(exited)
	}()

	return cmd.Process, exited
}

func TestStopProcess_NilProcess(t *testing.T) {
	t.Parallel()

	exited := make(chan struct{})
	err := process.StopProcess(nil, time.Second, exited)
	assert.NilError(t, err)
}

func TestStopProcess_GracefulShutdown(t *testing.T) {
	t.Parallel()

	proc, exited := startSleepProcess(t)

	err := process.StopProcess(proc, 5*time.Second, exited)
	assert.NilError(t, err)

	// Process should have exited.
	select {
	case <-exited:
	default:
		t.Fatal("process did not exit after StopProcess returned")
	}
}

func TestStopProcess_AlreadyExited(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("true")
	assert.NilError(t, cmd.Start())

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(exited)
	}()

	// Wait for it to actually exit.
	<-exited

	err := process.StopProcess(cmd.Process, time.Second, exited)
	assert.NilError(t, err)
}

func TestStopProcess_EscalatesToKill(t *testing.T) {
	t.Parallel()

	// Use a shell that traps SIGTERM and ignores it, forcing escalation to SIGKILL.
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 300")
	assert.NilError(t, cmd.Start())

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(exited)
	}()

	// Give the shell a moment to set up the trap.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	err := process.StopProcess(cmd.Process, 500*time.Millisecond, exited)
	elapsed := time.Since(start)

	assert.NilError(t, err)

	// Should have waited at least the shutdown timeout before killing.
	assert.Assert(t, elapsed >= 400*time.Millisecond,
		"expected at least 400ms for SIGTERM timeout, got %v", elapsed)

	select {
	case <-exited:
	default:
		t.Fatal("process did not exit after SIGKILL escalation")
	}
}

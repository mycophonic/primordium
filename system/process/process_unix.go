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

package process

import (
	"log/slog"
	"os"
	"syscall"
	"time"
)

// StopProcess sends SIGTERM, waits up to shutdownTimeout for the process to
// exit (via the exited channel), then sends SIGKILL.
func StopProcess(proc *os.Process, shutdownTimeout time.Duration, exited <-chan struct{}) error {
	if proc == nil {
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited.
		return waitForExit(exited)
	}

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	select {
	case <-exited:
		return nil
	case <-timer.C:
		slog.Warn("process did not exit within timeout, killing")

		if err := proc.Kill(); err != nil {
			slog.Warn("SIGKILL failed", "error", err)

			return waitForExit(exited)
		}

		return waitForExit(exited)
	}
}

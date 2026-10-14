//go:build !windows

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
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/mycophonic/primordium/fault"
)

// StopProcess sends proc SIGTERM and waits up to shutdownTimeout for exited,
// which the caller closes when its Wait returns; then SIGKILL and up to ten
// seconds more, after which ErrTimeout. A nil proc is nothing to stop; a nil
// exited is ErrInvalidArgument, since it could never close; a shutdownTimeout
// of zero or less kills at once.
func StopProcess(proc *os.Process, shutdownTimeout time.Duration, exited <-chan struct{}) error {
	if exited == nil {
		return fmt.Errorf("%w: StopProcess needs the channel closed when the process exits", fault.ErrInvalidArgument)
	}

	if proc == nil {
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Already gone, or being reaped: its Wait is about to return.
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
		}

		return waitForExit(exited)
	}
}

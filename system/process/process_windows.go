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
	"os"
	"time"

	"github.com/mycophonic/primordium/fault"
)

// StopProcess kills proc, Windows having no request to exit, and waits up to
// ten seconds for exited, which the caller closes when its Wait returns,
// after which ErrTimeout. A nil proc is nothing to stop; a nil exited is
// ErrInvalidArgument, since it could never close.
func StopProcess(proc *os.Process, _ time.Duration, exited <-chan struct{}) error {
	if exited == nil {
		return fmt.Errorf("%w: StopProcess needs the channel closed when the process exits", fault.ErrInvalidArgument)
	}

	if proc == nil {
		return nil
	}

	// A failure means the process is already gone: its Wait is about to return.
	_ = proc.Kill()

	return waitForExit(exited)
}

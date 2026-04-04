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
	"time"

	"github.com/mycophonic/primordium/fault"
)

// waitForExit waits for the process to exit with a bounded timeout.
func waitForExit(exited <-chan struct{}) error {
	timer := time.NewTimer(killTimeout)
	defer timer.Stop()

	select {
	case <-exited:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: process did not exit after SIGKILL", fault.ErrTimeout)
	}
}

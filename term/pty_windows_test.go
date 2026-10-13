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

package term_test

import (
	"os"
	"testing"
)

// openTerminal is the console's input, when the process has a console.
func openTerminal(t *testing.T) *os.File {
	t.Helper()

	console, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no console on this host: %v", err)
	}

	t.Cleanup(func() { _ = console.Close() })

	return console
}

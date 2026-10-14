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
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openTerminal is the slave side of a fresh pseudo-terminal; the master
// side is not a terminal to isatty on macOS. The slave is /dev/ttysNNN for
// the master's minor number, which spares the TIOCPTYGNAME ioctl x/sys
// has no wrapper for.
func openTerminal(t *testing.T) *os.File {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal on this host: %v", err)
	}

	t.Cleanup(func() { _ = master.Close() })

	fd := int(master.Fd())

	if err = unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatal(err)
	}

	if err = unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatal(err)
	}

	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("/dev/ttys%03d", unix.Minor(uint64(stat.Rdev)))

	slave, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = slave.Close() })

	return slave
}

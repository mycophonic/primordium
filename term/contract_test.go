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

// The contract, from term's docs: IsTerminal is true for a terminal, the
// console or a pseudo-terminal, and false for a file, a pipe or a closed
// descriptor.

import (
	"os"
	"testing"

	"github.com/mycophonic/primordium/term"
)

func TestNotTerminals(t *testing.T) {
	t.Parallel()

	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = null.Close() }()

	if term.IsTerminal(null.Fd()) {
		t.Error("the null device is a terminal")
	}

	file, err := os.CreateTemp(t.TempDir(), "plain")
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = file.Close() }()

	if term.IsTerminal(file.Fd()) {
		t.Error("a regular file is a terminal")
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reader.Close(); _ = writer.Close() }()

	if term.IsTerminal(reader.Fd()) || term.IsTerminal(writer.Fd()) {
		t.Error("a pipe is a terminal")
	}
}

func TestTerminal(t *testing.T) {
	t.Parallel()

	tty := openTerminal(t)

	if !term.IsTerminal(tty.Fd()) {
		t.Error("a terminal is not a terminal")
	}
}

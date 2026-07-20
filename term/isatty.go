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

package term

import "github.com/mattn/go-isatty"

// IsTerminal reports whether the file descriptor is a terminal.
//
// Beyond the ordinary termios/console check, it also treats a Cygwin/MSYS2
// pseudo-terminal (Git Bash, mintty) as a terminal — which plain
// isatty.IsTerminal and golang.org/x/term.IsTerminal both miss on Windows,
// where such a pty is a named pipe rather than a console. On every non-Windows
// platform isatty.IsCygwinTerminal is a constant false, so there this reduces
// to the ordinary terminal check.
func IsTerminal(fd uintptr) bool {
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

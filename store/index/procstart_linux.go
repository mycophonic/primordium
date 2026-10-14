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

package index

import (
	"os"
	"strconv"
	"strings"
)

// statStartField is the index of starttime among the fields that follow the
// command name in /proc/<pid>/stat: field 22 of the line, the name being
// field 2 and the first field after it, the state, field 3.
const statStartField = 22 - 3

// processStart is the process's start time in clock ticks since boot, from
// /proc/<pid>/stat. The command name there may hold spaces and parentheses,
// so the fields are counted from its closing parenthesis.
func processStart(pid int) (uint64, bool) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}

	line := string(stat)

	end := strings.LastIndexByte(line, ')')
	if end < 0 {
		return 0, false
	}

	fields := strings.Fields(line[end+1:])
	if len(fields) <= statStartField {
		return 0, false
	}

	started, err := strconv.ParseUint(fields[statStartField], 10, 64)
	if err != nil {
		return 0, false
	}

	return started, true
}

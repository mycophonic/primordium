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

package rlimit

// desiredNofile is a safety-net floor, not the actual limit inherited by
// children.  In practice, the inherited limit is whatever the OS hard
// limit is (minus one), which is typically much higher (1048576 on Linux,
// unlimited on macOS).  This constant only matters when the hard limit
// itself is unusually low (e.g., a misconfigured container).
const desiredNofile = 65536

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

// Package index implements a concurrent-safe flat-file hash index using
// memory-mapped I/O with shared-memory locking.
//
// Keys are uint64 identifiers. Values are opaque byte slices of a fixed width
// configured at creation time (default 65 bytes). Each record includes a
// timestamp. The index uses open-addressing with linear probing.
//
// In-process safety is provided by sync.RWMutex. Cross-process safety uses an
// atomic lock word embedded in the mmap'd header (offset 40): bit 63 is the
// write flag, bits 62–32 hold the writer PID, and bits 31–0 hold the reader
// count. Stale writers are detected via PID liveness checks after a spin
// threshold. Stale readers are detected via PID slots in the mmap'd sidecar
// .lock file: each reader process claims a slot before incrementing the lock
// word and clears it after decrementing. Writers scan the slots and force
// acquire the lock when all registered PIDs are dead. The lock file has 64
// PID slots; if more than 64 processes hold concurrent read locks, the excess
// proceed without a slot and stale-reader recovery may not detect them.
// The .lock file also provides advisory flock for New/grow serialization.
package index

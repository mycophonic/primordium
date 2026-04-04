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

const (
	magic      = 0x5052494D // "PRIM"
	version    = 1
	headerSize = 48

	keySize        = 8
	tsSize         = 8
	defaultValSize = 65

	statusEmpty    = 0x00
	statusOccupied = 0x01
	statusDeleted  = 0x02

	maxLoadFactor = 0.70
	defaultCap    = 1024
	growFactor    = 2

	// Lock word layout (uint64 at header offset 40):
	//   Bit 63:     write flag
	//   Bits 62-32: writer PID (31 bits)
	//   Bits 31-0:  reader count
	lockOffset    = 40
	lockWriteFlag = uint64(1) << 63
	lockPIDShift  = 32
	lockPIDMask   = uint64(0x7FFFFFFF) << lockPIDShift

	// Spin thresholds for cross-process lock acquisition.
	spinYieldCount    = 100
	stalePIDThreshold = 1000

	// Reader PID slot tracking in the lock file. Each concurrent reader
	// process claims one slot. If all slots are full, the reader proceeds
	// without a slot; stale-reader recovery cannot detect such readers
	// and may allow a writer to acquire the lock while they are active.
	maxReaderSlots = 64
	lockFileSize   = maxReaderSlots * 4 // 256 bytes
)

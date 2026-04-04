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

package cache

import "time"

const (
	cacheDataFile     = "data"
	cacheDataFileTemp = "_data"
	cacheLockFile     = "lock.read"
	cacheWriteLock    = "lock.write"
	cachePollInterval = 10 * time.Millisecond

	// DefaultCacheQuota is the default disk space quota for the cache (50GB).
	DefaultCacheQuota = 50 << 30

	errFmtEntryDir = "%w: entry directory: %w"
)

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

package transporter

import "time"

const (
	jitterMin   = 0.75 // ±25% jitter: multiplier range [0.75, 1.25].
	jitterRange = 0.5

	// slowRequestThreshold logs successful HTTP requests that exceed this
	// duration. Measured around the actual RoundTrip only (excludes rate
	// limiter wait and retry backoff).
	slowRequestThreshold = 10 * time.Second

	// progressInterval is the byte interval at which response body reads
	// are logged. Only responses whose body exceeds this threshold produce
	// any progress output.
	progressInterval int64 = 100 * 1024 * 1024 //nolint:mnd // 100 MiB.
)

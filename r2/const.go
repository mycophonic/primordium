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

package r2

const (
	// Download.

	progressBytes = 50 << 20 // Log progress every 50 MB.

	// Multi-part upload.

	minPartSize      = 5 << 20   // 5 MiB — R2/S3 minimum.
	defaultPartSize  = 100 << 20 // 100 MiB.
	maxParts         = 10_000    // S3/R2 maximum.
	listPartsMaxKeys = 1000      // S3 ListParts page size.
	stateFileMode    = 0o600
)

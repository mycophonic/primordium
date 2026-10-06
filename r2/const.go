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

import "github.com/mycophonic/primordium/bytesize"

const (
	// Download.

	progressBytes  = 50 * bytesize.MiB // Log progress every 50 MiB.
	copyBufferSize = 32 * bytesize.KiB

	// Multi-part upload.

	minPartSize      = 5 * bytesize.MiB // R2/S3 minimum.
	defaultPartSize  = 100 * bytesize.MiB
	maxParts         = 10_000 // S3/R2 maximum.
	listPartsMaxKeys = 1000   // S3 ListParts page size.
)

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

package sqlite

import (
	"fmt"

	"github.com/mycophonic/primordium/fault"
)

// The errors this package returns, each wrapping its fault class: ErrOpen and
// ErrClose are the package's own, ErrRead and ErrWrite are fault's.
var (
	ErrOpen  = fmt.Errorf("%w: sqlite open failure", fault.ErrFilesystemFailure)
	ErrClose = fmt.Errorf("%w: sqlite close failure", fault.ErrFilesystemFailure)
	ErrRead  = fault.ErrReadFailure
	ErrWrite = fault.ErrWriteFailure
)

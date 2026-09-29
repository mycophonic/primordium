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

package human

import "errors"

var (
	// ErrInvalidSize is returned when the number of a size is not decimal
	// digits with an optional fraction, or the size is too large for an
	// int64 count of bytes.
	ErrInvalidSize = errors.New("invalid size")
	// ErrInvalidUnit is returned when the unit of a size is not one of the
	// symbols ParseSize accepts.
	ErrInvalidUnit = errors.New("invalid unit")
)

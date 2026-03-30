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

package pathcheck

import (
	"errors"
	"fmt"

	"github.com/mycophonic/primordium/fault"
)

var (
	errInvalidPathTooLong = errors.New("path component must be strictly shorter than 256 characters")
	errInvalidPathEmpty   = errors.New("path component cannot be empty")

	errForbiddenChars    = errors.New("forbidden characters in path component")
	errForbiddenKeywords = errors.New("forbidden keywords in path component")

	// ErrInvalidPath is returned when a path is invalid.
	ErrInvalidPath = fmt.Errorf("%w: invalid filesystem path", fault.ErrInvalidArgument)
)

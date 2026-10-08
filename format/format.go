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

package format

import (
	"fmt"
	"io"

	"github.com/mycophonic/primordium/fault"
)

// Kind identifies an output format.
type Kind string

// Data holds the information to be formatted.
type Data struct {
	Object string         `json:"object"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// Formatter renders entries to a writer: JSON as one array, the others one
// after another with a rule between.
type Formatter interface {
	// PrintAll renders every entry, in order, and returns the first write's
	// error as ErrWriteFailure.
	PrintAll(data []*Data, writer io.Writer) error
}

// New returns the formatter of a kind; a Kind that names none is
// ErrInvalidArgument.
func New(kind Kind) (Formatter, error) {
	switch kind {
	case KindJSON:
		return jsonFormatter{}, nil
	case KindMarkdown:
		return markdown{}, nil
	case KindConsole:
		return console{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", fault.ErrInvalidArgument, kind)
	}
}

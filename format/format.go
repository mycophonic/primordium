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
	"errors"
	"fmt"
	"io"
)

// ErrUnknownFormat indicates an unsupported output format was requested.
var ErrUnknownFormat = errors.New("unknown format")

// Data holds the information to be formatted.
type Data struct {
	Object string         `json:"object"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// Formatter defines the interface for output formatters.
type Formatter interface {
	// PrintAll writes multiple data entries to the writer.
	// For JSON, this outputs an array. For other formats, entries are separated.
	PrintAll(data []*Data, writer io.Writer) error
}

// GetFormatter returns a formatter for the given format name.
func GetFormatter(format string) (Formatter, error) {
	switch format {
	case "json":
		return &JSON{}, nil
	case "markdown":
		return &Markdown{}, nil
	case "console":
		return &Console{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, format)
	}
}

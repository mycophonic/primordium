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
	"slices"
	"strings"

	"github.com/mycophonic/primordium/fault"
)

// printer writes to one io.Writer and keeps the first error: every print
// after it is a no-op, and done returns it, wrapped once. The formatters
// render without a check at every line, and a failure anywhere is a failure.
type printer struct {
	writer io.Writer
	err    error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.writer, format, args...)
	}
}

func (p *printer) done() error {
	if p.err != nil {
		return fmt.Errorf("%w: %w", fault.ErrWriteFailure, p.err)
	}

	return nil
}

// sortedKeys orders a map's keys, so that a rendering is the same every time.
func sortedKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

// separateFields splits a map into its scalar fields and its nested ones,
// maps and slices, which the formatters render differently.
func separateFields(data map[string]any) (scalars, nested map[string]any) {
	scalars = make(map[string]any)
	nested = make(map[string]any)

	for key, value := range data {
		switch value.(type) {
		case map[string]any, []any:
			nested[key] = value
		default:
			scalars[key] = value
		}
	}

	return scalars, nested
}

func indentation(level int) string {
	return strings.Repeat(indentUnit, level)
}

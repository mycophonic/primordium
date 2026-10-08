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
)

// console renders key: value lines, nested values indented under their key.
type console struct{}

// PrintAll writes all data entries with horizontal rule separators.
func (console) PrintAll(data []*Data, writer io.Writer) error {
	out := &printer{writer: writer}

	for i, entry := range data {
		if i > 0 {
			out.printf("\n%s\n\n", ruleSeparator)
		}

		out.printf("Path: %s\n", entry.Object)

		if len(entry.Meta) > 0 {
			out.printf("\n")
			out.consoleMap(entry.Meta, 0)
		}
	}

	return out.done()
}

func (p *printer) consoleMap(meta map[string]any, indent int) {
	for _, key := range sortedKeys(meta) {
		p.consoleValue(indentation(indent)+key, meta[key], indent)
	}
}

// consoleValue renders one labelled value: a scalar on the label's line, a
// map or a slice below it, one level in.
func (p *printer) consoleValue(label string, value any, indent int) {
	switch typed := value.(type) {
	case map[string]any:
		p.printf("%s:\n", label)
		p.consoleMap(typed, indent+1)
	case []any:
		p.printf("%s:\n", label)
		p.consoleSlice(typed, indent+1)
	default:
		p.printf("%s: %v\n", label, typed)
	}
}

func (p *printer) consoleSlice(slice []any, indent int) {
	for index, item := range slice {
		p.consoleValue(fmt.Sprintf("%s[%d]", indentation(indent), index), item, indent)
	}
}

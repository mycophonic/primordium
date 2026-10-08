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
	"strings"
)

// Markdown renders headings for nested values and tables for scalar fields.
type Markdown struct{}

// PrintAll writes all data entries with horizontal rule separators.
func (*Markdown) PrintAll(data []*Data, writer io.Writer) error {
	out := &printer{writer: writer}

	for i, entry := range data {
		if i > 0 {
			out.printf("\n%s\n\n", mdRuleSeparator)
		}

		out.printf("## %s\n\n", entry.Object)

		if len(entry.Meta) > 0 {
			out.markdownFields(entry.Meta, 3)
		}
	}

	return out.done()
}

// markdownFields renders a map: its scalar fields as one table, then each
// nested field as a section headed at level.
func (p *printer) markdownFields(data map[string]any, level int) {
	scalars, nested := separateFields(data)

	if len(scalars) > 0 {
		p.markdownTable(scalars)
	}

	for _, key := range sortedKeys(nested) {
		p.printf("%s %s\n\n", heading(level), key)
		p.markdownNested(nested[key], level)
	}
}

// markdownNested renders what sits under a heading at level: a map's fields,
// or a slice's items.
func (p *printer) markdownNested(value any, level int) {
	switch typed := value.(type) {
	case map[string]any:
		p.markdownFields(typed, level+1)
	case []any:
		p.markdownItems(typed, level)
	}
}

// markdownItems renders a slice's items under a heading at level: a scalar
// as a list entry, a map or a slice as a numbered section one level down.
func (p *printer) markdownItems(slice []any, level int) {
	listed := false

	for index, item := range slice {
		switch item.(type) {
		case map[string]any, []any:
			if listed {
				p.printf("\n")

				listed = false
			}

			p.printf("%s Item %d\n\n", heading(level+1), index+1)
			p.markdownNested(item, level+1)
		default:
			p.printf("- %s\n", oneLine(fmt.Sprintf("%v", item)))

			listed = true
		}
	}

	if listed {
		p.printf("\n")
	}
}

func (p *printer) markdownTable(fields map[string]any) {
	p.printf("| Field | Value |\n|-------|-------|\n")

	for _, key := range sortedKeys(fields) {
		p.printf("| %s | %s |\n", cell(key), cell(fmt.Sprintf("%v", fields[key])))
	}

	p.printf("\n")
}

// heading is the ATX marker for a level; Markdown has six.
func heading(level int) string {
	return strings.Repeat(headingChar, min(level, maxHeadingLevel))
}

// cell makes a string fit one table cell: a pipe would end the cell, and a
// line break the row.
func cell(s string) string {
	return oneLine(strings.ReplaceAll(s, "|", `\|`))
}

// oneLine keeps a string on one line, as a list item or a table row must be:
// a line break becomes <br>.
func oneLine(s string) string {
	return strings.NewReplacer("\r\n", "<br>", "\n", "<br>", "\r", "<br>").Replace(s)
}

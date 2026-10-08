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

package format_test

// The contract, from format's docs and the formats' own (CommonMark for
// Markdown, RFC 8259 for JSON):
//   - a formatter renders every entry, in order, each starting from its
//     Object; every key and every scalar of an entry's Meta appears in the
//     rendering, keys in sorted order at each level, so the same input renders
//     the same bytes every time;
//   - JSON renders the entries as one array, an empty array for none, that
//     decodes back to the entries; a value JSON cannot carry (a NaN, an
//     infinity) is ErrInvalidArgument, and nothing is written;
//   - Markdown is well formed: a heading is at most six '#', a table's rows
//     have the columns of its header, a pipe or a line break inside a cell
//     does not end it nor a line break inside a list item, and a heading, a
//     table or a list is set off from what follows by a blank line;
//   - a write that fails, wherever in the rendering, is ErrWriteFailure, and
//     nothing is written past it;
//   - a Kind that names no format is ErrInvalidArgument.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/format"
)

// failingWriter fails every write once limit bytes have been accepted, and
// counts the writes asked of it after that, which a formatter that stops at
// the first failure never makes.
type failingWriter struct {
	limit    int
	accepted int
	failed   bool
	after    int
}

var (
	errWriter = errors.New("writer failed")
	errShape  = errors.New("markdown shape")
)

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.failed {
		w.after++

		return 0, errWriter
	}

	if w.accepted+len(p) > w.limit {
		took := w.limit - w.accepted
		w.accepted = w.limit
		w.failed = true

		return took, errWriter
	}

	w.accepted += len(p)

	return len(p), nil
}

// leaves collects every key and every scalar of a value, rendered as the
// formatters render a scalar.
func leaves(value any) (keys, scalars []string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			keys = append(keys, key)

			childKeys, childScalars := leaves(child)
			keys = append(keys, childKeys...)
			scalars = append(scalars, childScalars...)
		}
	case []any:
		for _, child := range typed {
			childKeys, childScalars := leaves(child)
			keys = append(keys, childKeys...)
			scalars = append(scalars, childScalars...)
		}
	default:
		scalars = append(scalars, fmt.Sprintf("%v", typed))
	}

	return keys, scalars
}

// trees are every Meta shape within the bound: scalars of three kinds, maps
// and slices of one or two children, nested up to depth deep.
func trees(depth int) []any {
	all := []any{"text", 7, 2.5}
	if depth == 0 {
		return all
	}

	below := trees(depth - 1)

	for _, first := range below {
		all = append(all, map[string]any{"a": first}, []any{first})

		for _, second := range below {
			all = append(all, map[string]any{"a": first, "b": second}, []any{first, second})
		}
	}

	return all
}

// entry wraps a Meta tree as the one entry a rendering is checked on; a
// scalar tree is one field.
func entry(tree any) *format.Data {
	meta, ok := tree.(map[string]any)
	if !ok {
		meta = map[string]any{"value": tree}
	}

	return &format.Data{Object: "object", Meta: meta}
}

// formatters are the three, by kind.
func formatters() map[format.Kind]format.Formatter {
	all := map[format.Kind]format.Formatter{}

	for _, kind := range []format.Kind{format.KindJSON, format.KindMarkdown, format.KindConsole} {
		formatter, err := format.GetFormatter(kind)
		if err != nil {
			panic(err)
		}

		all[kind] = formatter
	}

	return all
}

// formatter is the one of a kind, which GetFormatter has for each.
func formatter(t *testing.T, kind format.Kind) format.Formatter {
	t.Helper()

	made, err := format.GetFormatter(kind)
	if err != nil {
		t.Fatal(err)
	}

	return made
}

// markdownShape checks a Markdown rendering's structure and reports the
// first fault.
func markdownShape(out string) error {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

	for i, line := range lines {
		previous := ""
		if i > 0 {
			previous = lines[i-1]
		}

		switch {
		case strings.HasPrefix(line, "#"):
			if marks := len(line) - len(strings.TrimLeft(line, "#")); marks > 6 {
				return fmt.Errorf("%w: heading of %d marks: %q", errShape, marks, line)
			}

			if previous != "" {
				return fmt.Errorf("%w: heading not set off: %q after %q", errShape, line, previous)
			}
		case strings.HasPrefix(line, "|"):
			if cells := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|"); cells != 3 {
				return fmt.Errorf("%w: row of %d pipes, want 3: %q", errShape, cells, line)
			}
		case strings.HasPrefix(line, "- "):
		case line == "" || line == "---":
		default:
			return fmt.Errorf("%w: line of no known shape: %q", errShape, line)
		}
	}

	return nil
}

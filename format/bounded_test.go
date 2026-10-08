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

// The bounded check: every Meta shape within the bound through every
// formatter, held to the contract; a write failing at every byte of a
// rendering; the depth past Markdown's six heading levels.

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/format"
)

func TestBoundedRendering(t *testing.T) {
	t.Parallel()

	for _, tree := range trees(2) {
		data := []*format.Data{entry(tree), {Object: "second"}}
		keys, scalars := leaves(entry(tree).Meta)

		for kind, formatter := range formatters() {
			var first, again bytes.Buffer

			if err := formatter.PrintAll(data, &first); err != nil {
				t.Fatalf("%s of %v: %v", kind, tree, err)
			}

			if err := formatter.PrintAll(data, &again); err != nil || !bytes.Equal(first.Bytes(), again.Bytes()) {
				t.Fatalf("%s of %v: rendered differently the second time", kind, tree)
			}

			out := first.String()

			for _, want := range append(append(keys, scalars...), "object", "second") {
				if !strings.Contains(out, want) {
					t.Fatalf("%s of %v: %q is not in the rendering:\n%s", kind, tree, want, out)
				}
			}

			switch kind {
			case format.KindJSON:
				var decoded []*format.Data
				if err := json.Unmarshal(first.Bytes(), &decoded); err != nil {
					t.Fatalf("JSON of %v does not decode: %v\n%s", tree, err, out)
				}

				if !reflect.DeepEqual(decoded, viaJSON(t, data)) {
					t.Fatalf("JSON of %v decodes to %v, want %v", tree, decoded, data)
				}
			case format.KindMarkdown:
				if err := markdownShape(out); err != nil {
					t.Fatalf("Markdown of %v: %v\n%s", tree, err, out)
				}
			case format.KindConsole:
				if !strings.HasPrefix(out, "Path: object\n") {
					t.Fatalf("Console of %v does not start from its Object:\n%s", tree, out)
				}
			}
		}
	}
}

// viaJSON is what data looks like after a JSON round trip of its own, numbers
// as float64, so a decoded rendering can be compared to it.
func viaJSON(t *testing.T, data []*format.Data) []*format.Data {
	t.Helper()

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var decoded []*format.Data
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	return decoded
}

// TestSortedKeys: at every level, keys come out sorted, whatever order the
// map was built in.
func TestSortedKeys(t *testing.T) {
	t.Parallel()

	meta := map[string]any{
		"zeta": 1, "alpha": 2, "mid": map[string]any{"z": 1, "a": 2}, "list": []any{map[string]any{"y": 1, "b": 2}},
	}

	for kind, formatter := range formatters() {
		var out bytes.Buffer
		if err := formatter.PrintAll([]*format.Data{{Object: "o", Meta: meta}}, &out); err != nil {
			t.Fatal(err)
		}

		rendered := out.String()

		for _, pair := range [][2]string{{"alpha", "zeta"}, {"a", "z"}, {"b", "y"}} {
			if strings.Index(rendered, pair[0]) > strings.Index(rendered, pair[1]) {
				t.Fatalf("%s: %q renders after %q:\n%s", kind, pair[0], pair[1], rendered)
			}
		}
	}
}

// TestBoundedWriteFailure: a writer failing after any number of bytes of a
// rendering makes PrintAll fail with ErrWriteFailure, and nothing is written
// past the failure.
func TestBoundedWriteFailure(t *testing.T) {
	t.Parallel()

	data := []*format.Data{entry(trees(2)[len(trees(2))-1]), {Object: "second", Meta: map[string]any{"k": "v"}}}

	for kind, formatter := range formatters() {
		var whole bytes.Buffer
		if err := formatter.PrintAll(data, &whole); err != nil {
			t.Fatal(err)
		}

		for limit := range whole.Len() {
			writer := &failingWriter{limit: limit}

			err := formatter.PrintAll(data, writer)
			if !errors.Is(err, fault.ErrWriteFailure) || !errors.Is(err, errWriter) {
				t.Fatalf(
					"%s, writer failing after %d of %d bytes: %v, want ErrWriteFailure with the cause",
					kind,
					limit,
					whole.Len(),
					err,
				)
			}

			if writer.after > 0 {
				t.Fatalf(
					"%s, writer failing after %d bytes: %d writes asked of it past the failure",
					kind,
					limit,
					writer.after,
				)
			}
		}
	}
}

// TestJSONEdges: no entries is an empty array; a NaN is ErrInvalidArgument and
// nothing is written.
func TestJSONEdges(t *testing.T) {
	t.Parallel()

	formatter := formatter(t, format.KindJSON)

	for _, data := range [][]*format.Data{nil, {}} {
		var out bytes.Buffer
		if err := formatter.PrintAll(data, &out); err != nil || out.String() != "[]\n" {
			t.Fatalf("JSON of %v = %q, %v; want \"[]\n\"", data, out.String(), err)
		}
	}

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		var out bytes.Buffer

		err := formatter.PrintAll([]*format.Data{{Object: "o", Meta: map[string]any{"v": value}}}, &out)
		if !errors.Is(err, fault.ErrInvalidArgument) || out.Len() != 0 {
			t.Fatalf("JSON of %v = %q, %v; want ErrInvalidArgument and nothing written", value, out.String(), err)
		}
	}
}

// TestMarkdownDepth: past six levels of nesting, headings stay at six marks
// and the shape holds.
func TestMarkdownDepth(t *testing.T) {
	t.Parallel()

	deep := map[string]any{"leaf": "end"}
	for range 8 {
		deep = map[string]any{"down": deep, "item": []any{[]any{"x", map[string]any{"in": "list"}}}}
	}

	var out bytes.Buffer
	if err := formatter(t, format.KindMarkdown).PrintAll([]*format.Data{{Object: "o", Meta: deep}}, &out); err != nil {
		t.Fatal(err)
	}

	if err := markdownShape(out.String()); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "###### ") || strings.Contains(out.String(), "#######") {
		t.Fatalf("headings past the sixth level are not capped:\n%s", out.String())
	}
}

// TestMarkdownCells: a pipe or a line break in a key or a value stays inside
// its cell.
func TestMarkdownCells(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	meta := map[string]any{
		"a|b":   "left|right",
		"lines": "one\ntwo\r\nthree\rfour",
		"items": []any{"first\nsecond", "x|y"},
	}
	if err := formatter(t, format.KindMarkdown).PrintAll([]*format.Data{{Object: "o", Meta: meta}}, &out); err != nil {
		t.Fatal(err)
	}

	if err := markdownShape(out.String()); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	for _, want := range []string{`| a\|b | left\|right |`, "| lines | one<br>two<br>three<br>four |", "- first<br>second\n- x|y\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("%q is not in:\n%s", want, out.String())
		}
	}
}

// TestKindValues: the kinds are the words a user types for them.
func TestKindValues(t *testing.T) {
	t.Parallel()

	for kind, word := range map[format.Kind]string{format.KindJSON: "json", format.KindMarkdown: "markdown", format.KindConsole: "console"} {
		if string(kind) != word {
			t.Fatalf("Kind %q, want %q", kind, word)
		}
	}
}

// TestUnknownKind: a Kind that names no format is ErrInvalidArgument.
func TestUnknownKind(t *testing.T) {
	t.Parallel()

	if formatter, err := format.GetFormatter("bogus"); !errors.Is(err, fault.ErrInvalidArgument) || formatter != nil {
		t.Fatalf("GetFormatter(bogus) = %v, %v; want ErrInvalidArgument", formatter, err)
	}
}

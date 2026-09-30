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

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/format"
)

func TestGetFormatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind    format.Kind
		wantTyp string
	}{
		{format.KindJSON, "*format.JSON"},
		{format.KindMarkdown, "*format.Markdown"},
		{format.KindConsole, "*format.Console"},
	}

	for _, tt := range tests {
		f, err := format.GetFormatter(tt.kind)
		if err != nil {
			t.Fatalf("GetFormatter(%q) returned error: %v", tt.kind, err)
		}

		if f == nil {
			t.Fatalf("GetFormatter(%q) returned nil", tt.kind)
		}
	}
}

func TestGetFormatterInvalidKind(t *testing.T) {
	t.Parallel()

	f, err := format.GetFormatter("bogus")
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}

	if f != nil {
		t.Fatalf("expected nil formatter, got %v", f)
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got: %v", err)
	}
}

// testData returns a Data struct with nested maps, slices, and scalars.
func testData() *format.Data {
	return &format.Data{
		Object: "/music/track.flac",
		Meta: map[string]any{
			"loudness": "-14.0 LUFS",
			"duration": 245.3,
			"format": map[string]any{
				"codec":       "flac",
				"sample_rate": 44100,
			},
			"streams": []any{
				map[string]any{
					"channels": 2,
					"bitrate":  "1411 kbps",
				},
			},
		},
	}
}

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.JSON{}

	input := []*format.Data{testData()}
	if err := f.PrintAll(input, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	var decoded []format.Data
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded))
	}

	if decoded[0].Object != "/music/track.flac" {
		t.Fatalf("Object = %q, want %q", decoded[0].Object, "/music/track.flac")
	}

	if decoded[0].Meta["loudness"] != "-14.0 LUFS" {
		t.Fatalf("Meta[loudness] = %v, want %q", decoded[0].Meta["loudness"], "-14.0 LUFS")
	}
}

func TestJSONEmptyMeta(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.JSON{}

	if err := f.PrintAll([]*format.Data{{Object: "test.wav"}}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	var decoded []format.Data
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if decoded[0].Object != "test.wav" {
		t.Fatalf("Object = %q, want %q", decoded[0].Object, "test.wav")
	}
}

func TestConsoleBasic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Console{}

	data := &format.Data{
		Object: "/music/track.flac",
		Meta: map[string]any{
			"bitrate":  "1411 kbps",
			"duration": 245.3,
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "Path: /music/track.flac\n\nbitrate: 1411 kbps\nduration: 245.3\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestConsoleNested(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Console{}

	data := &format.Data{
		Object: "test.flac",
		Meta: map[string]any{
			"info": map[string]any{
				"codec": "flac",
			},
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "Path: test.flac\n\ninfo:\n  codec: flac\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestConsoleSlice(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Console{}

	data := &format.Data{
		Object: "test.flac",
		Meta: map[string]any{
			"items": []any{"alpha", "beta"},
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "Path: test.flac\n\nitems:\n  [0]: alpha\n  [1]: beta\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestConsoleEmptyMeta(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Console{}

	if err := f.PrintAll([]*format.Data{{Object: "bare.wav"}}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "Path: bare.wav\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestConsoleSeparator(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Console{}

	data := []*format.Data{
		{Object: "a.wav"},
		{Object: "b.wav"},
	}

	if err := f.PrintAll(data, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("────")) {
		t.Fatalf("expected rule separator between entries, got:\n%s", out)
	}

	if !bytes.Contains(buf.Bytes(), []byte("Path: a.wav")) {
		t.Fatal("missing first entry")
	}

	if !bytes.Contains(buf.Bytes(), []byte("Path: b.wav")) {
		t.Fatal("missing second entry")
	}
}

func TestMarkdownTopLevelScalars(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := &format.Data{
		Object: "track.flac",
		Meta: map[string]any{
			"loudness": "-14.0 LUFS",
			"duration": 245.3,
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## track.flac\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		"| duration | 245.3 |\n" +
		"| loudness | -14.0 LUFS |\n\n"

	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownNestedMap(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := &format.Data{
		Object: "track.flac",
		Meta: map[string]any{
			"format": map[string]any{
				"codec":       "flac",
				"sample_rate": 44100,
			},
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## track.flac\n\n" +
		"### format\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		"| codec | flac |\n" +
		"| sample_rate | 44100 |\n\n"

	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownSlice(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := &format.Data{
		Object: "track.flac",
		Meta: map[string]any{
			"streams": []any{
				map[string]any{"channels": 2},
			},
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## track.flac\n\n" +
		"### streams\n\n" +
		"#### Item 1\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		"| channels | 2 |\n\n"

	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownPipeEscaping(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := &format.Data{
		Object: "test.wav",
		Meta: map[string]any{
			"note": "left|right",
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## test.wav\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		`| note | left\|right |` + "\n\n"

	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownEmptyMeta(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	if err := f.PrintAll([]*format.Data{{Object: "bare.wav"}}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## bare.wav\n\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownSeparator(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := []*format.Data{
		{Object: "a.wav"},
		{Object: "b.wav"},
	}

	if err := f.PrintAll(data, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## a.wav\n\n\n---\n\n## b.wav\n\n"
	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestMarkdownMixedScalarsAndNested(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	f := &format.Markdown{}

	data := &format.Data{
		Object: "track.flac",
		Meta: map[string]any{
			"bitrate": "1411 kbps",
			"info": map[string]any{
				"codec": "flac",
			},
		},
	}

	if err := f.PrintAll([]*format.Data{data}, &buf); err != nil {
		t.Fatalf("PrintAll: %v", err)
	}

	want := "## track.flac\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		"| bitrate | 1411 kbps |\n\n" +
		"### info\n\n" +
		"| Field | Value |\n" +
		"|-------|-------|\n" +
		"| codec | flac |\n\n"

	if buf.String() != want {
		t.Fatalf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

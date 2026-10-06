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

package pathcheck_test

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

// itfState is one state of a trace generated from spec/pathcheck.qnt: a
// platform, a text as Unicode code points, and what the spec says pathcheck
// answers for it.
type itfState struct {
	Platform  itfVariant  `json:"platform"`
	Text      []itfBigInt `json:"text"`
	Component bool        `json:"component"`
	Path      bool        `json:"path"`
	Socket    bool        `json:"socket"`
}

// itfVariant is a sum type's value; Platform's variants carry nothing.
type itfVariant struct {
	Tag string `json:"tag"`
}

// itfBigInt is an integer, which ITF writes as a decimal string.
type itfBigInt struct {
	BigInt string `json:"#bigint"`
}

func readTrace(file string, trace any) error {
	compressed, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = compressed.Close() }()

	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return err
	}

	return json.NewDecoder(reader).Decode(trace)
}

// TestQuintTraces replays every state of the gzipped traces in testdata/quint
// against pathcheck: the spec is the oracle.
func TestQuintTraces(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob(filepath.Join("testdata", "quint", "*.itf.json.gz"))
	assert.NilError(t, err)
	assert.Assert(t, len(files) > 0, "no trace in testdata/quint")

	platforms := map[string]pathcheck.Platform{
		"Linux":   pathcheck.Linux(),
		"Darwin":  pathcheck.Darwin(),
		"Windows": pathcheck.Windows(),
	}

	for _, file := range files {
		var trace struct {
			States []itfState `json:"states"`
		}

		assert.NilError(t, readTrace(file, &trace), file)

		for index, state := range trace.States {
			platform, ok := platforms[state.Platform.Tag]
			assert.Assert(t, ok, "%s#%d: unknown platform %q", file, index, state.Platform.Tag)

			runes := make([]rune, 0, len(state.Text))

			for _, point := range state.Text {
				code, err := strconv.ParseInt(point.BigInt, 10, 32)
				assert.NilError(t, err)

				runes = append(runes, rune(code))
			}

			text := string(runes)
			at := filepath.Base(file) + "#" + strconv.Itoa(index) + " " + state.Platform.Tag + " " + strconv.Quote(text)

			assert.Equal(t, platform.ValidateComponent(text) == nil, state.Component, "ValidateComponent "+at)
			assert.Equal(t, platform.Validate(text) == nil, state.Path, "Validate "+at)
			assert.Equal(t, platform.ValidateSocket(text) == nil, state.Socket, "ValidateSocket "+at)
		}
	}
}

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

// The fuzz targets hold the package to the same contract as the bounded check,
// past its alphabet: CI's fuzz job mutates bytes freely. A class they find
// belongs in the alphabet. FuzzNativeNamesAreCreatable alone checks against
// the host itself.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mycophonic/primordium/filesystem/pathcheck"
)

func FuzzValidateComponent(f *testing.F) {
	for _, seed := range append(atoms(), deviceAtoms()...) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		for _, c := range contracts() {
			checkComponent(t, c, name)
		}
	})
}

func FuzzValidate(f *testing.F) {
	for _, prefix := range pathPrefixes() {
		f.Add(prefix + `a\b`)
		f.Add(prefix + "a/b")
	}

	f.Fuzz(func(t *testing.T, path string) {
		for _, c := range contracts() {
			checkPath(t, c, path)
		}
	})
}

func FuzzValidateSocket(f *testing.F) {
	for _, length := range []int{0, 103, 104, 107, 108} {
		f.Add(strings.Repeat("x", length))
	}

	f.Fuzz(func(t *testing.T, path string) {
		for _, c := range contracts() {
			checkSocket(t, c, path)
		}
	})
}

// FuzzNativeNamesAreCreatable checks the contract against the host: a name the
// running platform accepts can be created, in an empty directory, as a regular
// file the directory then lists under exactly that name. A host that refuses
// the name, alters it (a dropped trailing character, another Unicode form), or
// opens a device instead fails it. The converse does not hold: pathcheck is
// stricter than a filesystem on purpose.
func FuzzNativeNamesAreCreatable(f *testing.F) {
	for _, seed := range append(atoms(), deviceAtoms()...) {
		f.Add(seed)
		f.Add("a" + seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		if pathcheck.Native().ValidateComponent(name) != nil {
			return
		}

		dir := t.TempDir()

		file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatalf("pathcheck accepts %q, but the host cannot create it: %v", name, err)
		}

		if err = file.Close(); err != nil {
			t.Fatalf("closing %q: %v", name, err)
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("listing the directory holding %q: %v", name, err)
		}

		if len(entries) != 1 || entries[0].Name() != name || !entries[0].Type().IsRegular() {
			t.Fatalf("pathcheck accepts %q, but the host made %v of it", name, entries)
		}
	})
}

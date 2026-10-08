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

package xos_test

// The bounded check: every open flag combination, every starting state, every
// write and read after it; every file size around the read buffer; every
// directory shape; every temporary-name pattern form; every truncation size;
// every pair of states a rename goes from and to. Each xos call is held to its
// os namesake.

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mycophonic/primordium/filesystem/xos"
)

// openFlags are every combination of access mode, the three and the one that
// is none (O_WRONLY|O_RDWR), and the flags os.OpenFile documents.
func openFlags() []int {
	var flags []int

	for _, access := range []int{os.O_RDONLY, os.O_WRONLY, os.O_RDWR, os.O_WRONLY | os.O_RDWR} {
		for bits := range 32 {
			flag := access

			for i, extra := range []int{os.O_CREATE, os.O_EXCL, os.O_TRUNC, os.O_APPEND, os.O_SYNC} {
				if bits&(1<<i) != 0 {
					flag |= extra
				}
			}

			flags = append(flags, flag)
		}
	}

	return flags
}

// openAndUse opens path, then writes and reads through the file as its access
// mode allows, and reports what the caller saw.
func openAndUse(open func(string, int, os.FileMode) (*os.File, error), path string, flag int, perm os.FileMode) string {
	dir := filepath.Dir(path)

	file, err := open(path, flag, perm)
	if err != nil {
		return "open: " + errorSeen(err, dir)
	}

	defer func() { _ = file.Close() }()

	seen := "opened"

	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		_, err = file.WriteString("xy")
		seen += ", write: " + errorSeen(err, dir)
	}

	if flag&os.O_WRONLY == 0 {
		if _, err = file.Seek(0, io.SeekStart); err == nil {
			data, readErr := io.ReadAll(file)
			seen += ", read " + string(data) + ": " + errorSeen(readErr, dir)
		}
	}

	return seen
}

func TestBoundedOpenFile(t *testing.T) {
	t.Parallel()

	for _, start := range states(t) {
		for _, flag := range openFlags() {
			for _, perm := range []os.FileMode{0o644, 0o444} {
				ours, theirs := pair(t, start.setup)

				got := openAndUse(xos.OpenFile, ours, flag, perm)
				want := openAndUse(os.OpenFile, theirs, flag, perm)

				if got != want {
					t.Fatalf("%s, flag %#x, perm %v: xos %q, os %q", start.name, flag, perm, got, want)
				}

				if got, want := onDisk(ours), onDisk(theirs); got != want {
					t.Fatalf("%s, flag %#x, perm %v: xos left %q, os %q", start.name, flag, perm, got, want)
				}
			}
		}
	}
}

// TestBoundedOpenAndCreate: Open is OpenFile read-only, Create is OpenFile
// read-write, created or truncated; both as os has them.
func TestBoundedOpenAndCreate(t *testing.T) {
	t.Parallel()

	for _, start := range states(t) {
		for name, calls := range map[string][2]func(string) (*os.File, error){
			"Open":   {xos.Open, os.Open},
			"Create": {xos.Create, os.Create},
		} {
			ours, theirs := pair(t, start.setup)

			use := func(open func(string) (*os.File, error), path string) string {
				return openAndUse(
					func(p string, _ int, _ os.FileMode) (*os.File, error) { return open(p) },
					path,
					os.O_RDWR,
					0,
				)
			}

			if name == "Open" {
				use = func(open func(string) (*os.File, error), path string) string {
					return openAndUse(
						func(p string, _ int, _ os.FileMode) (*os.File, error) { return open(p) },
						path,
						os.O_RDONLY,
						0,
					)
				}
			}

			if got, want := use(calls[0], ours), use(calls[1], theirs); got != want {
				t.Fatalf("%s, %s: xos %q, os %q", name, start.name, got, want)
			}

			if got, want := onDisk(ours), onDisk(theirs); got != want {
				t.Fatalf("%s, %s: xos left %q, os %q", name, start.name, got, want)
			}
		}
	}
}

// sizes are file sizes around the 512-byte read buffer and its doubling.
func sizes() []int { return []int{0, 1, 511, 512, 513, 1023, 1024, 1025, 4097} }

func TestBoundedReadFile(t *testing.T) {
	t.Parallel()

	shapes := states(t)

	for _, size := range sizes() {
		data := strings.Repeat("r", size)
		shapes = append(shapes, state{"size " + strconv.Itoa(size), func(t *testing.T, path string) {
			t.Helper()

			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}})
	}

	for _, start := range shapes {
		ours, theirs := pair(t, start.setup)

		got, gotErr := xos.ReadFile(ours)
		want, wantErr := os.ReadFile(theirs)

		if !bytes.Equal(got, want) ||
			errorSeen(gotErr, filepath.Dir(ours)) != errorSeen(wantErr, filepath.Dir(theirs)) {
			t.Fatalf("%s: xos read %d bytes, %v; os %d bytes, %v", start.name, len(got), gotErr, len(want), wantErr)
		}
	}
}

func TestBoundedWriteFile(t *testing.T) {
	t.Parallel()

	for _, start := range states(t) {
		for _, size := range []int{0, 1, 600} {
			for _, perm := range []os.FileMode{0o600, 0o644, 0o444} {
				ours, theirs := pair(t, start.setup)
				data := []byte(strings.Repeat("w", size))

				gotErr, wantErr := xos.WriteFile(ours, data, perm), os.WriteFile(theirs, data, perm)

				if errorSeen(gotErr, filepath.Dir(ours)) != errorSeen(wantErr, filepath.Dir(theirs)) {
					t.Fatalf("%s, %d bytes, %v: xos %v, os %v", start.name, size, perm, gotErr, wantErr)
				}

				t.Cleanup(func() { _ = os.Chmod(ours, 0o644); _ = os.Chmod(theirs, 0o644) })

				if got, want := onDisk(ours), onDisk(theirs); got != want {
					t.Fatalf("%s, %d bytes, %v: xos left %q, os %q", start.name, size, perm, got, want)
				}
			}
		}
	}
}

// dirShapes are directories to read: empty, one entry, entries out of order,
// of every kind.
func dirShapes() map[string][]string {
	return map[string][]string{
		"empty":     nil,
		"one":       {"a"},
		"unordered": {"c", "a", "b"},
		"mixed":     {"z", "dir/", "B", "a.txt", ".hidden"},
	}
}

func TestBoundedReadDir(t *testing.T) {
	t.Parallel()

	describe := func(dir string, entries []os.DirEntry, err error) string {
		var seen strings.Builder

		seen.WriteString(errorSeen(err, dir))

		for _, entry := range entries {
			seen.WriteString(" " + entry.Name())

			if entry.IsDir() {
				seen.WriteString("/")
			}
		}

		return seen.String()
	}

	for name, entries := range dirShapes() {
		ours, theirs := pair(t, func(t *testing.T, path string) {
			t.Helper()

			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}

			for _, entry := range entries {
				var err error

				if dir, ok := strings.CutSuffix(entry, "/"); ok {
					err = os.Mkdir(filepath.Join(path, dir), 0o755)
				} else {
					err = os.WriteFile(filepath.Join(path, entry), nil, 0o644)
				}

				if err != nil {
					t.Fatal(err)
				}
			}
		})

		gotEntries, gotErr := xos.ReadDir(ours)
		wantEntries, wantErr := os.ReadDir(theirs)

		if got, want := describe(
			filepath.Dir(ours),
			gotEntries,
			gotErr,
		), describe(
			filepath.Dir(theirs),
			wantEntries,
			wantErr,
		); got != want {
			t.Fatalf("%s: xos %q, os %q", name, got, want)
		}
	}

	for _, start := range states(t) {
		if start.name == "a directory" {
			continue
		}

		ours, theirs := pair(t, start.setup)

		gotEntries, gotErr := xos.ReadDir(ours)
		wantEntries, wantErr := os.ReadDir(theirs)

		if got, want := describe(
			filepath.Dir(ours),
			gotEntries,
			gotErr,
		), describe(
			filepath.Dir(theirs),
			wantEntries,
			wantErr,
		); got != want {
			t.Fatalf("%s: xos %q, os %q", start.name, got, want)
		}
	}
}

func TestBoundedTruncate(t *testing.T) {
	t.Parallel()

	for _, start := range states(t) {
		for _, size := range []int64{-1, 0, 2, 5, 9} {
			ours, theirs := pair(t, start.setup)

			gotErr, wantErr := xos.Truncate(ours, size), os.Truncate(theirs, size)

			if errorSeen(gotErr, filepath.Dir(ours)) != errorSeen(wantErr, filepath.Dir(theirs)) {
				t.Fatalf("%s, size %d: xos %v, os %v", start.name, size, gotErr, wantErr)
			}

			if got, want := onDisk(ours), onDisk(theirs); got != want {
				t.Fatalf("%s, size %d: xos left %q, os %q", start.name, size, got, want)
			}
		}
	}
}

func TestBoundedStat(t *testing.T) {
	t.Parallel()

	for _, start := range states(t) {
		ours, theirs := pair(t, start.setup)

		got, gotErr := xos.Stat(ours)
		want, wantErr := os.Stat(theirs)

		if errorSeen(gotErr, filepath.Dir(ours)) != errorSeen(wantErr, filepath.Dir(theirs)) {
			t.Fatalf("%s: xos %v, os %v", start.name, gotErr, wantErr)
		}

		if gotErr == nil && wantErr == nil &&
			(got.Mode() != want.Mode() || got.Size() != want.Size() || got.Name() != want.Name()) {
			t.Fatalf(
				"%s: xos %v %d %s, os %v %d %s",
				start.name,
				got.Mode(),
				got.Size(),
				got.Name(),
				want.Mode(),
				want.Size(),
				want.Name(),
			)
		}
	}
}

// patterns are every form a temporary-name pattern takes: empty, no "*", a
// "*" first, last, in the middle, more than one, and with a separator.
func patterns() []string {
	return []string{"", "x", "*", "*x", "x*", "a*b", "a*b*c", "a/b", "a" + string(os.PathSeparator) + "b"}
}

// tempShape is a temporary name as the caller has it, its directory masked and
// its random part replaced by a placeholder.
func tempShape(dir, name string) string {
	rest, inside := strings.CutPrefix(name, dir)
	if !inside {
		return "outside " + dir + ": " + name
	}

	return "DIR" + digits.ReplaceAllString(rest, "N")
}

// digits matches the random part of a temporary name.
var digits = regexp.MustCompile(`\d+`)

func TestBoundedTemp(t *testing.T) {
	t.Parallel()

	for _, pattern := range patterns() {
		for _, dirForm := range []string{"plain", "trailing separator", "missing"} {
			dir := t.TempDir()

			switch dirForm {
			case "trailing separator":
				dir += string(os.PathSeparator)
			case "missing":
				dir = filepath.Join(dir, "missing")
			}

			describeFile := func(file *os.File, err error) string {
				if err != nil {
					return "error: " + digits.ReplaceAllString(errorSeen(err, dir), "N")
				}

				defer func() { _ = file.Close() }()

				info, statErr := file.Stat()
				if statErr != nil {
					return "stat: " + errorKind(statErr)
				}

				return tempShape(dir, file.Name()) + " " + info.Mode().String()
			}

			if got, want := describeFile(
				xos.CreateTemp(dir, pattern),
			), describeFile(
				os.CreateTemp(dir, pattern),
			); got != want {
				t.Fatalf("CreateTemp(%s dir, %q): xos %q, os %q", dirForm, pattern, got, want)
			}

			describeDir := func(name string, err error) string {
				if err != nil {
					return "error: " + digits.ReplaceAllString(errorSeen(err, dir), "N")
				}

				info, statErr := os.Stat(name)
				if statErr != nil {
					return "stat: " + errorKind(statErr)
				}

				return tempShape(dir, name) + " " + info.Mode().String()
			}

			//nolint:usetesting // os.MkdirTemp is the reference xos.MkdirTemp is held to, not a test's scratch directory
			if got, want := describeDir(
				xos.MkdirTemp(dir, pattern),
			), describeDir(
				os.MkdirTemp(dir, pattern),
			); got != want {
				t.Fatalf("MkdirTemp(%s dir, %q): xos %q, os %q", dirForm, pattern, got, want)
			}
		}
	}
}

// TestBoundedRename: from every state to every state, xos.Rename and os.Rename
// agree on the error and on what both paths hold afterwards.
func TestBoundedRename(t *testing.T) {
	t.Parallel()

	for _, from := range states(t) {
		for _, to := range states(t) {
			ours, theirs := t.TempDir(), t.TempDir()

			for _, dir := range []string{ours, theirs} {
				from.setup(t, filepath.Join(dir, "old"))
				to.setup(t, filepath.Join(dir, "new"))
			}

			got := errorSeen(xos.Rename(filepath.Join(ours, "old"), filepath.Join(ours, "new")), ours)
			want := errorSeen(os.Rename(filepath.Join(theirs, "old"), filepath.Join(theirs, "new")), theirs)

			if got != want {
				t.Fatalf("from %s to %s: xos %q, os %q", from.name, to.name, got, want)
			}

			for _, name := range []string{"old", "new"} {
				if got, want := onDisk(filepath.Join(ours, name)), onDisk(filepath.Join(theirs, name)); got != want {
					t.Fatalf("from %s to %s: xos left %s %q, os %q", from.name, to.name, name, got, want)
				}
			}
		}
	}
}

// TestRenameOverHeld: a file held open through xos (FILE_SHARE_DELETE on
// Windows) is replaced by Rename on every platform, and the holder keeps
// reading the file it opened; one held through os is replaced where os would
// replace it, which on Windows is not at all.
func TestRenameOverHeld(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(string) (*os.File, error){"xos.Open": xos.Open, "os.Open": os.Open} {
		dir := t.TempDir()
		oldPath, newPath := filepath.Join(dir, "old"), filepath.Join(dir, "new")

		for path, data := range map[string]string{oldPath: "incoming", newPath: "held"} {
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		holder, err := open(newPath)
		if err != nil {
			t.Fatal(err)
		}

		err = xos.Rename(oldPath, newPath)

		switch {
		case name == "os.Open" && runtime.GOOS == "windows":
			if err == nil {
				t.Fatalf("%s held without FILE_SHARE_DELETE: Rename succeeded, where os refuses", name)
			}
		case err != nil:
			t.Fatalf("%s held: Rename = %v, want the file replaced", name, err)
		default:
			// What a file of those bytes looks like here, mode included.
			reference := filepath.Join(dir, "reference")
			if err = os.WriteFile(reference, []byte("incoming"), 0o644); err != nil {
				t.Fatal(err)
			}

			if got, want := onDisk(newPath), onDisk(reference); got != want {
				t.Fatalf("%s held: the path holds %q after Rename, want %q", name, got, want)
			}

			if still, readErr := io.ReadAll(holder); readErr != nil || string(still) != "held" {
				t.Fatalf("%s held: the holder reads %q, %v; want the file it opened", name, still, readErr)
			}
		}

		if err = holder.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSharedDelete: on Windows, a file open through xos can be renamed and
// removed while open (FILE_SHARE_DELETE), as it always can on Unix.
func TestSharedDelete(t *testing.T) {
	t.Parallel()

	for name, open := range map[string]func(string) (*os.File, error){
		"Open":     xos.Open,
		"Create":   xos.Create,
		"OpenFile": func(p string) (*os.File, error) { return xos.OpenFile(p, os.O_RDWR, 0) },
	} {
		path := filepath.Join(t.TempDir(), "open")
		if err := os.WriteFile(path, []byte("held"), 0o644); err != nil {
			t.Fatal(err)
		}

		file, err := open(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if err = os.Rename(path, path+".moved"); err != nil {
			t.Fatalf("%s on %s: renaming the open file: %v", name, runtime.GOOS, err)
		}

		if err = os.Remove(path + ".moved"); err != nil {
			t.Fatalf("%s on %s: removing the open file: %v", name, runtime.GOOS, err)
		}

		_ = file.Close()

		if _, err = os.Stat(path + ".moved"); !errorIs(err, fs.ErrNotExist) {
			t.Fatalf("%s: the removed file is still there: %v", name, err)
		}
	}
}

func errorIs(err, target error) bool { return errorKind(err) == errorKind(target) }

// TestTempNamesAreDistinct: "Multiple programs or goroutines calling
// CreateTemp simultaneously will not choose the same file", and likewise
// MkdirTemp: every one of many calls racing on one pattern gets a name of its
// own.
func TestTempNamesAreDistinct(t *testing.T) {
	t.Parallel()

	const calls = 64

	dir := t.TempDir()

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		names = map[string]bool{}
	)

	record := func(name string, err error) {
		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			t.Errorf("%v", err)

			return
		}

		if names[name] {
			t.Errorf("%q chosen twice", name)
		}

		names[name] = true
	}

	for range calls {
		wg.Go(func() {
			file, err := xos.CreateTemp(dir, "same*pattern")
			if err == nil {
				defer func() { _ = file.Close() }()

				record(file.Name(), nil)

				return
			}

			record("", err)
		})

		wg.Go(func() { record(xos.MkdirTemp(dir, "same*pattern")) })
	}

	wg.Wait()

	if len(names) != 2*calls {
		t.Fatalf("%d distinct names for %d calls", len(names), 2*calls)
	}
}

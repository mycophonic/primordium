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

package refcount_test

// The contract, from refcount's docs:
//   - New panics on a root that is not a valid path; a key that is not a
//     single path component is ErrInvalidArgument, and nothing is created;
//   - the factory runs on every Acquire, alone in the entry directory, which
//     is the key under the root; Acquire returns the factory's path;
//   - the entry lives while any holder, in any process, has it; the last
//     release removes it and runs that releaser's cleanup only; release is
//     idempotent; a holder that dies releases;
//   - a factory error is returned as it is, the entry released as a holder's.
//
// The holder in another process is this test binary, told by
// PRIMORDIUM_REFCOUNT_CHILD what to do with the key under
// PRIMORDIUM_REFCOUNT_ROOT.

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/store/refcount"
)

const (
	childEnv = "PRIMORDIUM_REFCOUNT_CHILD"
	rootEnv  = "PRIMORDIUM_REFCOUNT_ROOT"
	key      = "shared"
	dataFile = "data"
)

var errFactory = errors.New("factory failed")

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "hold":
		os.Exit(hold(false))
	case "hold-factory":
		os.Exit(hold(true))
	default:
		os.Exit(2)
	}
}

// hold acquires the key, says "held <path>", and holds it until stdin closes,
// then releases and says "released"; it returns the exit code. With pause, its
// factory writes a partial data file, says "in factory", and completes the
// file once a line arrives.
func hold(pause bool) int {
	in := bufio.NewReader(os.Stdin)
	locker := refcount.New(os.Getenv(rootEnv))

	path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		dataPath := filepath.Join(dir, dataFile)

		if pause {
			if err := filesystem.WriteFile(dataPath, []byte("partial"), 0o600); err != nil {
				return "", nil, err
			}

			say("in factory")

			if _, err := in.ReadString('\n'); err != nil {
				return "", nil, err
			}
		}

		if err := filesystem.WriteFile(dataPath, []byte("complete"), 0o600); err != nil {
			return "", nil, err
		}

		return dataPath, nil, nil
	})
	if err != nil {
		say("error " + err.Error())

		return 1
	}

	say("held " + path)

	_, _ = io.Copy(io.Discard, in)

	release()
	say("released")

	return 0
}

func say(line string) {
	_, _ = os.Stdout.WriteString(line + "\n")
}

// holder is this binary holding the key under root in another process.
type holder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
}

func startHolder(t *testing.T, root, mode string) *holder {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(), childEnv+"="+mode, rootEnv+"="+root)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	return &holder{cmd: cmd, stdin: stdin, out: bufio.NewReader(stdout)}
}

// said is the child's next line, which must start with want; the rest is
// returned.
func (h *holder) said(t *testing.T, want string) string {
	t.Helper()

	line, err := h.out.ReadString('\n')
	if err != nil {
		t.Fatalf("child said %q then %v, want %q", line, err, want)
	}

	line = strings.TrimSuffix(line, "\n")
	if !strings.HasPrefix(line, want) {
		t.Fatalf("child said %q, want %q", line, want)
	}

	return strings.TrimPrefix(strings.TrimPrefix(line, want), " ")
}

// done closes the child's stdin, which releases, and waits for it.
func (h *holder) done(t *testing.T) {
	t.Helper()

	_ = h.stdin.Close()
	h.said(t, "released")

	if err := h.cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

// writing is a factory that writes data unless it is there already.
func writing(dir string) (string, func(), error) {
	dataPath := filepath.Join(dir, dataFile)
	if _, err := os.Stat(dataPath); errors.Is(err, fs.ErrNotExist) {
		if err := filesystem.WriteFile(dataPath, []byte("complete"), 0o600); err != nil {
			return "", nil, err
		}
	}

	return dataPath, nil, nil
}

// cleaning is writing with a cleanup that counts its runs.
func cleaning(ran *atomic.Int32) refcount.ResourceFactory {
	return func(dir string) (string, func(), error) {
		path, _, err := writing(dir)

		return path, func() { ran.Add(1) }, err
	}
}

func gone(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is still there: %v", path, err)
	}
}

func present(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s is gone: %v", path, err)
	}
}

func TestNewPanicsOnAnInvalidRoot(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New accepted a root with a .. component")
		}
	}()

	sep := string(filepath.Separator)

	refcount.New(t.TempDir() + sep + ".." + sep + "x")
}

func TestAnInvalidKeyCreatesNothing(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "root")
	locker := refcount.New(root)

	for _, bad := range []string{"", " ", ".", "..", "a/b"} {
		_, _, err := locker.Acquire(bad, writing)
		if !errors.Is(err, fault.ErrInvalidArgument) {
			t.Fatalf("Acquire(%q) = %v, want ErrInvalidArgument", bad, err)
		}
	}

	gone(t, root)
}

func TestTheFactoryRunsOnEveryAcquireAlone(t *testing.T) {
	t.Parallel()

	const holders = 20

	root := t.TempDir()
	locker := refcount.New(root)
	entry := filepath.Join(root, key)

	var (
		calls, inFactory atomic.Int32
		wg               sync.WaitGroup
	)

	wg.Add(holders)

	for range holders {
		go func() {
			defer wg.Done()

			path, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
				calls.Add(1)

				if inFactory.Add(1) != 1 {
					t.Error("two factories ran at once")
				}

				defer inFactory.Add(-1)

				if dir != entry {
					t.Errorf("factory ran in %q, want %q", dir, entry)
				}

				return writing(dir)
			})
			if err != nil {
				t.Error(err)

				return
			}

			defer release()

			if path != filepath.Join(entry, dataFile) {
				t.Errorf("Acquire = %q, want the factory's path", path)
			}

			present(t, path)
		}()
	}

	wg.Wait()

	if calls.Load() != holders {
		t.Fatalf("the factory ran %d times for %d acquires", calls.Load(), holders)
	}

	gone(t, entry)
}

func TestTheLastReleaseRemovesTheEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)
	entry := filepath.Join(root, key)

	var ranA, ranB atomic.Int32

	path, releaseA, err := locker.Acquire(key, cleaning(&ranA))
	if err != nil {
		t.Fatal(err)
	}

	pathB, releaseB, err := locker.Acquire(key, cleaning(&ranB))
	if err != nil {
		t.Fatal(err)
	}

	if pathB != path {
		t.Fatalf("second Acquire = %q, first %q", pathB, path)
	}

	releaseA()
	releaseA()
	present(t, path)

	if ranA.Load()+ranB.Load() != 0 {
		t.Fatal("a cleanup ran while a holder remained")
	}

	releaseB()
	releaseB()
	gone(t, entry)

	if ranA.Load() != 0 || ranB.Load() != 1 {
		t.Fatalf("cleanups ran A %d, B %d; want the last releaser's once", ranA.Load(), ranB.Load())
	}
}

func TestAFailedFactoryReleasesAsAHolder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)
	entry := filepath.Join(root, key)
	failing := func(string) (string, func(), error) { return "", nil, errFactory }

	if _, _, err := locker.Acquire(key, failing); !errors.Is(err, errFactory) {
		t.Fatalf("Acquire = %v, want the factory's error", err)
	}

	gone(t, entry)

	path, release, err := locker.Acquire(key, writing)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := locker.Acquire(key, failing); !errors.Is(err, errFactory) {
		t.Fatalf("Acquire = %v, want the factory's error", err)
	}

	present(t, path)
	release()
	gone(t, entry)
}

func TestAHolderInAnotherProcessKeepsTheEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)
	entry := filepath.Join(root, key)

	child := startHolder(t, root, "hold")
	childPath := child.said(t, "held")

	path, release, err := locker.Acquire(key, writing)
	if err != nil {
		t.Fatal(err)
	}

	if path != childPath {
		t.Fatalf("Acquire = %q, the other process holds %q", path, childPath)
	}

	release()
	present(t, path)

	child.done(t)
	gone(t, entry)
}

func TestAHolderThatDiesReleases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)
	entry := filepath.Join(root, key)

	child := startHolder(t, root, "hold")
	childPath := child.said(t, "held")

	if err := child.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	_ = child.cmd.Wait()

	present(t, childPath)

	var found []byte

	_, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
		found, _ = os.ReadFile(filepath.Join(dir, dataFile))

		return writing(dir)
	})
	if err != nil {
		t.Fatal(err)
	}

	if string(found) != "complete" {
		t.Fatalf("the dead holder's entry held %q", found)
	}

	release()
	gone(t, entry)
}

func TestFactoriesRunAloneAcrossProcesses(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	locker := refcount.New(root)

	child := startHolder(t, root, "hold-factory")
	child.said(t, "in factory")

	found := make(chan []byte, 1)

	go func() {
		_, release, err := locker.Acquire(key, func(dir string) (string, func(), error) {
			data, _ := os.ReadFile(filepath.Join(dir, dataFile))
			found <- data

			return writing(dir)
		})
		if err != nil {
			t.Error(err)

			found <- nil

			return
		}

		release()
	}()

	// A factory not kept out would run during this pause and see the part.
	time.Sleep(100 * time.Millisecond)

	if _, err := io.WriteString(child.stdin, "go\n"); err != nil {
		t.Fatal(err)
	}

	child.said(t, "held")

	if data := <-found; string(data) != "complete" {
		t.Fatalf("our factory saw %q while the other process's ran", data)
	}

	child.done(t)
}

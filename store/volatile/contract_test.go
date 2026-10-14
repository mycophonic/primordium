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

package volatile_test

// The contract, from volatile's docs:
//   - New panics on a root that is not a valid path;
//   - Acquire returns the path of a file under the root holding content: the
//     same path for the same content in any instance of the same algorithm,
//     another for other content or another algorithm;
//   - the file stays while any holder, in any process, has it; the last
//     release removes it and leaves the root empty; release is idempotent; a
//     holder that dies releases.
//
// The holder in another process is this test binary, told by
// PRIMORDIUM_VOLATILE_CHILD to hold the shared content under
// PRIMORDIUM_VOLATILE_ROOT.

import (
	"bufio"
	"bytes"
	"context"
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

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/store/volatile"
)

const (
	childEnv = "PRIMORDIUM_VOLATILE_CHILD"
	rootEnv  = "PRIMORDIUM_VOLATILE_ROOT"
)

const sharedContent = "shared content"

// shared is the content the holders share.
func shared() []byte { return []byte(sharedContent) }

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "hold":
		os.Exit(hold())
	default:
		os.Exit(2)
	}
}

// hold acquires the shared content, says "held <path>", and holds it until
// stdin closes, then releases and says "released"; it returns the exit code.
func hold() int {
	store := volatile.New(os.Getenv(rootEnv), digest.SHA256)

	path, release, err := store.Acquire(shared())
	if err != nil {
		say("error " + err.Error())

		return 1
	}

	say("held " + path)

	_, _ = io.Copy(io.Discard, os.Stdin)

	release()
	say("released")

	return 0
}

func say(line string) {
	_, _ = os.Stdout.WriteString(line + "\n")
}

// holder is this binary holding the shared content under root in another
// process.
type holder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
}

func startHolder(t *testing.T, root string) *holder {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)
	cmd.Stderr = os.Stderr

	cmd.Env = append(os.Environ(), childEnv+"=hold", rootEnv+"="+root)

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

func empty(t *testing.T, root string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Fatalf("the root still holds %d entries", len(entries))
	}
}

func holds(t *testing.T, path string, content []byte) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, content) {
		t.Fatalf("the file holds %d bytes, want %d", len(data), len(content))
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

	volatile.New(t.TempDir()+sep+".."+sep+"x", digest.SHA256)
}

func TestTheFileHoldsTheContent(t *testing.T) {
	t.Parallel()

	pattern := make([]byte, bytesize.MiB)
	for i := range pattern {
		pattern[i] = byte(i)
	}

	for name, content := range map[string][]byte{"empty": {}, "short": shared(), "a MiB": pattern} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			store := volatile.New(root, digest.SHA256)

			path, release, err := store.Acquire(content)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.HasPrefix(path, root+string(filepath.Separator)) {
				t.Fatalf("Acquire = %q, not under the root %q", path, root)
			}

			holds(t, path, content)
			release()
			release()
			empty(t, root)
		})
	}
}

func TestThePathIsTheContents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := volatile.New(root, digest.SHA256)
	second := volatile.New(root, digest.SHA256)
	other := volatile.New(root, digest.BLAKE2b256)

	path, release, err := first.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	defer release()

	again, releaseAgain, err := second.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	defer releaseAgain()

	if again != path {
		t.Fatalf("another instance gave %q for the same content, want %q", again, path)
	}

	different, releaseDifferent, err := first.Acquire([]byte("other content"))
	if err != nil {
		t.Fatal(err)
	}

	defer releaseDifferent()

	if different == path {
		t.Fatal("other content got the same path")
	}

	byOther, releaseByOther, err := other.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	defer releaseByOther()

	if byOther == path {
		t.Fatal("another algorithm got the same path")
	}

	holds(t, byOther, shared())
}

// TestHoldersOverlap: every holder waits for all the others to hold the
// content before it releases: holds that excluded one another could never
// all overlap. The limit is a clock bound, about six times the slowest run
// seen (10.3 s on windows-2025); reaching it means they never did.
func TestHoldersOverlap(t *testing.T) {
	t.Parallel()

	const (
		holders = 100
		limit   = 60 * time.Second
	)

	root := t.TempDir()
	store := volatile.New(root, digest.SHA256)

	var (
		wg      sync.WaitGroup
		arrived atomic.Int64
		paths   sync.Map
	)

	allHeld := make(chan struct{})
	deadline, cancel := context.WithTimeout(t.Context(), limit)

	defer cancel()

	arrive := func() {
		if arrived.Add(1) == holders {
			close(allHeld)
		}
	}

	for i := range holders {
		wg.Go(func() {
			path, release, err := store.Acquire(shared())
			if err != nil {
				t.Error(err)
				arrive()

				return
			}

			paths.Store(i, path)
			holds(t, path, shared())
			arrive()

			select {
			case <-allHeld:
			case <-deadline.Done():
				t.Error("the holders never all held the content at once")
			}

			release()
		})
	}

	finished := make(chan struct{})

	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-deadline.Done():
		t.Fatalf("after %v, %d of %d holders had acquired: the holds never all overlapped",
			limit, arrived.Load(), holders)
	}

	var first string

	paths.Range(func(_, value any) bool {
		path, ok := value.(string)
		if !ok {
			t.Fatalf("a holder stored %T", value)
		}

		if first == "" {
			first = path
		} else if path != first {
			t.Errorf("a holder got %q, another %q", path, first)
		}

		return true
	})

	empty(t, root)
}

func TestReacquireAfterTheLastRelease(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := volatile.New(root, digest.SHA256)

	path, release, err := store.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	release()
	empty(t, root)

	again, releaseAgain, err := store.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	defer releaseAgain()

	if again != path {
		t.Fatalf("Acquire = %q after a release, %q before", again, path)
	}

	holds(t, again, shared())
}

func TestAHolderInAnotherProcessKeepsTheFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := volatile.New(root, digest.SHA256)

	child := startHolder(t, root)
	childPath := child.said(t, "held")

	path, release, err := store.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	if path != childPath {
		t.Fatalf("Acquire = %q, the other process holds %q", path, childPath)
	}

	release()
	holds(t, path, shared())

	child.done(t)
	empty(t, root)
}

func TestAHolderThatDiesReleases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := volatile.New(root, digest.SHA256)

	child := startHolder(t, root)
	childPath := child.said(t, "held")

	if err := child.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}

	_ = child.cmd.Wait()

	holds(t, childPath, shared())

	_, release, err := store.Acquire(shared())
	if err != nil {
		t.Fatal(err)
	}

	release()
	empty(t, root)

	if _, err := os.Stat(childPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the dead holder's file is still there: %v", err)
	}
}

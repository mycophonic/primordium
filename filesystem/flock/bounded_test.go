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

package flock_test

// The bounded check: every sequence of up to four calls on one file, over
// three holders, trying and blocking, held to the contract after each; every set of holders the
// contract allows against every kind of blocked lock; and every way a lock
// can fail to be placed.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/filesystem/flock"
)

// call is one step: lock holder slot (exclusive or not, blocking or trying),
// or unlock it. A blocking lock is only taken where the model says it is
// free: it must then be granted at once, and what it holds is tested by the
// calls after it.
type call struct {
	slot      int
	unlock    bool
	exclusive bool
	blocking  bool
}

func calls() []call {
	var all []call

	for slot := range 3 {
		all = append(all,
			call{slot: slot}, call{slot: slot, exclusive: true},
			call{slot: slot, blocking: true}, call{slot: slot, exclusive: true, blocking: true},
			call{slot: slot, unlock: true})
	}

	return all
}

// newLockFile makes a lock file in a directory of its own, and returns its
// path.
func newLockFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// play runs one sequence of Try calls and Unlocks, checking the contract
// after each, then lets every holder go.
func play(t *testing.T, path string, sequence []call) {
	t.Helper()

	locks := map[int]*flock.Lock{}
	model := map[int]holder{}

	defer func() {
		for _, lock := range locks {
			_ = lock.Unlock()
		}
	}()

	for i, step := range sequence {
		if step.unlock {
			lock, held := locks[step.slot]

			err := lock.Unlock()

			switch {
			case !held && !errors.Is(err, flock.ErrUnlockFail):
				t.Fatalf("%+v, step %d: Unlock of no lock = %v, want ErrUnlockFail", sequence, i, err)
			case held && err != nil:
				t.Fatalf("%+v, step %d: Unlock = %v", sequence, i, err)
			case held && !errors.Is(lock.Unlock(), flock.ErrUnlockFail):
				t.Fatalf("%+v, step %d: a second Unlock was not ErrUnlockFail", sequence, i)
			}

			delete(locks, step.slot)
			delete(model, step.slot)

			continue
		}

		if _, held := locks[step.slot]; held {
			continue // a slot holds one lock at a time
		}

		blocked := conflicts(model, step.exclusive)

		if step.blocking {
			if blocked {
				continue // it would wait for a holder this sequence never releases
			}

			take := flock.Shared
			if step.exclusive {
				take = flock.Exclusive
			}

			locks[step.slot] = grantedWithin(t, take, path, fmt.Sprintf("%+v, step %d", sequence, i))
			model[step.slot] = holder{exclusive: step.exclusive}

			continue
		}

		try := flock.TryShared
		if step.exclusive {
			try = flock.TryExclusive
		}

		lock, err := try(path)

		switch {
		case blocked && (!errors.Is(err, flock.ErrLockWouldBlock) || errors.Is(err, flock.ErrLockFail) || lock != nil):
			t.Fatalf(
				"%+v, step %d: a conflicting Try = %v, %v; want ErrLockWouldBlock alone, no lock",
				sequence,
				i,
				lock,
				err,
			)
		case !blocked && (err != nil || lock == nil):
			t.Fatalf("%+v, step %d: a free Try = %v, %v; want the lock", sequence, i, lock, err)
		case !blocked:
			locks[step.slot] = lock
			model[step.slot] = holder{exclusive: step.exclusive}
		}
	}
}

func TestBoundedTry(t *testing.T) {
	t.Parallel()

	path := newLockFile(t)

	var walk func(prefix []call)

	walk = func(prefix []call) {
		play(t, path, prefix)

		if len(prefix) < 4 {
			for _, next := range calls() {
				walk(append(slices.Clip(prefix), next))
			}
		}
	}

	walk(nil)
}

// errFunction is what a scoped lock's function returns.
var errFunction = errors.New("function failed")

// earlyGrantWindow is how long a blocked lock is given to be granted before
// any holder lets go, which it must not be.
const earlyGrantWindow = 20 * time.Millisecond

// grantLimit bounds how long a blocked lock may take to be granted once it
// should be; reaching it means it never was, not that the runner is slow.
const grantLimit = 30 * time.Second

// TestBoundedBlocking: for every set of holders the contract allows, and every
// kind of blocking lock, the blocked lock is granted once every conflicting
// holder has let go, not before: when granted, it reads how many have.
func TestBoundedBlocking(t *testing.T) {
	t.Parallel()

	holderSets := [][]bool{{false}, {true}, {false, false}}

	for _, held := range holderSets {
		for _, exclusive := range []bool{false, true} {
			for _, scoped := range []bool{false, true} {
				blockedOn(t, held, exclusive, scoped)
			}
		}
	}
}

// blockedOn holds held (true for exclusive), then takes a blocking lock and
// releases the holders one by one.
func blockedOn(t *testing.T, held []bool, exclusive, scoped bool) {
	t.Helper()

	path := newLockFile(t)

	locks := make([]*flock.Lock, len(held))

	for i, holderExclusive := range held {
		take := flock.Shared
		if holderExclusive {
			take = flock.Exclusive
		}

		locks[i] = grantedWithin(t, take, path, fmt.Sprintf("holders %v, lock %d", held, i))
	}

	conflicting := 0
	if conflicts(map[int]holder{0: {exclusive: slices.Contains(held, true)}}, exclusive) {
		conflicting = len(held)
	}

	var released atomic.Int32

	started := make(chan struct{})
	granted := make(chan int32, 1)
	done := make(chan error, 1)

	go func() {
		close(started)

		done <- lockBlocking(path, exclusive, scoped, func() { granted <- released.Load() })
	}()

	// Give a lock that would be granted too early the time to be: a correct
	// one cannot be, so the pause can only expose a wrong grant, never fail a
	// right one.
	<-started
	time.Sleep(earlyGrantWindow)

	for _, lock := range locks {
		// Every release is counted before it happens: a lock granted on it
		// reads it.
		released.Add(1)

		if err := lock.Unlock(); err != nil {
			t.Fatalf("holders %v: Unlock: %v", held, err)
		}
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("holders %v, exclusive %v: the blocked lock: %v", held, exclusive, err)
		}
	case <-time.After(grantLimit):
		t.Fatalf("holders %v, exclusive %v: the blocked lock was never granted", held, exclusive)
	}

	if got := <-granted; got < int32(conflicting) {
		t.Fatalf("holders %v, exclusive %v: granted after %d releases, want %d", held, exclusive, got, conflicting)
	}
}

// grantedWithin takes a lock the model says is free, which must then be
// granted at once: one not granted within grantLimit fails the test there,
// rather than leaving it to hang until the package's timeout.
func grantedWithin(t *testing.T, take func(string) (*flock.Lock, error), path, what string) *flock.Lock {
	t.Helper()

	type result struct {
		lock *flock.Lock
		err  error
	}

	done := make(chan result, 1)

	go func() {
		lock, err := take(path)
		done <- result{lock, err}
	}()

	select {
	case got := <-done:
		if got.err != nil || got.lock == nil {
			t.Fatalf("%s: a free lock = %v, %v; want the lock", what, got.lock, got.err)
		}

		return got.lock
	case <-time.After(grantLimit):
		t.Fatalf("%s: a free lock was not granted within %v", what, grantLimit)
	}

	return nil
}

// lockBlocking takes a blocking lock, runs granted while holding it, and lets
// it go: through WithExclusive or WithShared when scoped, through Exclusive
// or Shared and Unlock otherwise.
func lockBlocking(path string, exclusive, scoped bool, granted func()) error {
	if scoped {
		with := flock.WithShared
		if exclusive {
			with = flock.WithExclusive
		}

		return with(path, func() error {
			granted()

			return nil
		})
	}

	take := flock.Shared
	if exclusive {
		take = flock.Exclusive
	}

	lock, err := take(path)
	if err != nil {
		return err
	}

	granted()

	return lock.Unlock()
}

// TestScopedLocks: WithExclusive and WithShared hold their lock while their
// function runs, release it after, and return its error.
func TestScopedLocks(t *testing.T) {
	t.Parallel()

	for _, exclusive := range []bool{false, true} {
		path := newLockFile(t)

		with := flock.WithShared
		if exclusive {
			with = flock.WithExclusive
		}

		err := with(path, func() error {
			lock, tryErr := flock.TryExclusive(path)
			if !errors.Is(tryErr, flock.ErrLockWouldBlock) {
				_ = lock.Unlock()

				t.Fatalf("exclusive %v: a conflicting lock was placed while the scoped one held: %v", exclusive, tryErr)
			}

			return errFunction
		})
		if !errors.Is(err, errFunction) {
			t.Fatalf("exclusive %v: scoped lock returned %v, want its function's error", exclusive, err)
		}

		lock, err := flock.TryExclusive(path)
		if err != nil {
			t.Fatalf("exclusive %v: the scoped lock was not released: %v", exclusive, err)
		}

		_ = lock.Unlock()
	}
}

// everyLock is each way to take a lock, by name.
func everyLock() map[string]func(string) (*flock.Lock, error) {
	return map[string]func(string) (*flock.Lock, error){
		"Exclusive": flock.Exclusive, "Shared": flock.Shared,
		"TryExclusive": flock.TryExclusive, "TryShared": flock.TryShared,
	}
}

// TestLockFailures: a lock that cannot be placed is ErrLockFail, from every
// call, and a scoped one does not run its function: on a path that holds no
// file, in a directory that is missing or there, and on a directory. A
// missing file is fs.ErrNotExist too, and is not created.
func TestLockFailures(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()

	for what, refused := range map[string]struct {
		path    string
		missing bool
	}{
		"in a missing directory": {filepath.Join(t.TempDir(), "no-such-directory", "lock"), true},
		"on a missing file":      {filepath.Join(empty, "lock"), true},
		"on a directory":         {t.TempDir(), false},
	} {
		for name, take := range everyLock() {
			lock, err := take(refused.path)
			if !errors.Is(err, flock.ErrLockFail) || lock != nil || errors.Is(err, fs.ErrNotExist) != refused.missing {
				t.Fatalf("%s %s = %v, %v; want ErrLockFail, no lock", name, what, lock, err)
			}
		}

		for name, with := range map[string]func(string, func() error) error{
			"WithExclusive": flock.WithExclusive, "WithShared": flock.WithShared,
		} {
			ran := false

			err := with(refused.path, func() error {
				ran = true

				return nil
			})
			if !errors.Is(err, flock.ErrLockFail) || ran {
				t.Fatalf("%s %s = %v, ran %v; want ErrLockFail, not run", name, what, err, ran)
			}
		}
	}

	alone(t, "after locks on a missing file", empty)
}

// TestLeavesTheLockFileAlone: a lock, held then released, leaves its
// directory with the lock file and nothing else.
func TestLeavesTheLockFileAlone(t *testing.T) {
	t.Parallel()

	for name, take := range everyLock() {
		path := newLockFile(t)

		lock, err := take(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		alone(t, name+", held", filepath.Dir(path), path)

		if err = lock.Unlock(); err != nil {
			t.Fatal(err)
		}

		alone(t, name+", released", filepath.Dir(path), path)
	}
}

// alone fails unless dir holds the files at paths and nothing else.
func alone(t *testing.T, what, dir string, paths ...string) {
	t.Helper()

	want := make([]string, len(paths))
	for i, path := range paths {
		want[i] = filepath.Base(path)
	}

	slices.Sort(want)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, len(entries))
	for i, entry := range entries {
		got[i] = entry.Name()
	}

	if !slices.Equal(got, want) {
		t.Fatalf("%s: the directory holds %d files, want the %d lock files alone", what, len(got), len(want))
	}
}

// TestLongName: a name of 255 characters, the longest a file system takes,
// is locked like any other: its holder conflicts with a second, another long
// name that starts alike is a lock of its own, and the two files are alone
// in their directory.
func TestLongName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, strings.Repeat("a", 254)+"1")
	second := filepath.Join(dir, strings.Repeat("a", 254)+"2")

	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	held, err := flock.Exclusive(first)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = flock.TryExclusive(first); !errors.Is(err, flock.ErrLockWouldBlock) {
		t.Fatalf("TryExclusive on a held long name = %v, want ErrLockWouldBlock", err)
	}

	other, err := flock.TryExclusive(second)
	if err != nil {
		t.Fatalf("TryExclusive on another long name = %v, want a lock of its own", err)
	}

	for _, lock := range []*flock.Lock{held, other} {
		if err = lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	}

	alone(t, "two long names", dir, first, second)
}

// waiting takes a lock on path behind holder, which the caller then lets go:
// it returns once the call is inside, holding the file open, since one that
// had not opened it yet would prove nothing about the file it waited on.
func waiting(take func(string) (*flock.Lock, error), path string) <-chan result {
	started := make(chan struct{})
	waited := make(chan result, 1)

	go func() {
		close(started)

		lock, err := take(path)
		waited <- result{lock, err}
	}()

	<-started
	time.Sleep(earlyGrantWindow)

	return waited
}

type result struct {
	lock *flock.Lock
	err  error
}

func outcome(t *testing.T, what string, waited <-chan result) result {
	t.Helper()

	select {
	case got := <-waited:
		return got
	case <-time.After(grantLimit):
		t.Fatalf("%s: the waiting call never returned", what)
	}

	return result{}
}

// TestRemovedWhileWaiting: a lock file its exclusive holder removes, as its
// last act, retires the lock: a call that waited for it fails as one that
// found no file does. Left holding the removed file, it would hold a lock
// nobody else can reach.
func TestRemovedWhileWaiting(t *testing.T) {
	t.Parallel()

	for name, take := range map[string]func(string) (*flock.Lock, error){
		"Exclusive": flock.Exclusive, "Shared": flock.Shared,
	} {
		path := newLockFile(t)

		holder, err := flock.Exclusive(path)
		if err != nil {
			t.Fatal(err)
		}

		waited := waiting(take, path)

		if err = os.Remove(path); err != nil {
			t.Fatalf("%s: removing the held lock file: %v", name, err)
		}

		if err = holder.Unlock(); err != nil {
			t.Fatal(err)
		}

		got := outcome(t, name, waited)
		if !errors.Is(got.err, flock.ErrLockFail) || !errors.Is(got.err, fs.ErrNotExist) || got.lock != nil {
			_ = got.lock.Unlock()

			t.Fatalf(
				"%s: the waiting call = %v, %v; want ErrLockFail and fs.ErrNotExist, no lock",
				name,
				got.lock,
				got.err,
			)
		}

		alone(t, name+", after the removal", filepath.Dir(path))
	}
}

// TestReplacedWhileWaiting: a call that waited on a lock file replaced
// meanwhile by another holds the file now at the path: a third caller
// conflicts with it.
func TestReplacedWhileWaiting(t *testing.T) {
	t.Parallel()

	for name, take := range map[string]func(string) (*flock.Lock, error){
		"Exclusive": flock.Exclusive, "Shared": flock.Shared,
	} {
		path := newLockFile(t)

		holder, err := flock.Exclusive(path)
		if err != nil {
			t.Fatal(err)
		}

		waited := waiting(take, path)

		if err = os.Remove(path); err != nil {
			t.Fatalf("%s: removing the held lock file: %v", name, err)
		}

		if err = os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		if err = holder.Unlock(); err != nil {
			t.Fatal(err)
		}

		got := outcome(t, name, waited)
		if got.err != nil {
			t.Fatalf("%s: the waiting call: %v", name, got.err)
		}

		third, err := flock.TryExclusive(path)
		if !errors.Is(err, flock.ErrLockWouldBlock) {
			_ = third.Unlock()

			t.Fatalf("%s: a third caller's TryExclusive = %v while the waiter holds, want ErrLockWouldBlock", name, err)
		}

		if err = got.lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReadOnlyLockFile: a lock file the user may read and not write takes
// every lock, and an exclusive one still excludes. Root writes any file, so
// the case does not exist for it.
func TestReadOnlyLockFile(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root writes any file")
	}

	path := filepath.Join(t.TempDir(), "lock")
	if err := os.WriteFile(path, nil, 0o400); err != nil {
		t.Fatal(err)
	}

	for name, take := range everyLock() {
		lock, err := take(path)
		if err != nil {
			t.Fatalf("%s on a read-only lock file: %v", name, err)
		}

		if err = lock.Unlock(); err != nil {
			t.Fatal(err)
		}
	}

	held, err := flock.Exclusive(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = flock.TryShared(path); !errors.Is(err, flock.ErrLockWouldBlock) {
		t.Fatalf("TryShared under an exclusive lock on a read-only lock file = %v, want ErrLockWouldBlock", err)
	}

	if err = held.Unlock(); err != nil {
		t.Fatal(err)
	}
}

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
	"os"
	"path/filepath"
	"slices"
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

func newLockedFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "locked")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// play runs one sequence of Try calls and Unlocks, checking the contract
// after each, then lets every holder go.
func play(t *testing.T, path string, sequence []call) {
	t.Helper()

	files := map[int]*os.File{}
	model := map[int]holder{}

	defer func() {
		for _, file := range files {
			_ = flock.Unlock(file)
		}
	}()

	for i, step := range sequence {
		if step.unlock {
			file, held := files[step.slot]

			err := flock.Unlock(file)

			switch {
			case !held && !errors.Is(err, flock.ErrLockIsNil):
				t.Fatalf("%+v, step %d: Unlock of no lock = %v, want ErrLockIsNil", sequence, i, err)
			case held && err != nil:
				t.Fatalf("%+v, step %d: Unlock = %v", sequence, i, err)
			case held && file.Close() == nil:
				t.Fatalf("%+v, step %d: Unlock left its file open", sequence, i)
			}

			delete(files, step.slot)
			delete(model, step.slot)

			continue
		}

		if _, held := files[step.slot]; held {
			continue // a slot holds one lock at a time
		}

		blocked := conflicts(model, step.exclusive)

		if step.blocking {
			if blocked {
				continue // it would wait for a holder this sequence never releases
			}

			lock := flock.ReadOnlyLock
			if step.exclusive {
				lock = flock.Lock
			}

			file, err := lock(path)
			if err != nil || file == nil {
				t.Fatalf("%+v, step %d: a free blocking lock = %v, %v; want the lock", sequence, i, file, err)
			}

			files[step.slot] = file
			model[step.slot] = holder{exclusive: step.exclusive}

			continue
		}

		try := flock.TryReadOnlyLock
		if step.exclusive {
			try = flock.TryLock
		}

		file, err := try(path)

		switch {
		case blocked && (!errors.Is(err, flock.ErrLockWouldBlock) || errors.Is(err, flock.ErrLockFail) || file != nil):
			t.Fatalf(
				"%+v, step %d: a conflicting Try = %v, %v; want ErrLockWouldBlock alone, no file",
				sequence,
				i,
				file,
				err,
			)
		case !blocked && (err != nil || file == nil):
			t.Fatalf("%+v, step %d: a free Try = %v, %v; want the lock", sequence, i, file, err)
		case !blocked:
			files[step.slot] = file
			model[step.slot] = holder{exclusive: step.exclusive}
		}
	}
}

func TestBoundedTry(t *testing.T) {
	t.Parallel()

	path := newLockedFile(t)

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

	path := newLockedFile(t)

	files := make([]*os.File, len(held))

	for i, holderExclusive := range held {
		var err error

		if holderExclusive {
			files[i], err = flock.Lock(path)
		} else {
			files[i], err = flock.ReadOnlyLock(path)
		}

		if err != nil {
			t.Fatalf("holders %v: lock %d: %v", held, i, err)
		}
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

	for _, file := range files {
		// Every release is counted before it happens: a lock granted on it
		// reads it.
		released.Add(1)

		if err := flock.Unlock(file); err != nil {
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

// lockBlocking takes a blocking lock, runs granted while holding it, and lets
// it go: through WithLock or WithReadOnlyLock when scoped, through Lock or
// ReadOnlyLock and Unlock otherwise.
func lockBlocking(path string, exclusive, scoped bool, granted func()) error {
	if scoped {
		with := flock.WithReadOnlyLock
		if exclusive {
			with = flock.WithLock
		}

		return with(path, func() error {
			granted()

			return nil
		})
	}

	lock := flock.ReadOnlyLock
	if exclusive {
		lock = flock.Lock
	}

	file, err := lock(path)
	if err != nil {
		return err
	}

	granted()

	return flock.Unlock(file)
}

// TestScopedLocks: WithLock and WithReadOnlyLock hold their lock while their
// function runs, release it after, and return its error.
func TestScopedLocks(t *testing.T) {
	t.Parallel()

	for _, exclusive := range []bool{false, true} {
		path := newLockedFile(t)

		with := flock.WithReadOnlyLock
		if exclusive {
			with = flock.WithLock
		}

		err := with(path, func() error {
			file, tryErr := flock.TryLock(path)
			if !errors.Is(tryErr, flock.ErrLockWouldBlock) {
				_ = flock.Unlock(file)

				t.Fatalf("exclusive %v: a conflicting lock was placed while the scoped one held: %v", exclusive, tryErr)
			}

			return errFunction
		})
		if !errors.Is(err, errFunction) {
			t.Fatalf("exclusive %v: scoped lock returned %v, want its function's error", exclusive, err)
		}

		file, err := flock.TryLock(path)
		if err != nil {
			t.Fatalf("exclusive %v: the scoped lock was not released: %v", exclusive, err)
		}

		_ = flock.Unlock(file)
	}
}

// TestLockFailures: a lock that cannot be placed is ErrLockFail, from every
// call, and a scoped one does not run its function.
func TestLockFailures(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "no-such-directory", "locked")

	for name, lock := range map[string]func(string) (*os.File, error){
		"Lock": flock.Lock, "ReadOnlyLock": flock.ReadOnlyLock,
		"TryLock": flock.TryLock, "TryReadOnlyLock": flock.TryReadOnlyLock,
	} {
		file, err := lock(missing)
		if !errors.Is(err, flock.ErrLockFail) || file != nil {
			t.Fatalf("%s in a missing directory = %v, %v; want ErrLockFail, no file", name, file, err)
		}
	}

	for name, with := range map[string]func(string, func() error) error{
		"WithLock": flock.WithLock, "WithReadOnlyLock": flock.WithReadOnlyLock,
	} {
		ran := false

		err := with(missing, func() error {
			ran = true

			return nil
		})
		if !errors.Is(err, flock.ErrLockFail) || ran {
			t.Fatalf("%s in a missing directory = %v, ran %v; want ErrLockFail, not run", name, err, ran)
		}
	}
}

// TestCleanup: after every lock is released and Cleanup runs, the directory
// holds the locked file alone.
func TestCleanup(t *testing.T) {
	t.Parallel()

	path := newLockedFile(t)

	for _, lock := range []func(string) (*os.File, error){flock.Lock, flock.ReadOnlyLock, flock.TryLock, flock.TryReadOnlyLock} {
		file, err := lock(path)
		if err != nil {
			t.Fatal(err)
		}

		if err = flock.Unlock(file); err != nil {
			t.Fatal(err)
		}
	}

	flock.Cleanup(path)

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("after Cleanup the directory holds %v, want the locked file alone", entries)
	}
}

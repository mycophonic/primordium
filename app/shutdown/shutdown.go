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

package shutdown

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"
)

//nolint:gochecknoglobals // the process's one shutdown state
var (
	handlers  []func()
	cancels   []context.CancelFunc
	mu        sync.Mutex
	once      sync.Once
	listening sync.Once
)

// SetDefaults returns a context cancelled by the first shutdown signal. After
// the cancellation the handlers run, with ten seconds to finish, and the
// process exits with 128 plus the signal's number, or 1 when they run out of
// time. The listener is installed once; every context handed out is cancelled.
func SetDefaults(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)

	mu.Lock()

	cancels = append(cancels, cancel)
	mu.Unlock()

	listening.Do(func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, shutdownSignals...)

		go exitOnSignal(signals)
	})

	return ctx
}

// Register adds a handler to run on shutdown, after those registered before it.
func Register(handler func()) {
	mu.Lock()

	handlers = append(handlers, handler)
	mu.Unlock()
}

// Shutdown runs the handlers in reverse order of registration, once; a later
// call returns when the first has finished. A handler must not call it: it
// would wait for itself.
func Shutdown() {
	once.Do(func() {
		mu.Lock()
		registered := slices.Clone(handlers)
		mu.Unlock()

		for _, handler := range slices.Backward(registered) {
			handler()
		}
	})
}

// ExitError is an exit status a program asks for, by returning it or an error
// wrapping it from the function Run runs. Run exits with it and says nothing:
// a program that asks for a status has said what it had to.
type ExitError int

func (e ExitError) Error() string {
	return "exit status " + strconv.Itoa(int(e))
}

// Run runs function with panic recovery, then the handlers, and exits: with
// the ExitError function returned or wrapped, silently; with 0 on nil; with 1 and
// the error logged on any other error; with 1 and the panic logged on a panic.
// A handler that panics is not recovered: the process crashes with its stack.
//
// Run never returns.
func Run(ctx context.Context, function func(context.Context) error) {
	var exitCode int

	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "panic", "value", r, "stack", string(debug.Stack()))

				exitCode = 1
			}
		}()

		var asked ExitError

		switch err := function(ctx); {
		case err == nil:
		case errors.As(err, &asked):
			exitCode = int(asked)
		default:
			slog.ErrorContext(ctx, "fatal", "error", err)

			exitCode = 1
		}
	}()

	Exit(exitCode)
}

// Exit runs the handlers and ends the process with code. It is the exit for a
// program that cannot end through Run, as one whose command library exits by
// itself does: urfave/cli's OsExiter takes it as it is.
func Exit(code int) {
	Shutdown()
	os.Exit(code) //revive:disable-line:deep-exit
}

// Go launches fn in a new goroutine with panic recovery. If fn panics,
// Shutdown runs and the process exits with code 1.
func Go(function func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic", "value", r, "stack", string(debug.Stack()))
				Exit(1)
			}
		}()

		function()
	}()
}

// exitOnSignal waits for the first signal, cancels every context, runs the
// handlers under the deadline and exits. A second signal during the handlers
// gets the runtime's default treatment, since the listener has stopped.
func exitOnSignal(signals chan os.Signal) {
	sig := <-signals
	signal.Stop(signals)

	mu.Lock()
	for _, cancel := range cancels {
		cancel()
	}
	mu.Unlock()

	done := make(chan struct{})

	go func() {
		Shutdown()
		close(done)
	}()

	select {
	case <-done:
		if syssig, ok := sig.(syscall.Signal); ok {
			//nolint:mnd // 128 + signal is the shell convention
			os.Exit(128 + int(syssig)) //revive:disable-line:deep-exit
		}

		os.Exit(0) //revive:disable-line:deep-exit
	case <-time.After(shutdownTimeout):
		slog.Error("shutdown timed out, some operations may not have completed cleanly")
		os.Exit(1) //revive:disable-line:deep-exit
	}
}

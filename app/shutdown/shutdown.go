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
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"sync"
	"syscall"
	"time"
)

//nolint:gochecknoglobals // Shutdown state.
var (
	shutdownHandlers []func()
	shutdownMu       sync.Mutex
	shutdownOnce     sync.Once
	setDefaultsOnce  sync.Once
)

// SetDefaults registers signal handlers, exit with timeout.
// Safe to call multiple times; only the first call has effect.
func SetDefaults(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)

	setDefaultsOnce.Do(func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, shutdownSignals...)

		go func() {
			sig := <-sigChan
			signal.Stop(sigChan)
			cancel()

			// Run shutdown handlers with timeout.
			done := make(chan struct{})

			go func() {
				Shutdown()
				close(done)
			}()

			select {
			case <-done:
				// Graceful shutdown completed, use conventional signal exit code (128 + signal number).
				if syssig, ok := sig.(syscall.Signal); ok {
					//nolint:mnd // 128 + signal is conventional.
					os.Exit(128 + int(syssig)) //revive:disable-line:deep-exit
				}

				os.Exit(0) //revive:disable-line:deep-exit
			case <-time.After(shutdownTimeout):
				slog.Error("shutdown timed out, some operations may not have completed cleanly")
				os.Exit(1) //revive:disable-line:deep-exit
			}
		}()
	})

	return ctx
}

// Register adds a handler to be run on shutdown.
func Register(handler func()) {
	shutdownMu.Lock()

	shutdownHandlers = append(shutdownHandlers, handler)

	shutdownMu.Unlock()
}

// Shutdown executes handlers in reverse order, exactly once.
func Shutdown() {
	shutdownOnce.Do(func() {
		shutdownMu.Lock()

		handlers := make([]func(), len(shutdownHandlers))
		copy(handlers, shutdownHandlers)
		shutdownMu.Unlock()

		for _, v := range slices.Backward(handlers) {
			v()
		}
	})
}

// Run executes fn with panic recovery. On panic or non-nil error return,
// it runs Shutdown and exits with code 1. On normal return, it runs
// Shutdown and exits with code 0.
//
// Shutdown is called outside the recovery scope — if a handler panics,
// it crashes with a stack trace rather than being silently swallowed.
//
// Run never returns.
func Run(ctx context.Context, function func(context.Context) error) {
	var exitCode int

	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic", "value", r, "stack", string(debug.Stack()))

				exitCode = 1
			}
		}()

		if err := function(ctx); err != nil {
			slog.Error("fatal", "error", err)

			exitCode = 1
		}
	}()

	Shutdown()
	os.Exit(exitCode) //revive:disable-line:deep-exit
}

// Go launches fn in a new goroutine with panic recovery. If fn panics,
// Shutdown runs and the process exits with code 1.
func Go(function func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic", "value", r, "stack", string(debug.Stack()))
				Shutdown()
				os.Exit(1) //revive:disable-line:deep-exit
			}
		}()

		function()
	}()
}

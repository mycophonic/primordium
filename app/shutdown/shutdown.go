package shutdown

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

//nolint:gochecknoglobals // Shutdown state
var (
	shutdownHandlers []func()
	shutdownMu       sync.Mutex
	shutdownOnce     sync.Once
)

// SetDefaults registers signal handlers, exit with timeout.
func SetDefaults(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)

	// Run garbage collection on shutdown
	shutdownMu.Lock()
	defer shutdownMu.Unlock()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		signal.Stop(sigChan)
		cancel()

		// Run shutdown handlers with timeout
		done := make(chan struct{})

		go func() {
			Shutdown()
			close(done)
		}()

		select {
		case <-done:
			// Graceful shutdown completed, use conventional signal exit code (128 + signal number)
			if syssig, ok := sig.(syscall.Signal); ok {
				//nolint:mnd // 128 + signal is conventional
				os.Exit(128 + int(syssig)) //revive:disable-line:deep-exit
			}

			os.Exit(0) //revive:disable-line:deep-exit
		case <-time.After(shutdownTimeout):
			slog.Error("shutdown timed out, some operations may not have completed cleanly")
			os.Exit(1) //revive:disable-line:deep-exit
		}
	}()

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

		for i := len(handlers) - 1; i >= 0; i-- {
			handlers[i]()
		}
	})
}

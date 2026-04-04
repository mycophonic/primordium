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

package app

import (
	"context"
	"log/slog"

	"github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/reporter"
	"github.com/mycophonic/primordium/app/shutdown"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/network"
	"github.com/mycophonic/primordium/store"
	"github.com/mycophonic/primordium/system/rlimit"
)

// Options provides configurable hooks for applications that will decide of app-derived common locations and crash
// reporter behavior.
type Options struct {
	Name        string
	Version     string
	Environment string
	DSN         string
}

// New configures application lifecycle and returns a context that is
// cancelled on shutdown signals.
func New(ctx context.Context, opts *Options) context.Context {
	if opts.Name == "" {
		panic("app.New: Options.Name must not be empty")
	}

	// Initialize subsystems

	// Filesystem first. Wipe out system umask, set the app name for locations.
	filesystem.Initialize(opts.Name)

	// Set the logger: honor LOG_LEVEL env var, set the zerolog wrapper for slog.
	debug := logger.SetDefaultsForLogger(ctx)

	// Configure the default http and ssh configurations.
	network.SetDefaults()

	// Setup Sentry.
	if opts.DSN != "" {
		if err := reporter.Initialize(&reporter.Config{
			DSN:              opts.DSN,
			Debug:            debug,
			PII:              true,
			Release:          opts.Version,
			Environment:      opts.Environment,
			TracesSampleRate: 1.0,
		}); err != nil {
			slog.Error("failed to initialize reporter", "err", err)
		}
	} else {
		slog.Warn("dsn not provided: crash collection disabled")
	}

	// On modern systems, this is generally reasonable.
	rlimit.RaiseNoFileLimit()

	// Register shutdown handlers. Handlers run in reverse order, so reporter
	// flushes last (after stores close) to capture any final error events.
	shutdown.Register(reporter.Shutdown)
	shutdown.Register(store.Shutdown)

	ctx = shutdown.SetDefaults(ctx)

	return ctx
}

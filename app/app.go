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

// Options names the program. Name decides its directories and must be set;
// Version and Environment tag its crash reports; DSN is where they go, and an
// empty one disables crash collection.
type Options struct {
	Name        string
	Version     string
	Environment string
	DSN         string
}

// New sets the process up and returns a context cancelled by a shutdown
// signal. Once per process: the network defaults refuse a second install.
// The reporter and the stores are shutdown handlers, which run on a signal
// or through shutdown.Run, never on a plain return from main.
func New(ctx context.Context, opts *Options) context.Context {
	if opts.Name == "" {
		panic("app.New: Options.Name must not be empty")
	}

	// Locations first: everything after may log or write under the name.
	filesystem.Initialize(opts.Name)

	debug := logger.SetDefaultsForLogger(ctx)

	network.SetDefaults()

	if opts.DSN != "" {
		if err := reporter.Initialize(&reporter.Config{
			DSN:              opts.DSN,
			Debug:            debug,
			PII:              true,
			Release:          opts.Version,
			Environment:      opts.Environment,
			TracesSampleRate: 1.0,
		}); err != nil {
			slog.ErrorContext(ctx, "failed to initialize reporter", "err", err)
		}
	} else {
		slog.WarnContext(ctx, "dsn not provided: crash collection disabled")
	}

	rlimit.RaiseNoFileLimit()

	// Reverse order: the reporter flushes last, after the stores have closed
	// and logged whatever closing them raised.
	shutdown.Register(reporter.Shutdown)
	shutdown.Register(store.Shutdown)

	ctx = shutdown.SetDefaults(ctx)

	return ctx
}

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

// Options names the program and sets its crash reporting. Name decides its
// directories and must be set; Version and Environment tag its crash reports;
// DSN is where they go, and an empty one disables crash collection. PII lets
// the reports carry user and request data; TracesSampleRate is the share of
// transactions traced, and zero traces none.
type Options struct {
	Name             string
	Version          string
	Environment      string
	DSN              string
	PII              bool
	TracesSampleRate float64
}

// Main sets the process up under opts and runs run with a context a shutdown
// signal cancels. When run returns, the shutdown handlers run and the process
// exits: with the shutdown.ExitError run returned or wrapped, 0 on nil, 1 on any
// other error or a panic. Main never returns. Once per process: the network
// defaults refuse a second install. A program whose command library exits by
// itself, as urfave/cli does through its OsExiter, points that exit at
// shutdown.Exit, so that the handlers run first.
func Main(opts *Options, run func(ctx context.Context) error) {
	shutdown.Run(setUp(context.Background(), opts), run)
}

// setUp is Main before run: the locations, the logger, the network defaults,
// the reporter, the file limit, the handlers, and the context.
func setUp(ctx context.Context, opts *Options) context.Context {
	if opts.Name == "" {
		panic("app.Main: Options.Name must not be empty")
	}

	// Locations first: everything after may log or write under the name.
	filesystem.Initialize(opts.Name)

	debug := logger.SetDefaultsForLogger(ctx)

	network.SetDefaults()

	if opts.DSN != "" {
		if err := reporter.Initialize(&reporter.Config{
			DSN:              opts.DSN,
			Debug:            debug,
			PII:              opts.PII,
			Release:          opts.Version,
			Environment:      opts.Environment,
			TracesSampleRate: opts.TracesSampleRate,
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

	return shutdown.SetDefaults(ctx)
}

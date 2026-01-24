package app

import (
	"context"

	"github.com/farcloser/primordium/app/logger"
	"github.com/farcloser/primordium/app/shutdown"
	"github.com/farcloser/primordium/filesystem"
	"github.com/farcloser/primordium/network"
)

// New does configure application lifecycle.
func New(ctx context.Context, name string) {
	logger.SetDefaultsForLogger(ctx)
	network.SetDefaults()
	shutdown.SetDefaults(ctx)

	filesystem.Inititalize(name)
}

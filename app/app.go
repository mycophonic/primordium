package app

import (
	"context"

	"github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/shutdown"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/network"
)

// New does configure application lifecycle.
func New(ctx context.Context, name string) {
	logger.SetDefaultsForLogger(ctx)
	network.SetDefaults()
	shutdown.SetDefaults(ctx)

	filesystem.Inititalize(name)
}

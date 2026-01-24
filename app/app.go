package app

import (
	"context"

	"github.com/farcloser/primordium/app/logger"
	"github.com/farcloser/primordium/app/shutdown"
	"github.com/farcloser/primordium/filesystem"
	"github.com/farcloser/primordium/network"
)

// Application is the requirement for app lifecycle to start .
type Application interface {
	Name() string
}

// New does configure application lifecycle.
func New(ctx context.Context, definition Application) {
	logger.SetDefaultsForLogger(ctx)
	network.SetDefaults()
	shutdown.SetDefaults(ctx)

	filesystem.Inititalize(definition.Name())
}

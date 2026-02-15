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

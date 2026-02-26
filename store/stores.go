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

package store

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
)

const (
	cacheSubdir        = "store"
	contentIndexSubdir = "content-index"
	volatileSubdir     = "volatile"
)

//nolint:gochecknoglobals // Global stores with lazy initialization
var (
	cacheStore    *Cache
	contentStore  *ContentStore
	volatileStore *Volatile
	cacheOnce     sync.Once
	contentOnce   sync.Once
	volatileOnce  sync.Once
)

// GetStoreCache returns the global cache store instance, initializing it on first access.
// Registers a shutdown handler to run garbage collection on exit.
func GetStoreCache() *Cache {
	cacheOnce.Do(func() {
		cacheDir, err := filesystem.CacheDir()
		if err != nil {
			panic(fmt.Errorf("%w: %w", fault.ErrSystemFailure, err))
		}

		cacheStore = NewCache(filepath.Join(cacheDir, cacheSubdir))
	})

	return cacheStore
}

// GetContentStore returns the global content store instance, initializing it on first access.
// The content store provides identifier-based lookup over the shared cache.
func GetContentStore() *ContentStore {
	contentOnce.Do(func() {
		cache := GetStoreCache()

		cacheDir, err := filesystem.CacheDir()
		if err != nil {
			panic(fmt.Errorf("%w: %w", fault.ErrSystemFailure, err))
		}

		contentStore = NewContentStore(cache, filepath.Join(cacheDir, contentIndexSubdir))
	})

	return contentStore
}

// GetStoreVolatile returns the global volatile store instance, initializing it on first access.
func GetStoreVolatile() *Volatile {
	volatileOnce.Do(func() {
		runtimeDir, err := filesystem.RuntimeDir()
		if err != nil {
			panic(fmt.Errorf("%w: %w", fault.ErrSystemFailure, err))
		}

		volatileStore = NewVolatile(filepath.Join(runtimeDir, volatileSubdir), digest.BLAKE2b256)
	})

	return volatileStore
}

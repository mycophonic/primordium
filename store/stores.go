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
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/mycophonic/primordium/digest"
	"github.com/mycophonic/primordium/filesystem/dirs"
	"github.com/mycophonic/primordium/store/cache"
	"github.com/mycophonic/primordium/store/content"
	"github.com/mycophonic/primordium/store/volatile"
)

//nolint:gochecknoglobals // Global stores with lazy initialization
var (
	cacheStore    *cache.Cache
	contentStore  *content.Store
	volatileStore *volatile.Volatile
	cacheOnce     sync.Once
	contentOnce   sync.Once
	volatileOnce  sync.Once
)

// GetCache returns the global cache store instance, initializing it on first access.
// Registering a shutdown handler to run garbage collection is the caller's responsibility.
func GetCache() *cache.Cache {
	cacheOnce.Do(func() {
		cacheDir, err := dirs.CacheDir()
		if err != nil {
			panic(err)
		}

		cacheStore = cache.New(filepath.Join(cacheDir, cacheSubdir), cache.DefaultCacheQuota)
	})

	return cacheStore
}

// GetContent returns the global content store instance, initializing it on first access.
// The content store provides identifier-based lookup over its own internal cache.
func GetContent() *content.Store {
	contentOnce.Do(func() {
		cacheDir, err := dirs.CacheDir()
		if err != nil {
			panic(err)
		}

		contentStore, err = content.New(filepath.Join(cacheDir, contentSubdir), nil)
		if err != nil {
			panic(err)
		}
	})

	return contentStore
}

// GetVolatile returns the global volatile store instance, initializing it on first access.
func GetVolatile() *volatile.Volatile {
	volatileOnce.Do(func() {
		runtimeDir, err := dirs.RuntimeDir()
		if err != nil {
			panic(err)
		}

		volatileStore = volatile.New(filepath.Join(runtimeDir, volatileSubdir), digest.BLAKE2b256)
	})

	return volatileStore
}

// Shutdown closes stores that were initialized. Safe to call even if no
// stores were ever created. Intended to be registered via shutdown.Register
// from the app package.
func Shutdown() {
	if contentStore != nil {
		if err := contentStore.Close(); err != nil {
			slog.Error("content store close failed", "error", err)
		}
	}
}

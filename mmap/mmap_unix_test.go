//go:build unix

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

//revive:disable:add-constant
package mmap_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/mmap"
)

// createTempFile creates a temp file of the given size, filled with zeros.
func createTempFile(t *testing.T, size int) *os.File {
	t.Helper()

	f, err := xos.CreateTemp(t.TempDir(), "mmap-test-*")
	assert.NilError(t, err)

	assert.NilError(t, f.Truncate(int64(size)))

	return f
}

func TestMapFile_WriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	f := createTempFile(t, 4096)
	defer f.Close()

	data, mapping, err := mmap.MapFile(f, 4096)
	assert.NilError(t, err)
	assert.Assert(t, len(data) == 4096)

	// Write through the mapping.
	copy(data, []byte("hello mmap"))

	// Sync to disk.
	assert.NilError(t, mmap.SyncFile(data, f))

	// Read back from the file directly.
	buf := make([]byte, 10)
	_, err = f.ReadAt(buf, 0)
	assert.NilError(t, err)
	assert.Equal(t, string(buf), "hello mmap")

	assert.NilError(t, mmap.UnmapFile(data, mapping))
}

func TestMapFile_PersistsAfterUnmap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "persist-test")

	f, err := xos.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644) //nolint:mnd
	assert.NilError(t, err)
	assert.NilError(t, f.Truncate(64))

	data, mapping, err := mmap.MapFile(f, 64)
	assert.NilError(t, err)

	copy(data, []byte("persistent"))

	assert.NilError(t, mmap.SyncFile(data, f))
	assert.NilError(t, mmap.UnmapFile(data, mapping))
	assert.NilError(t, f.Close())

	// Reopen and verify.
	content, err := xos.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(content[:10]), "persistent")
}

func TestMapFile_ZeroSize(t *testing.T) {
	t.Parallel()

	f := createTempFile(t, 0)
	defer f.Close()

	_, _, err := mmap.MapFile(f, 0)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, fault.ErrInvalidArgument))
}

func TestMapFile_NegativeSize(t *testing.T) {
	t.Parallel()

	f := createTempFile(t, 0)
	defer f.Close()

	_, _, err := mmap.MapFile(f, -1)
	assert.Assert(t, err != nil)
	assert.Assert(t, errors.Is(err, fault.ErrInvalidArgument))
}

func TestUnmapFile_EmptySlice(t *testing.T) {
	t.Parallel()

	err := mmap.UnmapFile(nil, mmap.Mapping{})
	assert.NilError(t, err)

	err = mmap.UnmapFile([]byte{}, mmap.Mapping{})
	assert.NilError(t, err)
}

func TestSyncFile_EmptySlice(t *testing.T) {
	t.Parallel()

	err := mmap.SyncFile(nil, nil)
	assert.NilError(t, err)

	err = mmap.SyncFile([]byte{}, nil)
	assert.NilError(t, err)
}

func TestMapFile_MultipleRegionsIndependent(t *testing.T) {
	t.Parallel()

	f := createTempFile(t, 8192)
	defer f.Close()

	data1, mapping1, err := mmap.MapFile(f, 8192)
	assert.NilError(t, err)

	data2, mapping2, err := mmap.MapFile(f, 8192)
	assert.NilError(t, err)

	// Write through one mapping, read through the other.
	copy(data1[4096:], []byte("shared"))

	assert.NilError(t, mmap.SyncFile(data1, f))

	assert.Equal(t, string(data2[4096:4096+6]), "shared")

	assert.NilError(t, mmap.UnmapFile(data1, mapping1))
	assert.NilError(t, mmap.UnmapFile(data2, mapping2))
}

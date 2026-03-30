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
package r2_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/r2"
)

func TestUpload_ZeroSize(t *testing.T) {
	t.Parallel()

	env := setup(t)

	err := env.client.Upload(
		context.Background(),
		"zero.bin",
		bytes.NewReader(nil),
		0,
		r2.MultipartOptions{StateDir: t.TempDir()},
	)
	if err == nil {
		t.Fatal("Upload with totalSize=0 should fail")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected fault.ErrInvalidArgument, got: %v", err)
	}
}

func TestUpload_NegativeSize(t *testing.T) {
	t.Parallel()

	env := setup(t)

	err := env.client.Upload(
		context.Background(),
		"negative.bin",
		bytes.NewReader(nil),
		-1,
		r2.MultipartOptions{StateDir: t.TempDir()},
	)
	if err == nil {
		t.Fatal("Upload with negative totalSize should fail")
	}

	if !errors.Is(err, fault.ErrInvalidArgument) {
		t.Errorf("expected fault.ErrInvalidArgument, got: %v", err)
	}
}

func TestUpload_SinglePart(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 1024)
	source := bytes.NewReader(data)

	stateDir := t.TempDir()
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"single-part.bin",
		source,
		int64(len(data)),
		r2.MultipartOptions{
			PartSize: 5 << 20,
			StateDir: stateDir,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// State file should be cleaned up.
	entries, _ := xos.ReadDir(stateDir)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			t.Errorf("state file not cleaned up: %s", entry.Name())
		}
	}

	// Verify content round-trips through download.
	err = env.client.Download(context.Background(), "single-part.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "single-part.bin"))
	if err != nil {
		t.Fatalf("read downloaded: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("round-trip content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestUpload_MultiPart(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// 5 MiB minimum part size, 12 MiB total → 3 parts (5 + 5 + 2).
	partSize := int64(5 << 20)
	totalSize := int64(12 << 20)

	data := randomBytes(t, int(totalSize))
	source := bytes.NewReader(data)

	stateDir := t.TempDir()
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"multi-part.bin",
		source,
		totalSize,
		r2.MultipartOptions{
			PartSize:    partSize,
			StateDir:    stateDir,
			Concurrency: 1,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Verify round-trip.
	err = env.client.Download(context.Background(), "multi-part.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "multi-part.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("multipart round-trip mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestUpload_ConcurrentWorkers(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// 4 parts with 4 concurrent workers.
	partSize := int64(5 << 20)
	totalSize := int64(20 << 20)

	data := randomBytes(t, int(totalSize))
	source := bytes.NewReader(data)

	stateDir := t.TempDir()
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"concurrent.bin",
		source,
		totalSize,
		r2.MultipartOptions{
			PartSize:    partSize,
			StateDir:    stateDir,
			Concurrency: 4,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Verify content is correct despite concurrent uploads.
	err = env.client.Download(context.Background(), "concurrent.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "concurrent.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("concurrent upload round-trip mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestUpload_ExactPartBoundary(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// Exactly 2 full parts, no remainder.
	partSize := int64(5 << 20)
	totalSize := int64(10 << 20)

	data := randomBytes(t, int(totalSize))
	source := bytes.NewReader(data)

	stateDir := t.TempDir()
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"exact-boundary.bin",
		source,
		totalSize,
		r2.MultipartOptions{
			PartSize:    partSize,
			StateDir:    stateDir,
			Concurrency: 2,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	err = env.client.Download(context.Background(), "exact-boundary.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "exact-boundary.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("exact boundary round-trip mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestUpload_SmallPartSize_NormalisedToDefault(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// PartSize below minimum — should be normalised to default (100 MiB).
	// A 1 KiB file with 100 MiB part size = 1 part.
	data := randomBytes(t, 1024)
	source := bytes.NewReader(data)

	stateDir := t.TempDir()
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"small-partsize.bin",
		source,
		int64(len(data)),
		r2.MultipartOptions{
			PartSize: 100, // Way below minimum.
			StateDir: stateDir,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	err = env.client.Download(context.Background(), "small-partsize.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "small-partsize.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after upload with normalised part size")
	}
}

func TestUpload_StateCleanedUpOnSuccess(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 5<<20+1) // Just over 1 part.
	source := bytes.NewReader(data)

	stateDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"state-cleanup.bin",
		source,
		int64(len(data)),
		r2.MultipartOptions{
			PartSize: 5 << 20,
			StateDir: stateDir,
		},
	)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// No .json files should remain in stateDir.
	entries, err := xos.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read state dir: %v", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			t.Errorf("state file not cleaned up after successful upload: %s", entry.Name())
		}
	}
}

func TestUpload_OverwritesExistingObject(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// Upload first version.
	dataV1 := randomBytes(t, 1024)
	sourceV1 := bytes.NewReader(dataV1)

	stateDir := t.TempDir()

	err := env.client.Upload(
		context.Background(),
		"overwrite.bin",
		sourceV1,
		int64(len(dataV1)),
		r2.MultipartOptions{StateDir: stateDir},
	)
	if err != nil {
		t.Fatalf("Upload v1: %v", err)
	}

	// Upload second version (different content, same key).
	dataV2 := randomBytes(t, 2048)
	sourceV2 := bytes.NewReader(dataV2)

	err = env.client.Upload(
		context.Background(),
		"overwrite.bin",
		sourceV2,
		int64(len(dataV2)),
		r2.MultipartOptions{StateDir: stateDir},
	)
	if err != nil {
		t.Fatalf("Upload v2: %v", err)
	}

	// Download and verify v2 content.
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err = env.client.Download(context.Background(), "overwrite.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "overwrite.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, dataV2) {
		t.Error("downloaded content should be v2, not v1")
	}
}

func TestUpload_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 1024)

	tests := []struct {
		name string
		key  string
	}{
		{"dotdot", "../escape.bin"},
		{"empty", ""},
		{"leading slash", "/etc/passwd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := env.client.Upload(
				context.Background(),
				tc.key,
				bytes.NewReader(data),
				int64(len(data)),
				r2.MultipartOptions{StateDir: t.TempDir()},
			)
			if err == nil {
				t.Fatalf("expected error for key %q, got nil", tc.key)
			}

			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Errorf("expected fault.ErrInvalidArgument for key %q, got: %v", tc.key, err)
			}
		})
	}
}

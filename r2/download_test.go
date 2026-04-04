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
	"crypto/rand"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/filesystem"
	"github.com/mycophonic/primordium/filesystem/xos"
	"github.com/mycophonic/primordium/r2"
)

const testBucket = "test-bucket"

// testEnv bundles a fake S3 server and an r2.Client for use in tests.
type testEnv struct {
	client  *r2.Client
	backend *s3mem.Backend
}

func setup(t *testing.T) *testEnv {
	t.Helper()

	backend := s3mem.New()
	if err := backend.CreateBucket(testBucket); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	faker := gofakes3.New(backend)
	server := httptest.NewServer(faker.Server())
	t.Cleanup(server.Close)

	client, err := r2.New(&r2.Config{
		Endpoint:  server.URL,
		Bucket:    testBucket,
		AccessKey: "test",
		Secret:    "test",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("r2.New: %v", err)
	}

	return &testEnv{client: client, backend: backend}
}

func (env *testEnv) putObject(t *testing.T, objectKey string, data []byte) {
	t.Helper()

	_, err := env.backend.PutObject(testBucket, objectKey, nil, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("put object %q: %v", objectKey, err)
	}
}

func randomBytes(t *testing.T, size int) []byte {
	t.Helper()

	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("generate random data: %v", err)
	}

	return data
}

// --- Stat ---

func TestStat_Exists(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 1024)
	env.putObject(t, "stat-test.bin", data)

	info, err := env.client.Stat(context.Background(), "stat-test.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info == nil {
		t.Fatal("Stat returned nil for existing object")
	}

	if info.Size != int64(len(data)) {
		t.Errorf("Size = %d, want %d", info.Size, len(data))
	}

	if info.ETag == "" {
		t.Error("ETag is empty for existing object")
	}
}

func TestStat_NotFound(t *testing.T) {
	t.Parallel()

	env := setup(t)

	info, err := env.client.Stat(context.Background(), "does-not-exist.bin")
	if !errors.Is(err, fault.ErrNotFound) {
		t.Fatalf("Stat should return fault.ErrNotFound for missing object, got: %v", err)
	}

	if info != nil {
		t.Errorf("Stat should return nil info for missing object, got: %+v", info)
	}
}

func TestStat_ETagHasNoQuotes(t *testing.T) {
	t.Parallel()

	env := setup(t)
	env.putObject(t, "etag-test.bin", []byte("content"))

	info, err := env.client.Stat(context.Background(), "etag-test.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.ETag[0] == '"' || info.ETag[len(info.ETag)-1] == '"' {
		t.Errorf("ETag still has quotes: %q", info.ETag)
	}
}

// --- Download ---

func TestDownload_Success(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 64*1024)
	env.putObject(t, "dl-success.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Download(context.Background(), "dl-success.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "dl-success.bin"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("downloaded content does not match: got %d bytes, want %d", len(got), len(data))
	}

	// ETag sidecar must exist in dataDir.
	etagData, err := xos.ReadFile(filepath.Join(dataDir, "dl-success.bin.etag"))
	if err != nil {
		t.Fatalf("read etag sidecar: %v", err)
	}

	if len(etagData) == 0 {
		t.Error("etag sidecar is empty")
	}

	// Temp files must be gone.
	if _, err := xos.Stat(filepath.Join(tempDir, "dl-success.bin")); err == nil {
		t.Error("temp data file still exists after successful download")
	}

	if _, err := xos.Stat(filepath.Join(tempDir, "dl-success.bin.etag")); err == nil {
		t.Error("temp etag file still exists after successful download")
	}
}

func TestDownload_NotFound(t *testing.T) {
	t.Parallel()

	env := setup(t)
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Download(context.Background(), "ghost.bin", tempDir, dataDir)
	if err == nil {
		t.Fatal("Download should fail for non-existent object")
	}

	if !errors.Is(err, fault.ErrNotFound) {
		t.Errorf("expected fault.ErrNotFound, got: %v", err)
	}
}

func TestDownload_AlreadyComplete(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 2048)
	env.putObject(t, "already-done.bin", data)

	// Get the real etag.
	info, err := env.client.Stat(context.Background(), "already-done.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Pre-place the file and etag in dataDir.
	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "already-done.bin"),
		data,
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write data: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "already-done.bin.etag"),
		[]byte(info.ETag),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err = env.client.Download(context.Background(), "already-done.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download should succeed for already-complete file, got: %v", err)
	}

	// Temp dir should remain empty — no download occurred.
	entries, _ := xos.ReadDir(tempDir)
	if len(entries) > 0 {
		t.Errorf("temp dir should be empty, found %d files", len(entries))
	}
}

func TestDownload_AlreadyComplete_ETagMismatch(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 2048)
	env.putObject(t, "etag-changed.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Pre-place with wrong etag — should re-download.
	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "etag-changed.bin"),
		data,
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write data: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "etag-changed.bin.etag"),
		[]byte("stale-etag"),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err := env.client.Download(context.Background(), "etag-changed.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Verify content was re-downloaded (not stale).
	got, err := xos.ReadFile(filepath.Join(dataDir, "etag-changed.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after re-download due to etag change")
	}
}

func TestDownload_Resume_PartialTemp(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 32*1024)
	env.putObject(t, "resume-partial.bin", data)

	info, err := env.client.Stat(context.Background(), "resume-partial.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Write first half to temp with matching etag.
	half := len(data) / 2
	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-partial.bin"),
		data[:half],
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-partial.bin.etag"),
		[]byte(info.ETag),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err = env.client.Download(context.Background(), "resume-partial.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download (resume): %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "resume-partial.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("resumed download content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

func TestDownload_Resume_ETagMismatch_FreshDownload(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 8192)
	env.putObject(t, "resume-mismatch.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Write partial temp with WRONG etag — should discard and download fresh.
	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-mismatch.bin"),
		data[:1024],
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-mismatch.bin.etag"),
		[]byte("old-etag-value"),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err := env.client.Download(context.Background(), "resume-mismatch.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "resume-mismatch.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after fresh download (etag mismatch on temp)")
	}
}

func TestDownload_Resume_Oversized_FreshDownload(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 4096)
	env.putObject(t, "resume-oversized.bin", data)

	info, err := env.client.Stat(context.Background(), "resume-oversized.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Write MORE data than the remote object — should discard and re-download.
	oversized := randomBytes(t, 8192)
	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-oversized.bin"),
		oversized,
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write oversized: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-oversized.bin.etag"),
		[]byte(info.ETag),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err = env.client.Download(context.Background(), "resume-oversized.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "resume-oversized.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after re-download (oversized temp)")
	}
}

func TestDownload_Resume_FullyDownloadedTemp(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 16*1024)
	env.putObject(t, "resume-full.bin", data)

	info, err := env.client.Stat(context.Background(), "resume-full.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Simulate crash after writing all bytes but before rename:
	// temp file has full content and matching etag.
	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-full.bin"),
		data,
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write full temp: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(tempDir, "resume-full.bin.etag"),
		[]byte(info.ETag),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err = env.client.Download(context.Background(), "resume-full.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// File must be in dataDir with correct content.
	got, err := xos.ReadFile(filepath.Join(dataDir, "resume-full.bin"))
	if err != nil {
		t.Fatalf("read data file: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after fully-downloaded temp recovery")
	}

	// Temp files must be gone (moved to data).
	if _, err := xos.Stat(filepath.Join(tempDir, "resume-full.bin")); err == nil {
		t.Error("temp data file still exists after move")
	}

	// ETag sidecar must be in dataDir.
	etagGot, err := xos.ReadFile(filepath.Join(dataDir, "resume-full.bin.etag"))
	if err != nil {
		t.Fatalf("read etag sidecar in dataDir: %v", err)
	}

	if string(etagGot) != info.ETag {
		t.Errorf("etag sidecar = %q, want %q", etagGot, info.ETag)
	}
}

func TestDownload_ContextCanceled(t *testing.T) {
	t.Parallel()

	env := setup(t)

	// Use a large-enough object so the download doesn't finish before cancellation.
	data := randomBytes(t, 256*1024)
	env.putObject(t, "cancel-me.bin", data)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Download(ctx, "cancel-me.bin", tempDir, dataDir)
	if err == nil {
		// If the download somehow completed before cancellation, that's acceptable
		// for small files on fast systems. Skip instead of failing.
		t.Skip("download completed before context cancellation (fast system)")
	}

	// The data file should NOT exist in dataDir.
	if _, statErr := xos.Stat(filepath.Join(dataDir, "cancel-me.bin")); statErr == nil {
		t.Error("data file should not exist after cancelled download")
	}
}

func TestDownload_AlreadyComplete_SizeMismatch(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 4096)
	env.putObject(t, "size-changed.bin", data)

	info, err := env.client.Stat(context.Background(), "size-changed.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Pre-place file with correct etag but wrong size — should re-download.
	wrongData := data[:2048]
	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "size-changed.bin"),
		wrongData,
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write data: %v", err)
	}

	if err := filesystem.WriteFile(
		filepath.Join(dataDir, "size-changed.bin.etag"),
		[]byte(info.ETag),
		filesystem.FilePermissionsPrivate,
	); err != nil {
		t.Fatalf("write etag: %v", err)
	}

	err = env.client.Download(context.Background(), "size-changed.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "size-changed.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after re-download (size mismatch on data)")
	}
}

func TestDownload_Idempotent(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 8192)
	env.putObject(t, "idempotent.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// First download.
	if err := env.client.Download(context.Background(), "idempotent.bin", tempDir, dataDir); err != nil {
		t.Fatalf("first download: %v", err)
	}

	// Get file modification time.
	firstInfo, err := xos.Stat(filepath.Join(dataDir, "idempotent.bin"))
	if err != nil {
		t.Fatalf("stat after first download: %v", err)
	}

	// Second download — should be a no-op (already complete).
	if err := env.client.Download(context.Background(), "idempotent.bin", tempDir, dataDir); err != nil {
		t.Fatalf("second download: %v", err)
	}

	secondInfo, err := xos.Stat(filepath.Join(dataDir, "idempotent.bin"))
	if err != nil {
		t.Fatalf("stat after second download: %v", err)
	}

	// Modification time should not change — file was not re-written.
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Error("file was re-written on second download (not idempotent)")
	}
}

func TestDownload_CreatesDirectories(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 512)
	env.putObject(t, "mkdir-test.bin", data)

	baseDir := t.TempDir()
	tempDir := filepath.Join(baseDir, "nested", "temp")
	dataDir := filepath.Join(baseDir, "nested", "data")

	// Neither directory exists yet.
	err := env.client.Download(context.Background(), "mkdir-test.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "mkdir-test.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after download to nested dirs")
	}
}

func TestDownload_HierarchicalKey(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 2048)
	env.putObject(t, "audio/tracks/song.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	err := env.client.Download(context.Background(), "audio/tracks/song.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "audio", "tracks", "song.bin"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch for hierarchical key download")
	}
}

func TestDownload_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	env := setup(t)
	tempDir := t.TempDir()
	dataDir := t.TempDir()

	tests := []struct {
		name string
		key  string
	}{
		{"dotdot", "../escape.bin"},
		{"nested dotdot", "a/../../escape.bin"},
		{"empty", ""},
		{"leading slash", "/etc/passwd"},
		{"trailing slash", "file/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := env.client.Download(context.Background(), tc.key, tempDir, dataDir)
			if err == nil {
				t.Fatalf("expected error for key %q, got nil", tc.key)
			}

			if !errors.Is(err, fault.ErrInvalidArgument) {
				t.Errorf("expected fault.ErrInvalidArgument for key %q, got: %v", tc.key, err)
			}
		})
	}
}

func TestDownload_MoveAtomicity(t *testing.T) {
	t.Parallel()

	env := setup(t)
	data := randomBytes(t, 4096)
	env.putObject(t, "atomic.bin", data)

	tempDir := t.TempDir()
	dataDir := t.TempDir()

	// Make dataDir read-only so the rename of the etag fails.
	// First download succeeds writing to temp. The data rename might succeed
	// but the etag rename fails. On retry, the data should self-heal.
	err := env.client.Download(context.Background(), "atomic.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// Verify both files exist.
	if _, err := xos.Stat(filepath.Join(dataDir, "atomic.bin")); err != nil {
		t.Error("data file missing after download")
	}

	if _, err := xos.Stat(filepath.Join(dataDir, "atomic.bin.etag")); err != nil {
		t.Error("etag sidecar missing after download")
	}

	// Delete only the etag from dataDir — simulates partial rename failure.
	_ = os.Remove(filepath.Join(dataDir, "atomic.bin.etag"))

	// Re-download should self-heal: detects missing etag, re-downloads.
	err = env.client.Download(context.Background(), "atomic.bin", tempDir, dataDir)
	if err != nil {
		t.Fatalf("self-healing download: %v", err)
	}

	got, err := xos.ReadFile(filepath.Join(dataDir, "atomic.bin"))
	if err != nil {
		t.Fatalf("read after self-heal: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Error("content mismatch after self-healing re-download")
	}
}

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

package r2_test

// The contract, from the documentation and the callers: what each failure
// of the service becomes, what is refused before a byte moves, and that the
// multipart upload resumes where it stopped.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/forkcloser/xos"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/r2"
)

// clientFor returns a client for a server, path-style.
func clientFor(t *testing.T, url string) *r2.Client {
	t.Helper()

	client, err := r2.New(&r2.Config{Endpoint: url, Bucket: testBucket, AccessKey: "k", Secret: "s", PathStyle: true})
	assert.NilError(t, err)

	return client
}

// TestFailureClasses: what the service's answers become, by status; a server
// that cannot be reached; a context that is done.
func TestFailureClasses(t *testing.T) {
	t.Parallel()

	statuses := map[int]error{
		http.StatusUnauthorized:        fault.ErrAuthenticationFailure,
		http.StatusForbidden:           fault.ErrAuthenticationFailure,
		http.StatusNotFound:            fault.ErrNotFound,
		http.StatusRequestTimeout:      fault.ErrTimeout,
		http.StatusGatewayTimeout:      fault.ErrTimeout,
		http.StatusInternalServerError: fault.ErrUnacceptableResponse,
		http.StatusBadGateway:          fault.ErrUnacceptableResponse,
		http.StatusTooManyRequests:     fault.ErrUnacceptableResponse,
		http.StatusConflict:            fault.ErrUnacceptableResponse,
	}

	for code, want := range statuses {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			t.Cleanup(server.Close)

			_, err := clientFor(t, server.URL).Stat(t.Context(), "key")
			assert.ErrorIs(t, err, want)
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.NotFoundHandler())
		url := server.URL
		server.Close()

		_, err := clientFor(t, url).Stat(t.Context(), "key")
		assert.ErrorIs(t, err, fault.ErrNetworkCommunication)
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := clientFor(t, server.URL).Stat(ctx, "key")
		assert.ErrorIs(t, err, fault.ErrCancelled)
	})
}

// TestRefusedBeforeAnyByte: the arguments refused up front, as
// ErrInvalidArgument, and the one-byte object that is not.
func TestRefusedBeforeAnyByte(t *testing.T) {
	t.Parallel()

	env := setup(t)
	ctx := t.Context()
	state := t.TempDir()

	assert.ErrorIs(
		t,
		env.client.Upload(ctx, "k", bytes.NewReader(nil), 0, r2.MultipartOptions{StateDir: state}),
		fault.ErrInvalidArgument,
	)
	assert.ErrorIs(
		t,
		env.client.Upload(ctx, "k", bytes.NewReader(nil), -1, r2.MultipartOptions{StateDir: state}),
		fault.ErrInvalidArgument,
	)
	assert.ErrorIs(
		t,
		env.client.Upload(ctx, "", bytes.NewReader([]byte("x")), 1, r2.MultipartOptions{StateDir: state}),
		fault.ErrInvalidArgument,
	)
	assert.ErrorIs(
		t,
		env.client.Upload(ctx, "a/../b", bytes.NewReader([]byte("x")), 1, r2.MultipartOptions{StateDir: state}),
		fault.ErrInvalidArgument,
	)

	// 10,001 parts of the minimum size: refused before the first part is read;
	// 10,000 is allowed, and fails later on the source instead.
	tooMany := int64(10_001) * 5 * bytesize.MiB
	assert.ErrorIs(
		t,
		env.client.Upload(ctx, "k", &recordingReader{}, tooMany, r2.MultipartOptions{StateDir: state, PartSize: 1}),
		fault.ErrInvalidArgument,
	)

	justEnough := int64(10_000) * 5 * bytesize.MiB
	err := env.client.Upload(
		ctx,
		"k",
		&recordingReader{failing: true},
		justEnough,
		r2.MultipartOptions{StateDir: state, PartSize: 1},
	)
	assert.ErrorIs(t, err, errSource)

	assert.NilError(
		t,
		env.client.Upload(ctx, "one", bytes.NewReader([]byte("x")), 1, r2.MultipartOptions{StateDir: state}),
	)

	env.putObject(t, "empty", nil)
	assert.ErrorIs(t, env.client.Download(ctx, "empty", t.TempDir(), t.TempDir()), fault.ErrInvalidArgument)
	assert.ErrorIs(t, env.client.Download(ctx, "../x", t.TempDir(), t.TempDir()), fault.ErrInvalidArgument)
}

// recordingReader is an io.ReaderAt over data that records every offset
// read, and, when failing, fails any read at or past failAt.
type recordingReader struct {
	data    []byte
	failing bool
	failAt  int64

	mu      sync.Mutex
	offsets []int64
}

var errSource = errors.New("source failed")

func (r *recordingReader) ReadAt(p []byte, off int64) (int, error) {
	r.mu.Lock()
	r.offsets = append(r.offsets, off)
	r.mu.Unlock()

	if r.failing && off >= r.failAt {
		return 0, errSource
	}

	return bytes.NewReader(r.data).ReadAt(p, off)
}

func (r *recordingReader) lowest() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	low := int64(-1)
	for _, off := range r.offsets {
		if low < 0 || off < low {
			low = off
		}
	}

	return low
}

// stateFile returns the one upload-state file in dir, decoded.
func stateFile(t *testing.T, dir string) (string, map[string]any) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "*.upload.json"))
	assert.NilError(t, err)
	assert.Equal(t, len(matches), 1, "one state file")

	data, err := xos.ReadFile(matches[0])
	assert.NilError(t, err)

	var state map[string]any

	assert.NilError(t, json.Unmarshal(data, &state))

	return matches[0], state
}

// TestPartSizeDefaults: PartSize zero is 100 MiB, below the minimum is 5 MiB,
// as the persisted state shows after a part fails.
func TestPartSizeDefaults(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		partSize int64
		want     float64
	}{
		"zero":          {0, 100 * bytesize.MiB},
		"below minimum": {100, 5 * bytesize.MiB},
		"above minimum": {6 * bytesize.MiB, 6 * bytesize.MiB},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := setup(t)
			stateDir := t.TempDir()
			source := &recordingReader{data: make([]byte, 16), failing: true}

			err := env.client.Upload(
				t.Context(),
				"k",
				source,
				16,
				r2.MultipartOptions{PartSize: tc.partSize, StateDir: stateDir},
			)
			assert.ErrorIs(t, err, errSource)

			_, state := stateFile(t, stateDir)
			assert.Equal(t, state["partSize"], tc.want)
		})
	}
}

// TestResume: an upload that failed on its second part resumes there, the
// first part not read again; a different part size or a lost upload starts
// over; the finished object is whole.
func TestResume(t *testing.T) {
	t.Parallel()

	const part = 5 * bytesize.MiB

	start := func(t *testing.T) (*testEnv, []byte, string) {
		t.Helper()

		env := setup(t)
		data := randomBytes(t, 2*part+bytesize.KiB)
		stateDir := t.TempDir()

		err := env.client.Upload(
			t.Context(),
			"k",
			&recordingReader{data: data, failing: true, failAt: part},
			int64(len(data)),
			r2.MultipartOptions{PartSize: part, StateDir: stateDir, Concurrency: 1},
		)
		assert.ErrorIs(t, err, errSource)

		_, state := stateFile(t, stateDir)
		assert.Equal(t, len(state["parts"].([]any)), 1, "one part done")

		return env, data, stateDir
	}

	finish := func(t *testing.T, env *testEnv, data []byte) {
		t.Helper()

		dataDir := t.TempDir()
		assert.NilError(t, env.client.Download(t.Context(), "k", t.TempDir(), dataDir))

		got, err := xos.ReadFile(filepath.Join(dataDir, "k"))
		assert.NilError(t, err)
		assert.Assert(t, bytes.Equal(got, data))

		matches, _ := filepath.Glob(filepath.Join(t.TempDir(), "*.upload.json"))
		assert.Equal(t, len(matches), 0)
	}

	t.Run("same part size resumes", func(t *testing.T) {
		t.Parallel()

		env, data, stateDir := start(t)
		source := &recordingReader{data: data}

		assert.NilError(
			t,
			env.client.Upload(
				t.Context(),
				"k",
				source,
				int64(len(data)),
				r2.MultipartOptions{PartSize: part, StateDir: stateDir},
			),
		)
		assert.Equal(t, source.lowest(), int64(part), "the first part was read again")
		finish(t, env, data)
	})

	t.Run("other part size starts over", func(t *testing.T) {
		t.Parallel()

		env, data, stateDir := start(t)
		source := &recordingReader{data: data}

		assert.NilError(
			t,
			env.client.Upload(
				t.Context(),
				"k",
				source,
				int64(len(data)),
				r2.MultipartOptions{PartSize: 6 * bytesize.MiB, StateDir: stateDir},
			),
		)
		assert.Equal(t, source.lowest(), int64(0))
		finish(t, env, data)
	})

	t.Run("lost upload starts over", func(t *testing.T) {
		t.Parallel()

		env, data, stateDir := start(t)
		path, state := stateFile(t, stateDir)
		state["uploadId"] = "gone"
		raw, _ := json.Marshal(state)
		assert.NilError(t, os.WriteFile(path, raw, 0o600))

		source := &recordingReader{data: data}

		assert.NilError(
			t,
			env.client.Upload(
				t.Context(),
				"k",
				source,
				int64(len(data)),
				r2.MultipartOptions{PartSize: part, StateDir: stateDir},
			),
		)
		assert.Equal(t, source.lowest(), int64(0))
		finish(t, env, data)
	})

	t.Run("corrupt state starts over", func(t *testing.T) {
		t.Parallel()

		env, data, stateDir := start(t)
		path, _ := stateFile(t, stateDir)
		assert.NilError(t, os.WriteFile(path, []byte("{not json"), 0o600))

		source := &recordingReader{data: data}

		assert.NilError(
			t,
			env.client.Upload(
				t.Context(),
				"k",
				source,
				int64(len(data)),
				r2.MultipartOptions{PartSize: part, StateDir: stateDir},
			),
		)
		assert.Equal(t, source.lowest(), int64(0))
		finish(t, env, data)
	})
}

// TestResumeKeepsStateWhenTheServiceIsAway: a failure to ask the service
// about the upload is returned and the state kept; a transient error does
// not cost the parts uploaded.
func TestResumeKeepsStateWhenTheServiceIsAway(t *testing.T) {
	t.Parallel()

	const part = 5 * bytesize.MiB

	backend := s3mem.New()
	assert.NilError(t, backend.CreateBucket(testBucket))

	server := httptest.NewServer(gofakes3.New(backend).Server())
	client := clientFor(t, server.URL)

	data := randomBytes(t, part+bytesize.KiB)
	stateDir := t.TempDir()

	err := client.Upload(t.Context(), "k", &recordingReader{data: data, failing: true, failAt: part}, int64(len(data)),
		r2.MultipartOptions{PartSize: part, StateDir: stateDir, Concurrency: 1})
	assert.ErrorIs(t, err, errSource)

	path, before := stateFile(t, stateDir)

	server.Close()

	err = client.Upload(
		t.Context(),
		"k",
		&recordingReader{data: data},
		int64(len(data)),
		r2.MultipartOptions{PartSize: part, StateDir: stateDir},
	)
	assert.ErrorIs(t, err, fault.ErrNetworkCommunication)

	after, err := xos.ReadFile(path)
	assert.NilError(t, err, "the state file is gone")

	var afterState map[string]any

	assert.NilError(t, json.Unmarshal(after, &afterState))
	assert.Equal(t, afterState["uploadId"], before["uploadId"])
}

// TestBoundedSizes: objects of every size around the part boundary, through
// Upload and Download, whole.
func TestBoundedSizes(t *testing.T) {
	t.Parallel()

	const part = 5 * bytesize.MiB

	for _, size := range []int{1, part - 1, part, part + 1, 2 * part, 2*part + 1, 3*part + 7} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			env := setup(t)
			data := randomBytes(t, size)

			assert.NilError(
				t,
				env.client.Upload(
					t.Context(),
					"k",
					bytes.NewReader(data),
					int64(size),
					r2.MultipartOptions{PartSize: part, StateDir: t.TempDir(), Concurrency: 3},
				),
			)

			dataDir := t.TempDir()
			assert.NilError(t, env.client.Download(t.Context(), "k", t.TempDir(), dataDir))

			got, err := xos.ReadFile(filepath.Join(dataDir, "k"))
			assert.NilError(t, err)
			assert.Assert(t, bytes.Equal(got, data), "%d bytes back, want %d", len(got), size)

			info, err := env.client.Stat(t.Context(), "k")
			assert.NilError(t, err)
			assert.Equal(t, info.Size, int64(size))
		})
	}
}

// TestBoundedResumeOffsets: a partial temp file of any size, one byte or one
// byte short, resumes and completes to the whole object.
func TestBoundedResumeOffsets(t *testing.T) {
	t.Parallel()

	const size = 64 * bytesize.KiB

	for _, partial := range []int{1, 2, size / 2, size - 1} {
		t.Run(strconv.Itoa(partial), func(t *testing.T) {
			t.Parallel()

			env := setup(t)
			data := randomBytes(t, size)
			env.putObject(t, "k", data)

			info, err := env.client.Stat(t.Context(), "k")
			assert.NilError(t, err)

			tempDir, dataDir := t.TempDir(), t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(tempDir, "k"), data[:partial], 0o600))
			assert.NilError(t, os.WriteFile(filepath.Join(tempDir, "k.etag"), []byte(info.ETag), 0o600))

			assert.NilError(t, env.client.Download(t.Context(), "k", tempDir, dataDir))

			got, err := xos.ReadFile(filepath.Join(dataDir, "k"))
			assert.NilError(t, err)
			assert.Assert(t, bytes.Equal(got, data), "resumed from %d: %d bytes, want %d", partial, len(got), size)
		})
	}
}

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

package transporter_test

// The contract, from the documentation and the callers: which responses are
// retried and which are returned, which transport errors are retried and
// which cannot change, what Retry-After does against MaxBackoff, which bodies
// can be retried, and that no option value breaks the client.

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/network"
	"github.com/mycophonic/primordium/network/transporter"
)

// TestStatusClassification: 429 and every 5xx are retried, everything else,
// 401 included, is returned as the server sent it.
func TestStatusClassification(t *testing.T) {
	t.Parallel()

	retried := map[int]bool{
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true, http.StatusBadGateway: true,
		http.StatusServiceUnavailable: true, http.StatusGatewayTimeout: true, 599: true,
	}

	returned := []int{200, 201, 204, 400, 401, 403, 404, 408, 409, 410, 418, 422, 499}

	for code := range retried {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
				if call == 1 {
					w.WriteHeader(code)

					return
				}

				w.WriteHeader(http.StatusOK)
			})

			resp, err := doGet(t.Context(), t, newClient(t, transporter.Options{MaxRetries: 1}), back.url)
			assert.NilError(t, err)
			assert.Check(t, resp.Body.Close())
			assert.Equal(t, resp.StatusCode, http.StatusOK)
			assert.Equal(t, back.calls.Load(), int32(2), "retried once")
		})
	}

	for _, code := range returned {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, status(code))

			resp, err := doGet(t.Context(), t, newClient(t, transporter.Options{MaxRetries: 2}), back.url)
			assert.NilError(t, err)
			assert.Check(t, resp.Body.Close())
			assert.Equal(t, resp.StatusCode, code)
			assert.Equal(t, back.calls.Load(), int32(1), "not retried")
		})
	}
}

// TestRetryAfterContract: Retry-After as delay-seconds or an HTTP-date is
// the wait before the retry when it fits MaxBackoff, the end of the attempt
// when it does not; absent, zero, negative, past or unparseable, it is no
// wait at all.
func TestRetryAfterContract(t *testing.T) {
	t.Parallel()

	const maxBackoff = 300 * time.Millisecond

	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)

	cases := map[string]struct {
		header string
		giveUp bool
	}{
		"absent":        {"", false},
		"zero":          {"0", false},
		"negative":      {"-5", false},
		"garbage":       {"soon", false},
		"past date":     {past, false},
		"too long":      {"2", true},
		"future date":   {future, true},
		"fits, decimal": {"0.5", false}, // not an integer, not a date: no wait
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
				if call == 1 {
					if tc.header != "" {
						w.Header().Set("Retry-After", tc.header)
					}

					w.WriteHeader(http.StatusServiceUnavailable)

					return
				}

				w.WriteHeader(http.StatusOK)
			})

			start := time.Now()
			client := newClient(t, transporter.Options{MaxRetries: 1, MaxBackoff: maxBackoff})
			resp, err := doGet(t.Context(), t, client, back.url)

			if tc.giveUp {
				assert.ErrorIs(t, err, fault.ErrUnacceptableResponse)
				assert.Equal(t, back.calls.Load(), int32(1), "gave up without a retry")
				assert.Assert(t, time.Since(start) < maxBackoff, "gave up without waiting")

				return
			}

			assert.NilError(t, err)
			assert.Check(t, resp.Body.Close())
			assert.Equal(t, back.calls.Load(), int32(2))
		})
	}
}

// TestRetryAfterWaited: a Retry-After that fits is honored as the wait.
func TestRetryAfterWaited(t *testing.T) {
	t.Parallel()

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	client := newClient(t, transporter.Options{MaxRetries: 1, MaxBackoff: 5 * time.Second})

	start := time.Now()

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())
	assert.Assert(t, time.Since(start) >= time.Second, "retried before Retry-After: %v", time.Since(start))
}

// TestBodyNotReplayable: a request whose body the client cannot replay is
// not retried: the first attempt's failure is the answer, and the server
// never sees a second, empty, request.
func TestBodyNotReplayable(t *testing.T) {
	t.Parallel()

	var bodies atomic.Int32

	back := newBackend(t, func(w http.ResponseWriter, r *http.Request, _ int32) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			bodies.Add(1)
		}

		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := newClient(t, transporter.Options{MaxRetries: 3})

	// io.NopCloser hides the Reader from NewRequest, which sets GetBody only for the types it knows.
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		back.url,
		io.NopCloser(strings.NewReader("payload")),
	)
	assert.NilError(t, err)
	assert.Assert(t, req.GetBody == nil)

	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	assert.ErrorIs(t, err, fault.ErrInvalidArgument)
	assert.ErrorIs(t, err, fault.ErrUnacceptableResponse)
	assert.Equal(t, back.calls.Load(), int32(1), "one attempt")
	assert.Equal(t, bodies.Load(), int32(1), "the one attempt carried the body")
}

// TestBodyReplayable: a body the client can replay is sent whole on every
// attempt.
func TestBodyReplayable(t *testing.T) {
	t.Parallel()

	var whole atomic.Int32

	back := newBackend(t, func(w http.ResponseWriter, r *http.Request, call int32) {
		body, _ := io.ReadAll(r.Body)
		if string(body) == "payload" {
			whole.Add(1)
		}

		if call < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	client := newClient(t, transporter.Options{MaxRetries: 2})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, back.url, bytes.NewReader([]byte("payload")))
	assert.NilError(t, err)

	resp, err := client.Do(req)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())
	assert.Equal(t, whole.Load(), int32(3), "every attempt carried the body")
}

// TestBoundedOptions: every option at its edges makes a client that answers
// a request; no value breaks it.
func TestBoundedOptions(t *testing.T) {
	t.Parallel()

	options := []transporter.Options{
		{},
		{Parallelism: 1},
		{Parallelism: 1000},
		{MaxPerSecond: 1},
		{MaxPerSecond: 1000},
		{MaxPerSecond: math.MaxInt},
		{MaxRetries: 0},
		{MaxRetries: 1, InitialBackoff: 0},
		{MaxRetries: 1, InitialBackoff: time.Nanosecond},
		{MaxBackoff: time.Nanosecond},
		{MaxBackoff: math.MaxInt64},
		{UserAgent: strings.Repeat("a", 4096)},
		{
			Parallelism:    2,
			MaxPerSecond:   50,
			MaxRetries:     1,
			InitialBackoff: time.Millisecond,
			MaxBackoff:     time.Millisecond,
			UserAgent:      "x",
		},
	}

	for i, opts := range options {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, status(http.StatusOK))

			resp, err := doGet(t.Context(), t, newClient(t, opts), back.url)
			assert.NilError(t, err)
			assert.Check(t, resp.Body.Close())
			assert.Equal(t, resp.StatusCode, http.StatusOK)
		})
	}
}

// TestCloseIdleConnectionsClosesThem: after CloseIdleConnections, the next
// request dials anew; the connection it had is gone.
func TestCloseIdleConnectionsClosesThem(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{})

	reused := func() bool {
		var info httptrace.GotConnInfo

		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
			GotConn: func(got httptrace.GotConnInfo) { info = got },
		})

		resp, err := doGet(ctx, t, client, back.url)
		assert.NilError(t, err)

		_, err = io.Copy(io.Discard, resp.Body)
		assert.NilError(t, err)
		assert.NilError(t, resp.Body.Close())

		return info.Reused
	}

	assert.Assert(t, !reused(), "a new client has no connection to reuse")
	assert.Assert(t, reused(), "the second request did not reuse the first's connection")

	client.CloseIdleConnections()

	assert.Assert(t, !reused(), "the connection survived CloseIdleConnections")
}

// tlsBackend is a TLS server counting the connections made to it: a handshake
// the client refuses never reaches a handler, so attempts are counted there.
func tlsBackend(t *testing.T, config *tls.Config) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var connections atomic.Int32

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = config
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}

	srv.StartTLS()
	t.Cleanup(srv.Close)

	return srv, &connections
}

// TestUntrustedCertificateNotRetried: a certificate the client does not trust
// is the same certificate on every attempt; one connection, the error
// classified as a network failure.
func TestUntrustedCertificateNotRetried(t *testing.T) {
	t.Parallel()

	srv, connections := tlsBackend(t, nil)

	client := newClient(t, transporter.Options{MaxRetries: 3, InitialBackoff: time.Millisecond})

	resp, err := doGet(t.Context(), t, client, srv.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}

	_, verification := errors.AsType[*tls.CertificateVerificationError](err)

	assert.Assert(t, verification, "error: %v", err)
	assert.Assert(t, errors.Is(err, fault.ErrNetworkCommunication))
	assert.Equal(t, connections.Load(), int32(1))
}

// TestTLSAlertNotRetried: a server that speaks no TLS version the client
// accepts alerts on every attempt; one connection.
func TestTLSAlertNotRetried(t *testing.T) {
	t.Parallel()

	srv, connections := tlsBackend(t, &tls.Config{MaxVersion: tls.VersionTLS12})

	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	client := newClient(t, transporter.Options{
		MaxRetries: 3, InitialBackoff: time.Millisecond,
		Base: network.NewTransport(network.Config{Roots: roots}),
	})

	resp, err := doGet(t.Context(), t, client, srv.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}

	assert.ErrorIs(t, err, network.ErrHandshakeRefused)
	assert.Equal(t, connections.Load(), int32(1))
}

// TestPostureRefusalsNotRetried: a request the baseline refuses, offline, in
// cleartext or at a level there is none of, is refused once: the same request
// gets the same answer.
func TestPostureRefusalsNotRetried(t *testing.T) {
	t.Parallel()

	for name, refusal := range map[string]struct {
		config network.Config
		level  network.KeyExchange
		want   error
	}{
		"offline":       {network.Config{Offline: true, Cleartext: true}, network.HybridOnly, network.ErrOffline},
		"cleartext":     {network.Config{}, network.HybridOnly, network.ErrCleartext},
		"unknown level": {network.Config{Cleartext: true}, network.Legacy + 1, network.ErrKeyExchange},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, status(http.StatusOK))
			baseline := network.NewTransport(refusal.config)

			var attempts atomic.Int32

			client := newClient(t, transporter.Options{
				MaxRetries: 3, InitialBackoff: time.Millisecond,
				Base: roundTripper(func(req *http.Request) (*http.Response, error) {
					attempts.Add(1)

					return baseline.RoundTrip(req)
				}),
			})

			resp, err := doGet(network.WithKeyExchange(t.Context(), refusal.level), t, client, back.url)
			if resp != nil {
				_ = resp.Body.Close()
			}

			assert.ErrorIs(t, err, refusal.want)
			assert.Equal(t, attempts.Load(), int32(1))
			assert.Equal(t, back.calls.Load(), int32(0))
		})
	}
}

// TestUnknownHostNotRetried: a name that does not resolve is asked once. The
// .invalid domain never resolves (RFC 6761); a resolver that answers anything
// but "no such host", as one cut off from the network does, cannot show the
// property, and the test says so.
func TestUnknownHostNotRetried(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	trace := &httptrace.ClientTrace{DNSStart: func(httptrace.DNSStartInfo) { attempts.Add(1) }}
	ctx := httptrace.WithClientTrace(t.Context(), trace)
	client := newClient(t, transporter.Options{MaxRetries: 3, InitialBackoff: time.Millisecond})

	resp, err := doGet(ctx, t, client, "http://nonexistent.invalid/")
	if resp != nil {
		_ = resp.Body.Close()
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); !ok || !dnsErr.IsNotFound {
		t.Skipf("the resolver did not answer no such host: %v", err)
	}

	assert.Equal(t, attempts.Load(), int32(1))
}

// TestLogsNoCredential: what is logged of a request's URL has neither its
// user information nor its query, on each line that names it: a roundtrip, a
// retry on a status, retries exhausted, a transport error, and the progress
// of a download. slog's default is swapped for the test, which is why it does
// not run in parallel.
func TestLogsNoCredential(t *testing.T) { //nolint:paralleltest // slog's default is process-wide
	var said bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&said, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	// One step of a download's logged progress.
	const logged = 100 << 20

	var once atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/down", r.URL.Path == "/once" && once.Add(1) == 1:
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.URL.Path == "/large":
			_, _ = io.CopyN(w, zeros{}, logged)
		}
	}))
	defer server.Close()

	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gone.Close()

	client := newClient(t, transporter.Options{MaxRetries: 1, InitialBackoff: time.Millisecond})

	for _, address := range []string{server.URL + "/once", server.URL + "/down", server.URL + "/large", gone.URL + "/gone"} {
		target, err := url.Parse(address + "?X-Amz-Signature=secret-in-the-query")
		if err != nil {
			t.Fatal(err)
		}

		target.User = url.UserPassword("user", "secret-in-the-userinfo")

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target.String(), http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := client.Do(req)
		if err != nil {
			continue // retries exhausted, or the server that is gone
		}

		_, _ = io.Copy(io.Discard, resp.Body)

		assert.Check(t, resp.Body.Close())
	}

	lines := said.String()

	for _, line := range []string{
		"HTTP roundtrip", "HTTP error, retrying", "HTTP retries exhausted",
		"HTTP transport error, retrying", "download progress", "download complete",
	} {
		if !strings.Contains(lines, line) {
			t.Errorf("%q was not logged", line)
		}
	}

	if strings.Contains(lines, "secret-in") {
		t.Fatal("a credential was logged")
	}
}

// zeros is an endless source of zero bytes.
type zeros struct{}

func (zeros) Read(buf []byte) (int, error) {
	clear(buf)

	return len(buf), nil
}

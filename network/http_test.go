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

package network_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/network"
)

func TestMain(m *testing.M) {
	// Initialize defaults before any tests run
	network.SetDefaults()
	m.Run()
}

func TestRoundTripper_InjectsAuthHeader(t *testing.T) {
	t.Parallel()

	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rt := network.NewTransport()
	rt.TokenValue = "test-token-123"
	rt.TokenType = "Bearer"

	client := &http.Client{Transport: rt}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer func() { assert.Check(t, resp.Body.Close()) }()

	if capturedHeader != "Bearer test-token-123" {
		t.Errorf("Authorization header = %q, want %q", capturedHeader, "Bearer test-token-123")
	}
}

func TestRoundTripper_NoAuthWhenTokenEmpty(t *testing.T) {
	t.Parallel()

	var capturedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Authorization")

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rt := network.NewTransport()
	// TokenValue intentionally empty

	client := &http.Client{Transport: rt}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer func() { assert.Check(t, resp.Body.Close()) }()

	if capturedHeader != "" {
		t.Errorf("Authorization header = %q, want empty", capturedHeader)
	}
}

func TestRoundTripper_LogsRetryableStatus(t *testing.T) {
	t.Parallel()

	// Test that retryable status codes don't cause errors (just logging)
	for _, status := range network.RetryStatusCodes {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			rt := network.NewTransport()
			client := &http.Client{Transport: rt}

			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			defer func() { assert.Check(t, resp.Body.Close()) }()

			// RoundTripper should return the response, not error
			if resp.StatusCode != status {
				t.Errorf("status = %d, want %d", resp.StatusCode, status)
			}
		})
	}
}

// TestRoundTripper_TransportError: a request that cannot be sent is the
// transport's error, not a response.
func TestRoundTripper_TransportError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := server.URL
	server.Close()

	client := &http.Client{Transport: network.NewTransport()}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)

	resp, err := client.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	assert.Assert(t, err != nil, "a closed server answered")
}

func TestNewTransport_ClonesIndependently(t *testing.T) {
	t.Parallel()

	rt1 := network.NewTransport()
	rt2 := network.NewTransport()

	// Modifying one should not affect the other
	rt1.TokenValue = "token1"
	rt2.TokenValue = "token2"

	if rt1.TokenValue == rt2.TokenValue {
		t.Error("transports should be independent")
	}
}

func TestRetryStatusCodes_ContainsExpectedCodes(t *testing.T) {
	t.Parallel()

	expected := []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}

	if len(network.RetryStatusCodes) != len(expected) {
		t.Errorf("RetryStatusCodes has %d codes, want %d", len(network.RetryStatusCodes), len(expected))
	}

	for _, code := range expected {
		found := slices.Contains(network.RetryStatusCodes, code)

		if !found {
			t.Errorf("RetryStatusCodes missing %d (%s)", code, http.StatusText(code))
		}
	}
}

func TestNewTransport_TLSMinVersionTLS13(t *testing.T) {
	t.Parallel()

	rt := network.NewTransport()

	if rt.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}

	if rt.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("TLS MinVersion = %d, want %d (TLS 1.3)", rt.TLSClientConfig.MinVersion, tls.VersionTLS13)
	}
}

func TestNewTransport_TLSCurvePreferences(t *testing.T) {
	t.Parallel()

	rt := network.NewTransport()

	if rt.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}

	curves := rt.TLSClientConfig.CurvePreferences
	if len(curves) != 2 {
		t.Fatalf("CurvePreferences has %d curves, want 2", len(curves))
	}

	// First curve should be post-quantum hybrid
	if curves[0] != tls.X25519MLKEM768 {
		t.Errorf("CurvePreferences[0] = %v, want X25519MLKEM768", curves[0])
	}

	// Second curve should be X25519 fallback
	if curves[1] != tls.X25519 {
		t.Errorf("CurvePreferences[1] = %v, want X25519", curves[1])
	}
}

func TestNewTransport_TimeoutConfiguration(t *testing.T) {
	t.Parallel()

	rt := network.NewTransport()

	// Verify timeouts are set (non-zero)
	if rt.TLSHandshakeTimeout == 0 {
		t.Error("TLSHandshakeTimeout is zero")
	}

	if rt.IdleConnTimeout == 0 {
		t.Error("IdleConnTimeout is zero")
	}

	if rt.ExpectContinueTimeout == 0 {
		t.Error("ExpectContinueTimeout is zero")
	}

	// Verify connection pool settings
	if rt.MaxIdleConns == 0 {
		t.Error("MaxIdleConns is zero")
	}

	if rt.MaxIdleConnsPerHost == 0 {
		t.Error("MaxIdleConnsPerHost is zero")
	}

	if rt.MaxConnsPerHost == 0 {
		t.Error("MaxConnsPerHost is zero")
	}
}

//nolint:paralleltest // mutates the process-wide default transport
func TestSetDefaults_ConfiguresDefaultTransport(t *testing.T) {
	// Not parallel - modifies global state (already done in TestMain)
	transport, ok := http.DefaultTransport.(*network.RoundTripper)
	if !ok {
		t.Fatalf("http.DefaultTransport is %T, want *network.RoundTripper", http.DefaultTransport)
	}

	// Verify TLS is configured on the default transport
	if transport.TLSClientConfig == nil {
		t.Fatal("DefaultTransport TLSClientConfig is nil")
	}

	if transport.TLSClientConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf(
			"DefaultTransport TLS MinVersion = %d, want %d",
			transport.TLSClientConfig.MinVersion,
			tls.VersionTLS13,
		)
	}
}

// TestRoundTripper_OneOriginSpelledTwoWays: a redirect to the origin the
// caller asked keeps the token however it writes that origin: the host in
// another case, the scheme's own port spelled out. One to another host name
// on the same port, which is all that tells a registry from the storage it
// redirects to, gets none. Every name dials the one server.
func TestRoundTripper_OneOriginSpelledTwoWays(t *testing.T) {
	t.Parallel()

	const token = "Bearer for-the-first-host"

	var landed atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := token
		if r.Host == "other.test" {
			want = ""
		}

		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("%s on %s got Authorization %q, want %q", r.URL.Path, r.Host, got, want)
		}

		switch r.URL.Path {
		case "/name":
			http.Redirect(w, r, "http://other.test/landed", http.StatusFound)
		case "/case":
			http.Redirect(w, r, "http://ZZ.TEST/landed", http.StatusFound)
		case "/port":
			http.Redirect(w, r, "http://zz.test:80/landed", http.StatusFound)
		case "/landed":
			landed.Add(1)
		}
	}))
	defer server.Close()

	rt := network.NewTransport()
	rt.TokenValue = "for-the-first-host"
	rt.Proxy = nil
	rt.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}

	client := &http.Client{Transport: rt}

	for _, path := range []string{"/case", "/port", "/name"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://zz.test"+path, http.NoBody)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		assert.Check(t, resp.Body.Close())
	}

	if landed.Load() != 3 {
		t.Fatalf("%d of the 3 redirects were followed", landed.Load())
	}
}

// TestRoundTripper_TokenStaysWithItsOrigin: the token goes to the origin the
// caller asked, on every request to it, and to no other origin a redirect
// leads to.
func TestRoundTripper_TokenStaysWithItsOrigin(t *testing.T) {
	t.Parallel()

	const token = "Bearer for-the-first-host"

	var elsewhere, sameOrigin atomic.Pointer[string]

	// The other origin redirects once more within itself: the second
	// request to it is no more the caller's origin than the first.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if got != "" {
			t.Errorf("%s on the other origin got Authorization %q, want none", r.URL.Path, got)
		}

		if r.URL.Path == "/asset" {
			http.Redirect(w, r, "/moved", http.StatusFound)

			return
		}

		elsewhere.Store(&got)
	}))
	defer other.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if got != token {
			t.Errorf("%s on the first host got Authorization %q, want the token", r.URL.Path, got)
		}

		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/asset", http.StatusFound)
		case "/around":
			http.Redirect(w, r, "/landed", http.StatusFound)
		case "/landed":
			sameOrigin.Store(&got)
		}
	}))
	defer first.Close()

	rt := network.NewTransport()
	rt.TokenValue = "for-the-first-host"

	client := &http.Client{Transport: rt}

	for _, path := range []string{"/away", "/around"} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, first.URL+path, http.NoBody)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}

		assert.Check(t, resp.Body.Close())
	}

	switch got := elsewhere.Load(); {
	case got == nil:
		t.Error("the redirect to another origin was not followed")
	case *got != "":
		t.Errorf("a redirect to another origin carried Authorization %q, want none", *got)
	}

	switch got := sameOrigin.Load(); {
	case got == nil:
		t.Error("the redirect within the origin was not followed")
	case *got != token:
		t.Errorf("a redirect within the origin carried Authorization %q, want the token", *got)
	}
}

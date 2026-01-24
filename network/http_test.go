package network_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/farcloser/primordium/network"
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

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer resp.Body.Close()

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

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	defer resp.Body.Close()

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

			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			defer resp.Body.Close()

			// RoundTripper should return the response, not error
			if resp.StatusCode != status {
				t.Errorf("status = %d, want %d", resp.StatusCode, status)
			}
		})
	}
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
		found := false

		for _, rc := range network.RetryStatusCodes {
			if rc == code {
				found = true

				break
			}
		}

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

//nolint:paralleltest
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

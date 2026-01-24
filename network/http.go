package network

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// defaultTransport holds the configured transport before any wrapping.
// This allows NewTransport to clone from it.
//
//nolint:gochecknoglobals // Package-level reference needed for cloning.
var (
	defaultTransport *http.Transport

	// RetryStatusCodes contains HTTP status codes that indicate retryable errors.
	// This list is used both for logging in RoundTripper and can be passed to
	// libraries like go-containerregistry via remote.WithRetryStatusCodes().
	RetryStatusCodes = []int{
		http.StatusTooManyRequests,     // 429 - Rate limit
		http.StatusInternalServerError, // 500 - Server error
		http.StatusBadGateway,          // 502 - Proxy error
		http.StatusServiceUnavailable,  // 503 - Service overloaded
		http.StatusGatewayTimeout,      // 504 - Upstream timeout
	}

	// retryReasons maps status codes to human-readable reasons for logging.
	//
	retryReasons = map[int]string{
		http.StatusTooManyRequests:     "rate limited",
		http.StatusInternalServerError: "server error",
		http.StatusBadGateway:          "bad gateway",
		http.StatusServiceUnavailable:  "service unavailable",
		http.StatusGatewayTimeout:      "gateway timeout",
	}
)

// Transport wraps *http.Transport to expose TLSClientConfig for modification.
type Transport struct {
	*http.Transport
}

// RoundTripper wraps *http.Transport with logging for retry-worthy responses.
// Embedding *http.Transport exposes TLSClientConfig for direct access.
type RoundTripper struct {
	*http.Transport

	TokenValue string
	TokenType  string
}

// NewTransport returns a new RoundTripper cloned from the default configuration.
// The returned RoundTripper can be modified (e.g., adding client certificates)
// without affecting http.DefaultTransport.
// Panics if SetDefaults has not been called.
func NewTransport() *RoundTripper {
	if defaultTransport == nil {
		panic("NewTransport called before SetDefaults")
	}

	cloned := defaultTransport.Clone()
	cloned.TLSClientConfig = defaultTLSConfig()

	return &RoundTripper{
		Transport: cloned,
	}
}

// RoundTrip implements http.RoundTripper.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.TokenValue != "" {
		// Ensure we don't leak that if the req is getting reused.
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", fmt.Sprintf("%s %s", rt.TokenType, rt.TokenValue))
	}

	resp, err := rt.Transport.RoundTrip(req)
	if err != nil {
		return resp, err //nolint:wrapcheck // pass through
	}

	if reason, isRetryable := retryReasons[resp.StatusCode]; isRetryable {
		slog.DebugContext(req.Context(), "HTTP request received retryable status",
			slog.String("url", req.URL.String()),
			slog.Int("status", resp.StatusCode),
			slog.String("reason", reason))
	}

	return resp, nil
}

// defaultTLSConfig returns the TLS configuration used for all transports.
func defaultTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{
			tls.X25519MLKEM768, // Post-quantum hybrid (preferred)
			tls.X25519,         // Modern ECDH fallback
		},
	}
}

// Transport timeout and pool configuration constants.
const (
	dialTimeout           = 30 * time.Second
	dialKeepAlive         = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
	idleConnTimeout       = 90 * time.Second
	expectContinueTimeout = 1 * time.Second
	maxIdleConns          = 100
	maxIdleConnsPerHost   = 100
	maxConnsPerHost       = 100
)

// SetDefaults configures http.DefaultTransport with our TLS and connection settings,
// and wraps it with logging. Must be called once at startup before any HTTP requests.
func SetDefaults() {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("http.DefaultTransport has been tampered with")
	}

	// Proxy configuration
	transport.Proxy = http.ProxyFromEnvironment

	// Dialer configuration
	transport.DialContext = (&net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}).DialContext

	// Enable HTTP/2 - required when setting custom TLSClientConfig
	transport.ForceAttemptHTTP2 = true

	// Timeout configuration
	transport.TLSHandshakeTimeout = tlsHandshakeTimeout
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	transport.IdleConnTimeout = idleConnTimeout
	transport.ExpectContinueTimeout = expectContinueTimeout

	// Connection pool tuning - prevent connection churn
	transport.MaxIdleConns = maxIdleConns
	transport.MaxIdleConnsPerHost = maxIdleConnsPerHost
	transport.MaxConnsPerHost = maxConnsPerHost

	// TLS configuration
	transport.TLSClientConfig = defaultTLSConfig()

	// Store for cloning
	defaultTransport = transport

	// Wrap with logging for retry-worthy responses
	http.DefaultTransport = &RoundTripper{
		Transport: transport,
	}
}

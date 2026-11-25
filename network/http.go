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

package network

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/mycophonic/primordium/network/internal/redact"
)

// defaultTransport holds the configured transport before any wrapping.
// This allows NewTransport to clone from it.
//
//nolint:gochecknoglobals // Package-level reference needed for cloning.
var (
	defaultTransport *http.Transport

	// retryReasons maps retryable HTTP status codes to human-readable reasons for logging.
	// This is the single source of truth — RetryStatusCodes is derived from it.
	retryReasons = map[int]string{
		http.StatusTooManyRequests:     "rate limited",
		http.StatusInternalServerError: "server error",
		http.StatusBadGateway:          "bad gateway",
		http.StatusServiceUnavailable:  "service unavailable",
		http.StatusGatewayTimeout:      "gateway timeout",
	}

	// RetryStatusCodes contains HTTP status codes that indicate retryable errors.
	// Derived from retryReasons. Can be passed to libraries like go-containerregistry
	// via remote.WithRetryStatusCodes().
	RetryStatusCodes []int
)

//nolint:gochecknoinits // derives the exported list from the reason table once
func init() {
	RetryStatusCodes = make([]int, 0, len(retryReasons))
	for code := range retryReasons {
		RetryStatusCodes = append(RetryStatusCodes, code)
	}

	slices.Sort(RetryStatusCodes)
}

// RoundTripper wraps *http.Transport with auth header injection and
// logging for retryable responses. Callers can modify TLSClientConfig
// directly via the embedded transport (e.g., custom CAs, client certs).
//
// The token is for the origin the caller asks: its scheme, host and port. A
// redirect the client follows to another origin gets no token.
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
		TokenType: "Bearer",
	}
}

// RoundTrip implements http.RoundTripper.
func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.TokenValue != "" && !redirectedElsewhere(req) {
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
			slog.String("url", redact.URL(req.URL)),
			slog.Int("status", resp.StatusCode),
			slog.String("reason", reason))
	}

	return resp, nil
}

// redirectedElsewhere reports a request the client made by following a
// redirect to an origin other than the one the caller asked. net/http drops
// the caller's own Authorization header there, and a transport that adds one
// back hands the token to whoever the first host pointed at.
func redirectedElsewhere(req *http.Request) bool {
	asked := req

	// Response is set on a request made for a redirect, to the response that
	// caused it.
	for asked.Response != nil && asked.Response.Request != nil {
		asked = asked.Response.Request
	}

	return !sameOrigin(asked.URL, req.URL)
}

// sameOrigin compares two URLs as origins: the scheme, the host name
// whatever its case, and the port, the scheme's own when none is written.
func sameOrigin(one, other *url.URL) bool {
	return one.Scheme == other.Scheme &&
		strings.EqualFold(one.Hostname(), other.Hostname()) &&
		portOf(one) == portOf(other)
}

func portOf(target *url.URL) string {
	if written := target.Port(); written != "" {
		return written
	}

	switch target.Scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
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

// SetDefaults configures http.DefaultTransport with our TLS and connection settings,
// and wraps it with logging. Must be called once at startup before any HTTP requests.
func SetDefaults() {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("http.DefaultTransport has already been wrapped or is not http.Transport")
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
	//nolint:reassign // SetDefaults' contract: it installs the process-wide default transport
	http.DefaultTransport = &RoundTripper{
		Transport: transport,
	}
}

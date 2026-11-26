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

package transporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/network/internal/redact"
)

// retryTransport is an http.RoundTripper that adds concurrency limiting,
// rate limiting, retry with exponential backoff, Retry-After, and User-Agent.
type retryTransport struct {
	base        func() http.RoundTripper // ownTransport, taken at the first request
	baseTaken   atomic.Bool              // base has been called
	sem         chan struct{}            // parallelism semaphore; nil = unlimited
	limiter     *limiter                 // nil = unlimited
	maxRetries  int
	initBackoff time.Duration
	maxBackoff  time.Duration
	userAgent   string
	legacyTLS   bool // the clone gets Go's default TLS
}

func newRetryTransport(opts Options) *retryTransport {
	transport := &retryTransport{
		maxRetries:  opts.MaxRetries,
		initBackoff: opts.InitialBackoff,
		maxBackoff:  opts.MaxBackoff,
		userAgent:   opts.UserAgent,
		legacyTLS:   opts.LegacyTLS,
	}

	transport.base = sync.OnceValue(func() http.RoundTripper {
		transport.baseTaken.Store(true)

		return ownTransport(transport.legacyTLS)
	})

	if opts.Parallelism > 0 {
		transport.sem = make(chan struct{}, opts.Parallelism)
	}

	if opts.MaxPerSecond > 0 {
		// A rate past one per nanosecond is one per nanosecond.
		transport.limiter = &limiter{interval: max(time.Second/time.Duration(opts.MaxPerSecond), time.Nanosecond)}
	}

	return transport
}

// ownTransport returns a clone of http.DefaultTransport: a pool of its own, so
// that no other user of http.DefaultTransport (another client's
// CloseIdleConnections, every httptest.Server.Close) closes this client's
// connections, or fails a request that has just taken one. It is taken at the
// first request, not when the client is made: network.SetDefaults promises its
// configuration to requests made after it, and a client may be made before.
// The clone drops whatever wraps the *http.Transport: network.SetDefaults'
// wrapper adds only a header this client never sets and a log line. One that
// cannot be cloned is shared, as nothing else carries its configuration.
// LegacyTLS resets the clone's TLS config, which Clone copied, to Go's
// defaults for the version, key exchange and cipher suites, and nothing else:
// the roots and certificates stay, and http.DefaultTransport is untouched.
func ownTransport(legacyTLS bool) http.RoundTripper {
	cloner, ok := http.DefaultTransport.(interface{ Clone() *http.Transport })
	if !ok {
		return http.DefaultTransport
	}

	transport := cloner.Clone()

	if config := transport.TLSClientConfig; legacyTLS && config != nil {
		config.MinVersion = tls.VersionTLS12
		config.CurvePreferences = nil
		config.CipherSuites = nil
	}

	return transport
}

// RoundTrip implements http.RoundTripper.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A RoundTripper leaves the caller's request as it is: the header added
	// here and the body a retry rewinds are a copy's. Not Request.Clone: it
	// copies Trailer, which the caller fills while the body is read and the
	// transport reads from the request it was given.
	ctx := req.Context()
	req = req.WithContext(ctx)
	req.Header = req.Header.Clone()

	if t.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", t.userAgent)
	}

	if err := t.acquireSem(ctx); err != nil {
		return nil, err
	}

	if t.sem != nil {
		defer func() { <-t.sem }()
	}

	return t.retryLoop(req)
}

// CloseIdleConnections forwards to the base transport. Called by
// net/http.Client.CloseIdleConnections.
func (t *retryTransport) CloseIdleConnections() {
	// Before the first request there is no connection to close, and calling
	// base would take the clone before network.SetDefaults may have run.
	if !t.baseTaken.Load() {
		return
	}

	type closeIdler interface {
		CloseIdleConnections()
	}

	if ci, ok := t.base().(closeIdler); ok {
		ci.CloseIdleConnections()
	}
}

func (t *retryTransport) acquireSem(ctx context.Context) error {
	if t.sem == nil {
		return nil
	}

	select {
	case t.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", fault.ErrCancelled, ctx.Err())
	}
}

func (t *retryTransport) waitForToken(ctx context.Context) (time.Duration, error) {
	if t.limiter == nil {
		return 0, nil
	}

	start := time.Now()

	for {
		wait := t.limiter.take()
		if wait <= 0 {
			return time.Since(start), nil
		}

		timer := time.NewTimer(wait)

		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()

			return 0, fmt.Errorf("%w: %w", fault.ErrCancelled, ctx.Err())
		}
	}
}

// limiter spaces requests an interval apart, with no burst. It is computed
// from the clock: nothing runs between two requests, so there is nothing to
// start and nothing a CloseIdleConnections could stop.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time // when the next request may go
}

// take gives the caller the slot when its time has come, and returns zero;
// before that it takes nothing and returns how long until then. A slot is
// never held for a caller that waits: one that leaves, its context done,
// leaves nothing behind for the others to wait on.
func (l *limiter) take() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if wait := l.next.Sub(now); wait > 0 {
		return wait
	}

	// From the slot's own time when it was just missed, so that late wakings
	// do not add up; from now after an idle stretch, so that it buys no burst.
	if now.Sub(l.next) > l.interval {
		l.next = now
	}

	l.next = l.next.Add(l.interval)

	return 0
}

//nolint:gocognit // one retry protocol, read top to bottom: wait, attempt, classify, decide
func (t *retryTransport) retryLoop(req *http.Request) (*http.Response, error) {
	var (
		lastErr       error
		retryAfterVal time.Duration
	)

	// What the logs below hold of the URL: a signed URL's query is a
	// credential, and so is a password in it.
	target := redact.URL(req.URL)

	for attempt := range t.maxRetries + 1 {
		if attempt > 0 {
			backoff := max(retryAfterVal, t.backoffDuration(attempt))
			retryAfterVal = 0

			select {
			case <-req.Context().Done():
				return nil, fmt.Errorf("%w: %w", fault.ErrCancelled, lastErr)
			case <-time.After(backoff):
			}

			if err := resetBody(req); err != nil {
				return nil, fmt.Errorf("%w: %w", err, lastErr)
			}
		}

		tokenWait, err := t.waitForToken(req.Context())
		if err != nil {
			return nil, err
		}

		start := time.Now()
		resp, err := t.base().RoundTrip(req)
		elapsed := time.Since(start)

		if err != nil {
			lastErr = fmt.Errorf("%w: %w", fault.ErrNetworkCommunication, err)

			if req.Context().Err() != nil {
				return nil, fmt.Errorf("%w: %w", fault.ErrCancelled, lastErr)
			}

			if permanent(err) {
				slog.WarnContext(req.Context(), "HTTP transport error, not retried: it cannot change",
					"error", err,
					"url", target,
				)

				return nil, lastErr
			}

			// The request may have reached the server before the error did,
			// unless the error is the dial's: nothing was sent then.
			if !replayable(req) && !unsent(err) {
				slog.WarnContext(req.Context(), "HTTP transport error, not retried: the request is not idempotent",
					"error", err,
					"url", target,
				)

				return nil, lastErr
			}

			slog.WarnContext(req.Context(), "HTTP transport error, retrying",
				"attempt", attempt+1,
				"elapsed", elapsed,
				"token_wait", tokenWait,
				"delay", t.backoffDuration(attempt+1).String(),
				"error", err,
				"url", target,
			)

			continue
		}

		slog.DebugContext(req.Context(), "HTTP roundtrip",
			"status", resp.StatusCode,
			"elapsed", elapsed,
			"token_wait", tokenWait,
			"url", target,
		)

		// 401 is not among them: this client attaches no credential and has no
		// way to change one between attempts, so a retry asks the same question.
		retryable := resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= http.StatusInternalServerError
		if !retryable {
			if elapsed > slowRequestThreshold {
				slog.WarnContext(req.Context(), "slow HTTP request",
					"elapsed", elapsed,
					"status", resp.StatusCode,
					"url", target,
				)
			}

			resp.Body = &progressBody{
				ReadCloser: resp.Body,
				url:        target,
				start:      time.Now(),
				totalSize:  resp.ContentLength,
			}

			return resp, nil
		}

		// Drain body before retry to allow connection reuse.
		drainBody(resp.Body)

		lastErr = fmt.Errorf("%w: HTTP %d", fault.ErrUnacceptableResponse, resp.StatusCode)

		// The server may have acted on the request before it answered: the
		// answer is the error it is for any request, and it is not sent again.
		if !replayable(req) {
			slog.WarnContext(req.Context(), "HTTP error, not retried: the request is not idempotent",
				"status", resp.StatusCode,
				"url", target,
			)

			break
		}

		if attempt == t.maxRetries {
			slog.WarnContext(req.Context(), "HTTP retries exhausted",
				"status", resp.StatusCode,
				"attempts", t.maxRetries+1,
				"url", target,
			)

			break
		}

		retryAfterVal = retryAfter(resp.Header)

		if t.maxBackoff > 0 && retryAfterVal > t.maxBackoff {
			slog.WarnContext(req.Context(), "retry-after too large, giving up",
				"status", resp.StatusCode,
				"retry_after", retryAfterVal,
				"max", t.maxBackoff,
				"url", target,
			)

			break
		}

		nextBackoff := max(retryAfterVal, t.backoffDuration(attempt+1))

		slog.WarnContext(req.Context(), "HTTP error, retrying",
			"status", resp.StatusCode,
			"attempt", attempt+1,
			"elapsed", elapsed,
			"token_wait", tokenWait,
			"delay", nextBackoff.String(),
			"url", target,
		)
	}

	return nil, lastErr
}

// replayable reports a request that sent twice does what it does sent once:
// a method RFC 9110 defines as idempotent, or a request that says so itself
// with an idempotency key, the two headers net/http reads before it replays
// one. As there, the key's presence is what counts: one with no value marks
// the request and sends no header.
func replayable(req *http.Request) bool {
	switch req.Method {
	case "", http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	default:
		_, key := req.Header["Idempotency-Key"]
		_, legacy := req.Header["X-Idempotency-Key"]

		return key || legacy
	}
}

// unsent reports a transport error that came before anything was sent: the
// connection could not be made.
func unsent(err error) bool {
	opErr, ok := errors.AsType[*net.OpError](err)

	return ok && opErr.Op == "dial"
}

// permanent reports a transport error that no retry can change for the life
// of the request: an alert the server sent, as one that speaks no TLS version
// the client accepts does; a certificate the client does not trust; a host
// name that does not resolve. A timeout, a reset or a refused connection is
// not one.
func permanent(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsNotFound
	}

	// A TLS alert from the server is a net.OpError of that operation: the
	// alert type behind it is not exported.
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "remote error" {
		return true
	}

	_, verify := errors.AsType[*tls.CertificateVerificationError](err)
	_, unknown := errors.AsType[x509.UnknownAuthorityError](err)
	_, hostname := errors.AsType[x509.HostnameError](err)
	_, invalid := errors.AsType[x509.CertificateInvalidError](err)

	return verify || unknown || hostname || invalid
}

func (t *retryTransport) backoffDuration(attempt int) time.Duration {
	backoff := t.initBackoff

	for range attempt - 1 {
		backoff *= 2

		// Guard against int64 overflow and honour MaxBackoff for computed backoff.
		if t.maxBackoff > 0 && backoff > t.maxBackoff {
			backoff = t.maxBackoff

			break
		}
	}

	jitter := jitterMin + rand.Float64()*jitterRange // #nosec G404 -- Jitter, not crypto.

	return time.Duration(float64(backoff) * jitter)
}

// resetBody rewinds the request body for a retry. A request without a body
// needs nothing. One with a body the client cannot replay (no GetBody: a
// stream, a pipe, a file) cannot be retried: the first attempt consumed it,
// and a retry would send an empty body and report whatever the server made
// of that as success.
func resetBody(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}

	if req.GetBody == nil {
		return fmt.Errorf("%w: request body cannot be replayed for a retry", fault.ErrInvalidArgument)
	}

	body, err := req.GetBody()
	if err != nil {
		return fmt.Errorf("%w: reset request body: %w", fault.ErrNetworkCommunication, err)
	}

	req.Body = body

	return nil
}

// retryAfter parses the Retry-After header per RFC 9110 §10.2.3.
// Accepts either delay-seconds (integer) or an HTTP-date.
// Returns 0 if absent, unparseable, or in the past.
func retryAfter(header http.Header) time.Duration {
	val := header.Get("Retry-After")
	if val == "" {
		return 0
	}

	// Try delay-seconds first (most common).
	if seconds, err := strconv.Atoi(val); err == nil {
		if seconds <= 0 {
			return 0
		}

		return time.Duration(seconds) * time.Second
	}

	// Fall back to HTTP-date (RFC 1123, RFC 850, ASCTIME).
	target, err := http.ParseTime(val)
	if err != nil {
		return 0
	}

	delay := time.Until(target)
	if delay <= 0 {
		return 0
	}

	return delay
}

// drainBody reads and closes the response body to allow connection reuse.
func drainBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// progressBody wraps a response body to log download progress at regular
// intervals. Small responses (< progressInterval) produce no output.
type progressBody struct {
	io.ReadCloser

	url       string
	start     time.Time
	totalSize int64 // Content-Length; -1 if unknown.
	bytesRead int64
}

func (p *progressBody) Read(buf []byte) (int, error) {
	bytesRead, err := p.ReadCloser.Read(buf)

	if bytesRead > 0 {
		prev := p.bytesRead
		p.bytesRead += int64(bytesRead)

		if p.bytesRead/progressInterval > prev/progressInterval {
			slog.Debug("download progress",
				"url", p.url,
				"bytes", p.bytesRead,
				"total", p.totalSize,
				"elapsed", time.Since(p.start),
			)
		}
	}

	return bytesRead, err //nolint:wrapcheck // transparent wrapper
}

func (p *progressBody) Close() error {
	err := p.ReadCloser.Close()

	if p.bytesRead >= progressInterval {
		slog.Debug("download complete",
			"url", p.url,
			"bytes", p.bytesRead,
			"total", p.totalSize,
			"elapsed", time.Since(p.start),
		)
	}

	return err //nolint:wrapcheck // transparent wrapper
}

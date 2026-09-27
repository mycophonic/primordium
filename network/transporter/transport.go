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
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/mycophonic/primordium/fault"
)

// retryTransport is an http.RoundTripper that adds concurrency limiting,
// rate limiting, retry with exponential backoff, Retry-After, and User-Agent.
type retryTransport struct {
	base        http.RoundTripper
	sem         chan struct{} // parallelism semaphore; nil = unlimited
	limiter     chan struct{} // rate limiter tokens; nil = unlimited
	stopLimiter context.CancelFunc
	maxRetries  int
	initBackoff time.Duration
	maxBackoff  time.Duration
	userAgent   string
}

func newRetryTransport(opts Options) *retryTransport {
	transport := &retryTransport{
		base:        http.DefaultTransport,
		maxRetries:  opts.MaxRetries,
		initBackoff: opts.InitialBackoff,
		maxBackoff:  opts.MaxBackoff,
		userAgent:   opts.UserAgent,
	}

	if opts.Parallelism > 0 {
		transport.sem = make(chan struct{}, opts.Parallelism)
	}

	if opts.MaxPerSecond > 0 {
		transport.limiter = make(chan struct{}, 1)
		transport.limiter <- struct{}{} // Pre-fill so the first request doesn't wait for a tick.

		ctx, cancel := context.WithCancel(context.Background())
		transport.stopLimiter = cancel

		go transport.refill(ctx, opts.MaxPerSecond)
	}

	return transport
}

// RoundTrip implements http.RoundTripper.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", t.userAgent)
	}

	if err := t.acquireSem(req.Context()); err != nil {
		return nil, err
	}

	if t.sem != nil {
		defer func() { <-t.sem }()
	}

	return t.retryLoop(req)
}

// CloseIdleConnections stops the rate limiter goroutine and forwards to the
// base transport. Called by net/http.Client.CloseIdleConnections.
func (t *retryTransport) CloseIdleConnections() {
	if t.stopLimiter != nil {
		t.stopLimiter()
	}

	type closeIdler interface {
		CloseIdleConnections()
	}

	if ci, ok := t.base.(closeIdler); ok {
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

	select {
	case <-t.limiter:
		return time.Since(start), nil
	case <-ctx.Done():
		return 0, fmt.Errorf("%w: %w", fault.ErrCancelled, ctx.Err())
	}
}

func (t *retryTransport) retryLoop(req *http.Request) (*http.Response, error) {
	var last outcome

	for attempt := range t.maxRetries + 1 {
		if attempt > 0 {
			backoff := max(last.retryAfter, t.backoffDuration(attempt))

			if err := waitToRetry(req, backoff, last.err); err != nil {
				return nil, err
			}
		}

		var resp *http.Response

		resp, last = t.attempt(req, attempt)
		if resp != nil {
			return resp, nil
		}

		if last.final {
			return nil, last.err
		}
	}

	return nil, last.err
}

// outcome is what an attempt without a final response decided: an error
// to hand back, or one to remember while another attempt is made, after at
// least the delay the server asked for.
type outcome struct {
	err        error
	retryAfter time.Duration
	final      bool
}

// attempt makes one round trip, once a rate-limit token is available. A
// response it returns is final; without one, the outcome says what
// happened.
func (t *retryTransport) attempt(req *http.Request, attempt int) (*http.Response, outcome) {
	ctx := req.Context()

	tokenWait, err := t.waitForToken(ctx)
	if err != nil {
		return nil, outcome{err: err, final: true}
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		lastErr := fmt.Errorf("%w: %w", fault.ErrNetworkCommunication, err)

		if ctx.Err() != nil {
			return nil, outcome{err: fmt.Errorf("%w: %w", fault.ErrCancelled, lastErr), final: true}
		}

		slog.WarnContext(ctx, "HTTP transport error, retrying",
			"attempt", attempt+1,
			"elapsed", elapsed,
			"token_wait", tokenWait,
			"delay", t.backoffDuration(attempt+1).String(),
			"error", err,
			"url", req.URL.String(),
		)

		return nil, outcome{err: lastErr}
	}

	slog.DebugContext(ctx, "HTTP roundtrip",
		"status", resp.StatusCode,
		"elapsed", elapsed,
		"token_wait", tokenWait,
		"url", req.URL.String(),
	)

	if !retryableStatus(resp.StatusCode) {
		return accepted(req, resp, elapsed), outcome{}
	}

	// Drain body before retry to allow connection reuse.
	drainBody(resp.Body)

	return nil, t.statusFailure(req, resp.StatusCode, resp.Header, attempt, elapsed, tokenWait)
}

// statusFailure decides after a response whose status earns a retry. The
// attempts end after the last one, and when the server asks for a longer
// delay than maxBackoff allows.
func (t *retryTransport) statusFailure(
	req *http.Request,
	status int,
	header http.Header,
	attempt int,
	elapsed, tokenWait time.Duration,
) outcome {
	ctx := req.Context()
	lastErr := fmt.Errorf("%w: HTTP %d", fault.ErrUnacceptableResponse, status)

	if attempt == t.maxRetries {
		slog.WarnContext(ctx, "HTTP retries exhausted",
			"status", status,
			"attempts", t.maxRetries+1,
			"url", req.URL.String(),
		)

		return outcome{err: lastErr, final: true}
	}

	retryAfterVal := retryAfter(header)

	if t.maxBackoff > 0 && retryAfterVal > t.maxBackoff {
		slog.WarnContext(ctx, "retry-after too large, giving up",
			"status", status,
			"retry_after", retryAfterVal,
			"max", t.maxBackoff,
			"url", req.URL.String(),
		)

		return outcome{err: lastErr, final: true}
	}

	nextBackoff := max(retryAfterVal, t.backoffDuration(attempt+1))

	slog.WarnContext(ctx, "HTTP error, retrying",
		"status", status,
		"attempt", attempt+1,
		"elapsed", elapsed,
		"token_wait", tokenWait,
		"delay", nextBackoff.String(),
		"url", req.URL.String(),
	)

	return outcome{err: lastErr, retryAfter: retryAfterVal}
}

// waitToRetry waits out backoff before the next attempt, unless the request
// is cancelled first, then rewinds the request body for the resend.
func waitToRetry(req *http.Request, backoff time.Duration, lastErr error) error {
	select {
	case <-req.Context().Done():
		return fmt.Errorf("%w: %w", fault.ErrCancelled, lastErr)
	case <-time.After(backoff):
	}

	if err := resetBody(req); err != nil {
		return fmt.Errorf("%w: %w", fault.ErrNetworkCommunication, err)
	}

	return nil
}

// retryableStatus reports the statuses worth another attempt: rate limiting,
// an authorization a retry may renew, and server errors.
func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusUnauthorized ||
		code >= http.StatusInternalServerError
}

// accepted hands a final response to the caller, its body wrapped to report
// download progress, and logs it when it was slow.
func accepted(req *http.Request, resp *http.Response, elapsed time.Duration) *http.Response {
	if elapsed > slowRequestThreshold {
		slog.WarnContext(req.Context(), "slow HTTP request",
			"elapsed", elapsed,
			"status", resp.StatusCode,
			"url", req.URL.String(),
		)
	}

	resp.Body = &progressBody{
		ReadCloser: resp.Body,
		url:        req.URL.String(),
		start:      time.Now(),
		totalSize:  resp.ContentLength,
	}

	return resp
}

// refill sends a token into the limiter channel at the configured rate.
func (t *retryTransport) refill(ctx context.Context, maxPerSecond int) {
	interval := time.Second / time.Duration(maxPerSecond)
	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case t.limiter <- struct{}{}:
			default: // Drop token if buffer full (no bursting).
			}
		}
	}
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

// resetBody rewinds the request body for retry. No-op for bodyless requests (GET).
func resetBody(req *http.Request) error {
	if req.Body == nil || req.GetBody == nil {
		return nil
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

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

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/bytesize"
	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/network/transporter"
)

// schedulingSlack is how late a backoff timer and the loopback round trip
// usually run past the backoff itself. Nothing bounds it for every retry: a
// stalled runner delays whichever retries the stall overlaps. So the backoff
// tests hold most retries to their upper bound and every retry only to twice
// it, a ceiling a stall stays under and a wrong backoff does not. Timers never
// fire early, so lower bounds hold for each retry and need no slack.
const schedulingSlack = 150 * time.Millisecond

// overBounds counts the gaps over their upper bound, and fails the test for any
// gap over twice it. A single retry wrong by less than that ceiling passes, as
// a stall would: the price of tolerating one.
func overBounds(t *testing.T, gaps []time.Duration, high func(retry int) time.Duration) int {
	t.Helper()

	late := 0

	for i, gap := range gaps {
		bound := high(i)

		assert.Assert(t, gap <= 2*bound, "retry %d: backoff %v over twice its bound %v: %v", i+1, gap, bound, gaps)

		if gap > bound {
			late++
		}
	}

	return late
}

// backend is an HTTP test server recording when each request arrived.
type backend struct {
	url   string
	calls atomic.Int32

	mu       sync.Mutex
	arrivals []time.Time
}

// newBackend serves handler, which is given the 1-based number of the call.
func newBackend(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, call int32)) *backend {
	t.Helper()

	back := &backend{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		back.mu.Lock()
		back.arrivals = append(back.arrivals, time.Now())
		back.mu.Unlock()

		handler(w, r, back.calls.Add(1))
	}))
	t.Cleanup(srv.Close)

	back.url = srv.URL

	return back
}

// gaps returns the delay between each request and the one before it.
func (b *backend) gaps() []time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	gaps := make([]time.Duration, 0, len(b.arrivals))
	for i := 1; i < len(b.arrivals); i++ {
		gaps = append(gaps, b.arrivals[i].Sub(b.arrivals[i-1]))
	}

	return gaps
}

func status(code int) func(http.ResponseWriter, *http.Request, int32) {
	return func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.WriteHeader(code)
	}
}

func statusWithHeader(code int, key, value string) func(http.ResponseWriter, *http.Request, int32) {
	return func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.Header().Set(key, value)
		w.WriteHeader(code)
	}
}

// hangUp drops the connection without answering, which the client sees as a
// transport error.
func hangUp(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		t.Errorf("hijack: %v", err)

		return
	}

	_ = conn.Close()
}

func newClient(t *testing.T, opts transporter.Options) *http.Client {
	t.Helper()

	client := transporter.NewClient(opts)
	t.Cleanup(client.CloseIdleConnections)

	return client
}

func doGet(ctx context.Context, t *testing.T, client *http.Client, url string) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	assert.NilError(t, err)

	return client.Do(req)
}

// --- Core retry behavior ---

func TestSuccessNoRetry(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	assert.Equal(t, back.calls.Load(), int32(1))
}

func TestNonRetryableStatus(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, status(code))
			client := newClient(t, transporter.Options{
				MaxRetries:     3,
				InitialBackoff: time.Millisecond,
			})

			resp, err := doGet(t.Context(), t, client, back.url)
			assert.NilError(t, err)
			assert.Equal(t, resp.StatusCode, code)
			assert.Check(t, resp.Body.Close())

			assert.Equal(t, back.calls.Load(), int32(1))
		})
	}
}

func TestRetryableStatusExhaustsRetries(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusInternalServerError,
		http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusBadGateway,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, status(code))
			client := newClient(t, transporter.Options{
				MaxRetries:     2,
				InitialBackoff: time.Millisecond,
			})

			resp, err := doGet(t.Context(), t, client, back.url)
			if resp != nil {
				assert.Check(t, resp.Body.Close())
			}

			assert.Assert(t, resp == nil)
			assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
			assert.Equal(t, back.calls.Load(), int32(3)) // 1 initial + 2 retries
		})
	}
}

func TestRetryableStatusEventualSuccess(t *testing.T) {
	t.Parallel()

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call <= 2 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	assert.Equal(t, back.calls.Load(), int32(3))
}

func TestTransportErrorRetried(t *testing.T) {
	t.Parallel()

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		hangUp(t, w)
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrNetworkCommunication))
	assert.Equal(t, back.calls.Load(), int32(3))
}

func TestTransportErrorEventualSuccess(t *testing.T) {
	t.Parallel()

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call == 1 {
			hangUp(t, w)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	assert.Equal(t, back.calls.Load(), int32(2))
}

func TestMaxRetriesZeroSingleAttempt(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusInternalServerError))
	client := newClient(t, transporter.Options{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
	assert.Equal(t, back.calls.Load(), int32(1))
}

// --- Retry-After header ---

func TestRetryAfterHonored(t *testing.T) {
	t.Parallel()

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
		if call == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	// A MaxBackoff just above one second also pins the parsed value from
	// above: anything larger would be given up on instead of waited for.
	client := newClient(t, transporter.Options{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     1100 * time.Millisecond,
	})

	start := time.Now()

	resp, err := doGet(t.Context(), t, client, back.url)
	elapsed := time.Since(start)

	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	// Retry-After = 1s should dominate over InitialBackoff = 1ms.
	assert.Assert(t, elapsed >= time.Second,
		"expected at least 1s delay from Retry-After, got %v", elapsed)
}

func TestRetryAfterExceedsMaxBackoff(t *testing.T) {
	t.Parallel()

	back := newBackend(t, statusWithHeader(http.StatusTooManyRequests, "Retry-After", "60"))
	client := newClient(t, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Second,
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
	// Should abandon after seeing Retry-After > MaxBackoff, not retry all 3 times.
	assert.Equal(t, back.calls.Load(), int32(1))
}

// TestRetryAfterParsing reads the parsed Retry-After off the give-up rule: a
// value above MaxBackoff abandons after one call, anything else is retried.
func TestRetryAfterParsing(t *testing.T) {
	t.Parallel()

	fixed := func(value string) func() string { return func() string { return value } }

	tests := []struct {
		name       string
		value      func() string
		maxBackoff time.Duration
		wantCalls  int32
	}{
		{"seconds", fixed("5"), 4900 * time.Millisecond, 1},
		// The date is taken as the response is written: a parallel subtest may
		// wait seconds for its turn, eating a date taken when the table is built.
		{"future_date", func() string {
			return time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
		}, 5 * time.Second, 1},
		{"zero", fixed("0"), time.Millisecond, 2},
		{"negative", fixed("-1"), time.Millisecond, 2},
		{"empty", fixed(""), time.Millisecond, 2},
		{"past_date", fixed("Thu, 01 Dec 2025 16:00:00 GMT"), time.Millisecond, 2},
		{"float", fixed("1.5"), time.Millisecond, 2},
		{"garbage", fixed("not-a-date-or-number"), time.Millisecond, 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
				if value := test.value(); value != "" {
					w.Header().Set("Retry-After", value)
				}

				w.WriteHeader(http.StatusTooManyRequests)
			})
			client := newClient(t, transporter.Options{
				MaxRetries:     1,
				InitialBackoff: time.Millisecond,
				MaxBackoff:     test.maxBackoff,
			})

			resp, err := doGet(t.Context(), t, client, back.url)
			if resp != nil {
				assert.Check(t, resp.Body.Close())
			}

			assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
			assert.Equal(t, back.calls.Load(), test.wantCalls)
		})
	}
}

// --- Context cancellation ---

func TestContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, 1)
	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.WriteHeader(http.StatusInternalServerError)

		arrived <- struct{}{}
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Second,
	})

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan error, 1)

	go func() {
		resp, err := doGet(ctx, t, client, back.url)
		if resp != nil {
			assert.Check(t, resp.Body.Close())
		}

		done <- err
	}()

	// Let the first attempt fail and enter its ten-second backoff.
	<-arrived
	time.Sleep(50 * time.Millisecond)
	cancel()

	roundTripErr := <-done

	assert.Assert(t, errors.Is(roundTripErr, fault.ErrCancelled))
	assert.Equal(t, back.calls.Load(), int32(1))
}

func TestContextCancelledDuringSemaphoreWait(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, 1)
	back := newBackend(t, func(_ http.ResponseWriter, r *http.Request, _ int32) {
		arrived <- struct{}{}

		// Hold the only slot until the client gives up.
		<-r.Context().Done()
	})
	client := newClient(t, transporter.Options{
		Parallelism:    1,
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})

	// Fill the semaphore with a blocking request.
	blockCtx, blockCancel := context.WithCancel(t.Context())
	defer blockCancel()

	go func() {
		resp, _ := doGet(blockCtx, t, client, back.url)
		if resp != nil {
			assert.Check(t, resp.Body.Close())
		}
	}()

	<-arrived

	// Second request should fail to acquire semaphore when cancelled.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	resp, err := doGet(ctx, t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
	assert.Equal(t, back.calls.Load(), int32(1))
}

func TestContextCancelledDuringRateLimitWait(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusOK))
	// 1 request per second — after pre-filled token is consumed, next token takes ~1s.
	client := newClient(t, transporter.Options{
		MaxPerSecond:   1,
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})

	// Consume the pre-filled token.
	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	// Next request must wait for a token; cancel before it arrives.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	resp, err = doGet(ctx, t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
	assert.Equal(t, back.calls.Load(), int32(1))
}

func TestContextCancelledDuringRoundTrip(t *testing.T) {
	t.Parallel()

	arrived := make(chan struct{}, 1)
	back := newBackend(t, func(_ http.ResponseWriter, r *http.Request, _ int32) {
		arrived <- struct{}{}

		<-r.Context().Done()
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		<-arrived
		cancel()
	}()

	resp, err := doGet(ctx, t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
	assert.Equal(t, back.calls.Load(), int32(1))
}

// --- Body reset on retry ---

func TestBodyResentOnRetry(t *testing.T) {
	t.Parallel()

	bodyContent := "request-body-payload"

	var (
		mu     sync.Mutex
		bodies []string
	)

	back := newBackend(t, func(w http.ResponseWriter, r *http.Request, call int32) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		mu.Lock()

		bodies = append(bodies, string(data))

		mu.Unlock()

		if call == 1 {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	client := newClient(t, transporter.Options{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	})

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, back.url,
		bytes.NewReader([]byte(bodyContent)),
	)
	assert.NilError(t, err)

	resp, err := client.Do(req)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	mu.Lock()
	defer mu.Unlock()

	assert.DeepEqual(t, bodies, []string{bodyContent, bodyContent})
}

// --- User-Agent injection ---

// receivedUserAgent returns the User-Agent the server saw for a request
// carrying the given one ("" for none).
func receivedUserAgent(t *testing.T, opts transporter.Options, sent string) string {
	t.Helper()

	received := make(chan string, 1)
	back := newBackend(t, func(_ http.ResponseWriter, r *http.Request, _ int32) {
		received <- r.Header.Get("User-Agent")
	})
	client := newClient(t, opts)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, back.url, http.NoBody)
	assert.NilError(t, err)

	if sent != "" {
		req.Header.Set("User-Agent", sent)
	}

	resp, err := client.Do(req)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())

	return <-received
}

func TestUserAgentInjectedWhenAbsent(t *testing.T) {
	t.Parallel()

	got := receivedUserAgent(t, transporter.Options{UserAgent: "test-agent/1.0"}, "")
	assert.Equal(t, got, "test-agent/1.0")
}

func TestUserAgentPreservedWhenPresent(t *testing.T) {
	t.Parallel()

	got := receivedUserAgent(t, transporter.Options{UserAgent: "test-agent/1.0"}, "custom-agent/2.0")
	assert.Equal(t, got, "custom-agent/2.0")
}

func TestNoUserAgentWhenEmpty(t *testing.T) {
	t.Parallel()

	// Nothing is injected, so the request goes out with net/http's own default.
	got := receivedUserAgent(t, transporter.Options{}, "")
	assert.Assert(t, strings.HasPrefix(got, "Go-http-client/"), "got User-Agent %q", got)
}

// --- Concurrency limiting ---

func TestConcurrencyLimiting(t *testing.T) {
	t.Parallel()

	var (
		inflight    atomic.Int32
		maxInflight atomic.Int32
	)

	const parallelism = 2

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		current := inflight.Add(1)

		for {
			old := maxInflight.Load()
			if current <= old || maxInflight.CompareAndSwap(old, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // Hold the slot.
		inflight.Add(-1)

		w.WriteHeader(http.StatusOK)
	})
	client := newClient(t, transporter.Options{
		Parallelism: parallelism,
		MaxRetries:  0,
	})

	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			resp, err := doGet(t.Context(), t, client, back.url)
			if err != nil {
				t.Errorf("request: %v", err)

				return
			}

			assert.Check(t, resp.Body.Close())
		})
	}

	wg.Wait()

	assert.Assert(t, maxInflight.Load() <= int32(parallelism),
		"max inflight %d exceeded parallelism %d", maxInflight.Load(), parallelism)
	assert.Assert(t, maxInflight.Load() > 1,
		"expected concurrent requests, got max inflight %d", maxInflight.Load())
}

// --- Rate limiting ---

func TestRateLimiting(t *testing.T) {
	t.Parallel()

	const maxPerSecond = 10

	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{
		MaxPerSecond: maxPerSecond,
		MaxRetries:   0,
	})

	// First request uses the pre-filled token, so it's instant.
	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Check(t, resp.Body.Close())

	// Subsequent requests must wait for refill. Issue a few and measure total time.
	const requests = 5

	start := time.Now()

	for range requests {
		resp, err = doGet(t.Context(), t, client, back.url)
		assert.NilError(t, err)
		assert.Equal(t, resp.StatusCode, http.StatusOK)
		assert.Check(t, resp.Body.Close())
	}

	elapsed := time.Since(start)
	// 5 requests at 10/s → at least 500ms (with some tolerance).
	expectedMin := time.Duration(requests) * time.Second /
		time.Duration(maxPerSecond) * 3 / 4

	assert.Assert(t, elapsed >= expectedMin,
		"expected at least %v for %d requests at %d/s, got %v",
		expectedMin, requests, maxPerSecond, elapsed)
}

// --- CloseIdleConnections ---

// TestConnectionsAreTheClients checks that a client's connections are its own:
// another client closing its idle connections, or http.DefaultTransport
// closing its own (as every httptest.Server.Close does), leaves them open.
func TestConnectionsAreTheClients(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{})
	other := newClient(t, transporter.Options{})

	reused := func(c *http.Client) bool {
		t.Helper()

		var info httptrace.GotConnInfo

		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
			GotConn: func(got httptrace.GotConnInfo) { info = got },
		})

		resp, err := doGet(ctx, t, c, back.url)
		assert.NilError(t, err)

		_, err = io.Copy(io.Discard, resp.Body)
		assert.NilError(t, err)
		assert.NilError(t, resp.Body.Close())

		return info.Reused
	}

	assert.Assert(t, !reused(client), "a new client has no connection to reuse")
	assert.Assert(t, !reused(other), "a new client has no connection to reuse")

	other.CloseIdleConnections()
	http.DefaultTransport.(interface{ CloseIdleConnections() }).CloseIdleConnections()

	assert.Assert(t, reused(client), "another client closed this client's connection")
}

// TestDefaultTransportTakenAtFirstRequest checks that a client made, and even
// closed, before http.DefaultTransport is configured, as network.SetDefaults
// does at startup, still requests with that configuration.
//
//nolint:paralleltest // replaces the process-wide default transport
func TestDefaultTransportTakenAtFirstRequest(t *testing.T) {
	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{})
	client.CloseIdleConnections()

	var dialed atomic.Bool

	configured := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed.Store(true)

			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	previous := http.DefaultTransport
	http.DefaultTransport = configured //nolint:reassign // the configuration under test

	t.Cleanup(func() {
		configured.CloseIdleConnections()

		http.DefaultTransport = previous //nolint:reassign // restores the configuration under test
	})

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())

	assert.Assert(t, dialed.Load(), "the request did not use http.DefaultTransport's configuration")
}

// TestLegacyTLS checks that a client's TLS is its own: against a server at
// TLS 1.2 with P-256 as its only key exchange, a client at the process's
// default (1.3, X25519 hybrids only) fails the handshake, one with LegacyTLS
// completes it, and the process's default is as it was for the clients made
// after.
//
//nolint:paralleltest // replaces the process-wide default transport
func TestLegacyTLS(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{ // a server that speaks no TLS 1.3 and no X25519
		MaxVersion:       tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{tls.CurveP256},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// As network.SetDefaults leaves it, trusting the test server's certificate.
	curves := []tls.CurveID{tls.X25519MLKEM768, tls.X25519}
	configured := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: curves,
		RootCAs:          srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
	}}

	previous := http.DefaultTransport
	http.DefaultTransport = configured //nolint:reassign // the configuration under test

	t.Cleanup(func() {
		configured.CloseIdleConnections()

		http.DefaultTransport = previous //nolint:reassign // restores the configuration under test
	})

	// refused: the server alerts on the version, and there is no response.
	refused := func(opts transporter.Options) {
		t.Helper()

		resp, err := doGet(t.Context(), t, newClient(t, opts), srv.URL)
		if resp != nil {
			_ = resp.Body.Close()
		}

		assert.ErrorContains(t, err, "protocol version not supported")
	}

	refused(transporter.Options{})

	resp, err := doGet(t.Context(), t, newClient(t, transporter.Options{LegacyTLS: true}), srv.URL)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	assert.Equal(t, resp.TLS.Version, uint16(tls.VersionTLS12))

	refused(transporter.Options{})
	assert.Equal(t, configured.TLSClientConfig.MinVersion, uint16(tls.VersionTLS13))
	assert.DeepEqual(t, configured.TLSClientConfig.CurvePreferences, curves)
}

func TestCloseIdleConnectionsStopsRateLimiter(t *testing.T) {
	t.Parallel()

	back := newBackend(t, status(http.StatusOK))
	client := newClient(t, transporter.Options{
		MaxPerSecond: 1,
		MaxRetries:   0,
	})

	// Consume pre-filled token.
	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)
	assert.Check(t, resp.Body.Close())

	// Stop the rate limiter.
	client.CloseIdleConnections()

	// After stopping, no new tokens are produced. A request with a short
	// timeout should fail because no token arrives.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	resp, err = doGet(ctx, t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
	assert.Equal(t, back.calls.Load(), int32(1))
}

// --- Backoff duration ---

// retryGaps exhausts opts.MaxRetries against a failing server and returns the
// delay the client left before each retry.
func retryGaps(t *testing.T, opts transporter.Options) []time.Duration {
	t.Helper()

	back := newBackend(t, status(http.StatusInternalServerError))
	client := newClient(t, opts)

	// Fail rather than hang when a backoff runs away.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	resp, err := doGet(ctx, t, client, back.url)
	if resp != nil {
		assert.Check(t, resp.Body.Close())
	}

	assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse), "got %v", err)

	gaps := back.gaps()
	assert.Equal(t, len(gaps), opts.MaxRetries)

	return gaps
}

func TestBackoffExponential(t *testing.T) {
	t.Parallel()

	// Long enough that the backoff, not schedulingSlack, sets each bound: a
	// retry grown too fast then passes twice its bound, whatever its jitter.
	const initial = 100 * time.Millisecond

	gaps := retryGaps(t, transporter.Options{
		MaxRetries:     5,
		InitialBackoff: initial,
	})

	// Expected center before retry n: initial * 2^(n-1), jittered by ±25%.
	for i, gap := range gaps {
		assert.Assert(t, gap >= (initial<<i)*3/4, "retry %d: backoff %v under %v", i+1, gap, (initial<<i)*3/4)
	}

	late := overBounds(t, gaps, func(retry int) time.Duration { return (initial<<retry)*5/4 + schedulingSlack })
	assert.Assert(t, late*2 < len(gaps), "%d of %d retries past their range: %v", late, len(gaps), gaps)
}

func TestBackoffJitterRange(t *testing.T) {
	t.Parallel()

	const backoff = 40 * time.Millisecond

	// MaxBackoff equal to InitialBackoff holds every retry at the same center.
	gaps := retryGaps(t, transporter.Options{
		MaxRetries:     40,
		InitialBackoff: backoff,
		MaxBackoff:     backoff,
	})

	minSeen := time.Duration(1<<63 - 1)

	for i, gap := range gaps {
		minSeen = min(minSeen, gap)

		// Jitter range: [0.75, 1.25] * 40ms = [30ms, 50ms].
		assert.Assert(t, gap >= backoff*3/4, "retry %d: backoff %v under the jitter range", i+1, gap)
	}

	late := overBounds(t, gaps, func(int) time.Duration { return backoff*5/4 + schedulingSlack })
	assert.Assert(t, late*2 < len(gaps), "%d of %d retries past the jitter range: %v", late, len(gaps), gaps)

	// Timers only ever run late, so a delay this far under the center can
	// only come from jitter.
	assert.Assert(t, minSeen < backoff*9/10,
		"no retry was jittered below the center: shortest backoff %v", minSeen)
}

func TestBackoffCappedByMaxBackoff(t *testing.T) {
	t.Parallel()

	const maxBackoff = 40 * time.Millisecond

	// Uncapped, retry 40 at 20ms initial would be 20ms * 2^39, past int64.
	gaps := retryGaps(t, transporter.Options{
		MaxRetries:     40,
		InitialBackoff: 20 * time.Millisecond,
		MaxBackoff:     maxBackoff,
	})

	// Capped at 40ms, with jitter [0.75, 1.25] → max 50ms. An uncapped backoff
	// passes it from the third retry on, and soon outlasts retryGaps' deadline.
	late := overBounds(t, gaps, func(int) time.Duration { return maxBackoff*5/4 + schedulingSlack })
	assert.Assert(t, late*2 < len(gaps), "%d of %d retries past the capped max %v * 1.25: %v",
		late, len(gaps), maxBackoff, gaps)
}

// --- Response body ---

func TestResponseBodyPassthrough(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("x"), 64*bytesize.KiB)

	back := newBackend(t, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	})
	client := newClient(t, transporter.Options{MaxRetries: 0})

	resp, err := doGet(t.Context(), t, client, back.url)
	assert.NilError(t, err)

	got, err := io.ReadAll(resp.Body)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, data)
	assert.NilError(t, resp.Body.Close())
}

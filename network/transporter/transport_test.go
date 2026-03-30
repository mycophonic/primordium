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
package transporter_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"github.com/mycophonic/primordium/fault"
	"github.com/mycophonic/primordium/network/transporter"
)

// fakeRT is a configurable fake http.RoundTripper for testing.
type fakeRT struct {
	handler func(req *http.Request) (*http.Response, error)
	calls   atomic.Int32
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)

	return f.handler(req)
}

func newResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func newResponseWithHeader(statusCode int, key, value string) *http.Response {
	resp := newResponse(statusCode)
	resp.Header.Set(key, value)

	return resp
}

func doGet(t *testing.T, rt http.RoundTripper) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	resp, rtErr := rt.RoundTrip(req)
	if rtErr != nil && resp != nil {
		resp.Body.Close()
	}

	return resp, rtErr
}

// --- Core retry behavior ---

func TestSuccessNoRetry(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	assert.Equal(t, fake.calls.Load(), int32(1))
}

func TestNonRetryableStatus(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden,
	} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()

			fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
				return newResponse(code), nil
			}}
			rt := transporter.NewTestTransport(fake, transporter.Options{
				MaxRetries:     3,
				InitialBackoff: time.Millisecond,
			})

			resp, err := doGet(t, rt)
			assert.NilError(t, err)
			assert.Equal(t, resp.StatusCode, code)
			resp.Body.Close()

			assert.Equal(t, fake.calls.Load(), int32(1))
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

			fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
				return newResponse(code), nil
			}}
			rt := transporter.NewTestTransport(fake, transporter.Options{
				MaxRetries:     2,
				InitialBackoff: time.Millisecond,
			})

			resp, err := doGet(t, rt)
			if resp != nil {
				resp.Body.Close()
			}

			assert.Assert(t, resp == nil)
			assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
			assert.Equal(t, fake.calls.Load(), int32(3)) // 1 initial + 2 retries
		})
	}
}

func TestRetryableStatusEventualSuccess(t *testing.T) {
	t.Parallel()

	var attempt atomic.Int32

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		call := attempt.Add(1)
		if call <= 2 {
			return newResponse(http.StatusInternalServerError), nil
		}

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	assert.Equal(t, attempt.Load(), int32(3))
}

func TestTransportErrorRetried(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t, rt)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrNetworkCommunication))
	assert.Equal(t, fake.calls.Load(), int32(3))
}

func TestTransportErrorEventualSuccess(t *testing.T) {
	t.Parallel()

	var attempt atomic.Int32

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		call := attempt.Add(1)
		if call == 1 {
			return nil, errors.New("transient network error")
		}

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	assert.Equal(t, attempt.Load(), int32(2))
}

func TestMaxRetriesZeroSingleAttempt(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusInternalServerError), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})

	resp, err := doGet(t, rt)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
	assert.Equal(t, fake.calls.Load(), int32(1))
}

// --- Retry-After header ---

func TestRetryAfterHonored(t *testing.T) {
	t.Parallel()

	var attempt atomic.Int32

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		call := attempt.Add(1)
		if call == 1 {
			return newResponseWithHeader(
				http.StatusTooManyRequests, "Retry-After", "1",
			), nil
		}

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	})

	start := time.Now()

	resp, err := doGet(t, rt)
	elapsed := time.Since(start)

	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	// Retry-After = 1s should dominate over InitialBackoff = 1ms.
	assert.Assert(t, elapsed >= 900*time.Millisecond,
		"expected at least ~1s delay from Retry-After, got %v", elapsed)
}

func TestRetryAfterExceedsMaxBackoff(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponseWithHeader(
			http.StatusTooManyRequests, "Retry-After", "60",
		), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     5 * time.Second,
	})

	resp, err := doGet(t, rt)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, resp == nil)
	assert.Assert(t, errors.Is(err, fault.ErrUnacceptableResponse))
	// Should abandon after seeing Retry-After > MaxBackoff, not retry all 3 times.
	assert.Equal(t, fake.calls.Load(), int32(1))
}

// --- Context cancellation ---

func TestContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusInternalServerError), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	done := make(chan error, 1)

	go func() {
		resp, rtErr := rt.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}

		done <- rtErr
	}()

	// Give the first attempt time to fail and enter backoff.
	time.Sleep(50 * time.Millisecond)
	cancel()

	roundTripErr := <-done

	assert.Assert(t, errors.Is(roundTripErr, fault.ErrCancelled))
}

func TestContextCancelledDuringSemaphoreWait(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		// Block forever to hold the semaphore.
		select {}
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		Parallelism:    1,
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})

	// Fill the semaphore with a blocking request.
	blockCtx, blockCancel := context.WithCancel(context.Background())
	defer blockCancel()

	blockReq, err := http.NewRequestWithContext(
		blockCtx, http.MethodGet, "http://example.com/block", nil,
	)
	assert.NilError(t, err)

	go func() {
		resp, _ := rt.RoundTrip(blockReq)
		if resp != nil {
			resp.Body.Close()
		}
	}()

	// Wait for blocker to acquire semaphore.
	time.Sleep(50 * time.Millisecond)

	// Second request should fail to acquire semaphore when cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
}

func TestContextCancelledDuringRateLimitWait(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK), nil
	}}
	// 1 request per second — after pre-filled token is consumed, next token takes ~1s.
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxPerSecond:   1,
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	})
	defer transporter.CloseTestTransport(rt)

	// Consume the pre-filled token.
	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	// Next request must wait for a token; cancel before it arrives.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	resp, err = rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
}

func TestContextCancelledDuringRoundTrip(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
}

// --- Body reset on retry ---

func TestBodyResentOnRetry(t *testing.T) {
	t.Parallel()

	bodyContent := "request-body-payload"

	var (
		bodies  []string
		attempt atomic.Int32
	)

	fake := &fakeRT{handler: func(req *http.Request) (*http.Response, error) {
		data, readErr := io.ReadAll(req.Body)
		assert.NilError(t, readErr)

		bodies = append(bodies, string(data))

		call := attempt.Add(1)
		if call == 1 {
			return newResponse(http.StatusInternalServerError), nil
		}

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	})

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "http://example.com/test",
		bytes.NewReader([]byte(bodyContent)),
	)
	assert.NilError(t, err)

	resp, err := rt.RoundTrip(req)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	assert.Equal(t, len(bodies), 2)
	assert.Equal(t, bodies[0], bodyContent)
	assert.Equal(t, bodies[1], bodyContent)
}

// --- User-Agent injection ---

func TestUserAgentInjectedWhenAbsent(t *testing.T) {
	t.Parallel()

	var capturedUA string

	fake := &fakeRT{handler: func(req *http.Request) (*http.Response, error) {
		capturedUA = req.Header.Get("User-Agent")

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		UserAgent: "test-agent/1.0",
	})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	resp.Body.Close()

	assert.Equal(t, capturedUA, "test-agent/1.0")
}

func TestUserAgentPreservedWhenPresent(t *testing.T) {
	t.Parallel()

	var capturedUA string

	fake := &fakeRT{handler: func(req *http.Request) (*http.Response, error) {
		capturedUA = req.Header.Get("User-Agent")

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		UserAgent: "test-agent/1.0",
	})

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	req.Header.Set("User-Agent", "custom-agent/2.0")

	resp, err := rt.RoundTrip(req)
	assert.NilError(t, err)
	resp.Body.Close()

	assert.Equal(t, capturedUA, "custom-agent/2.0")
}

func TestNoUserAgentWhenEmpty(t *testing.T) {
	t.Parallel()

	var capturedUA string

	fake := &fakeRT{handler: func(req *http.Request) (*http.Response, error) {
		capturedUA = req.Header.Get("User-Agent")

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	resp.Body.Close()

	assert.Equal(t, capturedUA, "")
}

// --- Concurrency limiting ---

func TestConcurrencyLimiting(t *testing.T) {
	t.Parallel()

	var (
		inflight    atomic.Int32
		maxInflight atomic.Int32
	)

	const parallelism = 2

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		current := inflight.Add(1)

		for {
			old := maxInflight.Load()
			if current <= old || maxInflight.CompareAndSwap(old, current) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // Hold the slot.
		inflight.Add(-1)

		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		Parallelism: parallelism,
		MaxRetries:  0,
	})

	done := make(chan struct{})

	for range 10 {
		go func() {
			req, _ := http.NewRequestWithContext(
				context.Background(), http.MethodGet, "http://example.com/test", nil,
			)

			resp, _ := rt.RoundTrip(req)
			if resp != nil {
				resp.Body.Close()
			}

			done <- struct{}{}
		}()
	}

	for range 10 {
		<-done
	}

	assert.Assert(t, maxInflight.Load() <= int32(parallelism),
		"max inflight %d exceeded parallelism %d", maxInflight.Load(), parallelism)
	assert.Assert(t, maxInflight.Load() > 1,
		"expected concurrent requests, got max inflight %d", maxInflight.Load())
}

// --- Rate limiting ---

func TestRateLimiting(t *testing.T) {
	t.Parallel()

	const maxPerSecond = 10

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK), nil
	}}

	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxPerSecond: maxPerSecond,
		MaxRetries:   0,
	})
	defer transporter.CloseTestTransport(rt)

	// First request uses the pre-filled token, so it's instant.
	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	assert.Equal(t, resp.StatusCode, http.StatusOK)
	resp.Body.Close()

	// Subsequent requests must wait for refill. Issue a few and measure total time.
	const requests = 5

	start := time.Now()

	for range requests {
		resp, err = doGet(t, rt)
		assert.NilError(t, err)
		assert.Equal(t, resp.StatusCode, http.StatusOK)
		resp.Body.Close()
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

func TestCloseIdleConnectionsStopsRateLimiter(t *testing.T) {
	t.Parallel()

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK), nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{
		MaxPerSecond: 1,
		MaxRetries:   0,
	})

	// Consume pre-filled token.
	resp, err := doGet(t, rt)
	assert.NilError(t, err)
	resp.Body.Close()

	// Stop the rate limiter.
	transporter.CloseTestTransport(rt)

	// After stopping, no new tokens are produced. A request with a short
	// timeout should fail because no token arrives.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "http://example.com/test", nil,
	)
	assert.NilError(t, err)

	resp, err = rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}

	assert.Assert(t, errors.Is(err, fault.ErrCancelled))
}

// --- Backoff duration ---

func TestBackoffExponential(t *testing.T) {
	t.Parallel()

	// Collect samples to average out jitter.
	const samples = 100

	for attempt := 1; attempt <= 4; attempt++ {
		var total time.Duration

		for range samples {
			total += transporter.ComputeBackoff(
				100*time.Millisecond, 0, attempt,
			)
		}

		avg := total / samples

		// Expected center: initBackoff * 2^(attempt-1).
		expectedCenter := time.Duration(
			float64(100*time.Millisecond) * float64(int(1)<<(attempt-1)),
		)

		// Allow ±30% tolerance for averaged jitter.
		low := time.Duration(float64(expectedCenter) * 0.7)
		high := time.Duration(float64(expectedCenter) * 1.3)

		assert.Assert(t, avg >= low && avg <= high,
			"attempt %d: avg backoff %v outside expected range [%v, %v]",
			attempt, avg, low, high)
	}
}

func TestBackoffJitterRange(t *testing.T) {
	t.Parallel()

	var minSeen, maxSeen time.Duration

	minSeen = time.Hour

	for range 1000 {
		duration := transporter.ComputeBackoff(time.Second, 0, 1)
		if duration < minSeen {
			minSeen = duration
		}

		if duration > maxSeen {
			maxSeen = duration
		}
	}

	// Jitter range: [0.75, 1.25] * 1s = [750ms, 1250ms].
	assert.Assert(t, minSeen >= 750*time.Millisecond,
		"min backoff %v below 750ms", minSeen)
	assert.Assert(t, maxSeen <= 1250*time.Millisecond,
		"max backoff %v above 1250ms", maxSeen)
	// Ensure we actually see spread.
	assert.Assert(t, maxSeen-minSeen > 200*time.Millisecond,
		"jitter spread too narrow: min=%v max=%v", minSeen, maxSeen)
}

func TestBackoffCappedByMaxBackoff(t *testing.T) {
	t.Parallel()

	maxBackoff := 5 * time.Second

	// At attempt 10 with 1s initial, uncapped backoff would be 512s.
	// With MaxBackoff=5s, it should be capped.
	for range 100 {
		duration := transporter.ComputeBackoff(
			time.Second, maxBackoff, 10,
		)

		// Capped at 5s, with jitter [0.75, 1.25] → max 6.25s.
		assert.Assert(t, duration <= time.Duration(
			float64(maxBackoff)*1.25+float64(time.Millisecond),
		),
			"backoff %v exceeded capped max %v * 1.25", duration, maxBackoff)
	}
}

func TestBackoffNoOverflowAtHighAttempts(t *testing.T) {
	t.Parallel()

	// Without the overflow guard, attempt 40 with 1s init would overflow int64.
	// With MaxBackoff, it must remain positive and bounded.
	for range 100 {
		duration := transporter.ComputeBackoff(
			time.Second, 30*time.Second, 40,
		)
		assert.Assert(t, duration > 0,
			"backoff must be positive at high attempt, got %v", duration)
		assert.Assert(t, duration <= time.Duration(
			float64(30*time.Second)*1.25+float64(time.Millisecond),
		),
			"backoff %v exceeded capped max", duration)
	}
}

// --- retryAfter parsing ---

func TestRetryAfterParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"valid_seconds", "5", 5 * time.Second},
		{"zero", "0", 0},
		{"negative", "-1", 0},
		{"empty", "", 0},
		{"past_date", "Thu, 01 Dec 2025 16:00:00 GMT", 0},
		{"float", "1.5", 0},
		{"garbage", "not-a-date-or-number", 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			header := make(http.Header)
			if test.value != "" {
				header.Set("Retry-After", test.value)
			}

			got := transporter.ParseRetryAfter(header)
			assert.Equal(t, got, test.expected)
		})
	}

	// Future HTTP-date: must return a positive duration.
	t.Run("future_date", func(t *testing.T) {
		t.Parallel()

		future := time.Now().Add(10 * time.Second)
		header := make(http.Header)
		header.Set("Retry-After", future.UTC().Format(http.TimeFormat))

		got := transporter.ParseRetryAfter(header)
		assert.Assert(t, got >= 8*time.Second && got <= 11*time.Second,
			"expected ~10s for future date, got %v", got)
	})
}

// --- NewClient integration ---

func TestNewClientTransportWiring(t *testing.T) {
	t.Parallel()

	// Verify that NewClient produces a functional *http.Client.
	client := transporter.NewClient(transporter.Options{
		MaxRetries: 0,
		UserAgent:  "integration-test",
	})
	assert.Assert(t, client != nil)
	assert.Assert(t, client.Transport != nil)
}

// --- Progress body ---

func TestProgressBodyPassthrough(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("abcdefghij"), 1024)

	pb := transporter.NewTestProgressBody(
		io.NopCloser(bytes.NewReader(data)),
		"http://example.com/file",
		int64(len(data)),
	)

	got, err := io.ReadAll(pb)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, data)
	assert.NilError(t, pb.Close())
}

func TestProgressBodyWrappedByTransport(t *testing.T) {
	t.Parallel()

	data := bytes.Repeat([]byte("x"), 64*1024)

	fake := &fakeRT{handler: func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(bytes.NewReader(data)),
			ContentLength: int64(len(data)),
			Header:        make(http.Header),
		}, nil
	}}
	rt := transporter.NewTestTransport(fake, transporter.Options{MaxRetries: 0})

	resp, err := doGet(t, rt)
	assert.NilError(t, err)

	got, err := io.ReadAll(resp.Body)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, data)
	assert.NilError(t, resp.Body.Close())
}

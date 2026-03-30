# Audit: `network/transporter`

**Date**: 2026-03-29
**Scope**: Retry-backoff HTTP transport client

5 files, ~475 lines of production code, ~930 lines of tests.

## File Inventory

| File | Lines | Purpose |
|---|---|---|
| `doc.go` | 18 | Package comment |
| `client.go` | 45 | `Options` struct, `NewClient` constructor |
| `transport.go` | 412 | `retryTransport`: retry loop, backoff, rate limiting, concurrency, progress logging |
| `export_test.go` | 67 | Test helpers exposing internals |
| `transport_test.go` | 930 | 28 tests covering retry, backoff, concurrency, rate limiting, context cancellation, progress body |

## Architecture

Wraps `net/http.DefaultTransport` in a `retryTransport` that implements
`http.RoundTripper` with:

- **Concurrency limiting**: channel-based semaphore (`sem`)
- **Rate limiting**: single-token channel refilled by a background goroutine
- **Retry with exponential backoff**: configurable retries on 5xx, 429, 401,
  and transport errors
- **Retry-After header**: parsed as delay-seconds or HTTP-date per RFC 9110,
  capped by `MaxBackoff`
- **User-Agent injection**: set on requests missing the header
- **Slow request warnings**: logs requests exceeding 10 seconds
- **Connection reuse**: body drained before retry
- **Download progress logging**: `progressBody` wraps response bodies to log
  download progress at 100 MiB intervals. Small responses produce no output.
  Logs a completion message on `Close()` for large downloads.

Lifecycle: `NewClient(opts)` → use client → `Client.CloseIdleConnections()`
(stops rate limiter goroutine).

### Consumers

| Caller | Repository |
|---|---|
| `data/cc0/discogs/live/api.go` | audio/data |
| `data/cc0/wiki/live/api.go` | audio/data |
| `data/cc0/image/warm.go` | audio/data |
| `data/cc0/source/discogs/internal/download/client.go` | audio/data |
| `data/cc0/source/wikipedia/live/api.go` | audio/data |
| `data/cc0/source/pdmx/internal/download/client.go` | audio/data |
| `data/cc0/source/lrclib/internal/download/client.go` | audio/data |
| `data/cc0/source/wikidata/entities/entities.go` | audio/data |
| `data/cc0/source/musicbrainz/internal/download/client.go` | audio/data |
| `data/cc0/source/wikidata/live/api.go` | audio/data |

All callers import with alias `transport` and call `transport.NewClient(opts)`.

## API Surface

| Symbol | Description |
|---|---|
| `Options` | Config struct: `Parallelism`, `MaxPerSecond`, `MaxRetries`, `InitialBackoff`, `MaxBackoff`, `UserAgent` |
| `NewClient(opts Options)` | Returns `*http.Client` backed by retry transport |

Internal:

| Symbol | Description |
|---|---|
| `retryTransport` | `http.RoundTripper` with retry, backoff, rate limiting |
| `newRetryTransport(opts)` | Constructor |
| `RoundTrip(req)` | Acquires semaphore, injects User-Agent, delegates to `retryLoop` |
| `CloseIdleConnections()` | Stops rate limiter goroutine, forwards to base transport |
| `retryLoop(req)` | Core retry logic with backoff and Retry-After |
| `refill(ctx, maxPerSecond)` | Background goroutine for rate limiter token refill |
| `backoffDuration(attempt)` | Exponential backoff with ±25% jitter, capped by `MaxBackoff` |
| `resetBody(req)` | Rewinds request body via `GetBody()` for retry |
| `retryAfter(header)` | Parses `Retry-After` header (delay-seconds or HTTP-date) |
| `drainBody(body)` | Reads and closes response body for connection reuse |
| `progressBody` | Wraps `io.ReadCloser` to log download progress at 100 MiB intervals |
| `NewTestProgressBody` | Test export: creates a `progressBody` for testing |

## Findings

### I1: `nolint:gosec // G706` annotations — justified

G706 is gosec's taint-analysis rule for log injection. The slog calls here
do log tainted values (`req.URL.String()`, `err`, `resp.StatusCode`), so
G706 fires correctly. However, slog encodes values as typed structured
fields, not via string interpolation, so log injection is not possible.
Suppressions are correct.

## Test Coverage

28 tests in `transport_test.go` using a fake `http.RoundTripper` for
deterministic control. All tests use `t.Parallel()` and pass with `-race`.

| Scenario | Test |
|---|---|
| Successful request (no retry) | `TestSuccessNoRetry` |
| Non-retryable 4xx returned immediately | `TestNonRetryableStatus` |
| Retryable status exhausts retries | `TestRetryableStatusExhaustsRetries` |
| Retryable status eventual success | `TestRetryableStatusEventualSuccess` |
| Transport error retried | `TestTransportErrorRetried` |
| Transport error eventual success | `TestTransportErrorEventualSuccess` |
| MaxRetries=0 → single attempt | `TestMaxRetriesZeroSingleAttempt` |
| Retry-After honored | `TestRetryAfterHonored` |
| Retry-After exceeds MaxBackoff | `TestRetryAfterExceedsMaxBackoff` |
| Context cancelled during backoff | `TestContextCancelledDuringBackoff` |
| Context cancelled during semaphore wait | `TestContextCancelledDuringSemaphoreWait` |
| Context cancelled during rate limit wait | `TestContextCancelledDuringRateLimitWait` |
| Context cancelled during RoundTrip | `TestContextCancelledDuringRoundTrip` |
| Body resent on retry | `TestBodyResentOnRetry` |
| User-Agent injected when absent | `TestUserAgentInjectedWhenAbsent` |
| User-Agent preserved when present | `TestUserAgentPreservedWhenPresent` |
| No User-Agent when empty | `TestNoUserAgentWhenEmpty` |
| Concurrency limiting | `TestConcurrencyLimiting` |
| Rate limiting | `TestRateLimiting` |
| CloseIdleConnections stops rate limiter | `TestCloseIdleConnectionsStopsRateLimiter` |
| Backoff exponential | `TestBackoffExponential` |
| Backoff jitter range | `TestBackoffJitterRange` |
| Backoff capped by MaxBackoff | `TestBackoffCappedByMaxBackoff` |
| Backoff no overflow at high attempts | `TestBackoffNoOverflowAtHighAttempts` |
| retryAfter parsing (seconds, dates, edge cases) | `TestRetryAfterParsing` |
| NewClient wiring | `TestNewClientTransportWiring` |
| Progress body passthrough | `TestProgressBodyPassthrough` |
| Progress body wrapped by transport | `TestProgressBodyWrappedByTransport` |

Gap: `drainBody` and `resetBody` tested indirectly via retry tests.

## Open Counts

| Severity | Count |
|---|---|
| INFORMATIONAL | 1 |

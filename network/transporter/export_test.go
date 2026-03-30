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
	"io"
	"net/http"
	"time"
)

// NewTestTransport creates a retryTransport with a custom base RoundTripper for testing.
func NewTestTransport(base http.RoundTripper, opts Options) http.RoundTripper {
	transport := newRetryTransport(opts)
	transport.base = base

	return transport
}

// CloseTestTransport stops the rate limiter goroutine on a test transport.
func CloseTestTransport(rt http.RoundTripper) {
	if transport, ok := rt.(*retryTransport); ok {
		transport.CloseIdleConnections()
	}
}

// ParseRetryAfter is exported for testing.
func ParseRetryAfter(header http.Header) time.Duration {
	return retryAfter(header)
}

// ComputeBackoff computes a backoff duration for a given attempt, using
// the provided InitialBackoff and MaxBackoff.
func ComputeBackoff(
	initialBackoff, maxBackoff time.Duration,
	attempt int,
) time.Duration {
	transport := &retryTransport{
		initBackoff: initialBackoff,
		maxBackoff:  maxBackoff,
	}

	return transport.backoffDuration(attempt)
}

// NewTestProgressBody creates a progressBody for testing.
func NewTestProgressBody(body io.ReadCloser, url string, contentLength int64) io.ReadCloser {
	return &progressBody{
		ReadCloser: body,
		url:        url,
		start:      time.Now(),
		totalSize:  contentLength,
	}
}

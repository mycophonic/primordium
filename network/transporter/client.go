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
	"net/http"
	"time"
)

// Options configures a pool-backed HTTP client with retry, backoff, concurrency,
// and rate limiting. Zero values disable the corresponding feature.
type Options struct {
	Parallelism    int           // Max concurrent in-flight requests (0 = unlimited).
	MaxPerSecond   int           // Rate limit (0 = unlimited).
	MaxRetries     int           // Retries on 5xx, 429 and transport errors that can change; a TLS alert, an untrusted certificate, a host that does not resolve, a request the base refuses by its posture and a body without GetBody are not retried.
	InitialBackoff time.Duration // Doubles each retry with ±25% jitter.
	MaxBackoff     time.Duration // Cap on Retry-After values; anything beyond is treated as failure.
	UserAgent      string

	// Base carries each attempt. With none given the client builds the
	// baseline itself, network.NewTransport(network.Config{}): https only,
	// TLS 1.3 with a post-quantum hybrid key exchange only. A client that
	// needs less passes a base made from the network.Config that says so.
	Base http.RoundTripper
}

// NewClient returns a standard *http.Client backed by a transport that handles
// concurrency limiting, rate limiting, retry with exponential backoff,
// Retry-After headers, and User-Agent injection.
//
// The client reads no global: its connections are its base's, and a base it
// builds itself is shared with no other client.
//
// If MaxPerSecond > 0, a background goroutine is started for rate limiting.
// Call CloseIdleConnections on the returned client when done to stop it.
func NewClient(opts Options) *http.Client {
	return &http.Client{
		Transport: newRetryTransport(opts),
	}
}

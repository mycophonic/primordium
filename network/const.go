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

import "time"

// https is the one scheme the baseline sends, and a credential travels.
const https = "https"

const (
	// DefaultStallTimeout is how long a read of a response body waits for a
	// byte under a Config that names no StallTimeout.
	DefaultStallTimeout = time.Minute
	// DefaultRedirects is how many redirects NewClient's client follows.
	DefaultRedirects = 10
)

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

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
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Credential is a token and the hosts it is for. It is sent to those hosts,
// over https, and to no other: not to a host a redirect leads to, and not in
// cleartext.
type Credential struct {
	// Scheme is the Authorization scheme, "Bearer" when empty.
	Scheme string
	// Token is the credential itself.
	Token string
	// Hosts is who the token is for: a host name, which stands for its
	// https port, or host:port, an IPv6 address in brackets. The name is
	// the whole name: a credential for example.com is not for
	// www.example.com.
	Hosts []string
}

// Authorize returns a transport that carries each request through base, with
// the first of credentials that is for its host. A request that already has
// an Authorization header keeps it.
func Authorize(base http.RoundTripper, credentials ...Credential) http.RoundTripper {
	return &authorizer{base: base, credentials: credentials}
}

type authorizer struct {
	base        http.RoundTripper
	credentials []Credential
}

func (a *authorizer) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == https && req.Header.Get("Authorization") == "" {
		for _, credential := range a.credentials {
			if !credential.covers(req.URL) {
				continue
			}

			scheme := credential.Scheme
			if scheme == "" {
				scheme = "Bearer"
			}

			// A RoundTripper leaves the caller's request as it is.
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", scheme+" "+credential.Token)

			break
		}
	}

	return a.base.RoundTrip(req) //nolint:wrapcheck // transparent wrapper
}

// CloseIdleConnections forwards to base.
func (a *authorizer) CloseIdleConnections() {
	if closer, ok := a.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

const httpsPort = "443"

func (c Credential) covers(target *url.URL) bool {
	port := target.Port()
	if port == "" {
		port = httpsPort
	}

	for _, host := range c.Hosts {
		name, named, err := net.SplitHostPort(host)
		if err != nil {
			// No port: the host alone, which a URL writes in brackets when
			// it is an IPv6 address and Hostname gives without them.
			name, named = strings.Trim(host, "[]"), httpsPort
		}

		if named == port && strings.EqualFold(name, target.Hostname()) {
			return true
		}
	}

	return false
}

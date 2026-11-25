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

// Package network is a network posture a program or a library builds as a
// value: NewTransport and NewClient make an HTTP transport and client under
// it, with no global read or written.
//
// The baseline is the strictest setting, and whatever is weaker is asked for
// by name: https only (Config.Cleartext), TLS 1.3 with a post-quantum hybrid
// key exchange only (Classical and Legacy, per transport or per request with
// WithKeyExchange), a token sent to the hosts it names only (Credential), no
// redirect from https to anything else (Redirects), and a response body that
// stops arriving fails its read (Config.StallTimeout).
//
// GetClientConfig is the matching SSH client posture (Curve25519 exchange,
// AEAD ciphers, ed25519 host keys).
package network

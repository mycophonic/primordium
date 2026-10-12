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

// Package network is the process's network posture: SetDefaults hardens
// http.DefaultTransport once at startup (TLS 1.3 with post-quantum hybrid key
// exchange, timeouts, a bounded pool), NewTransport clones it for a client
// that needs its own bearer token or TLS material, and GetClientConfig is the
// matching SSH client posture (Curve25519 exchange, AEAD ciphers, ed25519
// host keys).
package network

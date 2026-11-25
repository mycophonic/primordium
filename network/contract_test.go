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

package network_test

// The contract, from network's docs:
//   - the zero Config is the strictest posture, and each weaker one is asked
//     for by name: a request that is not https fails with ErrCleartext unless
//     Cleartext; a server with no post-quantum hybrid key exchange, or no TLS
//     1.3, fails the handshake with ErrHandshakeRefused unless the transport,
//     or the request with WithKeyExchange, names Classical or Legacy; a level
//     there is none of is ErrKeyExchange;
//   - Offline fails every request with ErrOffline, and nothing is dialed;
//   - a read of a response body that waits longer than StallTimeout for a
//     byte fails with ErrStalled, over HTTP/1 and HTTP/2; the time between
//     two reads is not counted; a Close from another goroutine ends a read;
//   - each level has connections of its own: one made at a weaker level
//     carries no request at a stricter one;
//   - a Credential is sent to the hosts it names, over https, and to no
//     other, a redirect's target included: the whole name, and the one port
//     it names, https's own when it names none; a request's own
//     Authorization header is kept;
//   - Redirects follows at most its limit, and none from https to anything
//     else: both are ErrRedirect;
//   - a transport reads and writes no global; SetDefaults alone installs one.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mycophonic/primordium/network"
)

// secure is an https server whose TLS configure may narrow, and the roots
// that trust it.
func secure(t *testing.T, handler http.HandlerFunc, configure func(*tls.Config)) (*httptest.Server, *x509.CertPool) {
	t.Helper()

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12}

	if configure != nil {
		configure(server.TLS)
	}

	server.StartTLS()
	t.Cleanup(server.Close)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())

	return server, roots
}

func ok(http.ResponseWriter, *http.Request) {}

// get makes one request and returns its status, or the error.
func get(ctx context.Context, t *testing.T, client *http.Client, address string) (int, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	if err = resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	return resp.StatusCode, nil
}

func TestCleartextIsAskedFor(t *testing.T) {
	t.Parallel()

	var reached atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	t.Cleanup(server.Close)

	if _, err := get(
		t.Context(),
		t,
		network.NewClient(network.Config{}),
		server.URL,
	); !errors.Is(
		err,
		network.ErrCleartext,
	) {
		t.Fatalf("the baseline over http = %v, want ErrCleartext", err)
	}

	if reached.Load() != 0 {
		t.Fatal("the baseline sent a request in cleartext")
	}

	status, err := get(t.Context(), t, network.NewClient(network.Config{Cleartext: true}), server.URL)
	if err != nil || status != http.StatusOK {
		t.Fatalf("Cleartext over http = %d, %v; want 200", status, err)
	}
}

func TestOffline(t *testing.T) {
	t.Parallel()

	var reached atomic.Int32

	server, roots := secure(t, func(http.ResponseWriter, *http.Request) { reached.Add(1) }, nil)

	client := network.NewClient(network.Config{Roots: roots, Offline: true})
	if _, err := get(t.Context(), t, client, server.URL); !errors.Is(err, network.ErrOffline) {
		t.Fatalf("an offline request = %v, want ErrOffline", err)
	}

	if reached.Load() != 0 {
		t.Fatal("an offline transport reached the server")
	}
}

// TestKeyExchangeLevels: each server is reached by the levels that accept
// what it offers, named by the transport or by the request, and refused by
// the stricter ones.
func TestKeyExchangeLevels(t *testing.T) {
	t.Parallel()

	servers := map[string]struct {
		configure func(*tls.Config)
		reached   []network.KeyExchange
	}{
		"hybrid": {
			nil,
			[]network.KeyExchange{network.HybridOnly, network.Classical, network.Legacy},
		},
		"TLS 1.3, classical only": {
			func(config *tls.Config) { config.CurvePreferences = []tls.CurveID{tls.X25519} },
			[]network.KeyExchange{network.Classical, network.Legacy},
		},
		"TLS 1.2": {
			func(config *tls.Config) { config.MaxVersion = tls.VersionTLS12 },
			[]network.KeyExchange{network.Legacy},
		},
	}

	for name, offer := range servers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server, roots := secure(t, ok, offer.configure)

			for _, level := range []network.KeyExchange{network.HybridOnly, network.Classical, network.Legacy} {
				byTransport := network.NewClient(network.Config{Roots: roots, KeyExchange: level})
				byRequest := network.NewClient(network.Config{Roots: roots})

				for how, attempt := range map[string]func() (int, error){
					"named by the transport": func() (int, error) {
						return get(t.Context(), t, byTransport, server.URL)
					},
					"named by the request": func() (int, error) {
						return get(network.WithKeyExchange(t.Context(), level), t, byRequest, server.URL)
					},
				} {
					status, err := attempt()

					switch {
					case slices.Contains(offer.reached, level) && (err != nil || status != http.StatusOK):
						t.Errorf("level %d, %s = %d, %v; want 200", level, how, status, err)
					case !slices.Contains(offer.reached, level) && !errors.Is(err, network.ErrHandshakeRefused):
						t.Errorf("level %d, %s = %d, %v; want ErrHandshakeRefused", level, how, status, err)
					}
				}
			}
		})
	}
}

func TestUnknownLevel(t *testing.T) {
	t.Parallel()

	server, roots := secure(t, ok, nil)

	for _, level := range []network.KeyExchange{-1, network.Legacy + 1} {
		client := network.NewClient(network.Config{Roots: roots})

		_, err := get(network.WithKeyExchange(t.Context(), level), t, client, server.URL)
		if !errors.Is(err, network.ErrKeyExchange) {
			t.Errorf("level %d = %v, want ErrKeyExchange", level, err)
		}
	}
}

// stalling serves "/stops", a body that starts and never ends, and "/paused",
// one whose second half is sent when resume is.
func stalling(resume, release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first"))

		w.(http.Flusher).Flush()

		if r.URL.Path == "/paused" {
			select {
			case <-resume:
				_, _ = w.Write([]byte("second"))
			case <-r.Context().Done():
			}

			return
		}

		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
}

// TestStalledBody: a body that stops arriving fails the read that waits for
// it, over HTTP/1 and HTTP/2; the time a caller takes between two reads is
// its own, and a body that goes on after it is read whole.
func TestStalledBody(t *testing.T) {
	t.Parallel()

	const stall = 200 * time.Millisecond

	for name, major := range map[string]int{"HTTP/1": 1, "HTTP/2": 2} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resume, release := make(chan struct{}, 1), make(chan struct{})

			server := httptest.NewUnstartedServer(stalling(resume, release))
			config := network.Config{StallTimeout: stall}

			if major == 1 {
				server.Start()

				config.Cleartext = true
			} else {
				server.EnableHTTP2 = true
				server.StartTLS()

				config.Roots = x509.NewCertPool()
				config.Roots.AddCert(server.Certificate())
			}

			t.Cleanup(server.Close)
			t.Cleanup(func() { close(release) })

			client := network.NewClient(config)

			open := func(path string) *http.Response {
				t.Helper()

				req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, http.NoBody)

				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}

				if resp.ProtoMajor != major {
					t.Fatalf("the response came over HTTP/%d, want HTTP/%d", resp.ProtoMajor, major)
				}

				return resp
			}

			stopped := open("/stops")

			content, err := io.ReadAll(stopped.Body)
			_ = stopped.Body.Close()

			if !errors.Is(err, network.ErrStalled) {
				t.Errorf("a body that stops: read %q, %v; want ErrStalled", content, err)
			}

			paused := open("/paused")

			first := make([]byte, len("first"))
			if _, err = io.ReadFull(paused.Body, first); err != nil {
				t.Fatal(err)
			}

			// Longer than the limit, and nobody is reading: not a stall.
			time.Sleep(3 * stall)

			resume <- struct{}{}

			rest, err := io.ReadAll(paused.Body)
			_ = paused.Body.Close()

			if err != nil || string(rest) != "second" {
				t.Errorf("after a pause between two reads: read %q, %v; want the rest of the body", rest, err)
			}
		})
	}
}

// TestCloseWhileReading: a Close from another goroutine ends a read that
// waits, which is how a caller gives up on one. The race detector is what
// judges it.
func TestCloseWhileReading(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	server := httptest.NewServer(stalling(nil, release))

	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	client := network.NewClient(network.Config{Cleartext: true})

	for range 20 {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/stops", http.NoBody)

		resp, err := client.Do(req) //nolint:bodyclose // closed below, under the goroutine that reads it
		if err != nil {
			t.Fatal(err)
		}

		done := make(chan error, 1)

		go func() {
			_, readErr := io.ReadAll(resp.Body)
			done <- readErr
		}()

		// Not a wait for the read to block: closing before, during or after
		// its first call are all the caller's right.
		time.Sleep(time.Millisecond)

		_ = resp.Body.Close()

		select {
		case readErr := <-done:
			if readErr == nil {
				t.Fatal("a read of a body that never ends, closed under it, ended without an error")
			}
		case <-time.After(30 * time.Second):
			t.Fatal("Close did not end the read")
		}
	}
}

// TestALevelHasItsOwnConnections: a connection made for a request that named
// a weaker level carries no request at the transport's own level.
func TestALevelHasItsOwnConnections(t *testing.T) {
	t.Parallel()

	server, roots := secure(t, ok, func(config *tls.Config) { config.MaxVersion = tls.VersionTLS12 })
	client := network.NewClient(network.Config{Roots: roots})

	status, err := get(network.WithKeyExchange(t.Context(), network.Legacy), t, client, server.URL)
	if err != nil || status != http.StatusOK {
		t.Fatalf("the request that named Legacy = %d, %v; want 200", status, err)
	}

	if _, err = get(t.Context(), t, client, server.URL); !errors.Is(err, network.ErrHandshakeRefused) {
		t.Fatalf("the next request, at the transport's level = %v, want ErrHandshakeRefused", err)
	}
}

// answering is a transport that answers every request itself.
type answering func(*http.Request)

func (f answering) RoundTrip(req *http.Request) (*http.Response, error) {
	f(req)

	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

// TestCredentialHostMatching: a credential is for the whole host name and
// the one port it names, and for nothing that looks like them.
func TestCredentialHostMatching(t *testing.T) {
	t.Parallel()

	var sent string

	transport := network.Authorize(
		answering(func(req *http.Request) { sent = req.Header.Get("Authorization") }),
		network.Credential{Token: "token", Hosts: []string{"registry.test", "ported.test:8443", "[::1]:8443", "[::2]"}},
	)

	for address, carried := range map[string]bool{
		"https://registry.test/":           true,
		"https://REGISTRY.TEST/":           true,
		"https://registry.test:443/":       true,
		"https://registry.test:8443/":      false,
		"https://notregistry.test/":        false,
		"https://sub.registry.test/":       false,
		"https://registry.test.evil.test/": false,
		"http://registry.test/":            false,
		"https://ported.test:8443/":        true,
		"https://ported.test/":             false,
		"https://[::1]:8443/":              true,
		"https://[::1]/":                   false,
		"https://[::2]/":                   true,
		"https://[::2]:8443/":              false,
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}

		sent = ""

		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}

		_ = resp.Body.Close()

		if want := map[bool]string{true: "Bearer token", false: ""}[carried]; sent != want {
			t.Errorf("%s: Authorization %q, want %q", address, sent, want)
		}
	}
}

// TestCredentialGoesToItsHostsOnly: the token reaches the host it names,
// and not the host a redirect leads to, a host it does not name, or a server
// in cleartext.
func TestCredentialGoesToItsHostsOnly(t *testing.T) {
	t.Parallel()

	const token = "Bearer for-the-first-host"

	var elsewhere, named, own atomic.Pointer[string]

	record := func(into *atomic.Pointer[string]) http.HandlerFunc {
		return func(_ http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Authorization")
			into.Store(&got)
		}
	}

	other, roots := secure(t, record(&elsewhere), nil)

	first, _ := secure(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/asset", http.StatusFound)
		case "/own":
			record(&own)(w, r)
		default:
			record(&named)(w, r)
		}
	}, nil)

	plain := httptest.NewServer(record(&elsewhere))
	t.Cleanup(plain.Close)

	hostOf := func(address string) string {
		parsed, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}

		return parsed.Host
	}

	credential := network.Credential{Token: "for-the-first-host", Hosts: []string{hostOf(first.URL), hostOf(plain.URL)}}
	client := &http.Client{
		Transport: network.Authorize(network.NewTransport(network.Config{Roots: roots, Cleartext: true}), credential),
	}

	expect := func(what string, got *atomic.Pointer[string], want string) {
		t.Helper()

		switch value := got.Swap(nil); {
		case value == nil:
			t.Errorf("%s: the server was not reached", what)
		case *value != want:
			t.Errorf("%s: Authorization %q, want %q", what, *value, want)
		}
	}

	for _, step := range []struct {
		what, address string
		got           *atomic.Pointer[string]
		want          string
	}{
		{"the named host", first.URL + "/", &named, token},
		{"a redirect to another host", first.URL + "/away", &elsewhere, ""},
		{"a host it does not name", other.URL, &elsewhere, ""},
		{"a named host in cleartext", plain.URL, &elsewhere, ""},
	} {
		if _, err := get(t.Context(), t, client, step.address); err != nil {
			t.Fatalf("%s: %v", step.what, err)
		}

		expect(step.what, step.got, step.want)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, first.URL+"/own", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Authorization", "Basic the-caller's")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = resp.Body.Close()

	expect("a request with its own header", &own, "Basic the-caller's")
}

func TestRedirects(t *testing.T) {
	t.Parallel()

	plain := httptest.NewServer(http.HandlerFunc(ok))
	t.Cleanup(plain.Close)

	server, roots := secure(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/down":
			http.Redirect(w, r, plain.URL, http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/hop":
			left, _ := strconv.Atoi(r.URL.Query().Get("left"))
			if left > 0 {
				http.Redirect(w, r, "/hop?left="+strconv.Itoa(left-1), http.StatusFound)
			}
		case "/once":
			http.Redirect(w, r, "/", http.StatusFound)
		}
	}, nil)

	client := network.NewClient(network.Config{Roots: roots, Cleartext: true})

	if status, err := get(t.Context(), t, client, server.URL+"/once"); err != nil || status != http.StatusOK {
		t.Errorf("one redirect within https = %d, %v; want 200", status, err)
	}

	for _, path := range []string{"/down", "/loop"} {
		if _, err := get(t.Context(), t, client, server.URL+path); !errors.Is(err, network.ErrRedirect) {
			t.Errorf("%s = %v, want ErrRedirect", path, err)
		}
	}

	atLimit := server.URL + "/hop?left=" + strconv.Itoa(network.DefaultRedirects)
	if status, err := get(t.Context(), t, client, atLimit); err != nil || status != http.StatusOK {
		t.Errorf("%d redirects = %d, %v; want 200", network.DefaultRedirects, status, err)
	}

	pastLimit := server.URL + "/hop?left=" + strconv.Itoa(network.DefaultRedirects+1)
	if _, err := get(t.Context(), t, client, pastLimit); !errors.Is(err, network.ErrRedirect) {
		t.Errorf("%d redirects = %v, want ErrRedirect", network.DefaultRedirects+1, err)
	}
}

// TestSetDefaults: nothing but SetDefaults touches http.DefaultTransport,
// and it may run twice. The default is the process's, which is why this does
// not run in parallel.
func TestSetDefaults(t *testing.T) { //nolint:paralleltest // http.DefaultTransport is process-wide
	previous := http.DefaultTransport

	t.Cleanup(func() { http.DefaultTransport = previous }) //nolint:reassign // restores what the test replaces

	server, roots := secure(t, ok, nil)

	if _, err := get(t.Context(), t, network.NewClient(network.Config{Roots: roots}), server.URL); err != nil {
		t.Fatal(err)
	}

	if http.DefaultTransport != previous {
		t.Fatal("a transport of its own replaced http.DefaultTransport")
	}

	network.SetDefaults(network.Config{})
	network.SetDefaults(network.Config{Roots: roots})

	if _, isBaseline := http.DefaultTransport.(*network.Transport); !isBaseline {
		t.Fatalf("after SetDefaults http.DefaultTransport is a %T", http.DefaultTransport)
	}

	if status, err := get(t.Context(), t, http.DefaultClient, server.URL); err != nil || status != http.StatusOK {
		t.Fatalf("http.DefaultClient after SetDefaults = %d, %v; want 200", status, err)
	}
}

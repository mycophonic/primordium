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
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	// ErrOffline is the error of every request through an offline transport.
	ErrOffline = errors.New("offline")
	// ErrCleartext is the error of a request that is not https, through a
	// transport that was not told to send one.
	ErrCleartext = errors.New("not https")
	// ErrKeyExchange is the error of a request that names a key exchange
	// level there is none of.
	ErrKeyExchange = errors.New("unknown key exchange level")
	// ErrHandshakeRefused is the error of a TLS handshake the server ended
	// with an alert: most often a server that offers no key exchange, or no
	// TLS version, the request's level accepts.
	ErrHandshakeRefused = errors.New("the server refused the TLS handshake")
	// ErrStalled is the error of a response body that stopped arriving.
	ErrStalled = errors.New("the response body stopped arriving")
	// ErrRedirect is the error of a redirect the client does not follow.
	ErrRedirect = errors.New("redirect refused")
)

// KeyExchange is what a connection may agree its keys with, and under which
// TLS versions. The zero value is the strictest, and each one after it gives
// something up: a request gets a weaker one only by naming it.
type KeyExchange int

const (
	// HybridOnly is TLS 1.3 with a post-quantum hybrid key exchange, and
	// nothing else.
	HybridOnly KeyExchange = iota
	// Classical is TLS 1.3 with any key exchange Go offers, the classical
	// ones included, for a server that has no hybrid.
	Classical
	// Legacy is TLS as Go defaults it, 1.2 included, for a server that has
	// no TLS 1.3.
	Legacy

	levels = iota
)

// Config is what varies between transports. The zero value is the baseline:
// https only, HybridOnly, the system's roots, the environment's proxy.
type Config struct {
	// KeyExchange is the level of a request that names none with
	// [WithKeyExchange].
	KeyExchange KeyExchange
	// Roots is the certificate authorities trusted; the system's when nil.
	Roots *x509.CertPool
	// Certificates is what the client presents to a server that asks.
	Certificates []tls.Certificate
	// Proxy picks a request's proxy; http.ProxyFromEnvironment when nil.
	Proxy func(*http.Request) (*url.URL, error)
	// StallTimeout is how long a read of a response body may wait for a
	// byte before it fails with [ErrStalled]. Zero is DefaultStallTimeout;
	// a negative value waits for ever.
	StallTimeout time.Duration
	// Cleartext sends a request that is not https. Without it, such a
	// request fails with [ErrCleartext].
	Cleartext bool
	// Offline fails every request with [ErrOffline], before anything is
	// dialed.
	Offline bool
}

// Transport is an http.RoundTripper under the baseline: its timeouts, a
// bounded pool, and TLS at the level of each request. It reads and writes no
// global; two of them share nothing.
type Transport struct {
	config Config

	mu sync.Mutex
	// built holds the transport of each level, made at its first request.
	built [levels]*http.Transport
}

// NewTransport returns a transport under config.
func NewTransport(config Config) *Transport {
	return &Transport{config: config}
}

type exchangeKey struct{}

// WithKeyExchange names the key exchange level of the requests made with the
// returned context, and of the redirects they lead to, whatever the level of
// the transport that carries them.
func WithKeyExchange(ctx context.Context, level KeyExchange) context.Context {
	return context.WithValue(ctx, exchangeKey{}, level)
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.config.Offline {
		return nil, fmt.Errorf("%w: %s was asked", ErrOffline, req.URL.Host)
	}

	if req.URL.Scheme != https && !t.config.Cleartext {
		return nil, fmt.Errorf("%w: %s://%s", ErrCleartext, req.URL.Scheme, req.URL.Host)
	}

	level := t.config.KeyExchange
	if named, ok := req.Context().Value(exchangeKey{}).(KeyExchange); ok {
		level = named
	}

	base, err := t.at(level)
	if err != nil {
		return nil, err
	}

	stall := t.config.StallTimeout
	if stall == 0 {
		stall = DefaultStallTimeout
	}

	if stall < 0 {
		resp, tripErr := base.RoundTrip(req)

		return resp, refused(tripErr)
	}

	// The body's reads are cut short by cancelling the request, which is the
	// one thing that unblocks a read on a connection gone quiet.
	ctx, cancel := context.WithCancelCause(req.Context())

	resp, err := base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel(nil)

		return nil, refused(err)
	}

	// A protocol switch hands the connection over as the body: it is the
	// caller's from here, and a quiet connection is not a stalled one.
	if stream, ok := resp.Body.(io.ReadWriteCloser); ok && resp.StatusCode == http.StatusSwitchingProtocols {
		resp.Body = &handedOver{ReadWriteCloser: stream, cancel: cancel}

		return resp, nil
	}

	resp.Body = watch(ctx, cancel, resp.Body, stall)

	return resp, nil
}

// CloseIdleConnections closes the idle connections of every level.
func (t *Transport) CloseIdleConnections() {
	t.mu.Lock()
	built := t.built
	t.mu.Unlock()

	for _, transport := range built {
		if transport != nil {
			transport.CloseIdleConnections()
		}
	}
}

// at is the transport of a level, built when the level is first used.
func (t *Transport) at(level KeyExchange) (*http.Transport, error) {
	if level < HybridOnly || level >= levels {
		return nil, fmt.Errorf("%w: %d", ErrKeyExchange, level)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.built[level] == nil {
		t.built[level] = t.build(level)
	}

	return t.built[level], nil
}

func (t *Transport) build(level KeyExchange) *http.Transport {
	proxy := t.config.Proxy
	if proxy == nil {
		proxy = http.ProxyFromEnvironment
	}

	return &http.Transport{
		Proxy:                 proxy,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       t.tls(level),
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       idleConnTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       maxConnsPerHost,
	}
}

func (t *Transport) tls(level KeyExchange) *tls.Config {
	config := &tls.Config{ // #nosec G402 -- Legacy is Go's own floor, and a request gets it only by name
		RootCAs:      t.config.Roots,
		Certificates: t.config.Certificates,
	}

	switch level {
	case HybridOnly:
		config.MinVersion = tls.VersionTLS13
		config.CurvePreferences = []tls.CurveID{tls.X25519MLKEM768, tls.SecP256r1MLKEM768, tls.SecP384r1MLKEM1024}
	case Classical:
		config.MinVersion = tls.VersionTLS13
	case Legacy:
	}

	return config
}

// refused marks a TLS alert from the server, which reaches here as a
// net.OpError of this operation: the alert type behind it is not exported.
func refused(err error) error {
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "remote error" {
		return errors.Join(ErrHandshakeRefused, err)
	}

	return err
}

// watchedBody fails a read that waits longer than limit for a byte. The time
// a caller takes between two reads is its own, and is not counted.
type watchedBody struct {
	io.ReadCloser

	limit  time.Duration
	timer  *time.Timer
	ctx    context.Context //nolint:containedctx // the request's, kept to read why a read failed
	cancel context.CancelCauseFunc
}

// watch wraps body. The timer is made here, stopped, and the field never
// written again: a Close from another goroutine is how a caller gives up on
// a read that blocks, and it reads the field while Read arms the timer.
func watch(ctx context.Context, cancel context.CancelCauseFunc, body io.ReadCloser, limit time.Duration) *watchedBody {
	timer := time.AfterFunc(time.Duration(math.MaxInt64), func() { cancel(ErrStalled) })
	timer.Stop()

	return &watchedBody{ReadCloser: body, limit: limit, timer: timer, ctx: ctx, cancel: cancel}
}

func (b *watchedBody) Read(buf []byte) (int, error) {
	b.timer.Reset(b.limit)

	read, err := b.ReadCloser.Read(buf)

	b.timer.Stop()

	if err != nil && errors.Is(context.Cause(b.ctx), ErrStalled) {
		err = errors.Join(ErrStalled, err)
	}

	return read, err
}

func (b *watchedBody) Close() error {
	b.timer.Stop()

	err := b.ReadCloser.Close()

	b.cancel(nil)

	return err //nolint:wrapcheck // transparent wrapper
}

// handedOver is the connection a protocol switch gave the caller, which
// releases the request's context when closed.
type handedOver struct {
	io.ReadWriteCloser

	cancel context.CancelCauseFunc
}

func (h *handedOver) Close() error {
	err := h.ReadWriteCloser.Close()

	h.cancel(nil)

	return err //nolint:wrapcheck // transparent wrapper
}

// Redirects is an http.Client.CheckRedirect that follows at most limit
// redirects, and none from https to anything else. A redirect it lets
// through is still the transport's to carry: one that leaves https fails
// there with [ErrCleartext] unless the transport sends cleartext.
func Redirects(limit int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) > limit {
			return fmt.Errorf("%w: more than %d", ErrRedirect, limit)
		}

		if req.URL.Scheme != https && via[len(via)-1].URL.Scheme == https {
			return fmt.Errorf("%w: from https to %s://%s", ErrRedirect, req.URL.Scheme, req.URL.Host)
		}

		return nil
	}
}

// NewClient returns an http.Client on a transport under config, following
// redirects as [Redirects] does, DefaultRedirects of them.
func NewClient(config Config) *http.Client {
	return &http.Client{
		Transport:     NewTransport(config),
		CheckRedirect: Redirects(DefaultRedirects),
	}
}

// SetDefaults makes a transport under config the process's
// http.DefaultTransport, for a main whose libraries use http.DefaultClient.
// Nothing in this module needs it.
func SetDefaults(config Config) {
	http.DefaultTransport = NewTransport(config) //nolint:reassign // its contract: it installs the process-wide default
}

// RetryStatusCodes is the HTTP statuses worth a retry, for a library that
// takes such a list, as go-containerregistry's remote.WithRetryStatusCodes
// does.
//
//nolint:gochecknoglobals // a list a caller passes on, which a constant cannot be
var RetryStatusCodes = []int{
	http.StatusTooManyRequests,
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

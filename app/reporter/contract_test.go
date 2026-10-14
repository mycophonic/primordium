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

package reporter_test

// The contract, from reporter's docs:
//   - Initialize configures Sentry from the DSN, or fails with
//     ErrSystemFailure, and routes the default logger: records at ERROR and
//     above are events, an error-typed attribute becoming the exception and
//     the message otherwise; lower records are breadcrumbs on the next event;
//     attributes travel under the "log" context, groups as dotted prefixes;
//     records the console does not show reach Sentry neither;
//   - CaptureMessage, CaptureException and CaptureEvent send one event each;
//   - Shutdown delivers what is buffered.
//
// Sentry is a local server here; the envelopes it receives are read back.
// The reporter and the default logger are process-wide, so the tests run in
// order, each restoring the logger it found.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log" //nolint:depguard // the test drives the log package on purpose: Go routes it into slog's default
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/app/reporter"
	"github.com/mycophonic/primordium/fault"
)

const (
	childEnv = "PRIMORDIUM_REPORTER_CHILD"
	dsnEnv   = "PRIMORDIUM_REPORTER_DSN"
)

var (
	errKaboom = errors.New("kaboom")
	errDirect = errors.New("direct error")
)

// TestMain: as a child, the logger first, then the reporter at the DSN given,
// one record of each kind, and the flush.
func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		m.Run()
	case "wrapped":
		logger.SetDefaultsForLogger(context.Background(), slog.LevelInfo)

		if err := reporter.Initialize(&reporter.Config{DSN: os.Getenv(dsnEnv)}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}

		slog.Info("kept on the console", "n", 1)
		slog.Error("and reported")
		reporter.Shutdown()
	default:
		os.Exit(99)
	}
}

// collector is a server standing in for Sentry, keeping every event it is sent.
type collector struct {
	mu     sync.Mutex
	events []map[string]any
	server *httptest.Server
}

func newCollector(t *testing.T) *collector {
	t.Helper()

	s := &collector{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		// An envelope: a header line, then item header and payload line pairs.
		lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
		for i := 1; i+1 < len(lines); i += 2 {
			var header struct {
				Type string `json:"type"`
			}

			if json.Unmarshal([]byte(lines[i]), &header) == nil && header.Type == "event" {
				var event map[string]any
				if json.Unmarshal([]byte(lines[i+1]), &event) == nil {
					s.mu.Lock()
					s.events = append(s.events, event)
					s.mu.Unlock()
				}
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.server.Close)

	return s
}

// dsn is the project 42 on the local server.
func (s *collector) dsn() string {
	return "http://key@" + strings.TrimPrefix(s.server.URL, "http://") + "/42"
}

func (s *collector) received() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]map[string]any(nil), s.events...)
}

// start points the reporter at the local server and arranges for the default
// logger to be the one found afterwards.
func start(t *testing.T) *collector {
	t.Helper()

	previous := slog.Default()

	t.Cleanup(func() { slog.SetDefault(previous) })

	s := newCollector(t)

	if err := reporter.Initialize(&reporter.Config{DSN: s.dsn(), TracesSampleRate: 1}); err != nil {
		t.Fatal(err)
	}

	// The hub's scope outlives Init: breadcrumbs from an earlier repetition
	// would ride on this one's first event.
	sentry.ConfigureScope(func(scope *sentry.Scope) { scope.ClearBreadcrumbs() })

	return s
}

func field(event map[string]any, path ...string) any {
	var current any = event

	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		current = m[key]
	}

	return current
}

// exceptionValue is the first exception's value, whichever shape the
// payload gives the list.
func exceptionValue(event map[string]any) string {
	list := event["exception"]
	if m, ok := list.(map[string]any); ok {
		list = m["values"]
	}

	items, ok := list.([]any)
	if !ok || len(items) == 0 {
		return ""
	}

	return fmt.Sprint(field(items[0].(map[string]any), "value"))
}

func breadcrumbMessages(event map[string]any) []string {
	list := event["breadcrumbs"]
	if m, ok := list.(map[string]any); ok {
		list = m["values"]
	}

	items, ok := list.([]any)
	if !ok {
		return nil
	}

	var messages []string

	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			messages = append(messages, fmt.Sprint(m["message"]))
		}
	}

	return messages
}

//nolint:paralleltest // process-wide reporter and default logger
func TestBadDSN(t *testing.T) {
	err := reporter.Initialize(&reporter.Config{DSN: "not a dsn"})
	if !errors.Is(err, fault.ErrSystemFailure) {
		t.Fatalf("bad DSN: %v", err)
	}
}

//nolint:paralleltest // process-wide reporter and default logger
func TestErrorWithException(t *testing.T) {
	s := start(t)

	slog.Debug("not shown, not a crumb")
	slog.Info("crumb", "step", 1)
	slog.Default().WithGroup("req").Error("request failed", "id", 7, "err", errKaboom)
	reporter.Shutdown()

	events := s.received()
	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}

	event := events[0]

	if got := exceptionValue(event); got != "kaboom" {
		t.Errorf("exception %q, want the err attribute", got)
	}

	if got := field(event, "level"); got != "error" {
		t.Errorf("level %v", got)
	}

	if got := field(event, "contexts", "log", "req.id"); fmt.Sprint(got) != "7" {
		t.Errorf("contexts.log.req.id = %v, want 7", got)
	}

	// The debug record is absent, the info one present.
	crumbs := breadcrumbMessages(event)
	if len(crumbs) != 1 || crumbs[0] != "crumb" {
		t.Errorf("breadcrumbs %q, want the one info record", crumbs)
	}
}

//nolint:paralleltest // process-wide reporter and default logger
func TestErrorWithoutException(t *testing.T) {
	s := start(t)

	slog.Error("plain failure", "code", 3)
	reporter.Shutdown()

	events := s.received()
	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}

	if got := field(events[0], "message"); got != "plain failure" {
		t.Errorf("message %v", got)
	}

	if got := exceptionValue(events[0]); got != "" {
		t.Errorf("exception %q from a record without an error", got)
	}
}

//nolint:paralleltest // process-wide reporter and default logger
func TestCaptures(t *testing.T) {
	s := start(t)

	if reporter.CaptureMessage("direct") == nil {
		t.Error("CaptureMessage returned no id")
	}

	if reporter.CaptureException(errDirect) == nil {
		t.Error("CaptureException returned no id")
	}

	if reporter.CaptureEvent(&reporter.Event{Message: "direct event"}) == nil {
		t.Error("CaptureEvent returned no id")
	}

	reporter.Shutdown()

	var got []string

	for _, event := range s.received() {
		if message, ok := event["message"].(string); ok && message != "" {
			got = append(got, message)
		} else {
			got = append(got, "exception: "+exceptionValue(event))
		}
	}

	if want := "[direct exception: direct error direct event]"; fmt.Sprint(got) != want {
		t.Errorf("events %s, want %s", got, want)
	}
}

// TestStandardLogKeepsWorking: with no logger installed before Initialize,
// the log package must still write and return; Go routes it into the default
// slog handler, and a wrapper around the built-in one would recurse.
//
//nolint:paralleltest // process-wide reporter and default logger
func TestStandardLogKeepsWorking(t *testing.T) {
	s := start(t)

	done := make(chan struct{})

	go func() {
		log.Print("through the log package")
		slog.Error("after it")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("log.Print did not return: the default handler recursed into itself")
	}

	reporter.Shutdown()

	if events := s.received(); len(events) != 1 || field(events[0], "message") != "after it" {
		t.Fatalf("events after log.Print: %v", events)
	}
}

// TestWrapsTheInstalledLogger: the console handler installed before
// Initialize keeps writing every record, JSON off a terminal; a child, so
// its stderr can be read.
func TestWrapsTheInstalledLogger(t *testing.T) {
	t.Parallel()

	s := newCollector(t)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), exe)

	cmd.Env = append(os.Environ(), childEnv+"=wrapped", dsnEnv+"="+s.dsn())

	var stderr strings.Builder

	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}

	for _, want := range []string{`"msg":"kept on the console"`, `"n":1`, `"msg":"and reported"`} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("console lost %s:\n%s", want, stderr.String())
		}
	}

	if events := s.received(); len(events) != 1 || field(events[0], "message") != "and reported" {
		t.Errorf("events: %v", events)
	}
}

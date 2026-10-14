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

package reporter

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"

	"github.com/getsentry/sentry-go"

	"github.com/mycophonic/primordium/app/logger"
	"github.com/mycophonic/primordium/fault"
)

// Config structure for minimum set of reporter parameters.
type Config struct {
	DSN              string
	Debug            bool
	PII              bool
	Release          string
	Environment      string
	TracesSampleRate float64 // 0 defaults to 1.0 (100%)
}

type (
	// EventID is a hexadecimal string representing a unique uuid4 for an Event.
	// An EventID must be 32 characters long, lowercase and not have any dashes.
	EventID = sentry.EventID
	// Event is the fundamental data structure that is sent to our reporter.
	Event = sentry.Event
)

// Initialize configures Sentry and routes the default logger into it: records
// at ERROR and above become events, lower ones breadcrumbs on the next event,
// while the console keeps every record. Once per process: a second call wraps
// the logger again and every error is reported twice.
func Initialize(conf *Config) error {
	sampleRate := conf.TracesSampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:   conf.DSN,
		Debug: conf.Debug,
		// SendDefaultPII maps conf.PII one-to-one. Its successor, DataCollection,
		// is granular (user info, cookies, headers, bodies) — choosing what PII
		// means for this reporter is a design decision, not a linter fix.
		SendDefaultPII:   conf.PII, //nolint:staticcheck // SA1019: see above
		AttachStacktrace: true,
		EnableTracing:    true,
		Environment:      conf.Environment,
		Release:          conf.Release,
		TracesSampleRate: sampleRate,
		// The default client, so Sentry traffic goes through the transport
		// network.SetDefaults installs.
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	// After Init: the handler captures through the client Init creates. The
	// handler it wraps must not be slog's built-in one: SetDefault routes the
	// log package into the new default, and the built-in handler writes back
	// through the log package, under the same lock, so a log.Print would
	// deadlock on itself. With nothing installed yet, logger's defaults go first.
	if reflect.TypeOf(slog.Default().Handler()).String() == "*slog.defaultHandler" {
		logger.SetDefaultsForLogger(context.Background())
	}

	slog.SetDefault(slog.New(newSentryHandler(slog.Default().Handler())))

	slog.Info("sentry reporter configured")

	return nil
}

// CaptureException captures an error.
func CaptureException(err error) *EventID {
	return sentry.CaptureException(err)
}

// CaptureMessage captures a message.
func CaptureMessage(msg string) *EventID {
	return sentry.CaptureMessage(msg)
}

// CaptureEvent captures a structured event.
func CaptureEvent(e *Event) *EventID {
	return sentry.CaptureEvent(e)
}

// Shutdown flushes buffered events before the program terminates.
func Shutdown() {
	if !sentry.Flush(flushTimeout) {
		slog.Warn("sentry flush incomplete: some events may not have been delivered")
	}
}

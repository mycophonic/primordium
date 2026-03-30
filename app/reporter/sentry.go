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
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getsentry/sentry-go"

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

// Initialize and configures underlying Sentry library.
func Initialize(conf *Config) error {
	sampleRate := conf.TracesSampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              conf.DSN,
		Debug:            conf.Debug,
		SendDefaultPII:   conf.PII,
		AttachStacktrace: true,
		EnableTracing:    true,
		Environment:      conf.Environment,
		Release:          conf.Release,
		TracesSampleRate: sampleRate,
		// EnableLogs activates Sentry's structured logging ingestion so that
		// slog output captured via the Sentry handler reaches the dashboard.
		EnableLogs: true,
		// Use the global default client so that Sentry traffic inherits the
		// TLS and transport settings configured by network.SetDefaults.
		HTTPClient: http.DefaultClient,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", fault.ErrSystemFailure, err)
	}

	slog.Info("Reporter Sentry configured")

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

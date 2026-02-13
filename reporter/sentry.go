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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

const flushTimeout = 2 * time.Second

// ErrReporterInitializationFail indicates an error with the report initialization parameters.
var ErrReporterInitializationFail = errors.New("reporter init error")

// Config structure for minimum set of reporter parameters.
type Config struct {
	Dsn         string
	Debug       bool
	Release     string
	Environment string
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
	err := sentry.Init(sentry.ClientOptions{
		Dsn: conf.Dsn,
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug: conf.Debug,
		// Adds request headers and IP for users,
		// visit: https://docs.sentry.io/platforms/go/data-management/data-collected/ for more infoCollapse
		// commentComment on line R52apostasie commented on Feb 13, 2026 apostasieon Feb 13, 2026More actionsLet's add
		// Environment as well. It will allow us to separate development from users.ReactWrite a replyResolve comment
		SendDefaultPII: true,
		EnableTracing:  true,
		Environment:    conf.Environment,
		Release:        conf.Release,
		// Set TracesSampleRate to 1.0 to capture 100%
		// of transactions for tracing.
		TracesSampleRate: 1.0,
		EnableLogs:       true,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReporterInitializationFail, err)
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
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	sentry.Flush(flushTimeout)
}
